// Package cases loads compat test cases from YAML files. A case file IS a
// real OCM component-constructor with `compat.ocm.software/*` labels
// carrying test metadata (case id, fixtures, per-phase expectations).
//
// LoadAll walks a directory tree; Materialize starts the case's fixtures,
// substitutes `${NAME.key}` in the constructor body, strips the compat.*
// labels, and writes a clean constructor.yaml.
package cases

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

// PhaseExpect declares what one leg is supposed to do in one phase. A
// bare YAML string decodes into Outcome only; a mapping decodes the
// full struct.
//
// ErrSubstr is the literal substring asserted against CLI output on a
// fail outcome. ExpectKind is the indirection mechanism: when many
// cases share a wording (e.g. v2 access plugins all emit
// `failed to get plugin for typ "..."`), they reference a registered
// name instead, so one line changes here when v2 reworks the wording,
// not N case YAMLs. ExpectKind and ErrSubstr are mutually exclusive
// and that is enforced at load.
type PhaseExpect struct {
	Outcome    Outcome `yaml:"outcome"`
	ErrSubstr  string  `yaml:"errSubstr,omitempty"`
	ExpectKind string  `yaml:"expectKind,omitempty"`
	Reason     string  `yaml:"reason,omitempty"`
}

// expectKinds maps a symbolic case-author label to the literal CLI
// substring v1/v2 currently emit. Add an entry when a failure mode
// repeats across cases; never inline the raw wording in case YAMLs.
//
// Each entry is the SHORTEST stable prefix of the real error that is
// unique enough to anchor on: long enough to not match unrelated
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

// resolvedErrSubstr returns the substring to assert on for this phase:
// the literal ErrSubstr when set, the registry lookup of ExpectKind
// otherwise. Empty means "no substring assertion"; only the fail/pass
// exit code matters.
func (p PhaseExpect) resolvedErrSubstr() string {
	if p.ErrSubstr != "" {
		return p.ErrSubstr
	}
	if p.ExpectKind != "" {
		return expectKinds[p.ExpectKind]
	}
	return ""
}

// UnmarshalYAML accepts either a scalar (`pass`) or a mapping
// (`{outcome: fail, errSubstr: ...}`). Outcome is validated against
// the closed set {pass, fail, skip} at load; an `outcome: passes`
// typo would otherwise survive load and only surface mid-run as a
// generic "unknown outcome" Fail() inside assertPhase.
func (p *PhaseExpect) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		p.Outcome = Outcome(node.Value)
	case yaml.MappingNode:
		type raw PhaseExpect
		var r raw
		if err := node.Decode(&r); err != nil {
			return err
		}
		*p = PhaseExpect(r)
	default:
		return fmt.Errorf("phase expectation: unsupported YAML kind %v", node.Kind)
	}
	switch p.Outcome {
	case OutcomePass, OutcomeFail, OutcomeSkip, "":
	default:
		return fmt.Errorf("phase expectation: unknown outcome %q (want pass, fail, or skip)", p.Outcome)
	}
	if p.ErrSubstr != "" && p.ExpectKind != "" {
		return fmt.Errorf("phase expectation: errSubstr and expectKind are mutually exclusive")
	}
	if p.ExpectKind != "" {
		if _, known := expectKinds[p.ExpectKind]; !known {
			names := make([]string, 0, len(expectKinds))
			for k := range expectKinds {
				names = append(names, k)
			}
			sort.Strings(names)
			return fmt.Errorf("phase expectation: unknown expectKind %q (registered: %s)",
				p.ExpectKind, strings.Join(names, ", "))
		}
	}
	return nil
}

// ResolvedErrSubstr is the public accessor for the substring assertion.
// "" disables the assertion; the test only checks the exit code.
func (p PhaseExpect) ResolvedErrSubstr() string { return p.resolvedErrSubstr() }

type LegExpect struct {
	V1 PhaseExpect `yaml:"v1"`
	V2 PhaseExpect `yaml:"v2"`
}

type Expectation struct {
	Construct LegExpect `yaml:"construct"`
	Transfer  LegExpect `yaml:"transfer"`
}

// FixtureSpec is one entry under `compat.ocm.software/fixtures`. Name
// is the identifier the constructor references via ${Name.key}; Kind
// names a fixtures.StartFunc; With is the per-instance config.
type FixtureSpec struct {
	Name string         `yaml:"name"`
	Kind string         `yaml:"kind"`
	With map[string]any `yaml:"with,omitempty"`
}

type Meta struct {
	ID    string `yaml:"id"`
	Notes string `yaml:"notes,omitempty"`
}

type Case struct {
	File string

	Meta     Meta
	Fixtures []FixtureSpec
	Expect   Expectation

	// Doc is the parsed constructor YAML; compat labels are still
	// present here and only stripped at Materialize time.
	Doc *yaml.Node

	// Projected from the first component for transfer.
	ComponentName    string
	ComponentVersion string
}

func (c *Case) ID() string {
	if c.Meta.ID != "" {
		return c.Meta.ID
	}
	return strings.TrimSuffix(filepath.Base(c.File), filepath.Ext(c.File))
}

