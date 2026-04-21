# reeve — Design Doc (Retired Seed)

> **Status:** retired as of Phase 0. This document seeded `openspec/specs/`.
> Authoritative per-capability behavior now lives in
> [`openspec/specs/`](../../openspec/specs/). This file is kept for the
> vision/principles and for traceability from seeded specs — it is
> explicitly **not** maintained in parallel with the specs (per its own §10.5).

> A PR-native, self-hosted GitOps orchestrator for Pulumi (and later, other IaC engines). No control plane, no vendor backend, no telemetry, no account — the user owns everything.

**Named after the medieval reeve:** an official empowered to enforce rules and manage an estate on behalf of those who own it. Fitting for a tool whose entire job is to enforce approval policy, manage locks, and act on infrastructure on behalf of the team — while owning none of it.

## 0. Anti-Positioning

What reeve **is not** and will **never become**:

- Not a SaaS. No hosted offering, ever.
- No "free tier with optional backend." No backend exists.
- No telemetry. No phone-home. No usage analytics. Not opt-in, not opt-out — the code does not contain the feature.
- No account, login, or registration. reeve never authenticates *to itself* because there is no self to authenticate to.
- No bait-and-switch license pivot. MIT, full stop. If a fork happens later, the original stays MIT under its existing maintainers.

This positioning is deliberate. Existing tools in this space (Digger, Terrateam, Atlantis to a lesser extent) have all either shipped with undisclosed control planes, pivoted to SaaS-gated features, or relicensed under their own pressure. reeve exists to be the option that *structurally cannot* do any of that, because there is nothing to pivot.

---

## 1. Motivation

Existing Terraform-oriented orchestrators (Atlantis, Digger, Terrateam) all advertise Pulumi support, but their architectures are Terraform-shaped: HCL parsing, plan-text regex, single-language assumptions, workspace models that don't match Pulumi's project/stack model. Fixes to Pulumi support tend to stall upstream.

Beyond that, every existing tool requires *some* control plane — a server, a SaaS backend, or at minimum a long-running orchestrator. For teams that care about zero-trust, vendor independence, and keeping credentials inside their own CI environment, there is no clean answer.

`reeve` is the answer: a single binary that runs inside the user's CI, coordinates through blob storage the user owns, talks to the VCS through its own API, and has no upstream service of any kind.

---

## 2. Core Principles

1. **No control plane.** No server, no database, no SaaS backend. The tool is a CLI/Action that runs inside user CI.
2. **User owns all state.** Locks, plans, run artifacts, and audit logs live in the user's S3/GCS/Azure Blob/R2 bucket. The tool never sees the data.
3. **Zero-trust auth.** Short-lived federated credentials only (WIF, OIDC→AWS role assumption, Azure federated creds). No stored long-lived secrets. The tool consumes these creds; it does not configure them for the user.
4. **Pure core, effectful edges.** All logic (stack discovery, rule resolution, lock transitions, comment rendering, precondition checks) is pure functions over plain data. Effects (blob I/O, VCS API, IaC CLI, clock) live behind interfaces.
5. **Local-first testing.** Every CI behavior is reproducible on a laptop in seconds, not minutes.
6. **Modular.** IaC engine, VCS, blob backend, notifications, observability, and auth are all pluggable. Pulumi + GitHub + (S3/GCS/Azure) + Slack first; others later.
7. **Explicit over clever.** When a rule fires, the user can ask "why?" and get a clear trace.
8. **CLI/config parity.** Every runtime behavior has both a CLI flag and a config setting. Flags are for experimentation ("test variations without editing files"); config is for commitment ("save typing, make it reproducible"). CLI overrides config; config overrides defaults. No config-only behaviors (an operator at 3am must be able to override from CLI). No flag-only behaviors except for genuinely ephemeral things (`--dry-run`, `--verbose`, `--explain`). Local preferences live at `~/.config/reeve/*.yaml` (separate `config_type: user`) and never in repo config.

---

## 3. High-Level Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        CI Runner (GH Actions)                    │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │                       reeve (binary)                       │  │
│  │                                                            │  │
│  │   ┌──────────────────────────────────────────────────┐    │  │
│  │   │                   Pure Core                      │    │  │
│  │   │  - stack discovery    - rule resolver            │    │  │
│  │   │  - lock state machine - precondition evaluator   │    │  │
│  │   │  - comment renderer   - summary builder          │    │  │
│  │   └──────────────────────────────────────────────────┘    │  │
│  │                         │                                  │  │
│  │   ┌─────────┬─────────┬─────────┬─────────┬─────────┬──────┐│  │
│  │   ▼         ▼         ▼         ▼         ▼         ▼      ▼│  │
│  │ IaC      VCS       Blob     Notify     Obs       Auth   Clock│  │
│  │(Pulumi)(GitHub) (S3/GCS)   (Slack)   (OTEL)    (WIF)        │  │
│  └────│────────│────────│────────│──────────│─────────│────────┘  │
└──────│────────│────────│────────│──────────│─────────│──────────-┘
       │        │        │        │          │         │
       ▼        ▼        ▼        ▼          ▼         ▼
   pulumi   GitHub    user's   Slack      user's    Cloud IAM
    CLI      API     bucket     API       OTEL     (federated)
                                         collector
```

Every arrow out of the runner is the user's own trust boundary. The tool holds nothing.

---

## 4. Module Boundaries

| Module | Responsibility | First impl | Pluggable for |
|---|---|---|---|
| **IaC** | Run preview/apply, parse output, save plans | Pulumi | Terraform, OpenTofu |
| **VCS** | PR comments, review state, CODEOWNERS, checks, team lookup | GitHub | GitLab, Codeberg, Bitbucket |
| **Blob** | Locks, run artifacts, audit log | S3 / GCS / Azure Blob / R2 | — |
| **Notifications** | Human-readable status to channels (PR-scoped) | Slack | Mattermost, Teams, webhook, PagerDuty |
| **Drift Sinks** | Drift event delivery (incident systems, alerting) | Slack, webhook, PagerDuty, OTEL annotation, GitHub issue | incident.io preset, Rootly preset, Opsgenie |
| **Observability** | Machine-readable signals (traces, metrics, annotations) | OpenTelemetry (OTLP) | Grafana annotations, Datadog events, webhook |
| **Auth** | Federated credential acquisition | GCP WIF, AWS OIDC | Azure federated, Vault |
| **Core** | Rules, state machines, rendering | — | — |

PR comments live in the **VCS module**, not Notifications. They are intrinsic to the VCS and carry data (full plan, collapsibles, rule status) that no external channel receives.

---

## 5. Feature Scope (v1)

### 5.1 PR Flow

1. PR opened or updated → reeve runs **preview** for all stacks touched
2. Single PR comment posted, edited in place on subsequent runs
3. Slack message posted/updated in parallel (if configured)
4. Reviewers approve per configured rules
5. On `/reeve apply` comment (or merge, depending on config) → reeve acquires locks, runs **apply**
6. Results update the PR comment and Slack message
7. Audit log entry written to bucket
8. Locks released, queue advanced

### 5.2 Locking

- **Granularity:** per-stack
- **Acquired at apply, not preview.** Previews are parallel-safe.
- **Storage:** single JSON object per stack in bucket, conditional writes (S3 `If-Match`, GCS generation preconditions) for atomicity
- **Queue:** FIFO, visible in PR comment and via `reeve locks list`
- **TTL:** configurable, default 4h, reaper releases stale locks
- **Release triggers:** PR merged, PR closed, TTL expiry, manual `/reeve unlock` (admin only)
- **Cross-PR visibility:** "you're blocked by PR #X" surfaced in comments on both sides

### 5.3 Approval Rules

Layered, not either/or:

```yaml
approvals:
  default:
    required_approvals: 1
    approvers: "@org/infra-reviewers"
    codeowners: true
  stacks:
    "prod/*":
      required_approvals: 2
      approvers: ["@org/sre", "@org/security"]
      require_all_groups: true
    "prod/payments":
      approvers: ["@org/payments-leads"]
      break_glass:
        allowed: ["@alice", "@bob"]
        requires_incident_link: true
