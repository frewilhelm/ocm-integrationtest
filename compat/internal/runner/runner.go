// Package runner drives one compat case through construct and
// transfer for both v1 and v2. The case YAML's per-phase expectations
// are the source of truth; the runner encodes only the CLI shape, not
// v1-vs-v2 semantics. Download is out of scope; each CLI's own tests
// cover that surface.
package runner

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/open-component-model/ocm-integrationtest/compat/internal/cases"
	"github.com/open-component-model/ocm-integrationtest/compat/internal/cli"
)

type RunContext struct {
	Case    *cases.Case
	Workdir string

	mat *cases.MaterializeResult

	// SetupSkip, if non-empty, means every phase short-circuits to Skip.
	SetupSkip string

	// constructErr remembers per-leg construct outcomes so Transfer
	// can auto-skip when its leg's CTF was never built. The container
	// uses ContinueOnFailure, which lets the v2 leg run after v1
	// fails. Transferring nothing is meaningless and produces a
	// confusing second failure with cryptic CLI output.
	constructErr map[cli.Leg]error
}

type PhaseResult struct {
	Skipped    bool
	SkipReason string

	Output string
	Err    error
}

func New(ctx context.Context, c *cases.Case, workdir string) (*RunContext, error) {
	rc := &RunContext{Case: c, Workdir: workdir, constructErr: map[cli.Leg]error{}}
	mat, err := c.Materialize(ctx, workdir)
	rc.mat = mat
	if err != nil {
		// Materialize hands back any cleanups it accumulated before
		// the failure; run them now so callers get the standard Go
		// (nil, err) contract without having to remember.
		rc.Cleanup()
		return nil, err
	}
	if mat.Skip != "" {
		rc.SetupSkip = mat.Skip
	}
	return rc, nil
}

func (rc *RunContext) Cleanup() {
	if rc == nil || rc.mat == nil {
		return
	}
	for i := len(rc.mat.Cleanups) - 1; i >= 0; i-- {
		fn := rc.mat.Cleanups[i]
		if fn != nil {
			fn()
		}
	}
}

func (rc *RunContext) extraArgsFor(leg cli.Leg) []string {
	if rc.mat == nil {
		return nil
	}
	if leg == cli.V1Leg {
		return rc.mat.ExtraV1Args
	}
	return rc.mat.ExtraV2Args
}

func (rc *RunContext) extraEnvFor(leg cli.Leg) []string {
	if rc.mat == nil {
		return nil
	}
	if leg == cli.V1Leg {
		return rc.mat.ExtraV1Env
	}
	return rc.mat.ExtraV2Env
}

// v1 and v2 write to separate CTFs so legs don't stomp on each other.
func ctfRel(leg cli.Leg) string { return "ctf-" + string(leg) }

// inContainer translates a relative workdir path into the form the
// CLI expects: docker mode wants /work/<rel>; bin mode runs with
// cmd.Dir = workdir, so the bare relative path is fine.
func inContainer(rel string) string {
	if cli.V1.Mode == cli.ModeDocker {
		return "/work/" + filepath.ToSlash(rel)
	}
	return rel
}

func (rc *RunContext) Construct(ctx context.Context, leg cli.Leg) PhaseResult {
	if rc.SetupSkip != "" {
		return PhaseResult{Skipped: true, SkipReason: rc.SetupSkip}
	}
	args, err := constructArgs(leg, inContainer(ctfRel(leg)), inContainer(rc.mat.ConstructorRel))
	if err != nil {
		return PhaseResult{Err: err}
	}
	out, err := cli.RunWithEnv(ctx, leg, rc.Workdir, rc.extraEnvFor(leg), rc.extraArgsFor(leg), args...)
	// Recording the error unconditionally. Transfer reads it to
	// decide whether to skip; a nil err records nil, which is correct.
	rc.constructErr[leg] = err
	return PhaseResult{Output: out, Err: err}
}

