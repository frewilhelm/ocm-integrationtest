// Package compat hosts the v1/v2 OCM CLI compatibility test.
//
// The suite drives the same OCM component-constructor through both CLIs
// (v1: github.com/open-component-model/ocm; v2:
// github.com/open-component-model/open-component-model) as subprocesses,
// then through construct and transfer per leg. Download is out of scope.
// Each CLI owns its own download tests.
//
// CLI selection happens via OCM_V1_CMD / OCM_V2_CMD, either
// `docker:<image>` or `bin:/path/to/ocm`. Cases are YAML files under
// compat/cases/; expectations ride on compat.ocm.software/* labels on the
// component. See compat/README.md for the case format, the fixture
// system, and how to add a case.
package compat
