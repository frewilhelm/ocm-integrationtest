package fixtures

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/open-component-model/ocm-integrationtest/compat/internal/cli"
)

const minioImage = "minio/minio:RELEASE.2024-01-16T16-07-38Z"

func init() { Register("s3", startS3Kind) }

// startS3Kind boots MinIO via testcontainers, creates the bucket, and
// PUTs the requested object. v1's access/S3 resolves AWS credentials
// via the OCM credentials context (not AWS_* env), so the case writes
// an ocmconfig.yaml registering a `type: S3` consumer for the MinIO
// endpoint+bucket and passes it via --config.
//
// v1's S3 downloader (api/utils/accessio/downloader/s3/downloader.go)
// builds its s3.Client from config.LoadDefaultConfig with no
// BaseEndpoint override and no UsePathStyle knob in the access spec;
// without env intervention v1 always routes to real AWS. The OCM
// credentials consumer only supplies keys, not routing. To keep the
// case hermetic the fixture exports AWS_ENDPOINT_URL_S3 (honored by
// aws-sdk-go-v2 since v1.32; points the client at the MinIO container)
// plus the keys via ExtraV1Env. v2's S3 plugin doesn't exist yet so
// the env is v1-only; mirror it into ExtraV2Env when v2 grows the
// plugin.
//
// aws-sdk-go-v2 has no env knob for UsePathStyle; it falls back to
// path-style automatically when the bucket name contains characters
// invalid in a DNS label. S3.yaml picks `compat.s3` for that reason.
//
//	with:
//	  bucket, key (required)
//	  region (default us-east-1)
//	  payload (default deterministic bytes)
//
//	outputs:
//	  endpoint, host, bucket, key, region, payload, accessKey, secretKey
type s3Spec struct {
	Bucket  string `json:"bucket"`
	Key     string `json:"key"`
	Region  string `json:"region,omitempty"`
	Payload string `json:"payload,omitempty"`
}

func startS3Kind(ctx context.Context, workdir string, with map[string]any) (StartResult, error) {
	if !cli.DockerOK() {
		return StartResult{Skip: "docker required for minio: " + cli.DockerError()}, nil
	}
	var spec s3Spec
	if err := decodeWith(with, "s3", &spec); err != nil {
		return StartResult{}, err
	}
	if spec.Bucket == "" || spec.Key == "" {
		return StartResult{}, fmt.Errorf("s3: bucket and key required")
	}
	if spec.Region == "" {
		spec.Region = "us-east-1"
	}
	if spec.Payload == "" {
		spec.Payload = "compat-s3-payload\n"
	}

	const (
		accessKey = "minioadmin"
		secretKey = "minioadmin"
	)

	mc, err := tcminio.Run(ctx, minioImage,
		tcminio.WithUsername(accessKey),
		tcminio.WithPassword(secretKey),
	)
	if err != nil {
		// cli.DockerOK gated us above, so a non-nil err here is a real
		// fixture error (image pull, port allocation, OOM). Surfacing
		// as fail rather than skip prevents a registry hiccup from
		// hiding as a green skip on every s3 case.
		return StartResult{}, fmt.Errorf("start minio: %w", err)
	}
	// Cleanup-on-error guard: every error path between here and the
	// final return leaks the MinIO container otherwise. `success = true`
	// at the bottom disarms the defer; the returned StartResult then
	// owns the container via its Cleanup.
	success := false
	defer func() {
		if !success {
			_ = mc.Terminate(context.Background())
		}
	}()

	hostPort, err := mc.ConnectionString(ctx)
	if err != nil {
		return StartResult{}, fmt.Errorf("minio endpoint: %w", err)
	}

	client, err := minio.New(hostPort, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return StartResult{}, fmt.Errorf("minio client: %w", err)
	}
	if err := client.MakeBucket(ctx, spec.Bucket, minio.MakeBucketOptions{Region: spec.Region}); err != nil {
		errResp := minio.ToErrorResponse(err)
		if errResp.Code != "BucketAlreadyOwnedByYou" && errResp.Code != "BucketAlreadyExists" {
			return StartResult{}, fmt.Errorf("minio make bucket %q: %w", spec.Bucket, err)
		}
	}
	body := []byte(spec.Payload)
	if _, err := client.PutObject(ctx, spec.Bucket, spec.Key, bytes.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
		return StartResult{}, fmt.Errorf("minio put object %s/%s: %w", spec.Bucket, spec.Key, err)
	}

	hostEndpoint := "http://" + hostPort
	cliEndpoint := cli.URLForContainer(workdir, hostEndpoint)

	// S3 consumer for v1's credentials context. Identity is a hostpath
	// matcher: hostname/port from the (rewritten) endpoint, pathprefix
	// from bucket/key (version is empty here). Wire names: type "S3";
	// credential properties awsAccessKeyID, awsSecretAccessKey.
	cfgHost, cfgPort, err := net.SplitHostPort(strings.TrimPrefix(cliEndpoint, "http://"))
	if err != nil {
		return StartResult{}, fmt.Errorf("split host:port from %q: %w", cliEndpoint, err)
	}
	pathPrefix := path.Join(spec.Bucket, spec.Key)
	cfg := fmt.Sprintf(`type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
  - identity:
      type: S3
      hostname: %q
      port: %q
      pathprefix: %q
    credentials:
    - type: Credentials
      properties:
        awsAccessKeyID: %q
        awsSecretAccessKey: %q
`, cfgHost, cfgPort, pathPrefix, accessKey, secretKey)
	hostCfgPath := filepath.Join(workdir, "ocmconfig-s3.yaml")
	// 0o644: same trust assumption as registry.go's ocmconfig. Fine
	// on hosted runners, tighten to 0o600 on shared hosts. The keys
	// here are MinIO's well-known minioadmin/minioadmin so disclosure
	// is moot today, but keep the note consistent for the next reader.
	if err := os.WriteFile(hostCfgPath, []byte(cfg), 0o644); err != nil {
		return StartResult{}, fmt.Errorf("write ocmconfig-s3: %w", err)
	}
	cfgArg := hostCfgPath
	if cli.V1.Mode == cli.ModeDocker {
		cfgArg = "/work/ocmconfig-s3.yaml"
	}

	cleanup := func() {
		_ = mc.Terminate(context.Background())
	}

	success = true
	return StartResult{
		Outputs: Outputs{
			"endpoint":  cliEndpoint,
			"host":      strings.TrimPrefix(strings.TrimPrefix(cliEndpoint, "http://"), "https://"),
			"bucket":    spec.Bucket,
			"key":       spec.Key,
			"region":    spec.Region,
			"payload":   spec.Payload,
			"accessKey": accessKey,
			"secretKey": secretKey,
		},
		ExtraV1Args: []string{"--config", cfgArg},
		ExtraV1Env: []string{
			// v1's S3 downloader has no endpoint override; route
			// aws-sdk-go-v2 to the MinIO container instead.
			"AWS_ENDPOINT_URL_S3=" + cliEndpoint,
			"AWS_ACCESS_KEY_ID=" + accessKey,
			"AWS_SECRET_ACCESS_KEY=" + secretKey,
			"AWS_REGION=" + spec.Region,
		},
		Cleanup: cleanup,
	}, nil
}
