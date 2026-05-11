// Package core and its subpackages contain reeve's pure logic: rule
// resolution, state machines, rendering, and pipelines. The packages are
// pure in the sense that I/O lives behind interfaces defined at use-sites
// (e.g. approvals.TeamExpander) and never inside core; adapters in
// internal/iac, internal/vcs, internal/blob, internal/auth, internal/slack,
// and internal/observability satisfy those interfaces.
//
// Imports allowed: stdlib, sibling internal/core/* packages, and a small
// curated set of pure-data dependencies that the .golangci.yml depguard rule
// permits (notably github.com/bmatcuk/doublestar/v4 for glob matching and
// github.com/robfig/cron/v3 for freeze-window expressions). Cloud SDKs,
// telemetry, CLI wiring, and engine code are denied.
package core