```

- Default baseline applies to all stacks
- Per-stack rules merge with default (union approvers, more specific overrides numeric fields)
- `require_all_groups` means one approval from each listed group, not N-of-any
- CODEOWNERS integration optional, honored alongside team rules
- Stale reviews dismissed on new commits (configurable)
- `reeve rules explain <stack>` shows full resolution trace

### 5.4 Apply Preconditions

Evaluated in order, fail fast, shown in PR comment:

1. Branch up-to-date with base
2. Required checks green
3. Fresh preview exists for current HEAD SHA (within freshness window)
4. Preview succeeded (no errors)
5. Policy passed (CrossGuard / OPA / Conftest)
6. Approvals satisfied for this specific stack
7. Lock acquirable
8. Not in freeze window (if configured)

### 5.5 PR Comment Layout

One comment per run, edited in place. Identified by hidden HTML marker.

```
## 🟢 reeve · preview · run #47 · [commit abc1234]

**3 stacks changed** · ⏱ 42s · [View run](ci-link)

| Stack | Env | ➕ Add | 🔄 Change | ➖ Delete | 🔁 Replace | Status |
|---|---|---|---|---|---|---|
| api     | prod    | 2 | 1 | 0 | 0 | 🔒 locked by #482 |
| worker  | prod    | 0 | 3 | 0 | 1 | ✅ ready |
| api     | staging | 5 | 0 | 0 | 0 | ✅ ready |

⚠️ Replacements detected in worker/prod — review carefully.

---

### api · prod · 🔒 blocked
  Queued behind #482.

  <details><summary>Summary (2 add, 1 change)</summary>
    (resource-level changes parsed from pulumi preview --json)
  </details>

  <details><summary>Full plan output</summary>
    (raw pulumi preview output)
  </details>

---

### worker · prod · ✅ ready to apply
  ...

---

🔐 api/prod apply gates:
  ✅ up-to-date with main
  ✅ checks passing
  ✅ preview fresh (3m ago)
  ✅ policy: 12/12 passed
  ❌ approvals: 1/2 (need @org/sre)
  ⏸ lock: held by #482
```

- Status-grouped sort: blocked first, then ready, then no-op
- No-op stacks collapse to a single line
- Apply comment mirrors preview structure, adds durations, floats failures to top
- Secrets marked by Pulumi are redacted

### 5.6 Slack Notifications

- **Runs last in the pipeline** — authoritative "what happened" surface, captures upstream failures accurately
- **One message per PR**, tracked by message ID in bucket
- **Main message:** high-level status (planned → applying → applied/failed/closed-unmerged)
- **Thread:** per-stack summaries (counts + high-level changed resources; never full plan)
- **Block Kit** for layout
- **Rule-gated:** e.g. `environment: prod` only
- **Always links back to the PR**
- **Module designed for:** Mattermost, Rocket.Chat, Teams, webhook later

### 5.7 Observability (OpenTelemetry)

Opt-in, off by default. When enabled, reeve emits structural telemetry to the user's OTEL collector. reeve never hosts or sees this data — it goes to whatever endpoint the user configured.

**Signal model:**

- **One trace per run.** `preview` and `apply` are separate runs, separate traces. PR number and commit SHA are span attributes on the root span, not a hierarchical parent. This keeps queries simple while supporting both patterns — "all runs for PR #482" (filter by `pr.number`) and "latest apply for stack X" (filter by `stack.name` + `op=apply`, order by time).

- **Per-stack spans.** Within a run, each stack is its own span, and operations within a stack (preview, policy-eval, lock-acquire, comment-render) are children. Per-stack metrics and annotations emit with stack labels so teams can reason about individual stacks over time.

- **Metrics** (examples, not exhaustive):
  - `reeve.runs.total` (counter, labeled by op, outcome)
  - `reeve.stack.duration` (histogram, labeled by project, stack, env, op)
  - `reeve.lock.wait_duration` (histogram)
  - `reeve.lock.queue_depth` (gauge)
  - `reeve.policy.violations` (counter, labeled by policy_name)
  - `reeve.preconditions.failed` (counter, labeled by gate)
  - `reeve.approvals.time_to_approval` (histogram)
  - `reeve.stack.changes` (counter, labeled by type: add/change/delete/replace)

- **Annotations** (via secondary emitters, not core OTEL): apply start and apply completion events posted to systems that support annotation APIs (Grafana, Datadog, Dash0, generic webhook). Per-stack granularity. Labeled with project, stack, env, PR number, commit SHA, outcome.

**Redaction discipline:**

What goes into telemetry:
- Counts (adds / changes / deletes / replaces)
- Durations and timestamps
- Stack name, project name, environment
- PR number, commit SHA, run ID
- Outcome (success / failed / blocked)
- Gate names for failed preconditions
- Policy names for violations

What NEVER goes into telemetry:
- Full plan output or resource diffs
- Resource names or values that could leak structure or secrets
- Approver identity in ways that create privacy issues
- Any value Pulumi marked `[secret]`

Same redaction principle as PR comments, extended to telemetry.

**Configuration follows OTEL conventions.** Standard env vars (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`) work out of the box. Config file can override. Resource attributes include a default `service.name=reeve` plus user-configured attributes (team, repo, etc.).

**Annotation emitters** are a thin layer on top: for each apply event, post to configured annotation endpoints in parallel, each with its own event-type filter. A Grafana annotation is an HTTP POST; same shape for Datadog, Dash0, webhook. These don't replace OTEL — they complement it for systems where explicit annotation events are more useful than trace-derived markers.

### 5.8 Bucket Layout

```
s3://user-bucket/reeve/
├── locks/
│   └── {project}/{stack}.json
├── runs/
│   └── pr-{number}/
│       ├── {run-id}/
│       │   ├── manifest.json
│       │   ├── {project}-{stack}/
│       │   │   ├── preview.json
│       │   │   ├── plan.bin              # pulumi --save-plan output
│       │   │   ├── summary.json
│       │   │   └── stdout.log
│       │   └── ...
│       └── latest -> {run-id}
├── drift/
│   ├── runs/
│   │   └── {run-id}/
│   │       ├── manifest.json             # schedule, scope, config snapshot
│   │       ├── results/
│   │       │   └── {project}-{stack}.json
│   │       └── report.md                 # rendered human-readable report
│   ├── state/
│   │   └── {project}/{stack}.json        # last-seen, fingerprint, ongoing_since
│   └── suppressions/
│       └── {project}/{stack}.json        # active suppressions with TTL
├── notifications/
│   └── pr-{number}/slack.json            # message IDs, channel IDs
└── audit/
    └── {year}/{month}/{day}/{run-id}.json
```

