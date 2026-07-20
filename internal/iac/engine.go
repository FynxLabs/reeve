// Package iac defines the engine-agnostic IaC interface. Adapters
// (internal/iac/pulumi, future: terraform, opentofu) implement it.
// Core never branches on engine name; consumers use Capabilities()
// for capability detection (PLAN.md §6.3).
package iac

// Capabilities describes what an engine can do. Extended as new engines
// reveal needs.
type Capabilities struct {
	SupportsSavedPlans   bool
	SupportsRefresh      bool
	SupportsPolicyNative bool
	SecretsProviderTypes []string
}

// Engine is the full contract every IaC adapter satisfies: identity and
// capability detection plus the operational surface (enumerate, preview,
// apply, drift check). Callers resolve an Engine through New (the registry,
// keyed by config engine.type) and stay engine-agnostic; consumers that need
// less depend on the narrow per-operation interfaces (Enumerator, Previewer,
// Applier, DriftChecker) instead.
type Engine interface {
	Name() string // display only - never branch on this
	Capabilities() Capabilities
	Enumerator
	Previewer
	Applier
	DriftChecker
}
