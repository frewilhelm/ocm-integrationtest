// Ginkgo bootstrap for the v1/v2 compat suite. User-facing docs live on
// the parent `package compat` in doc.go so godoc surfaces them.
package compat_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-component-model/ocm-integrationtest/compat/internal/cases"
	"github.com/open-component-model/ocm-integrationtest/compat/internal/cli"
	"github.com/open-component-model/ocm-integrationtest/compat/internal/fixtures"
	"github.com/open-component-model/ocm-integrationtest/compat/internal/runner"
)

func TestCompat(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "compat suite")
}

var _ = BeforeSuite(func() {
	cli.Probe()
	// Hard-fail rather than degrade into 18 cases all reporting
	// "invalid reference format": a parse / mixed-mode error leaves
	// V1/V2 unusable.
	Expect(cli.ParseError()).To(BeEmpty(), "OCM_V*_CMD parse / mixed-mode check failed")
	Expect(cli.V1.Value).NotTo(BeEmpty(), "OCM_V1_CMD spec did not parse")
	Expect(cli.V2.Value).NotTo(BeEmpty(), "OCM_V2_CMD spec did not parse")
	// bin/bin must stay runnable on a laptop without docker.
	if cli.V1.Mode == cli.ModeDocker || cli.V2.Mode == cli.ModeDocker {
		Expect(cli.DockerError()).To(BeEmpty(), "docker required for the selected leg(s)")
	}
})

var _ = AfterSuite(func() {
	fixtures.ShutdownRegistry()
})

var _ = Describe("compat", Label("compat"), func() {
	for _, c := range cases.All() {
		// ContinueOnFailure: default Ordered would skip every remaining
		// spec on the first failure, hiding the v2 result whenever v1
		// fails unexpectedly. Per-leg cascade gating lives in
		// runner.Transfer (it skips when its own leg's construct
		// produced no CTF).
		Context(c.ID, Ordered, ContinueOnFailure, Label(c.Kind), func() {
			var (
				rc      *runner.RunContext
				workdir string
			)

			BeforeAll(func(ctx SpecContext) {
				var err error
				workdir, err = os.MkdirTemp("", "compat-"+sanitize(c.ID)+"-")
				Expect(err).NotTo(HaveOccurred())
				rc, err = runner.New(ctx, c, workdir)
				Expect(err).NotTo(HaveOccurred(), "case Materialize failed")
			})

			AfterAll(func() {
				if rc != nil {
					rc.Cleanup()
				}
				if workdir != "" {
					_ = os.RemoveAll(workdir)
				}
			})

			// Source order matters: Ginkgo's source-order execution
			// preserves the construct → transfer dependency for free.
			specForPhase(c, &rc, "construct", cli.V1Leg, c.Expect.Construct.V1, func(ctx context.Context, leg cli.Leg) runner.PhaseResult {
				return rc.Construct(ctx, leg)
			})
			specForPhase(c, &rc, "construct", cli.V2Leg, c.Expect.Construct.V2, func(ctx context.Context, leg cli.Leg) runner.PhaseResult {
				return rc.Construct(ctx, leg)
			})
			specForPhase(c, &rc, "transfer", cli.V1Leg, c.Expect.Transfer.V1, func(ctx context.Context, leg cli.Leg) runner.PhaseResult {
				addr, err := fixtures.SharedRegistry(ctx)
				if err != nil {
					return runner.PhaseResult{Skipped: true, SkipReason: "shared registry unavailable: " + err.Error()}
				}
				return rc.Transfer(ctx, leg, addr)
			})
			specForPhase(c, &rc, "transfer", cli.V2Leg, c.Expect.Transfer.V2, func(ctx context.Context, leg cli.Leg) runner.PhaseResult {
				addr, err := fixtures.SharedRegistry(ctx)
				if err != nil {
					return runner.PhaseResult{Skipped: true, SkipReason: "shared registry unavailable: " + err.Error()}
				}
				return rc.Transfer(ctx, leg, addr)
			})
		})
	}
})

func specForPhase(
	c *cases.Case,
	rcPtr **runner.RunContext,
	phase string,
	leg cli.Leg,
	expect cases.PhaseExpect,
	phaseFn func(ctx context.Context, leg cli.Leg) runner.PhaseResult,
) {
	It(string(leg)+" "+phase, func(ctx SpecContext) {
		// A declared skip never invokes the CLI.
		if expect.Outcome == cases.OutcomeSkip {
			reason := expect.Reason
			if reason == "" {
				reason = "case declared " + phase + "." + string(leg) + ": skip"
			}
			Skip(reason)
		}
		rc := *rcPtr
		Expect(rc).NotTo(BeNil(), "BeforeAll did not populate the run context")
		res := phaseFn(ctx, leg)
		if res.Skipped {
			Skip(res.SkipReason)
		}
		assertPhase(string(leg)+" "+phase, expect, res)
	})
}

func assertPhase(label string, exp cases.PhaseExpect, res runner.PhaseResult) {
	GinkgoHelper()
	switch exp.Outcome {
	case cases.OutcomePass, "":
		Expect(res.Err).NotTo(HaveOccurred(),
			"%s expected to succeed; output:\n%s", label, res.Output)
	case cases.OutcomeFail:
		Expect(res.Err).To(HaveOccurred(),
			"%s expected to fail; output:\n%s", label, res.Output)
		// ResolvedErrSubstr returns the literal ErrSubstr or the
		// expectKind registry entry, whichever the case set. Empty
		// means no substring assertion.
		if sub := exp.ResolvedErrSubstr(); sub != "" {
			Expect(res.Output).To(ContainSubstring(sub),
				"%s expected error substring %q; output:\n%s", label, sub, res.Output)
		}
	case cases.OutcomeSkip:
		// Already handled by the Skip() above.
	default:
		Fail(fmt.Sprintf("%s: unknown outcome %q", label, exp.Outcome))
	}
}

// sanitize is the tmpdir-name variant: case-preserving (macOS/Linux
// tmpdirs are case-sensitive). runner.sanitizeID lower-cases for the
// same reason but on OCI repo paths, where v1's normalisation rules
// differ. They share no constraints, so they stay separate.
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return strings.TrimSpace(string(out))
}
