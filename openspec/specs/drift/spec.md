# Drift Detection

## Third run mode

Drift is a run mode alongside preview and apply — not a separate subsystem.
Reuses stack discovery, auth bindings, engine abstraction, bucket storage,
observability.

## CLI surface

Normalized verb/noun grammar: `run | status | report` are verbs at `drift`
level; `suppress` is a command group.

```
reeve drift run [--pattern ... | --schedule <name> | --if-stale]
reeve drift status [--since 24h] [--stack prod/api]
reeve drift report [--format markdown|slack|json]
reeve drift suppress add <stack>
reeve drift suppress list
reeve drift suppress clear <stack>
```

## Run pipeline

1. Scheduled trigger invokes `reeve drift run`.
2. Stack enumeration via shared discovery pipeline (no PR context).
3. Scoping: include/exclude patterns, named schedule, `--if-stale`.
4. Auth binding resolution honors `mode: drift` overrides.
5. Engine drift check per stack:
   - Pulumi: `preview --expect-no-changes`.
   - Terraform/OpenTofu: `plan -detailed-exitcode`.
   Refresh before check is default-on for drift.
6. Classify each stack: `no_drift | drift_detected | error | skipped_fresh`.
7. Compare against `drift/state/{project}/{stack}.json` to emit events:
   - `drift_detected` — first time drift appears.
   - `drift_ongoing` — persistent drift (usually silent, queryable).
   - `drift_resolved` — previously drifted stack is clean.
   - `check_failed` — run-level error.
8. Write artifacts under `drift/runs/{run-id}/`; update state files.
9. Sinks filter events per their `on:` rules, transform to their payload,
   deliver.
10. Report always rendered to `$GITHUB_STEP_SUMMARY` (free, zero-config).

## State bootstrap

`state_bootstrap.mode` options:
- `baseline` — first run is silent, establishes baseline.
- `alert_all` — first run emits `drift_detected` for every drifted stack.
- `require_manual` — refuse to run without `reeve drift bootstrap` command.

**Default for `prod/*` scopes is `require_manual`**, not `baseline`, to close
the "attacker deletes state file → baseline resets → alerts suppressed" gap.
`baseline_max_age` applies only to explicit `baseline` mode.

## Freshness

Before running a stack check:
- No prior state file → run (first time).
- `last_successful_check_at` older than window → run.
- Previous run errored and `respect_failures: true` → run (retry).
- Stack has active drift → **always run** (to detect resolution).
- Otherwise → skip, log `skipped_fresh`.

## Scoping

Three composable strategies:
- Pattern sharding (`--pattern`).
- Named schedules from config (`--schedule <name>`).
- Skip-if-fresh (`--if-stale` or config).

## Overlap with open PRs

When drift is detected on a stack with open PRs touching its paths, the
report prominently surfaces them. Raw event payloads include
`overlapping_prs: [{number, opened_at, author, paths}]`. Requires VCS
`ListOpenPRsTouchingPaths` (defined in `openspec/specs/vcs`).

## Error taxonomy for retries

`retry_on_transient_error` retries only:
- Network errors reaching the engine or cloud SDK.
- Auth-expired errors (rebind and retry once).

Not retried: engine crashes, plan-parse errors, policy failures.

## OTEL metrics

Drift-specific (stack-label cardinality gated per observability spec):
- `reeve.drift.detections.total{stack, env, outcome}` — counter
- `reeve.drift.duration{stack, env}` — histogram
- `reeve.drift.stacks_in_drift{env}` — gauge
- `reeve.drift.ongoing_duration{stack}` — gauge
- `reeve.drift.runs.total{outcome}` — counter
