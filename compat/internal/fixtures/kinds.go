package fixtures

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-component-model/ocm-integrationtest/compat/internal/cli"
)

// This file wires the YAML-facing fixture kinds. Each kind reads `with:`
// from the case YAML, calls the right helper, and returns outputs the
// resolver substitutes into the constructor as ${NAME.key}.

func init() {
	Register("httpFileServer", startHTTPFileServerKind)
	Register("hermeticMavenRepo", startHermeticMavenRepoKind)
	Register("ociArtifact", startOCIArtifactKind)
	Register("writeFile", startWriteFileKind)
	Register("writeDir", startWriteDirKind)
	Register("writeChart", startWriteChartKind)
}

// decodeWith re-decodes a fixture kind's `with:` map into a typed
// struct via JSON round-trip. yaml.v3 has already produced
// JSON-compatible values, so json.Unmarshal can validate field types
// against the struct's `json:` tags. Wrong types surface here
// ("json: cannot unmarshal number into Go struct field …of type
// string") instead of as a cryptic CLI failure three steps later.
// DisallowUnknownFields makes typos in `with:` a load error instead of
// a silent default.
func decodeWith[T any](with map[string]any, kind string, out *T) error {
	raw, err := json.Marshal(with)
	if err != nil {
		return fmt.Errorf("%s: encode with: %w", kind, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%s: with: %w", kind, err)
	}
	return nil
}

// safeJoin treats rel as a workdir-relative path and returns the
// absolute host path, refusing any input that resolves outside workdir.
// Case YAML comes from the PR branch under test, so this is the choke
// point that stops a malicious PR from writing into the runner's
// filesystem via writeFile / writeDir / writeChart.
func safeJoin(workdir, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be workdir-relative, not absolute", rel)
	}
	full := filepath.Join(workdir, rel)
	resolved, err := filepath.Rel(workdir, full)
	if err != nil {
		return "", fmt.Errorf("resolve %q against workdir: %w", rel, err)
	}
	if resolved == ".." || strings.HasPrefix(resolved, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes workdir", rel)
	}
	return full, nil
}

// httpFileServer
//
//	with:
//	  files: { "<rel-path>": "<contents>" }
//	outputs:
//	  url        base URL (container-rewritten if needed)
//	  <path>.url full URL for that file
type httpFileServerSpec struct {
	Files map[string]string `json:"files"`
}

func startHTTPFileServerKind(ctx context.Context, workdir string, with map[string]any) (StartResult, error) {
	var spec httpFileServerSpec
	if err := decodeWith(with, "httpFileServer", &spec); err != nil {
		return StartResult{}, err
	}
	if len(spec.Files) == 0 {
		return StartResult{}, fmt.Errorf("httpFileServer: with.files is required and non-empty")
	}
	files := make(map[string][]byte, len(spec.Files))
	for p, s := range spec.Files {
		files[p] = []byte(s)
	}
	url, cleanup, err := StartHTTPFileServer(files)
	if err != nil {
		return StartResult{}, err
	}
	url = cli.URLForContainer(workdir, url)
	out := Outputs{"url": url}
	for p := range files {
		out[p+".url"] = url + "/" + p
	}
	return StartResult{Outputs: out, Cleanup: cleanup}, nil
}

// hermeticMavenRepo
//
//	with:
//	  group, artifact, version: GAV
//	  jar: deterministic payload bytes
//	outputs:
//	  url base URL of the maven layout
type hermeticMavenRepoSpec struct {
	Group    string `json:"group"`
	Artifact string `json:"artifact"`
	Version  string `json:"version"`
	Jar      string `json:"jar,omitempty"`
}

