// Package run is the orchestration layer - the only place that imports
// from every other module. preview.go, apply.go, and drift.go compose
// pure-core pipelines with IaC, VCS, blob, auth, notifications, and
// observability adapters. See PLAN.md §6.1.
package run
