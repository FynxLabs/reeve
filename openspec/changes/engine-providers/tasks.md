# Engine providers — tasks

## E1 — engine registry (refactor, zero behavior change)

- [x] Extract the de-facto engine interface from the call sites:
      `iac.Engine` composes Name/Capabilities with Enumerator, Previewer,
      Applier, DriftChecker; shared option/result types
      (`PreviewOpts`/`PreviewResult` incl. `DriftedURNs`,
      `ApplyOpts`/`ApplyResult`) live in `internal/iac`.
- [x] Self-registering factory: `iac.Register(type, ctor)` +
      `iac.New(engineCfg)` resolving purely by `engine.type`; unknown type
      errors listing registered engines; pulumi registers in `init()`;
      `internal/iac/all` blank-import manifest imported by `cmd/reeve`.
- [x] Update the six call sites (`cmd/reeve/{apply,run,lint,drift,stacks}.go`)
      to resolve via the registry; first-engine-wins routing preserved;
      `reeve lint` fails when any `engine.type` doesn't resolve.
- [x] Pulumi `Capabilities()` accurate as the reference implementation
      (saved plans, refresh, native policy, secrets-provider types).
- [x] Tests: registry register/resolve/unknown-type/duplicate-panic;
      compile-time `var _ iac.Engine = (*pulumi.Engine)(nil)`; existing suite
      green unmodified.
- [x] Docs: configuration.md engine section notes that `engine.type` selects
      a registered engine.

## E2 — Terraform adapter (`engine.type: terraform`)

- [ ] Lifecycle: `init -input=false` → `plan -out=<file> -detailed-exitcode
      -json` / `show -json` → apply the saved plan file (plan-what-you-apply
      parity; `SupportsSavedPlans: true`).
- [ ] Stack model: root-module dir = project, `terraform workspace` = stack;
      dir-per-env layouts enumerate as `project/default`; explicit env-dir /
      var-file mapping in engine config; declared stacks work without init.
- [ ] Plan JSON → `PreviewResult`: op counts, property-level diffs from
      `resource_changes`, sensitive-value masking, drifted addresses
      (fingerprinting + rendering neutral to URN-vs-address).
- [ ] Drift: `plan -refresh-only -detailed-exitcode` (exit 2 = drift), fail
      closed on unparseable JSON.
- [ ] `stacks discover`: scan for root modules (dirs with .tf +
      terraform/backend block, excluding `modules/`).
- [ ] Auth via existing env-var credential bindings — no new auth surface.
- [ ] Golden fixtures from `terraform show -json` for parse tests.
- [ ] Example: `examples/toy-stack-terraform` (random/null provider, local
      backend — no cloud creds).

## E3 — OpenTofu (`engine.type: tofu`)

- [ ] Same adapter parameterized (binary name, display name, capability
      deltas as they diverge); registers both types.
- [ ] `reeve init` offers pulumi/terraform/tofu for real (drops
      "coming soon").
- [ ] Capabilities additions as needed (saved plans, refresh-only drift,
      workspace model) — each is a spec change.
- [ ] Archive this change: fold the delta into `openspec/specs/iac/spec.md`
      on merge.
