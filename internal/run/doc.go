// Package run is one of two orchestration layers (the other is
// internal/drift). preview.go and apply.go compose pure-core pipelines with
// IaC, VCS, blob, auth, notifications, and observability adapters. The
// drift runner does the same shape for scheduled drift detection.
//
// As an orchestrator, this package is allowed to import every adapter -
// no other package outside `cmd/` should.
package run
