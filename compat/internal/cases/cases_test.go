package cases

import (
	"strings"
	"testing"
)

// TestAllValidated is a sanity check that every registered case survived
// package init's validate() call. If a case was appended to allCases
// without validation, or a validate error path was accidentally weakened,
// the resulting slice would still be non-empty here but a downstream
// field (ComponentName / ComponentVersion) would be blank. Guard both.
func TestAllValidated(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("no cases registered")
	}
	// One case per current YAML: 8 access + 9 inputs. This will drift
	// as cases are added; when it does, update the expected number
	// deliberately rather than letting a silent removal slip through.
	const want = 17
	if len(all) != want {
		t.Errorf("registered case count: got %d, want %d", len(all), want)
	}
	seen := map[string]bool{}
	for _, c := range all {
		if c.ID == "" || c.Kind == "" {
			t.Errorf("case with empty ID/Kind: %+v", c)
		}
		if c.ComponentName == "" || c.ComponentVersion == "" {
			t.Errorf("%s: component name/version not projected: %q / %q",
				c.ID, c.ComponentName, c.ComponentVersion)
		}
		if seen[c.ID] {
			t.Errorf("duplicate case ID %q", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestResolvePlaceholders(t *testing.T) {
	t.Parallel()
	subs := map[string]string{
		"HTTP.url":         "http://127.0.0.1:43210",
		"HTTP.payload.txt": "http://127.0.0.1:43210/payload.txt",
		"image.ref":        "127.0.0.1:5000/repo:tag",
	}
	in := "url: ${HTTP.url}\nfull: ${HTTP.payload.txt}\nref: ${image.ref}\n"
	out, err := resolvePlaceholders(in, subs)
	if err != nil {
		t.Fatalf("resolvePlaceholders: unexpected error %v", err)
	}
	want := "url: http://127.0.0.1:43210\nfull: http://127.0.0.1:43210/payload.txt\nref: 127.0.0.1:5000/repo:tag\n"
	if out != want {
		t.Errorf("resolved:\n%s\nwant:\n%s", out, want)
	}
}

func TestResolvePlaceholdersMissing(t *testing.T) {
	t.Parallel()
	_, err := resolvePlaceholders("a: ${X.y}\nb: ${A.b}\n", map[string]string{})
	if err == nil {
		t.Fatal("expected error for unresolved placeholders")
	}
	msg := err.Error()
	// Both keys should be reported, sorted.
	if !strings.Contains(msg, "A.b") || !strings.Contains(msg, "X.y") {
		t.Errorf("error message missing keys: %s", msg)
	}
}

// TestValidateRejects covers the invariants validate() enforces beyond
// the happy-path All() coverage: mutually-exclusive ErrSubstr/ExpectKind,
// unknown ExpectKind, multi-component constructor.
func TestValidateRejects(t *testing.T) {
	t.Parallel()
	base := `components:
- name: ocm.software/compat/x
  version: 1.0.0
  provider: { name: ocm.software }
  resources: []
`
	tests := []struct {
		name    string
		c       Case
		wantSub string
	}{
		{
			name: "ErrSubstr and ExpectKind mutually exclusive",
			c: Case{
				ID:          "x:bad-exclusive",
				Constructor: base,
				Expect: Expectation{Construct: LegExpect{
					V1: Pass(),
					V2: PhaseExpect{Outcome: OutcomeFail, ErrSubstr: "foo", ExpectKind: "v2:access_plugin_missing"},
				}},
			},
			wantSub: "mutually exclusive",
		},
		{
			name: "unknown ExpectKind",
			c: Case{
				ID:          "x:bad-kind",
				Constructor: base,
				Expect: Expectation{Construct: LegExpect{V1: Pass(), V2: FailKind("v2:nope")}},
			},
			wantSub: "unknown expectKind",
		},
		{
			name: "multi-component constructor",
			c: Case{
				ID: "x:multi",
				Constructor: `components:
- name: a
  version: 1.0.0
  provider: { name: p }
  resources: []
- name: b
  version: 1.0.0
  provider: { name: p }
  resources: []
`,
			},
			wantSub: "exactly one component",
		},
		{
			name: "unknown outcome",
			c: Case{
				ID:          "x:bad-outcome",
				Constructor: base,
				Expect:      Expectation{Construct: LegExpect{V1: PhaseExpect{Outcome: Outcome("passes")}}},
			},
			wantSub: "unknown outcome",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.validate()
			if err == nil {
				t.Fatalf("expected validate error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}
