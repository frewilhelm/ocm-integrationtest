// Package cases holds the compat suite's test cases as Go tables. Each
// Case pairs a raw component-constructor YAML string with the fixtures
// it needs and the per-phase v1/v2 expectations. Cases live in
// access_cases.go and inputs_cases.go and are appended to the package
// registry at init time.
//
// Materialize starts a case's fixtures, resolves `${NAME.key}` placeholders
// in the constructor body, and writes a clean constructor.yaml to the
// case's workdir.
package cases

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/open-component-model/ocm-integrationtest/compat/internal/fixtures"
)

type Outcome string

const (
	OutcomePass Outcome = "pass"
	OutcomeFail Outcome = "fail"
	OutcomeSkip Outcome = "skip"
)

// PhaseExpect declares what one leg is supposed to do in one phase.
//
// ErrSubstr is the literal substring asserted against CLI output on a
// fail outcome. ExpectKind is the indirection mechanism: when many
// cases share a wording (e.g. v2 access plugins all emit
// `failed to get plugin for typ "..."`), they reference a registered
// name instead, so one line changes here when v2 reworks the wording,
// not N cases. ExpectKind and ErrSubstr are mutually exclusive and that
// is enforced at package-init.
type PhaseExpect struct {
	Outcome    Outcome
	ErrSubstr  string
	ExpectKind string
	Reason     string
}

// Pass / Fail / Skip are short constructors so case declarations read
// close to the old YAML shape. `Pass` for the common
// `v1: pass` / `v2: pass` lines; `Fail("...")` and `FailKind("...")`
// for the substring-anchored failure variants; `Skip("...")` for
// declared skips.
func Pass() PhaseExpect                       { return PhaseExpect{Outcome: OutcomePass} }
func Fail(substr string) PhaseExpect          { return PhaseExpect{Outcome: OutcomeFail, ErrSubstr: substr} }
func FailKind(kind string) PhaseExpect        { return PhaseExpect{Outcome: OutcomeFail, ExpectKind: kind} }
func Skip(reason string) PhaseExpect          { return PhaseExpect{Outcome: OutcomeSkip, Reason: reason} }

// expectKinds maps a symbolic case-author label to the literal CLI
// substring v1/v2 currently emit. Add an entry when a failure mode
// repeats across cases; never inline the raw wording in cases.
//
// Each entry is the SHORTEST stable prefix of the real error that is
// unique enough to anchor on: long enough not to match unrelated
// "plugin" mentions, short enough to survive minor message tweaks.
var expectKinds = map[string]string{
	// v2 has no plugin for the access type referenced in the
	// constructor. Emitted by v2's plugin resolver in `add cv` for
	// every unknown access (Wget, NPM, Maven, S3, GitHub, ...). Shape:
	//   failed to get plugin for typ "<TypeName>": ...
	"v2:access_plugin_missing": `failed to get plugin for typ "`,
	// v2 has no input plugin of the requested kind. Emitted in
	// `add cv` for unknown input specifications (maven, npm,
	// ociArtifact, wget, ...). The opening fragment is stable across
	// kinds because v2 formats `input specification of type %q`.
	"v2:input_kind_unsupported": `input specification of type "`,
	// v2 expects an OCI image manifest where the case points at a
	// layer blob. Only emitted today by access:OCIImageLayer.
	"v2:expected_oci_image": `: expected OCI image`,
}

// ResolvedErrSubstr returns the substring to assert on for this phase:
// the literal ErrSubstr when set, the registry lookup of ExpectKind
// otherwise. Empty means "no substring assertion"; only the fail/pass
// exit code matters.
func (p PhaseExpect) ResolvedErrSubstr() string {
	if p.ErrSubstr != "" {
		return p.ErrSubstr
	}
	if p.ExpectKind != "" {
		return expectKinds[p.ExpectKind]
	}
	return ""
}

type LegExpect struct {
	V1 PhaseExpect
	V2 PhaseExpect
}

type Expectation struct {
	Construct LegExpect
	Transfer  LegExpect
}

