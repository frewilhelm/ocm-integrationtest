// Package fixtures provides the named "fixture kinds" referenced from case
// YAML (compat.ocm.software/fixtures labels). A fixture stands up
// something the constructor needs to point at: an HTTP file server, a
// Maven repo, an OCI registry artefact, an S3 bucket, etc.
//
// Outputs are flat string-keyed values substituted into the constructor
// wherever `${NAME.key}` appears.
package fixtures

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Outputs map[string]string

type Cleanup func()

// StartResult bundles what a fixture returns:
//   - Outputs:   substituted into the constructor as ${NAME.key}
//   - Cleanup:   deferred per-case
//   - ExtraV*:   CLI args injected globally (e.g. v1PlainHTTPConfig's --config)
//   - ExtraV*Env: KEY=value env entries forwarded to the CLI process
//   - Skip:      non-empty makes the whole case skip
type StartResult struct {
	Outputs     Outputs
	Cleanup     Cleanup
	ExtraV1Args []string
	ExtraV2Args []string
	ExtraV1Env  []string
	ExtraV2Env  []string
	Skip        string
}

// StartFunc starts a fixture.
//
//   - Success: return a populated StartResult; its Cleanup (if any)
//     runs at teardown.
//   - Error: tear down any partial work yourself. The returned
//     StartResult, including any Cleanup, is dropped on error.
//   - Skip: `StartResult{Skip: "<reason>"}, nil`. Case skips, no
//     later fixtures run.
type StartFunc func(ctx context.Context, workdir string, with map[string]any) (StartResult, error)

// registered holds the StartFunc for each fixture kind. Populated at
// package init only. Register is NOT concurrent-safe and must not be
// called from goroutines. Reads during the test phase are safe because
// Go orders all init() functions before any test goroutine starts.
var registered = map[string]StartFunc{}

// Register panics on duplicate names; fixtures are wired once at init.
func Register(kind string, fn StartFunc) {
	if _, dup := registered[kind]; dup {
		panic("fixtures: duplicate kind " + kind)
	}
	registered[kind] = fn
}

func Lookup(kind string) (StartFunc, error) {
	if fn, ok := registered[kind]; ok {
		return fn, nil
	}
	names := make([]string, 0, len(registered))
	for k := range registered {
		names = append(names, k)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("fixtures: unknown kind %q (registered: %s)", kind, strings.Join(names, ", "))
}
