# IaC — engine registry delta

## ADDED Requirements

### Requirement: The engine interface is the full adapter contract

`iac.Engine` SHALL be the complete contract an adapter satisfies: `Name()`
(display only — core never branches on it), `Capabilities()`, and the
operational surface `EnumerateStacks`, `Preview`, `Apply`, `DriftCheck`,
composed from narrow per-operation interfaces (`Enumerator`, `Previewer`,
`Applier`, `DriftChecker`) so consumers that need less can depend on less.
Shared option and result types (`PreviewOpts`, `PreviewResult` including the
drifted-resource carrier, `ApplyOpts`, `ApplyResult`) SHALL live in
`internal/iac`, engine-neutral; engine-specific parsing stays in the adapter
package. `internal/iac` SHALL NOT import engine SDKs or shell out itself.

#### Scenario: Adapters satisfy the contract at compile time

- **WHEN** an adapter package (e.g. `internal/iac/pulumi`) builds
- **THEN** a compile-time assertion (`var _ iac.Engine = (*Engine)(nil)`)
  proves it implements the full contract

#### Scenario: Consumers stay engine-agnostic

- **WHEN** a core package (run pipeline, drift runner) needs engine
  operations
- **THEN** it depends on `iac` interface types and shared result types only,
  and the concrete engine is injected by command wiring

### Requirement: Engines self-register; the factory resolves by config type

Concrete engines SHALL live in their own packages under `internal/iac/<engine>/`
and register a constructor via `iac.Register(type, ctor)` in `init()`. The
factory `iac.New(engineCfg)` SHALL resolve purely by the config `engine.type`
string and SHALL NOT statically import concrete engines (modularity
contract). A default set is compiled in via blank imports
(`internal/iac/all`); a build can import a subset instead.

#### Scenario: Unknown engine type fails loudly

- **WHEN** a config declares an `engine.type` that no compiled-in engine
  registered
- **THEN** resolution fails with an error naming the unknown type and the
  registered types, and `reeve lint` fails on the same condition

#### Scenario: Registration is unique

- **WHEN** two packages register the same engine type
- **THEN** the second registration panics at startup, surfacing the wiring
  bug immediately

### Requirement: Single-engine routing picks the first engine config

Until multi-engine routing lands, commands SHALL resolve the first engine
config (`cfg.Engines[0]`, load order) through the factory. `reeve stacks
discover --engine <type>` SHALL resolve the requested type through the same
factory, preferring the matching engine config file's settings when present.

#### Scenario: Existing single-engine repos are unaffected

- **WHEN** a repo declares one engine config with `engine.type: pulumi`
- **THEN** every command behaves exactly as before the registry existed
