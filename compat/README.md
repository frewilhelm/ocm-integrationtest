# OCM v1 / v2 Compatibility Suite

A data-driven test suite that runs the same OCM component-constructor
through both CLIs:

- **v1**: [`open-component-model/ocm`](https://github.com/open-component-model/ocm)
- **v2**: [`open-component-model/open-component-model`](https://github.com/open-component-model/open-component-model)

Each case is exercised through two phases (construct, then transfer)
for both legs, and the result is compared against expectations
declared on the case itself.

## Prerequisites

- Go 1.26+ (pinned in `compat/go.mod`; install via
  [go.dev/dl](https://go.dev/dl/) or your platform package manager).
- A running Docker daemon if either leg uses `docker:` mode (the
  default), or if any case relies on testcontainers (OCI registry,
  MinIO). Bin/bin mode skips docker except for those fixtures.

## Layout

```
compat/
  cases/
    access/   one YAML per access type (Helm, OCIImage, S3, ...)
    inputs/   one YAML per input type (file, dir, helm, ...)
  internal/
    cases/    YAML loader, ${NAME.key} resolver, label stripper
    cli/      docker:/bin: invoker; URL/path rewrite for containers
    fixtures/ named fixture kinds (httpFileServer, ociArtifact, s3, ...)
    runner/   construct/transfer per leg
  compat_test.go   Ginkgo bootstrap; discovers cases at suite-init
```

## A case is a constructor + labels

A case file IS an OCM component-constructor. The test metadata rides on
`compat.ocm.software/*` labels on the component; the runner strips them
before invoking the CLI, so the constructor handed to v1/v2 is plain OCM.

```yaml
components:
- name: ocm.software/compat/access/wget
  version: 1.0.0
  provider: { name: ocm.software }
  labels:
  - name: compat.ocm.software/case
    value: { id: access:Wget }
  - name: compat.ocm.software/fixtures
    value:
    - name: HTTP
      kind: httpFileServer
      with:
        files: { payload.txt: "Wget access compat\n" }
  - name: compat.ocm.software/expect
    value:
      construct: { v1: pass, v2: { outcome: fail, errSubstr: 'failed to get plugin for typ "' } }
      transfer:  { v1: pass, v2: skip }
  resources:
  - name: r-wget
    type: plainText
    version: 1.0.0
    relation: external
    access:
      type: Wget/v1
      url: ${HTTP.payload.txt.url}
      mediaType: text/plain
```

Per phase, per leg, the outcome is one of:

- `pass`: CLI exits 0
- `{outcome: fail, errSubstr: ...}`: CLI exits non-zero, output contains the substring
- `{outcome: skip, reason: ...}`: phase is reported as skipped with the reason

Multiple resources on the same component capture spelling variants
(e.g. `Helm/v1`, `Helm`, `helm`, `helm/v1`); construct runs them all in
one shot so the test surfaces whichever spelling the leg rejects first.
The variant set differs per case today. Some cover all four, some only
canonical+`/v1`. Ideally the set would be driven from registry
introspection; until then, see the case YAMLs under `cases/access/`.

## Fixtures

A fixture stands up something the constructor needs to point at. Each
kind is wired via `Register()` from its own file in
`internal/fixtures/` (most live in `kinds.go`; `s3` registers itself in
`s3.go` next to its MinIO container plumbing). Outputs are flat keys
substituted into the constructor wherever `${NAME.key}` appears.

Registered kinds:

| Kind                | Outputs                                                        |
|---------------------|----------------------------------------------------------------|
| `httpFileServer`    | `url`, `<path>.url`                                            |
| `hermeticMavenRepo` | `url`                                                          |
| `ociArtifact`       | `ref`, `regAddr`, `repo`, `tag`, `digest`, `size`              |
| `s3`                | `endpoint`, `host`, `bucket`, `key`, `region`, `payload`, ...  |
| `writeFile`         | `path`, `sha256`                                               |
| `writeDir`          | `path`                                                         |
| `writeChart`        | `path`                                                         |

Fixtures that need docker (OCI registry, S3/MinIO) emit `Skip` when
docker is unavailable instead of failing the whole suite.

## Running

CLI selection is via env vars:

```
OCM_V1_CMD=docker:<image>   or   bin:/path/to/ocm
OCM_V2_CMD=docker:<image>   or   bin:/path/to/ocm
```

Defaults are `docker:ghcr.io/open-component-model/ocm:latest` and
`docker:ghcr.io/open-component-model/cli:latest`.

Both legs **must use the same mode** (docker or bin). The URL/path
rewrite that makes host-bound listeners reachable from inside a container
is a single global decision.

```
# Docker mode (the default)
ginkgo --label-filter=compat -p ./...

# Local binaries
OCM_V1_CMD=bin:$(which ocm) OCM_V2_CMD=bin:$(which ocm-v2) \
  ginkgo --label-filter=compat -p ./...
```

Filter to one kind: `--label-filter='compat&&access'` or `'compat&&inputs'`.

Filter to a single case by name (Ginkgo's `--focus`):

```sh
ginkgo --label-filter=compat --focus='access:Wget' -p ./...
```

Point the loader at a different case directory via `COMPAT_CASES`
(useful when bisecting a regression with a stripped-down case set):

```sh
COMPAT_CASES=/path/to/cases ginkgo --label-filter=compat ./...
```

> `-p` runs one process per CPU; each worker starts its own OCI registry
> and MinIO via testcontainers. Drop `-p` on memory-tight CI to share one
> set across the suite.

## Cases that depend on third-party content

These cases fetch from the public internet at test time, so they are
**not hermetic**. A third-party outage will fail the case even though
nothing in OCM or this suite changed:

| Case          | External dependency                                        |
|---------------|------------------------------------------------------------|
| `access:Helm` | `stefanprodan.github.io/podinfo` (Helm chart over HTTPS)   |
| `access:GitHub` | `github.com/stefanprodan/podinfo` (gh-pages commit fetch)|
| `access:NPM`  | `registry.npmjs.org` (yallist 5.0.0)                       |
| `input:npm`   | `registry.npmjs.org` (yallist 5.0.0)                       |

Maven, S3, and OCI image cases use hermetic in-process fixtures and do
not hit the network. Expect occasional flakes on the four above when
the upstream service hiccups.

## Debugging a failing case

The Ginkgo run captures every CLI invocation's combined stdout+stderr;
on failure it surfaces under the spec name in the test report. To run
one case verbose:

```sh
ginkgo --label-filter=compat --focus='access:S3' -v ./...
```

The case's materialised constructor sits in a `compat-<case-id>-*`
tmpdir on disk; the Ginkgo report includes its path on failure for
post-mortem inspection.

## Adding a case

Drop a YAML in `cases/access/` or `cases/inputs/`. The loader picks it up
at suite-init time; the new `Context` and `It`s appear without a Go change.
If the case needs a new fixture kind, add it under
`internal/fixtures/` and `Register()` it in `kinds.go`.

## CI

The `.github/workflows/compat-test.yaml` workflow runs nightly and on
pull_request affecting `compat/**`. `workflow_dispatch` accepts override
inputs for image tags or PR numbers, so you can point either leg at an
in-flight PR build instead of the default `:latest` images.
