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

// Engine is the minimum contract every IaC adapter satisfies.
// Concrete methods (EnumerateStacks, Preview, Apply, Refresh) are defined
// at use-sites; this type is the capability/identity anchor.
type Engine interface {
	Name() string // display only - never branch on this
	Capabilities() Capabilities
}
