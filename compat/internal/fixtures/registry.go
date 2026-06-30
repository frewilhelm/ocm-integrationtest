package fixtures

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"

	"github.com/open-component-model/ocm-integrationtest/compat/internal/cli"
)

const registryImage = "registry:3.0.0"

var (
	registryOnce    sync.Once
	registryAddr    string
	registryStartup error
	registryCleanup func()
)

// SharedRegistry lazily starts this process's testcontainers OCI
// registry and returns its address. Caller is responsible for
// cli.DockerOK() first.
func SharedRegistry(ctx context.Context) (string, error) {
	registryOnce.Do(func() {
		if !cli.DockerOK() {
			registryStartup = fmt.Errorf("docker unavailable: %s", cli.DockerError())
			return
		}
		container, err := registry.Run(ctx, registryImage,
			testcontainers.WithEnv(map[string]string{
				"REGISTRY_VALIDATION_DISABLED": "true",
			}),
		)
		if err != nil {
			registryStartup = fmt.Errorf("start registry: %w", err)
			return
		}
		addr, err := container.HostAddress(ctx)
		if err != nil {
			registryStartup = fmt.Errorf("registry address: %w", err)
			return
		}
		registryAddr = addr
		registryCleanup = func() {
			_ = testcontainers.TerminateContainer(container)
		}
	})
	return registryAddr, registryStartup
}

func ShutdownRegistry() {
	if registryCleanup != nil {
		registryCleanup()
	}
}

// SeededOCIArtifact captures everything a case YAML might point at:
// the full reference (repo:tag), the bare registry address, and the
// layer + manifest descriptors. OCIImage targets the manifest digest;
// OCIImageLayer targets a single layer digest.
type SeededOCIArtifact struct {
	Ref      string
	Manifest ocispec.Descriptor
	Layer    ocispec.Descriptor
}

// SeedTinyOCIArtifact pushes a one-layer OCI image into the registry.
func SeedTinyOCIArtifact(ctx context.Context, regAddr, repo, tag, payload string) (string, SeededOCIArtifact, error) {
	fail := func(err error) (string, SeededOCIArtifact, error) {
		return "", SeededOCIArtifact{}, err
	}
	store := memory.New()
	layerData := []byte(payload)
	layerDesc, err := pushBlob(ctx, store, "application/vnd.oci.image.layer.v1.tar", layerData)
	if err != nil {
		return fail(err)
	}
	configDesc, err := pushBlob(ctx, store, "application/vnd.oci.image.config.v1+json", []byte(`{}`))
	if err != nil {
		return fail(err)
	}
	manifest := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ocispec.Descriptor{layerDesc},
	}
	manifest.SchemaVersion = 2
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return fail(err)
	}
	manifestDesc, err := pushBlob(ctx, store, ocispec.MediaTypeImageManifest, manifestBytes)
	if err != nil {
		return fail(err)
	}
	if err := store.Tag(ctx, manifestDesc, tag); err != nil {
		return fail(err)
	}
	dst, err := remote.NewRepository(regAddr + "/" + repo)
	if err != nil {
		return fail(err)
	}
	dst.PlainHTTP = true
	if _, err := oras.Copy(ctx, store, tag, dst, tag, oras.DefaultCopyOptions); err != nil {
		return fail(fmt.Errorf("oras copy: %w", err))
	}
	ref := "http://" + regAddr + "/" + repo + ":" + tag
	// Ref carries the host-facing raw addr the registry is reachable at
	// from this process. Callers handing it to a CLI inside docker
	// must re-rewrite via cli.URLForContainer first (startOCIArtifactKind
	// does). Pre-rewriting here would break the seeder, which uses
	// regAddr directly to push.
	return ref, SeededOCIArtifact{
		Ref:      ref,
		Manifest: manifestDesc,
		Layer:    layerDesc,
	}, nil
}

func pushBlob(ctx context.Context, store content.Storage, mediaType string, data []byte) (ocispec.Descriptor, error) {
	d := digest.FromBytes(data)
	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    d,
		Size:      int64(len(data)),
	}
	if err := store.Push(ctx, desc, bytes.NewReader(data)); err != nil {
		return ocispec.Descriptor{}, err
	}
	return desc, nil
}

// V1PlainHTTPConfigArgs writes an OCM v1 ocmconfig registering regAddr
// as a plain-HTTP OCI registry and returns `--config <path>` global
// args.
//
// The critical block is `oci.config.ocm.software/v1` with an alias
// keyed by the bare `host:port` (matching what `oci.ParseRef(ref).Host`
// produces from a schemeless ref like OCIImageLayer's `ref` field).
// v1's ociblob.accessMethod.getBlob calls `ocictx.GetAlias(ref.Host)`
// BEFORE constructing the default repo spec. If the alias hits,
// `url.Parse` in ocireg.getInfo honors its `baseUrl: http://...`.
// Without it, `ocireg.NewRepositorySpec(ref.Host)` hits the
// docker-reference branch in getInfo that hardcodes `https://`.
//
// The credentials consumer is a no-op against the testcontainers
// registry (it serves anonymously) but kept in for hosts that do
// require auth.
func V1PlainHTTPConfigArgs(workdir, regAddr string) ([]string, error) {
	host, port, err := net.SplitHostPort(regAddr)
	if err != nil {
		return nil, err
	}
	cfg := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: oci.config.ocm.software/v1
  aliases:
    %q:
      type: OCIRegistry
      baseUrl: %q
- type: credentials.config.ocm.software
  consumers:
    - identity:
        type: OCIRegistry
        hostname: %q
        port: %q
      credentials:
      - type: Credentials/v1
        properties:
          username: anon
          password: anon
`, regAddr, "http://"+regAddr, host, port)
	hostPath := filepath.Join(workdir, "ocmconfig.yaml")
	// 0o644 is fine on GitHub-hosted runners (single tenant per job).
	// Tighten to 0o600 if this ever runs on a shared host: the config
	// embeds anon-auth credentials an unprivileged sibling process
	// could otherwise read.
	if err := os.WriteFile(hostPath, []byte(cfg), 0o644); err != nil {
		return nil, err
	}
	if cli.V1.Mode == cli.ModeDocker {
		return []string{"--config", "/work/ocmconfig.yaml"}, nil
	}
	return []string{"--config", hostPath}, nil
}
