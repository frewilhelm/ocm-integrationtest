// Package cli wraps the OCM v1 / v2 CLIs as subprocesses. It speaks two
// transports:
//
//	docker:<image>   `docker run` with workdir bind-mounted at /work and
//	                 --add-host=host.docker.internal:host-gateway.
//	bin:<path>       run the binary directly with cwd=workdir.
package cli

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Mode int

const (
	ModeDocker Mode = iota
	ModeBinary
)

// String makes error messages readable: a printf "%v" on Mode would
// otherwise print "0" / "1" and force the reader to look up the const.
func (m Mode) String() string {
	switch m {
	case ModeDocker:
		return "docker"
	case ModeBinary:
		return "bin"
	}
	return fmt.Sprintf("mode(%d)", int(m))
}

type Spec struct {
	Mode  Mode
	Value string // image ref for docker, binary path for bin
}

func (s Spec) Label() string {
	switch s.Mode {
	case ModeBinary:
		return "bin:" + s.Value
	case ModeDocker:
		return "docker:" + s.Value
	}
	return "?"
}

const (
	EnvV1 = "OCM_V1_CMD"
	EnvV2 = "OCM_V2_CMD"

	DefaultV1 = "docker:ghcr.io/open-component-model/ocm:latest"
	DefaultV2 = "docker:ghcr.io/open-component-model/cli:latest"
)

func ParseSpec(envVar, fallback string) (Spec, error) {
	raw := os.Getenv(envVar)
	if raw == "" {
		raw = fallback
	}
	switch {
	case strings.HasPrefix(raw, "docker:"):
		img := strings.TrimPrefix(raw, "docker:")
		if img == "" {
			return Spec{}, fmt.Errorf("%s: docker: prefix without image", envVar)
		}
		return Spec{Mode: ModeDocker, Value: img}, nil
	case strings.HasPrefix(raw, "bin:"):
		path := strings.TrimPrefix(raw, "bin:")
		if path == "" {
			return Spec{}, fmt.Errorf("%s: bin: prefix without path", envVar)
		}
		return Spec{Mode: ModeBinary, Value: path}, nil
	default:
		return Spec{}, fmt.Errorf("%s=%q: expected docker:<image> or bin:<path>", envVar, raw)
	}
}

var (
	V1 Spec
	V2 Spec

	parseOnce  sync.Once
	parseError string

	dockerOnce  sync.Once
	dockerOK    bool
	dockerError string
)

// Probe parses both env vars and checks docker availability. Split
// across two sync.Once gates so callers can distinguish a config error
// ("OCM_V1_CMD=garbage", mixed-mode) from a runtime docker error
// ("daemon unreachable"). Neither is fatal at Probe time; cli.Run
// surfaces them per-leg, BeforeSuite decides whether to hard-fail.
//
// Mixed mode (one leg docker, the other bin) is rejected at parse:
// fixture URLs and the constructor are materialised once and handed to
// both legs, so a single substituted value can't be simultaneously
// `host.docker.internal:port` (container) and `127.0.0.1:port` (host
// binary). It would also conflate CLI differences with docker's
// network/filesystem semantics, which is exactly the noise the suite
// is designed to isolate.
func Probe() {
	probeParse()
	probeDocker()
}

// probeParse populates V1 / V2 and enforces matched modes. Cheap and
// side-effect-free; safe to call repeatedly.
func probeParse() {
	parseOnce.Do(func() {
		var err error
		V1, err = ParseSpec(EnvV1, DefaultV1)
		if err != nil {
			parseError = err.Error()
			return
		}
		V2, err = ParseSpec(EnvV2, DefaultV2)
		if err != nil {
			parseError = err.Error()
			return
		}
		if V1.Mode != V2.Mode {
			parseError = fmt.Sprintf(
				"mixed-mode unsupported: v1=%s, v2=%s: both legs must be docker or both bin (see Probe docs)",
				V1.Label(), V2.Label())
			return
		}
	})
}