// Kind is the directory under cases/ (e.g. "inputs" or "access").
func (c *Case) Kind() string {
	rel := filepath.Dir(c.File)
	return filepath.Base(rel)
}

// LoadAll walks root and returns cases sorted by file path.
func LoadAll(root string) ([]*Case, error) {
	var out []*Case
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		c, err := loadOne(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, c)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, nil
}

func loadOne(path string) (*Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	c := &Case{File: path, Doc: &doc}
	if err := extractLabels(c); err != nil {
		return nil, err
	}
	if err := extractComponentInfo(c); err != nil {
		return nil, err
	}
	return c, nil
}

func extractLabels(c *Case) error {
	comp, err := firstComponentNode(c.Doc)
	if err != nil {
		return err
	}
	labels := mappingValue(comp, "labels")
	if labels == nil || labels.Kind != yaml.SequenceNode {
		return nil
	}
	for _, lbl := range labels.Content {
		if lbl.Kind != yaml.MappingNode {
			continue
		}
		nameNode := mappingValue(lbl, "name")
		valueNode := mappingValue(lbl, "value")
		if nameNode == nil || valueNode == nil {
			continue
		}
		switch nameNode.Value {
		case "compat.ocm.software/case":
			if err := valueNode.Decode(&c.Meta); err != nil {
				return fmt.Errorf("decode case label: %w", err)
			}
		case "compat.ocm.software/fixtures":
			if err := valueNode.Decode(&c.Fixtures); err != nil {
				return fmt.Errorf("decode fixtures label: %w", err)
			}
		case "compat.ocm.software/expect":
			if err := valueNode.Decode(&c.Expect); err != nil {
				return fmt.Errorf("decode expect label: %w", err)
			}
		}
	}
	return nil
}

func extractComponentInfo(c *Case) error {
	comp, err := firstComponentNode(c.Doc)
	if err != nil {
		return err
	}
	// Transfer source is a single (name, version); silently picking
	// the first of N would mask resources on later components, so
	// reject at load instead of mid-phase. When cross-component
	// references become a tested concern, drop this guard and grow
	// the runner a "transfer all components" loop.
	if n := componentsCount(c.Doc); n > 1 {
		return fmt.Errorf("multi-component constructors not supported (%d components found); split into separate case files", n)
	}
	if n := mappingValue(comp, "name"); n != nil {
		c.ComponentName = n.Value
	}
	if n := mappingValue(comp, "version"); n != nil {
		c.ComponentVersion = n.Value
	}
	return nil
}

func componentsCount(doc *yaml.Node) int {
	root := doc
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return 0
		}
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return 0
	}
	comps := mappingValue(root, "components")
	if comps == nil || comps.Kind != yaml.SequenceNode {
		return 0
	}
	return len(comps.Content)
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

// Materialize starts every declared fixture, builds the substitution
// table, walks the constructor YAML replacing `${NAME.key}` in scalars,
// strips the compat.* labels, and writes the resolved constructor to
// <workdir>/constructor.yaml. Partial cleanups are returned on error
// so the caller can still run them.
func (c *Case) Materialize(ctx context.Context, workdir string) (*MaterializeResult, error) {
	res := &MaterializeResult{}
	subs := map[string]string{}

	for _, fx := range c.Fixtures {
		fn, err := fixtures.Lookup(fx.Kind)
		if err != nil {
			return res, fmt.Errorf("%s: %w", c.File, err)
		}
		sr, err := fn(ctx, workdir, fx.With)
		if err != nil {
			return res, fmt.Errorf("%s: start fixture %s (%s): %w", c.File, fx.Name, fx.Kind, err)
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

	// Deep-copy so repeated Materialize calls don't see the stripped labels.
	resolved := cloneNode(c.Doc)
	if err := substituteScalars(resolved, subs); err != nil {
		return res, fmt.Errorf("%s: substitute: %w", c.File, err)
	}
	if err := stripCompatLabels(resolved); err != nil {
		return res, fmt.Errorf("%s: strip labels: %w", c.File, err)
	}
	// v1's templating pre-pass scans the whole file for $(...)/${...}
	// directives, comments included, and bails with "missing
	// closing brace" on a stray example. Drop comments so case YAML
	// headers can mention ${NAME.key} freely.
	stripComments(resolved)

	out, err := yaml.Marshal(resolved)
	if err != nil {
		return res, fmt.Errorf("%s: marshal resolved constructor: %w", c.File, err)
	}
	rel := "constructor.yaml"
	full := filepath.Join(workdir, rel)
	if err := os.WriteFile(full, out, 0o644); err != nil {
		return res, fmt.Errorf("%s: write constructor: %w", c.File, err)
	}
	res.ConstructorPath = full
	res.ConstructorRel = rel
	return res, nil
}

func firstComponentNode(doc *yaml.Node) (*yaml.Node, error) {
	root := doc
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) == 0 {
			return nil, fmt.Errorf("empty document")
		}
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("top level is %v, want mapping", root.Kind)
	}
	comps := mappingValue(root, "components")
	if comps == nil || comps.Kind != yaml.SequenceNode || len(comps.Content) == 0 {
		return nil, fmt.Errorf("no components[] in constructor")
	}
	return comps.Content[0], nil
}

