// Package drift implements drift detection as the third run mode alongside
// preview and apply. Reuses stack discovery, auth, engine abstraction,
// bucket storage, and observability. Different trigger (scheduled),
// different output (report + sinks), different urgency model.
// See PLAN.md §5.9.
package drift