func startHermeticMavenRepoKind(ctx context.Context, workdir string, with map[string]any) (StartResult, error) {
	var spec hermeticMavenRepoSpec
	if err := decodeWith(with, "hermeticMavenRepo", &spec); err != nil {
		return StartResult{}, err
	}
	if spec.Group == "" || spec.Artifact == "" || spec.Version == "" {
		return StartResult{}, fmt.Errorf("hermeticMavenRepo: group, artifact, version required")
	}
	if spec.Jar == "" {
		spec.Jar = "compat-maven-jar\n"
	}
	url, cleanup, err := StartHermeticMavenRepo(spec.Group, spec.Artifact, spec.Version, []byte(spec.Jar))
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{
		Outputs: Outputs{"url": cli.URLForContainer(workdir, url)},
		Cleanup: cleanup,
	}, nil
}

// ociArtifact pushes a tiny artifact to the shared registry.
//
//	with:
//	  repo, tag, payload
//	outputs:
//	  ref         full image ref (host-rewritten if running in a container)
//	  regAddr     bare host:port of the registry
//	  repo, tag   echoed back from with:
//	  digest      manifest digest (OCIImage / ociArtifact targets this)
//	  size        manifest size in bytes
//	  layerRef    bare repo path WITHOUT scheme/tag (`<host>/<repo>`):
//	              the form OCIImageLayer's `ref:` field expects
//	  layerDigest single layer's digest
//	  layerSize   single layer's size in bytes
//	  layerMedia  single layer's media type
type ociArtifactSpec struct {
	Repo    string `json:"repo"`
	Tag     string `json:"tag"`
	Payload string `json:"payload,omitempty"`
}

func startOCIArtifactKind(ctx context.Context, workdir string, with map[string]any) (StartResult, error) {
	if !cli.DockerOK() {
		return StartResult{Skip: "docker required for testcontainers registry: " + cli.DockerError()}, nil
	}
	var spec ociArtifactSpec
	if err := decodeWith(with, "ociArtifact", &spec); err != nil {
		return StartResult{}, err
	}
	if spec.Repo == "" || spec.Tag == "" {
		return StartResult{}, fmt.Errorf("ociArtifact: repo and tag required")
	}
	if spec.Payload == "" {
		spec.Payload = "compat-oci\n"
	}
	addr, err := SharedRegistry(ctx)
	if err != nil {
		// cli.DockerOK gated us above, so a non-nil err here means the
		// registry container itself failed to come up. A real fixture
		// failure, not a "docker missing" skip.
		return StartResult{}, fmt.Errorf("shared registry unavailable: %w", err)
	}
	_, seeded, err := SeedTinyOCIArtifact(ctx, addr, spec.Repo, spec.Tag, spec.Payload)
	if err != nil {
		return StartResult{}, fmt.Errorf("seed registry: %w", err)
	}
	hostPort := rewriteHostPort(workdir, addr)
	// v1 refuses plain HTTP to an OCI registry without an explicit
	// consumer config, and matches consumers by hostname:port. So
	// when v1 runs inside docker the config must register the
	// container-facing hostPort, not the raw loopback addr.
	v1Args, err := V1PlainHTTPConfigArgs(workdir, hostPort)
	if err != nil {
		return StartResult{}, fmt.Errorf("ociArtifact: write v1 plain-HTTP config: %w", err)
	}
	return StartResult{
		ExtraV1Args: v1Args,
		Outputs: Outputs{
			"ref":         cli.URLForContainer(workdir, seeded.Ref),
			"regAddr":     hostPort,
			"repo":        spec.Repo,
			"tag":         spec.Tag,
			"digest":      seeded.Manifest.Digest.String(),
			"size":        fmt.Sprintf("%d", seeded.Manifest.Size),
			"layerRef":    hostPort + "/" + spec.Repo,
			"layerDigest": seeded.Layer.Digest.String(),
			"layerSize":   fmt.Sprintf("%d", seeded.Layer.Size),
			"layerMedia":  seeded.Layer.MediaType,
		},
	}, nil
}

// rewriteHostPort applies cli.URLForContainer to a bare host:port (no scheme).
func rewriteHostPort(workdir, hostPort string) string {
	rewritten := cli.URLForContainer(workdir, "http://"+hostPort)
	return strings.TrimPrefix(rewritten, "http://")
}