- Run artifacts persist for PR lifetime
- On PR close/merge, artifacts move to `closed/` prefix with shorter retention
- Audit log is write-once, long retention, separate from runs
- Apply uses saved plan from most recent preview on current SHA
- Drift state drives event classification (new / ongoing / resolved) across runs
- Blob backend interface has `filesystem://` implementation for local testing

### 5.9 Drift Detection

Drift detection is a **third run mode alongside preview and apply**, not a separate subsystem. It reuses stack discovery, auth bindings, engine abstraction, bucket storage, and observability. Different trigger (scheduled), different output surface (report + sinks), different urgency model (alerts, not reviews).

**CLI surface:**

```
reeve drift run                     # execute a drift check
reeve drift run --pattern "prod/*"  # subset
reeve drift run --schedule daily    # run a named schedule from config
reeve drift run --if-stale          # skip stacks checked within freshness window

reeve drift status                  # read last run results from bucket
reeve drift status --since 24h
reeve drift status --stack prod/api

reeve drift report                  # render report (Glamour for local, or format target)
reeve drift report --format slack

reeve drift suppress <stack>        # create time-bounded suppression
reeve drift suppress list
reeve drift suppress clear <stack>
```

**Run model:**

1. Scheduled trigger (GH Actions cron, equivalent on other CIs) invokes `reeve drift run`
2. Stack enumeration uses the same discovery pipeline with no PR context
3. Scoping rules apply (include/exclude patterns, named schedule filter, freshness skip)
4. Auth binding resolution honors `mode: drift` if specified, otherwise defaults
5. Engine runs drift check per stack (`preview --expect-no-changes` for Pulumi; `plan -detailed-exitcode` for Terraform); refresh before check is default-on for drift
6. Each stack classified: `no_drift`, `drift_detected`, `error`, `skipped_fresh`
7. Results compared against `drift/state/{project}/{stack}.json` to emit lifecycle events:
   - `drift_detected` — first time drift appears on this stack
   - `drift_ongoing` — persistent drift from prior run (usually silent, queryable)
   - `drift_resolved` — previously drifted stack is clean now
   - `check_failed` — run-level errors (auth, network, engine crash)
8. Artifacts written to `drift/runs/{run-id}/`; state files updated
9. Sinks filter events per their `on:` rules, transform to their payload shape, deliver
10. Report rendered to `$GITHUB_STEP_SUMMARY` (always); exit code per config

**Event lifecycle is load-bearing.** Without it, alerting either spams every run (drift persists) or fires once and goes stale. Fingerprint is a hash of the drifted-resource set keyed by `(stack, fingerprint)`.

**Freshness model:**

For each stack in scope, before running the check:
- No prior state file → run (first time)
- `last_successful_check_at` older than window → run
- Previous run errored and `respect_failures: true` → run (retry)
- Stack has active drift → **always run** (so we can detect resolution)
- Otherwise → skip, log as `skipped_fresh`

Freshness piggybacks on state files we need anyway for event classification. Reporting surfaces skipped counts transparently.

**Scoping strategies — all three supported, composable:**

- **Sharding via patterns** (always available): `reeve drift run --pattern "prod/*"` in separate scheduled workflows
- **Named schedules** (config-declared): arbitrary user-named filter sets (`hourly`, `prod`, `legacy`, whatever), referenced by `--schedule <name>`
- **Skip-if-fresh** (config or `--if-stale`): deduplicate based on last successful check

Users mix as appropriate for their scale. Small teams use none; large monorepos use all three.

**Drift-specific OTEL metrics:**

- `reeve.drift.detections.total{stack, env, outcome}` — counter
- `reeve.drift.duration{stack, env}` — histogram
- `reeve.drift.stacks_in_drift{env}` — gauge (useful for dashboards: "how many stacks currently drifted")
- `reeve.drift.ongoing_duration{stack}` — gauge (how long drifted; powers alerts on persistent drift)
- `reeve.drift.runs.total{outcome}` — counter (run-level success/failure)

The `ongoing_duration` metric enables alerting like "page me when any prod stack has been drifted for more than 24 hours" in the monitoring system, rather than in reeve itself.

**Overlapping open PRs:**

When drift is detected on a stack that has open PRs touching its paths, the drift report surfaces them prominently. Long-lived IaC PRs over drifted stacks are a compounding risk — the plan reviewers approved a week ago no longer matches reality. The report shows:

- Which drifted stacks have open PRs touching them
- When those PRs were opened ("opened 9 days ago")
- PR numbers and authors

This information also flows into Slack dashboards and any sink that subscribes to drift events (raw event payloads include `overlapping_prs: [{number, opened_at, author, paths}]`). Incident tooling can use this to escalate appropriately.

This requires VCS module support for "list open PRs touching these paths" — a method on the VCS interface (`ListOpenPRsTouchingPaths`). Available in GitHub and GitLab APIs.

**GitHub Actions integration:**

Drift reports are always written to `$GITHUB_STEP_SUMMARY`. Free, zero-config, useful, consistent with apply/preview step summaries. The same rendered markdown that would be sent to a file-based sink.

**Sink types for v1:**

- `slack` — reuses notifications module, dashboard-style single message per run with threaded per-stack details
- `webhook` — generic HTTP POST with payload templating, named presets (`incident_io`, `rootly`, `opsgenie`, `raw`)
- `pagerduty` — Events API v2, change events or triggers configurable
- `otel_annotation` — annotation via observability module
- `github_issue` — open/update GitHub issue per drifted stack (for teams without dedicated incident tooling)

---

## 6. Module Architecture

### 6.1 Package Layout

Flat layout, one directory per module boundary:

