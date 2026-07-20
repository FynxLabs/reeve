package iac

import (
	"context"

	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/core/summary"
)

// Enumerator produces the engine's native stack list.
type Enumerator interface {
	EnumerateStacks(ctx context.Context, root string) ([]discovery.Stack, error)
}

// PreviewOpts configures a preview call.
type PreviewOpts struct {
	Cwd        string
	ExtraArgs  []string
	Env        map[string]string
	TimeoutSec int
}

// PreviewResult is what the engine returns per stack.
type PreviewResult struct {
	Counts      summary.Counts
	PlanSummary string // human short summary (+/-/~/± per resource)
	PlanDiff    string // pulumi preview --diff output
	FullPlan    string // raw JSON preview output, redacted upstream
	Error       string // non-empty if preview failed for this stack
	// DriftedURNs lists the URNs of resources that actually changed (drift
	// path only; excludes unchanged "same" steps). Used to fingerprint the
	// drifted set so a change in *which* resources drift re-fires an alert.
	DriftedURNs []string
	// Resources carries per-resource structured change info for drift-noise
	// filtering (the drift runner's classification.ignore_properties /
	// ignore_resources / treat_as_drift). Populated best-effort by engine
	// adapters that expose a structured diff (Pulumi detailedDiff, Terraform
	// resource_drift); left nil by adapters that don't, in which case drift
	// filtering degrades to a no-op and the raw Counts stand.
	Resources []ResourceChange
}

// ResourceChange is one resource's normalized change in a drift/preview
// result. It is the engine-agnostic shape the drift runner filters over.
type ResourceChange struct {
	// Address is the resource's stable identifier: a Pulumi URN or a
	// Terraform/OpenTofu address. Matched against ignore_resources globs and
	// used to fingerprint the drifted set.
	Address string
	// Type is the provider resource-type token (Pulumi
	// "aws:ec2/instance:Instance", Terraform "aws_instance"). Matched against
	// ignore_properties.resource_type.
	Type string
	// Op is the normalized change verb: create | update | delete | replace.
	// (read / no-op steps are never included.)
	Op string
	// Paths lists the changed property paths for update/replace ops
	// ("tags.LastScanned", "config.rules[3].expression"). Matched against
	// ignore_properties.properties. Best-effort: empty when the engine does
	// not expose a per-property diff (e.g. Pulumi create/delete steps).
	Paths []string
	// Category classifies the change for treat_as_drift filtering. Set by the
	// adapter, which alone knows its engine's drift semantics:
	//   - "changed"  resource exists both sides, properties differ
	//   - "orphaned" tracked in state, gone from the cloud
	//   - "missing"  present in the cloud, untracked by state
	Category string
}

// Drift categories for ResourceChange.Category.
const (
	DriftChanged  = "changed"
	DriftOrphaned = "orphaned"
	DriftMissing  = "missing"
)

// Previewer runs a preview for a single stack.
type Previewer interface {
	Preview(ctx context.Context, stack discovery.Stack, opts PreviewOpts) (PreviewResult, error)
}

// ApplyOpts configures an apply call.
type ApplyOpts struct {
	Cwd        string
	ExtraArgs  []string
	Env        map[string]string
	TimeoutSec int
}

// ApplyResult is what the engine returns per stack after apply.
type ApplyResult struct {
	Counts     summary.Counts
	Output     string // raw apply output (redacted upstream)
	Error      string // non-empty if apply failed
	DurationMS int64
}

// Applier runs apply for a single stack.
type Applier interface {
	Apply(ctx context.Context, stack discovery.Stack, opts ApplyOpts) (ApplyResult, error)
}
