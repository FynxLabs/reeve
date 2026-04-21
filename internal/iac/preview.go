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
	PlanSummary string // human short summary
	FullPlan    string // raw preview output (stdout), redacted upstream
	Error       string // non-empty if preview failed for this stack
}

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