```
cmd/
└── reeve/                    # CLI entry point (Cobra)

internal/
├── core/                     # pure logic: rules, state machines, pipelines
│   ├── approvals/            # approval rule resolution, dismissal tracking
│   ├── discovery/            # pattern matching, filtering, change mapping
│   ├── locks/                # lock state machine (no I/O)
│   ├── preconditions/        # apply gate evaluation
│   ├── render/               # PR comment rendering (pure string → string)
│   └── summary/              # structured plan/apply summary builders
│
├── iac/                      # IaC engine interfaces + adapters
│   ├── engine.go             # shared types, capability detection
│   └── pulumi/               # Pulumi adapter (implements interfaces)
│
├── vcs/                      # VCS interfaces + adapters
│   ├── vcs.go                # shared types
│   ├── codeowners/           # format-agnostic CODEOWNERS result types
│   └── github/               # GitHub adapter (implements interfaces, parses CODEOWNERS)
│
├── blob/                     # blob storage interfaces + adapters
│   ├── blob.go
│   ├── s3/
│   ├── gcs/
│   ├── azblob/
│   └── filesystem/           # for local testing
│
├── auth/                     # credential providers
│   ├── auth.go               # provider interface, binding resolution
│   └── providers/            # one subpackage per provider type
│
├── slack/                    # shared Slack client + Block Kit primitives
│   ├── client.go
│   └── blocks.go
│
├── notifications/            # PR-scoped human notifications
│   └── slack_templates.go    # PR-specific templates using slack primitives
│
├── drift/                    # drift detection + sinks
│   ├── runner.go
│   ├── state.go              # state file lifecycle + event classification
│   └── sinks/                # one subpackage per sink type
│       ├── slack/            # drift-specific Slack templates
│       ├── webhook/
│       ├── pagerduty/
│       ├── otel/
│       └── github_issue/
│
├── observability/            # OTEL + annotation emitters
│   ├── otel/
│   └── annotations/          # grafana, dash0, datadog, webhook
│
├── config/                   # YAML loading, schema validation, versioning
│   ├── loader.go
│   └── schemas/              # one schema per config_type
│
└── run/                      # orchestration: wires modules together
    ├── preview.go
    ├── apply.go
    └── drift.go
```

**Why this layout:**
- Flat modules keep imports shallow and discoverable
- `internal/core/` is the pure-logic island — no I/O, no external packages, testable with plain data
- Adapters live one level down from their interface parent (e.g. `internal/iac/pulumi/` implements `internal/iac/` interfaces)
- `internal/run/` is the orchestration layer that composes everything — the only place that imports from every other module
- `internal/slack/` is deliberately shared infra; `notifications` and `drift/sinks/slack` both consume it

### 6.2 Interface Design

**Small interfaces at use sites** (Go idiom). Each consumer defines the minimal interface it needs:

```go
// internal/core/approvals/resolver.go
type reviewLister interface {
    ListReviews(ctx context.Context, prNumber int) ([]Review, error)
}

type teamResolver interface {
    ResolveTeam(ctx context.Context, slug string) ([]string, error)
}

func Resolve(r reviewLister, t teamResolver, pr PR, rules Rules) Resolution {
    // ...
}
```

Each adapter package documents the **full set of interfaces it satisfies** in its package doc:

```go
// Package github implements the VCS adapter for GitHub.
//
// *Client satisfies these interfaces (see each package for details):
//   - vcs.PRReader
//   - vcs.CommentPoster
//   - approvals.ReviewLister
//   - approvals.TeamResolver
//   - apply.ChecksReader
//   - codeowners.Provider
//   - codeowners.Parser
package github
```

This gives contributors a single place to see the full contract without forcing every consumer to take a giant interface.

**Mocking for tests** is trivial: each test defines its own fake satisfying only the small interfaces the function under test needs. No mock generators required.

### 6.3 Engine Agnosticism

**Core never branches on engine name.** Capability detection only:

```go
// OK:
if run.Engine.Capabilities().SupportsSavedPlans() {
    run.Engine.SavePlan(ctx, plan)
}

// NOT OK:
if run.Engine.Name() == "pulumi" { ... }
```

Engine name is exposed (`Name()`) for display purposes — logs, reports, error messages — never for conditional logic.

**Capability types:**
```go
type Capabilities struct {
    SupportsSavedPlans        bool   // Pulumi yes, Terraform yes, OpenTofu yes
    SupportsRefresh           bool   // all yes
    SupportsPolicyNative      bool   // Pulumi CrossGuard = yes; others = no (use policy_hooks)
    SecretsProviderTypes      []string  // which secrets providers this engine supports
    PreviewOutputFormat       Format    // json, hcl-plan, etc.
    // ... extended as new engines reveal needs
}
```

**Adding a new engine:**
1. Implement `iac.Engine` interface in `internal/iac/<engine>/`
2. Implement `EnumerateStacks`, `Preview`, `Apply`, `Refresh`, `Capabilities`
3. Register in the engine factory (reads `engine.type` from config)
4. Ship

Target: ~500-1000 lines of Go per engine. No changes to `internal/core/`, no CLI changes, no config loader changes.

### 6.4 Stack Discovery Split

```
Engine owns:
  - EnumerateStacks(ctx, root) → flat list of (project, path, name, raw)
  - ValidateStack(ctx, stack)  → does this stack actually resolve?

Core owns:
  - Pattern matching (glob + re: prefix for regex)
  - Include/exclude filtering
  - Change mapping (which stacks affected by file changes)
  - Module dependency resolution
  - The pipeline running all of the above
  - `reeve stacks` and `reeve <engine> find` CLI logic
```

Engine adapter is thin — "speak this engine's format, return a list." Core owns the generic logic that behaves identically across all engines.

Pattern-generation for the `find` command is in core. A future `reeve terraform find` reuses the same pattern clustering — no per-engine reinvention.

### 6.5 VCS Abstraction

Designed for GitHub first, with moderate future-proofing for GitLab. Interfaces abstract enough to extend; we don't pressure-test against GitLab until the GitLab adapter lands.

**Key interfaces:**

```go
// internal/vcs/vcs.go
type PRReader interface {
    GetPR(ctx context.Context, number int) (*PR, error)
    ListChangedFiles(ctx context.Context, number int) ([]string, error)
    ListOpenPRsTouchingPaths(ctx context.Context, paths []string) ([]PR, error)  // for drift
}

type CommentPoster interface {
    UpsertComment(ctx context.Context, number int, body string, marker string) error
    Capabilities() CommentCapabilities
}

type CommentCapabilities struct {
    SupportsEdit bool   // GitHub + GitLab = true; if false, append fallback
}

// codeowners parsing owned by VCS module — format varies by platform
type CodeownersProvider interface {
    ResolveOwners(ctx context.Context, changedFiles []string) (map[string][]string, error)
}
```

**Approvals as pluggable sources:**

```go
// internal/core/approvals/source.go
type ApprovalSource interface {
    Name() string
    ListApprovals(ctx context.Context, pr PR) ([]Approval, error)
}
```

v1 ships with:
- `pr_review` source (default; reads PR reviews from VCS)
- `pr_comment` source (opt-in; parses `/reeve approve` in comments)

Future (v2+): `slack_interaction`, `webhook`. Each is a new source implementation — no core changes.

### 6.6 Policy Hook Model

Generic policy hook mechanism. Reeve does not integrate any specific policy system natively — users wire up OPA/Conftest/CrossGuard/custom via the hook.

```yaml
# in engine config (e.g. pulumi.yaml)
engine:
  type: pulumi
  policy_hooks:
    - name: opa-compliance
      command: ["conftest", "test", "--policy", "policies/", "{{plan_json}}"]
      on_fail: block              # block | warn
      required: true

    - name: crossguard
      command: ["pulumi", "policy", "validate", "policies/aws-compliance"]
      on_fail: block
      required: false             # skip silently if command not present
```

Template placeholders: `{{plan_json}}`, `{{stack_name}}`, `{{project}}`, `{{env}}`.

Exit code semantics:
- `0` = pass
- non-zero + `on_fail: block` = apply gate fails
- non-zero + `on_fail: warn` = warning in PR comment, apply proceeds