// Transfer copies the constructed CTF into a per-case sub-path on the
// shared registry, then verifies the CV is retrievable via
// `<leg> get cv`. The verify catches a regression where transfer
// exits 0 without actually pushing anything.
func (rc *RunContext) Transfer(ctx context.Context, leg cli.Leg, regAddr string) PhaseResult {
	if rc.SetupSkip != "" {
		return PhaseResult{Skipped: true, SkipReason: rc.SetupSkip}
	}
	// Skip when this leg's construct failed (or never ran): there is
	// nothing to transfer. The Ordered container runs construct
	// before transfer by source order, so a missing entry here means
	// the wiring is broken.
	if cerr, ran := rc.constructErr[leg]; !ran {
		return PhaseResult{Skipped: true, SkipReason: fmt.Sprintf("%s construct did not run; nothing to transfer", leg)}
	} else if cerr != nil {
		return PhaseResult{Skipped: true, SkipReason: fmt.Sprintf("%s construct failed; nothing to transfer", leg)}
	}
	if regAddr == "" {
		return PhaseResult{Skipped: true, SkipReason: "no shared registry available"}
	}
	env := rc.extraEnvFor(leg)
	extra := rc.extraArgsFor(leg)
	target := rc.transferTarget(leg, regAddr)
	src := inContainer(ctfRel(leg)) + "//" + rc.Case.ComponentName + ":" + rc.Case.ComponentVersion

	out, err := cli.RunWithEnv(ctx, leg, rc.Workdir, env, extra,
		"transfer", "cv", src, target)
	if err != nil {
		return PhaseResult{Output: out, Err: err}
	}

	// Round-trip through the same leg's `get cv` so a regression
	// where transfer exits 0 without actually pushing slips past
	// neither the exit-code check above nor here. Both CLIs accept
	// the `cv` alias and the `<repo>//<name>:<version>` ref shape.
	ref := target + "//" + rc.Case.ComponentName + ":" + rc.Case.ComponentVersion
	verifyOut, verifyErr := cli.RunWithEnv(ctx, leg, rc.Workdir, env, extra,
		"get", "cv", ref)
	if verifyErr != nil {
		return PhaseResult{
			Output: out + "\n--- transfer verify ---\n" + verifyOut,
			Err:    fmt.Errorf("transfer exited 0 but %s get failed: %w", leg, verifyErr),
		}
	}
	return PhaseResult{Output: out, Err: nil}
}

// constructArgs returns the argv for `<leg> add cv`. Both CLIs accept
// `cv` as the verb alias; only the flag shape diverges. v1 takes the
// constructor as a positional after `--file`, v2 uses distinct
// `--repository` / `--constructor` flags. transfer and get are
// leg-agnostic and inlined; this is the one helper that earns its keep.
//
// Unknown leg panics: the spec dispatcher only ever passes V1Leg or
// V2Leg, so an unknown leg is a programmer error. Panicking surfaces
// the wiring break immediately in tests rather than as a quiet
// "args: nil" run against the wrong binary.
func constructArgs(leg cli.Leg, ctfArg, ctorArg string) ([]string, error) {
	switch leg {
	case cli.V1Leg:
		return []string{
			"add", "cv",
			"--create",
			"--file", ctfArg,
			ctorArg,
		}, nil
	case cli.V2Leg:
		return []string{
			"add", "cv",
			"--repository", ctfArg,
			"--constructor", ctorArg,
			"--loglevel", "info",
		}, nil
	}
	panic(fmt.Sprintf("runner.constructArgs: unknown leg %q", leg))
}

func (rc *RunContext) transferTarget(leg cli.Leg, regAddr string) string {
	rewritten := strings.TrimPrefix(cli.URLForContainer(rc.Workdir, "http://"+regAddr), "http://")
	caseID := sanitizeID(rc.Case.ID())
	return "http://" + rewritten + "/compat/" + caseID + "/" + string(leg)
}

// sanitizeID turns a case id like "access:s3" into a registry-path-safe
// segment ("access-s3"). Lower-cases because OCI repository names are
// case-sensitive and v1 normalises them in surprising ways.
func sanitizeID(id string) string {
	out := make([]byte, 0, len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