// FixtureSpec is one fixture the case needs stood up before its
// constructor materialises. Name is the identifier the constructor
// references via `${Name.key}`; Kind names a fixtures.StartFunc; With
// is the per-instance config, same map-of-any shape the fixture kinds
// already accept.
type FixtureSpec struct {
	Name string
	Kind string
	With map[string]any
}

// Case is one compat test case. Constructor is a raw component-constructor
// YAML body; the fixture outputs are substituted into it at Materialize
// time. Fixtures / Expect / ID come from the case declaration, not from
// any embedded label — the constructor stays byte-for-byte what a real
// OCM user would author.
type Case struct {
	ID          string
	Kind        string // set by registerCases from the calling file
	Constructor string
	Fixtures    []FixtureSpec
	Expect      Expectation

	// Notes is free-form documentation for readers; not asserted on.
	Notes string

	// Projected from the constructor at package init.
	ComponentName    string
	ComponentVersion string
}

// allCases is populated by registerCases from each *_cases.go init().
var allCases []*Case

// All returns the registered cases in a stable order (kind, then id).
// Callers must not mutate the returned slice; the underlying *Case
// pointers are the singletons registered at init.
func All() []*Case {
	out := make([]*Case, len(allCases))
	copy(out, allCases)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// registerCases finalises each case's Kind, validates its shape, and
// appends it to allCases. Called from access_cases.go and
// inputs_cases.go init(). A malformed case panics at test-binary
// startup — same guarantee the old YAML load-time validation gave.
func registerCases(kind string, cs ...*Case) {
	for _, c := range cs {
		c.Kind = kind
		if err := c.validate(); err != nil {
			panic(fmt.Sprintf("compat case %q: %v", c.ID, err))
		}
		allCases = append(allCases, c)
	}
}

// constructorProbe is the minimal subset of a component-constructor
// needed to project the transfer source (name, version) and enforce
// the single-component invariant. Anything else in the YAML is passed
// through untouched by Materialize.
type constructorProbe struct {
	Components []struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"components"`
}

func (c *Case) validate() error {
	if c.ID == "" {
		return fmt.Errorf("ID is empty")
	}
	if strings.TrimSpace(c.Constructor) == "" {
		return fmt.Errorf("Constructor is empty")
	}
	var probe constructorProbe
	if err := yaml.Unmarshal([]byte(c.Constructor), &probe); err != nil {
		return fmt.Errorf("parse constructor: %w", err)
	}
	// Transfer source is a single (name, version); silently picking
	// the first of N would mask resources on later components, so
	// reject at init instead of mid-phase. When cross-component
	// references become a tested concern, drop this guard and grow
	// the runner a "transfer all components" loop.
	if n := len(probe.Components); n != 1 {
		return fmt.Errorf("constructor must declare exactly one component (found %d)", n)
	}
	c.ComponentName = probe.Components[0].Name
	c.ComponentVersion = probe.Components[0].Version
	if c.ComponentName == "" {
		return fmt.Errorf("component name is empty")
	}
	if c.ComponentVersion == "" {
		return fmt.Errorf("component version is empty")
	}
	phases := []struct {
		label string
		exp   PhaseExpect
	}{
		{"construct.v1", c.Expect.Construct.V1},
		{"construct.v2", c.Expect.Construct.V2},
		{"transfer.v1", c.Expect.Transfer.V1},
		{"transfer.v2", c.Expect.Transfer.V2},
	}
	for _, p := range phases {
		if err := validatePhase(p.exp); err != nil {
			return fmt.Errorf("%s: %w", p.label, err)
		}
	}
	return nil
}

func validatePhase(p PhaseExpect) error {
	switch p.Outcome {
	case OutcomePass, OutcomeFail, OutcomeSkip, "":
	default:
		return fmt.Errorf("unknown outcome %q (want pass, fail, or skip)", p.Outcome)
	}
	if p.ErrSubstr != "" && p.ExpectKind != "" {
		return fmt.Errorf("errSubstr and expectKind are mutually exclusive")
	}
	if p.ExpectKind != "" {
		if _, known := expectKinds[p.ExpectKind]; !known {
			names := make([]string, 0, len(expectKinds))
			for k := range expectKinds {
				names = append(names, k)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown expectKind %q (registered: %s)",
				p.ExpectKind, strings.Join(names, ", "))
		}
	}
	return nil
}

type MaterializeResult struct {
	// ConstructorPath is what the CLI is pointed at: in docker mode
	// the in-container view (/work/constructor.yaml); in bin mode the
	// absolute host path.
	ConstructorPath string
	ConstructorRel  string

	ExtraV1Args []string
	ExtraV2Args []string
	ExtraV1Env  []string
	ExtraV2Env  []string

	// Cleanups run in reverse order at case teardown.
	Cleanups []func()

	// Skip, when non-empty, signals that a fixture decided the whole
	// case must skip (e.g. docker unavailable).
	Skip string
}

// substRe matches `${NAME.key}` where key can contain further dots
// (e.g. ${HTTP.payload.txt.url}). Substitution is performed textually
// on the raw constructor string.
var substRe = regexp.MustCompile(`\$\{([A-Za-z0-9_]+\.[A-Za-z0-9_.]+)\}`)

// Materialize starts every declared fixture, resolves `${NAME.key}` in
// the constructor body against fixture outputs, and writes the result
// to <workdir>/constructor.yaml. Partial cleanups are returned on error
// so the caller can still run them.
//
// Substitution is purely textual: fixture outputs today are one of
// (HTTP URL, OCI ref host:port + path, digest hex, filesystem path)
// and all of them are valid YAML plain scalars, so the emitted document
// re-parses on both v1 and v2. If a future fixture surfaces a value
// containing YAML metacharacters (newlines, leading `!`, `&`/`*`
// anchors), the fixture itself must quote/escape before placing it on
// the substitution map; this helper is the wrong layer for that.
func (c *Case) Materialize(ctx context.Context, workdir string) (*MaterializeResult, error) {
	res := &MaterializeResult{}
	subs := map[string]string{}

	for _, fx := range c.Fixtures {
		fn, err := fixtures.Lookup(fx.Kind)
		if err != nil {
			return res, fmt.Errorf("%s: %w", c.ID, err)
		}
		sr, err := fn(ctx, workdir, fx.With)
		if err != nil {
			return res, fmt.Errorf("%s: start fixture %s (%s): %w", c.ID, fx.Name, fx.Kind, err)
		}
		if sr.Cleanup != nil {
			res.Cleanups = append(res.Cleanups, sr.Cleanup)
		}
		if sr.Skip != "" {
			res.Skip = sr.Skip
			return res, nil
		}
		for k, v := range sr.Outputs {
			subs[fx.Name+"."+k] = v
		}
		res.ExtraV1Args = append(res.ExtraV1Args, sr.ExtraV1Args...)
		res.ExtraV2Args = append(res.ExtraV2Args, sr.ExtraV2Args...)
		res.ExtraV1Env = append(res.ExtraV1Env, sr.ExtraV1Env...)
		res.ExtraV2Env = append(res.ExtraV2Env, sr.ExtraV2Env...)
	}

	resolved, err := resolvePlaceholders(c.Constructor, subs)
	if err != nil {
		return res, fmt.Errorf("%s: substitute: %w", c.ID, err)
	}

	rel := "constructor.yaml"
	full := filepath.Join(workdir, rel)
	if err := os.WriteFile(full, []byte(resolved), 0o644); err != nil {
		return res, fmt.Errorf("%s: write constructor: %w", c.ID, err)
	}
	res.ConstructorPath = full
	res.ConstructorRel = rel
	return res, nil
}

// resolvePlaceholders replaces every `${NAME.key}` in src with the
// corresponding subs entry. Unresolved keys are collected into a single
// error so a case with three broken references reports all three at
// once. Exported via the test file only.
func resolvePlaceholders(src string, subs map[string]string) (string, error) {
	var missing []string
	seen := map[string]bool{}
	out := substRe.ReplaceAllStringFunc(src, func(match string) string {
		key := match[2 : len(match)-1]
		if v, ok := subs[key]; ok {
			return v
		}
		if !seen[key] {
			seen[key] = true
			missing = append(missing, key)
		}
		return match
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("unresolved placeholder(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}