// probeDocker checks docker is on PATH and the daemon answers within
// 10s. Skipped on bin/bin mode since no case needs docker there,
// except the testcontainers fixtures (registry, MinIO), which check
// DockerOK() themselves and skip cleanly when absent.
func probeDocker() {
	dockerOnce.Do(func() {
		if _, err := exec.LookPath("docker"); err != nil {
			dockerError = "docker binary not found in PATH"
			return
		}
		// Bound the probe: a wedged docker socket would otherwise hang
		// BeforeSuite for the full 6h GHA job timeout. 10s clears a
		// healthy `docker version` (<2s on CI, <500ms locally) with
		// plenty of headroom, fails a stuck daemon in seconds.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", "version").CombinedOutput()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				dockerError = "docker daemon unreachable: `docker version` timed out after 10s"
				return
			}
			dockerError = fmt.Sprintf("docker daemon unreachable: %v: %s", err, string(out))
			return
		}
		dockerOK = true
	})
}

// ParseError returns the parse / mixed-mode error, or "".
func ParseError() string { return parseError }

func DockerOK() bool      { return dockerOK }
func DockerError() string { return dockerError }

// URLForContainer rewrites a URL so it's reachable from inside the v1
// docker container. Non-docker mode returns url unchanged.
//
//   - HTTP URLs whose host is 127.0.0.1 / 0.0.0.0 / localhost get
//     host.docker.internal substituted in (wired up via
//     --add-host=...:host-gateway). Only the authority is touched.
//   - file:// URLs whose path is inside workdir become file:///work/...
//     since workdir is bind-mounted at /work.
//
// Mixed mode is rejected at Probe, so checking V1.Mode is sufficient.
func URLForContainer(workdir, raw string) string {
	if V1.Mode != ModeDocker {
		return raw
	}
	if strings.HasPrefix(raw, "file://") {
		path := strings.TrimPrefix(raw, "file://")
		rel, err := filepath.Rel(workdir, path)
		if err == nil && !strings.HasPrefix(rel, "..") {
			return "file:///work/" + filepath.ToSlash(rel)
		}
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	host, port, splitErr := net.SplitHostPort(u.Host)
	if splitErr != nil {
		// Bare host, no port.
		host = u.Host
		port = ""
	}
	switch host {
	case "127.0.0.1", "0.0.0.0", "localhost":
		if port != "" {
			u.Host = net.JoinHostPort("host.docker.internal", port)
		} else {
			u.Host = "host.docker.internal"
		}
		return u.String()
	}
	return raw
}

// PathForContainer returns the in-container view of a host-side path:
// docker mode rewrites paths under workdir to /work/<rel>, bin mode is
// pass-through.
func PathForContainer(workdir, abs string) string {
	if V1.Mode != ModeDocker {
		return abs
	}
	rel, err := filepath.Rel(workdir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return "/work/" + filepath.ToSlash(rel)
}

type Leg string

const (
	V1Leg Leg = "v1"
	V2Leg Leg = "v2"
)

// Run invokes the chosen leg's CLI. extraGlobalArgs are placed BEFORE
// verbAndArgs, the slot for `--config <path>` and other root-level flags.
func Run(ctx context.Context, leg Leg, workdir string, extraGlobalArgs []string, verbAndArgs ...string) (string, error) {
	return RunWithEnv(ctx, leg, workdir, nil, extraGlobalArgs, verbAndArgs...)
}

// RunWithEnv is Run plus extra `KEY=value` env entries. Docker mode
// translates them to `-e KEY=value`; bin mode appends to os.Environ for
// the subprocess.
//
// Empty extraEnv in bin mode leaves cmd.Env nil, which makes os/exec
// inherit the parent environment (Go's documented default). A non-empty
// extraEnv builds cmd.Env = os.Environ() + extras explicitly, the same
// effective set. So nil and []string{} both mean "inherit + nothing
// extra"; the `len(extraEnv) > 0` branch is only ever taken for the
// non-empty case.
func RunWithEnv(ctx context.Context, leg Leg, workdir string, extraEnv []string, extraGlobalArgs []string, verbAndArgs ...string) (string, error) {
	spec := V1
	if leg == V2Leg {
		spec = V2
	}
	// Surface docker-unavailable as a clear error; otherwise every
	// phase emits an opaque `docker run` "Cannot connect to the Docker
	// daemon" / "invalid reference" failure.
	if spec.Mode == ModeDocker && !dockerOK {
		return "", fmt.Errorf("%s docker unavailable: %s", leg, dockerError)
	}
	switch spec.Mode {
	case ModeBinary:
		args := append([]string{}, extraGlobalArgs...)
		args = append(args, verbAndArgs...)
		cmd := exec.CommandContext(ctx, spec.Value, args...)
		cmd.Dir = workdir
		if len(extraEnv) > 0 {
			cmd.Env = append(os.Environ(), extraEnv...)
		}
		return runBounded(cmd)
	case ModeDocker:
		args := []string{
			"run", "--rm",
			"--add-host", "host.docker.internal:host-gateway",
		}
		// Map the container process to the host uid so files written
		// into the bind-mounted workdir are owned by the runner user.
		// Otherwise the v1 distroless-nonroot image (uid 65532) can't
		// write to /work (owned by the runner), and any files it does
		// write are unreadable by later legs. macOS Docker Desktop
		// masks this, hence Linux-CI-only.
		if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 {
			args = append(args, "-u", fmt.Sprintf("%d:%d", uid, gid))
		}
		for _, kv := range extraEnv {
			args = append(args, "-e", kv)
		}
		if leg == V2Leg {
			// v2 writes to /tmp; tmpfs keeps it off the bind-mounted
			// workdir and lets it be executable.
			args = append(args, "--tmpfs", "/tmp:exec")
		}
		args = append(args,
			"-v", fmt.Sprintf("%s:/work", workdir),
			"-w", "/work",
			spec.Value,
		)
		args = append(args, extraGlobalArgs...)
		args = append(args, verbAndArgs...)
		cmd := exec.CommandContext(ctx, "docker", args...)
		return runBounded(cmd)
	}
	return "", fmt.Errorf("unknown %s cli mode %s", leg, spec.Mode)
}

// maxCLIOutput caps captured stdout+stderr per CLI invocation. A
// misbehaving binary that streams indefinitely would otherwise be free
// to OOM the test process via the unbounded buffer cmd.CombinedOutput
// allocates. 4 MiB is two orders of magnitude over any healthy
// `add cv` / `transfer cv` output observed in practice.
const maxCLIOutput = 4 << 20 // 4 MiB

// runBounded executes cmd, capping captured output at maxCLIOutput.
// Anything beyond is dropped with a one-line truncation marker so the
// test report stays clean. The cmd.Run error propagates verbatim
// (matches CombinedOutput's contract).
func runBounded(cmd *exec.Cmd) (string, error) {
	var buf boundedBuffer
	buf.cap = maxCLIOutput
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// boundedBuffer accepts at most cap bytes and silently drops the rest,
// appending a one-line truncation marker on first overflow. Concurrent
// writes are not protected: os/exec only serialises stdout/stderr
// internally when stdout == stderr, which is exactly how runBounded
// wires it.
type boundedBuffer struct {
	buf       []byte
	cap       int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.truncated {
		// Lie about the byte count so the upstream writer doesn't
		// error out; the truncation marker is already recorded.
		return len(p), nil
	}
	remaining := b.cap - len(b.buf)
	if len(p) <= remaining {
		b.buf = append(b.buf, p...)
		return len(p), nil
	}
	b.buf = append(b.buf, p[:remaining]...)
	b.buf = append(b.buf, "\n... [output truncated at "+strconv.Itoa(b.cap)+" bytes] ...\n"...)
	b.truncated = true
	return len(p), nil
}

func (b *boundedBuffer) String() string { return string(b.buf) }
