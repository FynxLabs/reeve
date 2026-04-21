// Package core and its subpackages contain reeve's pure logic: rule resolution,
// state machines, rendering, and pipelines. No I/O, no external packages beyond
// stdlib and sibling core packages. Effects live behind interfaces defined at
// use-sites within each subpackage; adapters in internal/iac, internal/vcs,
// internal/blob, internal/auth, internal/slack, and internal/observability
// satisfy those interfaces.
package core