func mappingValue(mapNode *yaml.Node, key string) *yaml.Node {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i].Value == key {
			return mapNode.Content[i+1]
		}
	}
	return nil
}

var substRe = regexp.MustCompile(`\$\{([A-Za-z0-9_]+\.[A-Za-z0-9_.]+)\}`)

// looksTyped reports whether s parses cleanly as a non-string YAML
// scalar (int / float / bool / null). Conservative on purpose:
// anything with a colon, slash, or non-numeric punctuation falls
// through to false so URLs and refs stay !!str. Used by
// substituteScalars to decide whether to clear the resolved tag and
// let yaml.v3 re-infer on marshal.
func looksTyped(s string) bool {
	if s == "" {
		return false
	}
	// Numerics: plain decimal only. No leading "+", no hex, no
	// underscores. yaml.v3's core schema would accept more, but the
	// long tail is safer left as !!str than retyped and surprising
	// the case author.
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil &&
		// strconv.ParseFloat accepts "Inf" / "NaN" / "1e2"; the
		// decimal-point filter prevents retyping "Inf" (a perfectly
		// valid hostname / repo segment).
		strings.ContainsRune(s, '.') {
		return true
	}
	switch s {
	case "true", "false", "null", "~":
		return true
	}
	return false
}

// substituteScalars walks the YAML AST and resolves every
// `${NAME.key}` reference in scalar values against `subs`. The
// substituted value is inserted verbatim: no escaping, no quoting.
// That is safe today because every fixture output is one of (HTTP URL,
// OCI ref bare host:port + path, digest hex, filesystem path) and all
// of them survive raw YAML insertion. If a future fixture surfaces a
// string containing YAML metacharacters (newlines, leading `!`, `&`/
// `*` anchors, etc.) or a CLI-quoted value, the fixture itself must
// validate before placing it on the substitution map.
// substituteScalars is the wrong layer to do that filtering.
func substituteScalars(n *yaml.Node, subs map[string]string) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "${") {
		var missing []string
		replaced := substRe.ReplaceAllStringFunc(n.Value, func(match string) string {
			key := match[2 : len(match)-1]
			if v, ok := subs[key]; ok {
				return v
			}
			missing = append(missing, key)
			return match
		})
		if len(missing) > 0 {
			return fmt.Errorf("unresolved placeholder(s): %s", strings.Join(missing, ", "))
		}
		n.Value = replaced
		// Tag policy: clear the resolved tag for typed-looking values
		// (number / bool / null) so yaml.v3 re-infers on marshal.
		// `port: ${HTTP.port}` with output "43210" emits as
		// `port: 43210` (an !!int) rather than the string "43210".
		// Otherwise pin to !!str so a payload that happens to be
		// numeric in a string-typed context stays quoted. Most fixture
		// outputs are URLs/hosts/refs and fall into the second branch.
		if looksTyped(replaced) {
			n.Tag = ""
			n.Style = 0
		} else {
			n.Tag = "!!str"
		}
	}
	for _, c := range n.Content {
		if err := substituteScalars(c, subs); err != nil {
			return err
		}
	}
	return nil
}

func stripCompatLabels(doc *yaml.Node) error {
	comp, err := firstComponentNode(doc)
	if err != nil {
		return err
	}
	labels := mappingValue(comp, "labels")
	if labels == nil || labels.Kind != yaml.SequenceNode {
		return nil
	}
	kept := labels.Content[:0]
	for _, lbl := range labels.Content {
		nameNode := mappingValue(lbl, "name")
		if nameNode != nil && strings.HasPrefix(nameNode.Value, "compat.ocm.software/") {
			continue
		}
		kept = append(kept, lbl)
	}
	labels.Content = kept
	if len(labels.Content) == 0 {
		for i := 0; i+1 < len(comp.Content); i += 2 {
			if comp.Content[i].Value == "labels" {
				comp.Content = append(comp.Content[:i], comp.Content[i+2:]...)
				break
			}
		}
	}
	return nil
}

// cloneNode deep-copies a yaml.Node tree for in-place mutation
// (substitution + label stripping) without disturbing the cached
// original. The clone is alias-free by design: case YAMLs do not use
// anchors (`&name`) / aliases (`*name`, `<<:`), and this cloner does
// not preserve them. Alias targets would be silently dropped. If a
// case ever needs anchors, either reject them at loadOne or inline
// aliases by cloning n.Alias instead of zeroing it here.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	cp := *n
	cp.Alias = nil
	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			cp.Content[i] = cloneNode(child)
		}
	}
	return &cp
}

// stripComments recursively clears Head/Line/FootComment on every
// node. v1's templating pre-pass scans comments for ${...}/$(...)
// and a stray example would bail it with "missing closing brace".
func stripComments(n *yaml.Node) {
	if n == nil {
		return
	}
	n.HeadComment = ""
	n.LineComment = ""
	n.FootComment = ""
	for _, c := range n.Content {
		stripComments(c)
	}
}
