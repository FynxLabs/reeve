# IaC Engine Interface

Seeded from DESIGN.md §4, §6.3.

## Contract

Every adapter implements:

- `Name() string` — display only. Core **never branches** on this.
- `Capabilities() Capabilities` — for capability detection.
- `EnumerateStacks(ctx, root) → []Stack`
- `ValidateStack(ctx, stack) → error`
- `Preview(ctx, stack, opts) → Plan`
- `Apply(ctx, stack, plan) → ApplyResult`
- `Refresh(ctx, stack) → RefreshResult` (if `Capabilities.SupportsRefresh`)

## Capabilities

```go
type Capabilities struct {
    SupportsSavedPlans   bool
    SupportsRefresh      bool
    SupportsPolicyNative bool
    SecretsProviderTypes []string
    PreviewOutputFormat  Format
}
```

Extended as new engines reveal needs. Adding a capability is a spec change.

## Adding a new engine

1. Implement the interface in `internal/iac/<engine>/`.
2. Register in the engine factory (keyed by `engine.type` in config).
3. Ship.

Target: ~500–1000 lines of Go per engine. No changes to `internal/core/`,
no CLI changes, no config loader changes.