// writeFile
//
//	with:
//	  path, data
//	outputs:
//	  path   relative path (echoed for ${X.path})
type writeFileSpec struct {
	Path string `json:"path"`
	Data string `json:"data,omitempty"`
}

func startWriteFileKind(ctx context.Context, workdir string, with map[string]any) (StartResult, error) {
	var spec writeFileSpec
	if err := decodeWith(with, "writeFile", &spec); err != nil {
		return StartResult{}, err
	}
	if spec.Path == "" {
		return StartResult{}, fmt.Errorf("writeFile: path required")
	}
	full, err := safeJoin(workdir, spec.Path)
	if err != nil {
		return StartResult{}, fmt.Errorf("writeFile: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return StartResult{}, err
	}
	if err := os.WriteFile(full, []byte(spec.Data), 0o644); err != nil {
		return StartResult{}, err
	}
	return StartResult{Outputs: Outputs{"path": spec.Path}}, nil
}

// writeDir
//
//	with:
//	  path:  root dir (relative to workdir)
//	  files: { "<rel>": "<contents>" }
//	outputs:
//	  path
type writeDirSpec struct {
	Path  string            `json:"path"`
	Files map[string]string `json:"files"`
}

func startWriteDirKind(ctx context.Context, workdir string, with map[string]any) (StartResult, error) {
	var spec writeDirSpec
	if err := decodeWith(with, "writeDir", &spec); err != nil {
		return StartResult{}, err
	}
	if spec.Path == "" {
		return StartResult{}, fmt.Errorf("writeDir: path required")
	}
	if spec.Files == nil {
		return StartResult{}, fmt.Errorf("writeDir: with.files must be a map")
	}
	// safeJoin the root once so a `path: "../etc"` bails before any
	// file write. The per-entry joins below re-check, since `rel` can
	// independently traverse out of rootFull via `..`.
	rootFull, err := safeJoin(workdir, spec.Path)
	if err != nil {
		return StartResult{}, fmt.Errorf("writeDir: %w", err)
	}
	for rel, s := range spec.Files {
		full, err := safeJoin(rootFull, rel)
		if err != nil {
			return StartResult{}, fmt.Errorf("writeDir: entry %q: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return StartResult{}, err
		}
		if err := os.WriteFile(full, []byte(s), 0o644); err != nil {
			return StartResult{}, err
		}
	}
	return StartResult{Outputs: Outputs{"path": spec.Path}}, nil
}

// writeChart synthesises a minimal Helm chart at <workdir>/<path>.
//
//	with:
//	  path, name, version
//	outputs:
//	  path
type writeChartSpec struct {
	Path    string `json:"path,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

func startWriteChartKind(ctx context.Context, workdir string, with map[string]any) (StartResult, error) {
	var spec writeChartSpec
	if err := decodeWith(with, "writeChart", &spec); err != nil {
		return StartResult{}, err
	}
	if spec.Path == "" {
		spec.Path = "chart"
	}
	if spec.Name == "" {
		spec.Name = "compat"
	}
	if spec.Version == "" {
		spec.Version = "1.0.0"
	}
	root, err := safeJoin(workdir, spec.Path)
	if err != nil {
		return StartResult{}, fmt.Errorf("writeChart: %w", err)
	}
	files := map[string]string{
		"Chart.yaml":        fmt.Sprintf("apiVersion: v2\nname: %s\nversion: %s\ntype: application\nappVersion: %q\n", spec.Name, spec.Version, spec.Version),
		"values.yaml":       "greeting: hello\n",
		"templates/cm.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: compat\ndata:\n  greeting: \"{{ .Values.greeting }}\"\n",
	}
	for rel, data := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return StartResult{}, err
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			return StartResult{}, err
		}
	}
	return StartResult{Outputs: Outputs{"path": spec.Path}}, nil
}
