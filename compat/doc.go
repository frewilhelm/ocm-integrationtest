// Package compat hosts the v1/v2 OCM CLI compatibility test.
//
// The suite drives the same OCM component-constructor through both CLIs
// (v1: github.com/open-component-model/ocm; v2:
// github.com/open-component-model/open-component-model) as subprocesses,
// then through construct and transfer per leg. Download is out of scope.
// Each CLI owns its own download tests.
//
// CLI selection happens via OCM_V1_CMD / OCM_V2_CMD, either
// `docker:<image>` or `bin:/path/to/ocm`. Cases live as Go tables in
// compat/internal/cases (access_cases.go, inputs_cases.go); each entry
// pairs a raw component-constructor YAML body with its fixtures and the
// per-phase v1/v2 expectations. See compat/README.md for how to add a
// case and how the fixture system works.
package compat