Captured stdout surfaces in the PR comment under a "Policy" section.

No dedicated `config_type: policy` — hooks live in engine config (per-engine policy tooling is already engine-specific; shared OPA hooks can be templated into multiple engine configs if needed).

---

## 7. Tech Stack

- **Language:** Go (single binary, cross-platform, matches Pulumi ecosystem)
- **CLI:** Cobra
- **Config:** Viper, YAML source of truth, versioned schema (`version: 1`)
- **Schemas:** strong Go structs, strict unmarshaling, unknown keys error
- **Markdown rendering:** Charm Glamour for local/dry-run preview (stdout, never file)
- **IaC driver:** Pulumi CLI via Automation API where useful, shell-out where simpler
- **License:** MIT

---

## 8. Configuration

### 8.1 Config Layout

Config lives in a `.reeve/` directory at repo root. Each file covers one concern:

```
.reeve/
├── shared.yaml           # approvals, locking, bucket, freeze windows, comment behavior
├── auth.yaml             # credential providers and bindings
├── notifications.yaml    # slack, teams, webhook
├── observability.yaml    # otel, annotations
├── pulumi.yaml           # engine: pulumi — projects, stacks, modules, state
└── terraform.yaml        # engine: terraform (future)
```

A single-file `reeve.yaml` at repo root is supported for simple single-engine cases. If `.reeve/` exists, root-level `reeve.yaml` is ignored (ambiguity error in lint).

### 8.2 File Convention

Every config file starts with two required fields:

```yaml
version: 1
config_type: <type>
```

**`config_type` values (v1):** `shared`, `engine`, `auth`, `notifications`, `observability`, `drift`. Plus `user` for `~/.config/reeve/*.yaml` (local-only preferences, never in repo).

- Schema validation is per `config_type`; `reeve lint` verifies each file against the correct schema.
- `version` is per-file — breaking changes to any one schema bump only that file's version.
- Exactly one file per `config_type`, except `engine` (multiple engines allowed, each with unique `engine.type`).

### 8.3 Path Discovery and Stack Resolution

**Runtime behavior is always explicit.** reeve acts only on stacks that are either declared literally or match a declared pattern. There is no runtime auto-discovery mode.

Discovery pipeline, in order:

1. **Declare** — literal project entries and `pattern:` globs/regex from engine config
2. **Include** — if any include rules exist, only matching entries pass
3. **Exclude** — ignore rules drop matching entries
4. **Resolve** — engine verifies each remaining stack actually exists
5. **Map to changes** — stack is "affected" if changed files intersect its paths or declared dependencies

Regex patterns are opt-in via `re:` prefix. Everything else is doublestar glob.

**`reeve <engine> find` CLI** is a maintenance tool. It walks the repo, clusters discovered stacks by shared-prefix and stack-list, and generates suggested pattern entries. With `--write`, it mutates the engine config file (preserving comments via a structure-aware YAML library). Same command supports modules via `reeve <engine> find modules`.

The `find` command is for humans to generate config; it never affects runtime behavior.

### 8.4 shared.yaml

```yaml
version: 1
config_type: shared

bucket:
  type: s3                       # s3 | gcs | azblob | r2 | filesystem
  name: mycompany-reeve
  region: us-east-1
  prefix: reeve/                 # optional sub-prefix
  auth_provider: aws-reeve       # references a provider from auth.yaml
  retention:
    runs: 30d
    audit: 7y
    locks: indefinite

locking:
  ttl: 4h
  queue: fifo
  reaper_interval: 15m
  admin_override:
    allowed: ["@org/sre-leads"]
    requires_reason: true

approvals:
  # Pluggable sources. Order matters for tie-breaking attribution.
  sources:
    - type: pr_review             # default: VCS-native reviews (GitHub/GitLab)
      enabled: true
    - type: pr_comment            # opt-in: "/reeve approve" in PR comments
      enabled: false
      command: "/reeve approve"
    # Future (v2+):
    # - type: slack_interaction
    # - type: webhook

  default:
    required_approvals: 1
    approvers: ["@org/infra-reviewers"]
    codeowners: true
    dismiss_on_new_commit: true
    freshness: 24h
  stacks:
    "prod/*":
      required_approvals: 2
      approvers: ["@org/sre", "@org/security"]
      require_all_groups: true
    "prod/payments":
      approvers: ["@org/payments-leads"]
      break_glass:
        allowed: ["@alice", "@bob"]
        requires_incident_link: true

preconditions:
  require_up_to_date: true
  require_checks_passing: true
  preview_freshness: 2h
  preview_max_commits_behind: 5

freeze_windows:
  - name: friday-afternoon
    cron: "0 15 * * 5"
    duration: 65h
    stacks: ["prod/*"]

comments:
  sort: status_grouped           # status_grouped | alphabetical | env_priority
  collapse_threshold: 10
  show_gates: true
```

### 8.5 auth.yaml

Zero-trust by design. Short-lived federated credentials only, with a clearly-flagged escape hatch.

```yaml
version: 1
config_type: auth

providers:
  # ── Cloud federation (primary auth path) ──
  aws-prod:
    type: aws_oidc
    role_arn: arn:aws:iam::111111111111:role/reeve-prod
    session_name: reeve-${context:pr_number}
    duration: 1h
    region: us-east-1

  gcp-prod:
    type: gcp_wif
    workload_identity_provider: projects/111/locations/global/workloadIdentityPools/github/providers/reeve
    service_account: reeve-prod@prod.iam.gserviceaccount.com
    duration: 1h

  azure-prod:
    type: azure_federated
    tenant_id: ${env:AZURE_TENANT_ID}
    client_id: 44444444-4444-4444-4444-444444444444
    subscription_id: 55555555-5555-5555-5555-555555555555

  # ── Secret manager composition (sourced from cloud providers) ──
  cloudflare-token:
    type: aws_secrets_manager
    source: aws-prod
    secret_id: reeve/cloudflare/api-token
    ttl: 1h

  datadog-key:
    type: aws_ssm_parameter
    source: aws-prod
    parameter: /reeve/datadog/api-key

  # ── GitHub App token ──
  github-app:
    type: github_app
    app_id: 123456
    installation_id: 789012
    private_key: ${env:GITHUB_APP_PRIVATE_KEY}
    permissions: ["contents:read", "issues:write"]

  # ── Local development (refuses to run under CI=true) ──
  aws-local:
    type: aws_profile
    profile: mycompany-dev

  gcp-local:
    type: gcloud_adc

  # ── Read-only role for drift runs (optional, but recommended) ──
  aws-prod-readonly:
    type: aws_oidc
    role_arn: arn:aws:iam::111111111111:role/reeve-drift-readonly
    duration: 2h

  # ── Escape hatch: flagged by lint ──
  legacy-snowflake:
    type: env_passthrough
    env_vars:
      SNOWFLAKE_USER: SNOWFLAKE_USER
      SNOWFLAKE_PASSWORD: SNOWFLAKE_PASSWORD

bindings:
  # Default for preview/apply (no mode = all modes except where overridden)
  - match: { stack: "prod/*" }
    providers: [aws-prod, gcp-prod, cloudflare-token, datadog-key]

  - match: { stack: "staging/*" }
    providers: [aws-prod, gcp-prod]

  # Drift-specific binding: use a tighter, read-only role for scheduled drift checks
  - match: { stack: "prod/*", mode: drift }
    providers: [aws-prod-readonly]

  # Explicit override when a stack needs a different provider of the same scope
  - match: { stack: "prod/payments" }
    override: [aws-payments-strict]   # replaces aws-prod
    providers: [github-app]            # unions with remaining prod providers
```

**Provider types shipping in v1:**

| Category | Types |
|---|---|
| Cloud federation | `aws_oidc`, `gcp_wif`, `azure_federated` |
| Local dev (CI-refused) | `aws_profile`, `aws_sso`, `gcloud_adc` |
| Secret managers | `aws_secrets_manager`, `aws_ssm_parameter`, `gcp_secret_manager`, `azure_key_vault`, `github_secret` |
| Identity | `github_app` |
| Vault | `vault`, `vault_dynamic_secret` |
| Escape hatch | `env_passthrough` (flagged) |

**Resolution rules:**
- A stack activates the union of providers from all matching bindings, deduplicated by name.
- Bindings with a `mode:` field match only when that run mode is active (`preview`, `apply`, `drift`). Bindings without `mode:` apply to all modes.
- Each stack executes exactly once per run regardless of how many bindings match.
- Conflicting providers of the same logical scope (two AWS OIDC roles for one stack) error at lint time.
- `override:` explicitly replaces providers from more-general bindings.
- All credentials are acquired before run, discarded after.
- `duration:` defaults to 1h; lint warns above 4h.
- Local types (`aws_profile`, `gcloud_adc`, etc.) refuse to run when `CI=true` is detected; override via `--allow-local-creds-in-ci` for edge cases.

### 8.6 engine (e.g. pulumi.yaml)

```yaml
version: 1
config_type: engine

engine:
  type: pulumi                    # pulumi | terraform | opentofu (future)

  binary:
    path: pulumi
    version: "3.150.0"            # optional pin

  # State backend — reeve does not manage state, it configures the engine's
  # backend and logs in before each run using short-lived creds.
  state:
    backend: s3                   # s3 | gcs | azblob | pulumi_cloud | file
    url: s3://mycompany-pulumi-state
    auth_provider: aws-state      # from auth.yaml (separate from stack auth)

    secrets_provider:
      type: awskms                # awskms | gcpkms | azurekeyvault | hashivault | passphrase
      key: arn:aws:kms:us-east-1:111:key/abc-123-def

    stack_overrides:
      "dev/*":
        secrets_provider:
          type: passphrase
          passphrase: ${env:PULUMI_CONFIG_PASSPHRASE}

  # Stack declarations — literal or pattern-matched. Always explicit at runtime.
  stacks:
    # Literal
    - project: api
      path: projects/api
      stacks: [dev, staging, prod]

    # Pattern (glob)
    - pattern: "projects/*"
      stacks: [dev, staging, prod]

    # Regex escape hatch
    - pattern: "re:^services/[a-z]+-api$"
      stacks: [prod]

  filters:
    exclude:
      - "projects/sandbox/*"
      - "projects/experiments/*"
      - stack: "*/scratch"

  change_mapping:
    ignore_changes:
      - "**/*.md"
      - "**/docs/**"
      - "**/*.test.*"
    extra_triggers:
      - project: api
        paths: ["shared/types/**", "protos/**"]

  modules:
    roots: ["shared/", "components/", "lib/"]
    auto_discover: false          # v1: false; v1.x: opt-in true
    definitions:
      - name: shared-networking
        path: shared/networking
      - name: shared-iam
        path: shared/iam
      - name: common-tagging
        path: lib/tagging

  stack_dependencies:
    defaults:
      - project: api
        depends_on:
          modules: [shared-networking, shared-iam]
          stacks: ["{stack}/networking", "{stack}/platform"]
    overrides:
      - stack: prod/api
        depends_on:
          modules: [shared-networking, shared-iam, common-tagging]

  stack_overrides:
    "prod/payments":
      preview_args: ["--parallel", "1"]
      refresh_before_preview: true
      replacement_policy: fail_on_replace

  execution:
    max_parallel_stacks: 4
    preview_timeout: 10m
    apply_timeout: 30m
    default_args:
      preview: ["--diff"]
    env:
      # Only ${env:NAME} pass-through, no literal values
      PULUMI_SKIP_UPDATE_CHECK: ${env:PULUMI_SKIP_UPDATE_CHECK}

  policy_hooks:
    - name: opa-compliance
      command: ["conftest", "test", "--policy", "policies/", "{{plan_json}}"]
      on_fail: block              # block | warn
      required: true

    - name: crossguard
      command: ["pulumi", "policy", "validate", "policies/aws-compliance"]
      on_fail: block
      required: false             # skip silently if command not present

    - name: cost-check
      command: ["./scripts/cost-gate.sh", "{{plan_json}}"]
      on_fail: warn               # surfaces in comment but doesn't block
```

### 8.7 notifications.yaml

Notifications handles PR-scoped human-readable status surfaces. Slack is the first sink type. The underlying Slack API client (auth, message lifecycle, Block Kit primitives) lives in `internal/slack` and is shared with drift sinks — notifications owns PR-flow *templates*, drift owns drift-flow *templates*, both feed the same Slack client.

```yaml
version: 1
config_type: notifications

slack:
  enabled: true
  channel: "#infra-deploys"
  auth_token: ${env:SLACK_BOT_TOKEN}
  rules:
    - environments: [prod]
```

### 8.8 observability.yaml

```yaml
version: 1
config_type: observability

# Opt-in. Off by default. reeve never emits telemetry unless this file exists.
otel:
  enabled: true
  endpoint: ${env:OTEL_EXPORTER_OTLP_ENDPOINT}
  service_name: reeve
  resource_attributes:
    team: platform
    repo: ${env:GITHUB_REPOSITORY}

annotations:
  - type: grafana
    url: https://grafana.mycompany.internal
    api_key: ${env:GRAFANA_API_KEY}
    events: [apply_started, apply_completed, apply_failed]

  - type: dash0
    endpoint: https://ingress.dash0.mycompany.internal
    api_key: ${env:DASH0_API_KEY}
    events: [apply_completed, apply_failed]

  - type: webhook
    url: https://hooks.mycompany.internal/reeve
    events: [apply_started, apply_completed]
```

### 8.9 Policy hooks

Policy integration uses a generic command-execution hook, not a native integration. Reeve runs user-specified commands against plan JSON and treats exit codes as pass/fail. This keeps reeve engine-agnostic on policy while supporting any policy system (OPA, Conftest, CrossGuard, Sentinel, custom scripts).

Policy hooks live inside engine config (see §8.6 `policy_hooks`). Users who want cross-engine OPA policies template the same hook block into multiple engine configs.

No dedicated `config_type: policy` file — the hook model is simple enough that it doesn't justify the config surface.

Template placeholders available inside `command`:
- `{{plan_json}}` — path to the plan JSON file reeve wrote
- `{{stack_name}}`, `{{project}}`, `{{env}}` — current stack context

Hook behavior:
- Exit 0 → pass
- Non-zero + `on_fail: block` → apply gate fails, stdout in PR comment
- Non-zero + `on_fail: warn` → warning in PR comment, apply proceeds
- `required: false` → skip silently if command not present (for optional tooling)

### 8.10 drift.yaml

```yaml
version: 1
config_type: drift

# ── What to check ──
scope:
  include_patterns: ["prod/*", "staging/*"]
  exclude_patterns: ["*/scratch", "experiments/*"]

# ── How to check ──
behavior:
  refresh_before_check: true       # default true for drift (off for PR preview)
  max_parallel_stacks: 8
  timeout_per_stack: 15m
  retry_on_transient_error: 2
  exit_on:
    drift_detected: false          # default: don't fail CI on drift
    drift_ongoing: false
    run_error: true                # fail CI on run-level errors

  # State bootstrap: how to behave when no prior state file exists
  # (first run ever, or state migrated, or state file manually cleared)
  state_bootstrap:
    mode: baseline                 # baseline | alert_all | require_manual
    # baseline:        first run is silent, establishes a baseline
    # alert_all:       first run emits drift_detected for every drifted stack
    # require_manual:  refuse to run without `reeve drift bootstrap` command
    baseline_max_age: 7d            # state older than this is treated as bootstrap

# ── What counts as drift ──
classification:
  ignore_properties:
    - resource_type: "aws:ec2/instance:Instance"
      properties: ["tags.LastScanned", "tags.AutoManaged"]
    - resource_type: "aws:s3/bucketTagging:BucketTagging"
      properties: ["tags.CostCenter"]

  ignore_resources:
    - "urn:*:aws:autoscaling/group:*::*autoscaler-managed*"

  treat_as_drift:
    orphaned_state: true           # state exists, resource gone
    missing_state: true            # resource exists, no state tracks it

# ── Freshness (skip recently-checked stacks) ──
freshness:
  enabled: false                   # default off; opt in
  window: 4h                       # skip if last successful run within this window
  respect_failures: true           # retry failed stacks even if within window

# ── Schedules (free-form named filter sets) ──
# Use via: reeve drift run --schedule <name>
# Composable with --pattern and --if-stale flags.
schedules:
  critical:
    patterns: ["prod/payments", "prod/auth"]
  prod:
    patterns: ["prod/*"]
    exclude_patterns: ["prod/payments", "prod/auth"]  # covered by "critical"
  staging:
    patterns: ["staging/*"]
  slow-movers:
    patterns: ["dev/*", "experiments/*"]

# ── Suppressions (time-bounded, via CLI; listed here for audit) ──
# Managed via `reeve drift suppress`; active suppressions live in the bucket.
# This block is for permanent / config-level suppressions only.
permanent_suppressions: []

# ── Sinks: where drift events go ──
sinks:
  - type: slack
    channel: "#infra-drift"
    on: [drift_detected, check_failed]
    grouping: by_environment       # single dashboard message + threaded details

  - type: webhook
    name: incident-io
    url: https://api.incident.io/v2/alert_events/http/${env:INCIDENT_IO_TOKEN}
    on: [drift_detected]
    payload:
      format: incident_io          # named preset
      severity_map:
        prod: high
        staging: medium
        dev: low
      dedupe_key: "drift:{stack}"

  - type: webhook
    name: custom
    url: https://hooks.mycompany.internal/drift
    on: [drift_detected, drift_resolved, check_failed]
    headers:
      Authorization: "Bearer ${env:CUSTOM_HOOK_TOKEN}"
    payload:
      format: raw

  - type: pagerduty
    integration_key: ${env:PD_CHANGE_EVENTS_KEY}
    on: [drift_detected]
    severity_map:
      prod: error
      staging: warning

  - type: otel_annotation
    on: [drift_detected, drift_resolved]
    emitter: grafana               # references annotations.grafana in observability.yaml

  - type: github_issue
    on: [drift_detected]
    labels: [drift, infra]
    assignees: ["@org/sre"]
```

**Sink types for v1:** `slack`, `webhook` (with `incident_io`, `rootly`, `opsgenie`, `raw` presets), `pagerduty`, `otel_annotation`, `github_issue`.

**Event types sinks can subscribe to:** `drift_detected`, `drift_ongoing`, `drift_resolved`, `check_failed`.

### 8.11 user.yaml (~/.config/reeve/)

Local-only preferences that never belong in repo config. Repo config is canonical; user config fills in user-only fields (no overlap between the two schemas).

```yaml
version: 1
config_type: user

# Default renderer for commands that produce markdown output
rendering:
  editor: glow                 # glamour | glow | bat | less
  default_format: markdown     # markdown | json | slack (for reeve drift report)

# Local auth preferences — picked up by `reeve run --local`
local_auth:
  default_aws_profile: mycompany-dev
  default_gcp_adc: true        # use gcloud application-default creds
  prefer_sso: true             # prefer aws_sso over aws_profile when both exist

# CI detection override (escape hatch — use with caution)
ci_detection:
  allow_local_creds_in_ci: false    # default: refuse local creds when CI=true
```

Keys in user config that also appear as CLI flags (e.g. `--format`, `--editor`) follow the parity principle: CLI overrides user config, user config overrides defaults.

---

## 9. Local Testing Surface

All commands run with no side effects unless explicitly flagged. No writing to files unless the user asks for it — Glamour prints to stdout.

```
reeve lint                          # static config check across all .reeve/*.yaml
reeve stacks                        # list declared/matched stacks and their bindings
reeve rules explain <stack>         # show approval rule resolution trace
reeve plan-run                      # simulate PR run, no side effects
reeve render                        # render PR comment Markdown to stdout (via Glamour)
reeve run --local                   # execute against real cloud, local artifacts dir
reeve locks list                    # read-only bucket lock inspection
reeve locks explain <stack>         # lock holder, queue, TTL

# Drift
reeve drift run                     # execute drift check (honors --pattern, --schedule, --if-stale)
reeve drift status                  # read last run results from bucket
reeve drift status --since 24h
reeve drift report                  # render report to stdout (Glamour)
reeve drift report --format slack   # or json, markdown
reeve drift suppress <stack>        # time-bounded suppression
reeve drift suppress list
reeve drift suppress clear <stack>

# Engine-specific maintenance
reeve pulumi find                   # walk repo, suggest pattern entries for config
reeve pulumi find --write           # mutate engine config with generated entries
reeve pulumi find --diff            # show what --write would change
reeve pulumi find modules           # discover and suggest module definitions
```

**Target feedback loops:**
- Static checks: under 5 seconds
- Full simulated run (cached previews): under 30 seconds

**Contributor testing:**
- `filesystem://` blob backend for unit tests
- `FakeGitHubClient` recording calls in-memory
- Fixture-based `pulumi preview --json` outputs for core logic tests
- Golden-file tests for comment rendering
- Injectable clock for TTL/lock tests
- MinIO + localstack for integration tests

---

## 10. Development Workflow

reeve is developed using **[OpenSpec](https://openspec.dev/)** (Fission-AI) for spec-driven development. This design doc is the seed; OpenSpec takes over once implementation begins.

### 10.1 Why OpenSpec

This design doc is useful for *planning* but fails once implementation starts:
- Assumptions get revised during build; the monolithic doc goes stale
- Code drifts from the spec without clear review points
- New contributors can't find authoritative answers to "how does X behave?"
- AI-assisted implementation has no per-feature source of truth

OpenSpec addresses this without adding process weight:
- **Specs live in the repo** alongside code, organized by capability (one spec per module boundary)
- **Each change gets its own folder** with proposal, delta specs, design notes, task list — reviewable as a single PR
- **Delta specs** (`ADDED / MODIFIED / REMOVED`) make brownfield changes reviewable without restating whole documents
- **Vendor-neutral** — works with any coding agent (Claude Code, Cursor, Codex, GitHub Copilot, etc.), matching reeve's own "no vendor lock-in" philosophy
- **Lightweight** — no rigid phase gates, no heavyweight tooling; just markdown in a known structure

### 10.2 Repo Layout

```
openspec/
├── specs/                           # Source of truth per capability
│   ├── core/
│   │   ├── discovery/spec.md
│   │   ├── approvals/spec.md
│   │   ├── locking/spec.md
│   │   ├── preconditions/spec.md
│   │   └── rendering/spec.md
│   ├── iac/spec.md                  # Engine interface contract
│   ├── vcs/spec.md                  # VCS interface contract
│   ├── blob/spec.md
│   ├── auth/spec.md
│   ├── notifications/spec.md
│   ├── drift/spec.md
│   ├── observability/spec.md
│   └── config/spec.md               # Config_type schemas, loading rules
│
├── changes/                         # Active and archived proposals
│   ├── <active-change>/
│   │   ├── proposal.md              # why, what, scope
│   │   ├── design.md                # technical approach
│   │   ├── tasks.md                 # implementation checklist
│   │   └── specs/                   # delta specs
│   │       └── <capability>/spec.md # ADDED/MODIFIED/REMOVED
│   └── archive/
│       └── YYYY-MM-DD-<name>/
│
└── config.yaml                      # OpenSpec project config
```

### 10.3 Seeding from This Design Doc

When we initialize OpenSpec in the repo:

1. **§5 Feature Scope blocks** → initial capability specs in `openspec/specs/`
   - §5.1 PR Flow → `specs/core/pr-flow/spec.md`
   - §5.2 Locking → `specs/core/locking/spec.md`
   - §5.3 Approval Rules → `specs/core/approvals/spec.md`
   - §5.4 Apply Preconditions → `specs/core/preconditions/spec.md`
   - §5.5 PR Comment Layout → `specs/core/rendering/spec.md`
   - §5.6 Slack Notifications → `specs/notifications/spec.md`
   - §5.7 Observability → `specs/observability/spec.md`
   - §5.8 Bucket Layout → `specs/blob/spec.md`
   - §5.9 Drift Detection → `specs/drift/spec.md`

2. **§6 Module Architecture** → interface specs
   - §6.5 VCS Abstraction → `specs/vcs/spec.md`
   - §6.6 Policy Hook Model → `specs/iac/policy-hooks/spec.md`
   - Engine interface contract → `specs/iac/spec.md`

3. **§8 Configuration** → `specs/config/spec.md` with per-`config_type` requirements

4. **Open items from §11** become the first set of `changes/` proposals to refine before implementation.

This doc stays as the high-level design reference ("the napkin"), but the authoritative per-capability behavior lives in `openspec/specs/`. New work happens as proposals in `openspec/changes/`.

### 10.4 Working Model

- Every non-trivial feature or behavior change is a proposal in `openspec/changes/<name>/`
- Small fixes (typos, obvious bugs) can skip the proposal step
- Proposals include delta specs that become part of the canonical specs on archive
- AI-generated code is welcome per OpenSpec convention, with the coding agent + model noted in the PR
- Specs are reviewed like code — through PR review, by humans

### 10.5 What Lives Where

| Content | Location |
|---|---|
| High-level vision, anti-positioning, tech stack choices | This design doc (`DESIGN.md`) |
| Per-capability behavior, requirements, scenarios | `openspec/specs/` |
| Active proposals under discussion | `openspec/changes/` |
| Implementation tasks for an active change | `openspec/changes/<name>/tasks.md` |
| Archived historical decisions | `openspec/changes/archive/` |
| User-facing docs | `docs/` (separate, generated from specs where useful) |

The design doc gets retired — or compressed into a much shorter vision/principles doc — once specs are populated. It is explicitly *not* maintained in parallel with the specs.

---

## 11. Non-Goals (v1)

- Web UI / dashboard
- User management / SSO
- Managed/hosted offering
- "Approved drift" adoption (auto-refresh into state) — documented manual workflow only
- Multi-IaC in one run (v1 is Pulumi only)
- Non-GitHub VCS (interface ready, implementations later)
- Non-Slack notifications (interface ready, implementations later)
- Auto-discovery of module imports via source-code parsing (v1.x; v1 is explicit definitions only)
- Auto-tiering of drift schedules by change frequency (v1.x)

---

## 12. Open Questions

Resolved this planning cycle (now reflected in §6 / §8):
- Go package layout (flat)
- Interface granularity (small, per-use-site; adapters document full contract)
- Core engine-agnosticism (capability detection only, no engine-name branching)
- Stack discovery split (engine enumerates; core filters/maps)
- GitLab priority (moderate future-proofing)
- v1 MVP scope (PR flow + drift)
- CODEOWNERS ownership (VCS module owns parsing)
- PR comment edit-in-place (capability flag + append fallback)
- Policy config (generic hook, no dedicated config_type)
- Approval source of truth (pluggable sources; pr_review default, pr_comment opt-in)
- User config location and scope
- Slack code sharing (single client, per-domain templates)
- Drift state bootstrap (configurable: baseline / alert_all / require_manual)
- Drift + PR overlap (surfaced in report, raw event payload includes overlapping_prs)
- Drift → $GITHUB_STEP_SUMMARY (always)

Still open:
- Exact `find` YAML mutation library (need comment-preserving YAML writer in Go — `yaml.v3` preserves comments but has quirks; may need `goccy/go-yaml` or a custom approach)
- Secret redaction edge cases (Pulumi marks `[secret]` but stdout capture and error paths can still leak; need a pass through all log/output paths)
- PR comment sort order finalization (status-grouped is the default; may want env-priority as an option)
- Module auto-discovery strategy per engine (v1.x, but shape should be scoped now so v1 definitions don't close doors)
- OPA/Conftest evaluation timing (pre-preview vs post-preview; currently only post-preview via policy_hooks — revisit if users want pre-preview linting of configs)
- Competitive feature checklist (auth + locking + approvals + drift + notifications across Terrateam/Digger/Atlantis; Y/N markdown table)
- Multi-engine binding resolution edge cases (what if a PR touches stacks from two different engines and they share a project name?)
- GitHub Action packaging strategy (single action with mode switch, or separate actions per mode?)