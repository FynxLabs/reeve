# Configuration reference

Everything under `.reeve/` is strict YAML: unknown keys are errors, versions
are per-file, and schemas are stable within a major version.

## File layout

```text
.reeve/
├── shared.yaml           # bucket, approvals, locking, preconditions, freeze, apply
├── auth.yaml             # credential providers and bindings
├── notifications.yaml    # notification channels (slack, webhook, pagerduty, ...)
├── observability.yaml    # OTEL + annotations
├── drift.yaml            # drift scope, schedules, channels
├── pulumi.yaml           # engine: pulumi
└── terraform.yaml        # engine: terraform (future)
```

Every file begins with:

```yaml
version: 1
config_type: <shared|engine|auth|notifications|observability|drift|user>
```

- `version` is per-file. Bumps affect only that schema.
- `config_type` is one-per-file, except `engine` (one per unique `engine.type`).
- Unknown top-level keys fail `reeve lint`.

A single-file `reeve.yaml` at repo root is supported for simple cases.
When `.reeve/` exists, root-level `reeve.yaml` is ignored (ambiguity error).

---

## `shared.yaml`

```yaml
version: 1
config_type: shared

bucket:
  type: s3                         # filesystem | s3 | gcs | azblob | r2
  name: mycompany-reeve
  region: us-east-1
  prefix: reeve/                   # optional sub-prefix

comments:
  sort: status_grouped             # status_grouped (default) | alphabetical
  collapse_threshold: 10
  show_gates: true
  style: replace                   # replace (default) | append | section
  stack_view: all                  # all (default) | changed

retention:
  max_age: 720h                    # default 720h (1 month); 0 disables pruning

locking:
  ttl: 4h                          # default 4h; also bounds the lease of holders promoted from the queue
  queue: fifo                      # v1: fifo (only option)
  reaper_interval: 15m             # informational; reaper is opportunistic
  admin_override:                  # gates force-unlock (locks unlock without --pr);
    allowed: ["@org/sre-leads"]    # PR-scoped removal (--pr / "/reeve unlock") is
    requires_reason: true          # self-service and not gated here

approvals:
  sources:
    - type: pr_review              # default VCS reviews
      enabled: true
    - type: pr_comment             # opt-in: "/reeve approve" in PR comments
      enabled: false
      command: "/reeve approve"
  default:
    required_approvals: 1
    approvers: ["@org/infra-reviewers"]
    codeowners: true               # honor CODEOWNERS alongside team rules
    dismiss_on_new_commit: true
  stacks:
    "prod/*":
      required_approvals: 2
      approvers: ["@org/sre", "@org/security"]
      require_all_groups: true     # one from each group, not N-of-any
    "prod/payments":
      approvers: ["@org/payments-leads"]

preconditions:
  require_up_to_date: true
  require_checks_passing: true
  preview_freshness: 2h            # preview must be newer than this
  preview_max_commits_behind: 5

freeze_windows:
  - name: friday-afternoon
    cron: "0 15 * * 5"             # Fri 3pm
    duration: 65h                  # through Monday morning
    stacks: ["prod/*"]

apply:
  trigger: comment                 # comment (default) | merge
  command: "/reeve apply"
  allow_fork_prs: false            # deny-by-default - review risk before flipping
  auto_ready: false                # if true: when PR converts from draft to ready-for-review
                                   # and plan has succeeded, notify for approval via Slack + PR comment
```

### `comments.style`

Controls how the apply comment relates to the preview comment.

| Value | Behavior |
| --- | --- |
| `replace` (default) | Apply upserts using the same marker as preview, replacing it in-place. |
| `append` | Apply always posts a new comment; the preview comment is left untouched. |
| `section` | Apply upserts with a separate marker (`<!-- reeve:apply:v1 -->`), so preview and apply each occupy their own comment slot. |

> **Draft PRs:** apply is always blocked on draft PRs regardless of config.
> Convert to ready for review first. If `auto_ready: true` and a plan has succeeded,
> reeve fires `/reeve ready` automatically when the PR converts from draft to ready for review.

### `comments.stack_view`

Controls which stacks the comment table lists.

| Value | Behavior |
| --- | --- |
| `all` (default) | Lists every declared stack, no-ops included. |
| `changed` | Lists only stacks with planned/applied changes. |

### Apply timeline

Each apply run owns one PR comment, pinned by a per-run marker. Events append in order:

- 🚀 `apply starting`
- ✅ `applied` — with changed stack refs
- 🔴 `failed` — with failing stack refs
- 🔒 `blocked` — with gate reason
- ⏭️ `skipped` — commit already applied

### Already-applied guard

A fully-clean apply writes `runs/pr-<n>/applied/<sha>.json`. Re-running at the same commit:

- **apply** — skips, posts the ⏭️ timeline notice, exits success.
- **preview** — renders the plan with an "already applied" banner.
- `--force` — bypasses the guard on both.

### `retention.max_age`

Run artifacts under `runs/` (manifests, applied-state pointers) are pruned at run start.

- Go duration string; default `720h` (1 month).
- `0` or negative disables pruning.
- Age-based only — merged-PR cleanup needs VCS wiring reeve does not have, so artifacts age out.

### Approval rule merging

- `approvals.default` is the baseline.
- `approvals.stacks.<pattern>` entries merge with the default for matching
  stacks.
- Scalar fields (`required_approvals`, `require_all_groups`, `codeowners`,
  `dismiss_on_new_commit`, `freshness`) on a pattern **override** the
  default.
- `approvers` lists **union** (deduplicated).
- Patterns with more literal characters win specificity ties, and the
  more-specific pattern's scalar fields override the broader one's.
- `require_all_groups: true` changes semantics: every listed approver
  group must contribute one approval, regardless of `required_approvals`.

**Secure defaults.** reeve fails closed on approvals:

- A stack with **no matching approval policy** still requires **one**
  non-author approval — it does not auto-pass.
- `required_approvals: N` with **no `approvers` list** counts any `N`
  distinct non-author approvals (GitHub's "require N approvals" behavior),
  rather than being unsatisfiable.
- `dismiss_on_new_commit` defaults to **`true`**: pushing a new commit
  invalidates prior approvals. Set it to `false` explicitly to opt out.
- Only a reviewer's **most recent** review counts. A reviewer who approves
  and later requests changes (or whose approval is dismissed) no longer
  counts toward the gate.
- `freshness: <duration>` (opt-in, e.g. `24h`): an approval older than the
  window at evaluation time does not count and must be re-given. Stale
  approvals are called out in the rule trace and the missing list, so a
  blocked apply says exactly whose approval expired. `0`/unset means no
  freshness constraint. An approval without a submission timestamp fails
  closed when freshness is set.

### CODEOWNERS resolution

When `codeowners: true`, reeve parses the repo's `CODEOWNERS` file and
requires at least one approval from an owner of each changed file.

Owner resolution unions **all** matching rules for a file. For example:

```
* @org/platform
Pulumi.*.yaml @org/engineering
```

A `Pulumi.*.yaml` file matches both rules, so owners =
`[@org/platform, @org/engineering]`. Either team's member satisfies the
gate for that file.

Team slugs in CODEOWNERS are expanded the same way as `approvers` entries:
reeve resolves `org/team` → member logins via the VCS API before evaluation.

**Email owners are unenforceable.** GitHub allows email addresses as
CODEOWNERS entries (e.g. `docs@example.com`), but reeve has no
commit-email → login resolution, so email owners are excluded from the
gate: a path owned by both an email and a login/team still requires the
login/team's approval, and a path owned *only* by emails adds no
requirement (the evaluation trace notes the skipped entries instead of
wedging the gate forever). `reeve lint` warns about email owners in
CODEOWNERS.

Inspect the merged result:

```bash
reeve rules explain prod/payments
```

---

## `engine` (e.g. `pulumi.yaml`)

```yaml
version: 1
config_type: engine

engine:
  type: pulumi                     # pulumi | terraform | opentofu (future)

  binary:
    path: pulumi
    version: "3.150.0"             # optional pin

  # State backend - reeve configures the engine before each run using
  # short-lived creds. reeve does NOT manage state itself.
  state:
    backend: s3
    url: s3://mycompany-pulumi-state
    auth_provider: aws-state       # refers to auth.yaml provider
    secrets_provider:
      type: awskms
      key: arn:aws:kms:us-east-1:111:key/abc-123-def

  # Stack declarations. Runtime behavior is always explicit - either a
  # literal or a declared pattern must match.
  stacks:
    - project: api                 # literal
      path: projects/api
      stacks: [dev, staging, prod]

    - pattern: "projects/*"        # doublestar glob (no regex escape hatch in v1)
      stacks: [dev, staging, prod]

  filters:
    exclude:
      - "projects/sandbox/**"      # path glob
      - stack: "*/scratch"         # or stack-ref glob

  change_mapping:
    scope: auto                    # auto (default) | pulumi_only
    ignore_changes:
      - "**/docs/**"
    extra_triggers:
      - project: api
        paths: ["shared/types/**", "protos/**"]

  execution:
    max_parallel_stacks: 4
    preview_timeout: 10m
    apply_timeout: 30m

  policy_hooks:                    # see docs/policy-hooks.md
    - name: opa-compliance
      command: ["conftest", "test", "--policy", "policies/", "{{plan_json}}"]
      on_fail: block               # block | warn
      required: true
```

`engine.type` selects a registered engine adapter — the binary compiles in a
default set (pulumi today; terraform and opentofu are coming in this line),
and `reeve lint` fails when the type doesn't resolve to a compiled-in engine.

### Discovery pipeline

1. **Declare** - literal `{project, path, stacks}` entries and `pattern:`
   globs from this file.
2. **Include** - engine enumerates on disk via `Pulumi.yaml` +
   `Pulumi.<stack>.yaml` files.
3. **Exclude** - `filters.exclude` drops entries.
4. **Resolve** - engine validates each remaining stack.
5. **Map to changes** - drop skippable files, match the rest to stacks by path
   / `extra_triggers`; unmapped files broaden to all stacks (`scope: auto`).

**Shared directories.** Many stacks can live in one directory, each with its own `Pulumi.<name>.yaml`. Change-mapping is per-file:

- `Pulumi.<name>.yaml` change — affects only stack `<name>`.
- Sibling `Pulumi.<other>.yaml` — ignored.
- Shared `Pulumi.yaml`, program code, nested files — affect every stack in the directory.

**Docs/asset-only changes.** Built-in skip globs cover non-load-bearing files: `*.md`, `*.markdown`, `*.adoc`, `*.asciidoc`, `*.rst`, `*.txt`, `LICENSE`, images (`*.png/jpg/jpeg/gif/svg/webp`). Merged with `ignore_changes`.

- All changed files skippable — run nothing, post "Documentation/asset-only changes".
- `docs/` directories are not skipped; they can hold config or program-read data.

**`change_mapping.scope`.** Controls behavior when a changed file maps to no specific stack (shared lib, provider code, root `go.mod`).

| Value | Behavior |
| --- | --- |
| `auto` (default) | Preview/apply all stacks; post a header naming the unmapped files. |
| `pulumi_only` | Act only on files inside a stack directory; never broaden. |

Inspect it:

```bash
reeve stacks             # prints declared-and-resolved stacks
```

---

## `auth.yaml`

See [auth.md](auth.md) for the full provider catalog. Minimal shape:

```yaml
version: 1
config_type: auth

providers:
  aws-prod:
    type: aws_oidc
    role_arn: arn:aws:iam::111111111111:role/reeve-prod
    region: us-east-1
    duration: 1h

  aws-prod-readonly:                 # used only for drift
    type: aws_oidc
    role_arn: arn:aws:iam::111111111111:role/reeve-drift-readonly

bindings:
  - match: { stack: "prod/*" }
    providers: [aws-prod]

  - match: { stack: "prod/*", mode: drift }
    providers: [aws-prod-readonly]   # replaces aws-prod for drift runs
```

---

## `notifications.yaml`

Notification destinations ("channels") are declared generically: `type`
chooses the adapter, `on:` chooses the subscribed events. One channel
implementation serves both PR-flow events (`plan` … `blocked`) and drift
events (`drift_detected` …) — see [notifications.md](notifications.md)
for the full channel catalog and event list.

```yaml
version: 2
config_type: notifications

channels:
  - type: slack
    channel: "#infra-deploys"
    auth_token: ${env:SLACK_BOT_TOKEN}
    trigger: plan
    on: [plan, ready, approved, applying, applied, failed, blocked]

  - type: webhook
    name: audit-feed
    url: https://example.internal/hooks/reeve
    on: [applied, failed, drift_detected]

  # Deployment timeline (append-only activity heartbeat, default off):
  - type: timeline_slack
    channel: "#infra-deploys"
    auth_token: ${env:SLACK_BOT_TOKEN}
  - type: timeline_github
```

The `timeline_*` channels complement the dashboard surfaces above: the status
comment/message is the edited-in-place snapshot; the timeline is one entry
per event (SHA, timestamp, per-run CI link) — thread replies in Slack, one
comment per commit SHA on GitHub. See
[notifications.md](notifications.md#the-deployment-timeline).

### Converting from the original config

The original single `slack:` block (and drift.yaml's `sinks:` key) no
longer load — reeve errors with a conversion pointer. Run
`reeve migrate-config` to rewrite them to the `channels:` shape
(originals backed up as `*.bak`; `--dry-run` previews), or hand-edit —
see [notifications.md](notifications.md#converting-from-the-original-config).

```yaml
version: 1
config_type: notifications

slack:
  enabled: true
  channel: "#infra-deploys"
  auth_token: ${env:SLACK_BOT_TOKEN}

  # trigger controls when the initial Slack message is created.
  # Subsequent events always update the existing message in place.
  #
  #   apply  (default) - message created only when /reeve apply is invoked
  #   plan             - message created when a plan finishes (status: pending approval)
  #   ready            - message created only when /reeve ready is run
  trigger: plan

  # events lists which lifecycle events emit a Slack notification.
  # When omitted, all events at or after the trigger fire (default behavior).
  # Valid values: plan, ready, approved, applying, applied, failed, blocked
  #
  # Example: only notify on plan and final result, skip the intermediate steps:
  #   events: [plan, applied, failed, blocked]
  #
  # events: [plan, ready, approved, applying, applied, failed, blocked]

  # icons overrides the default emoji used in the message layout.
  # All fields are optional. Useful when your Slack workspace has custom emoji
  # (e.g. :pulumi:, :github:) that aren't available by default.
  icons:
    engine: ":building_construction:"   # repo/project header icon
    runner: ":runner:"                  # CI runner / view-run button
    author: ":bust_in_silhouette:"      # PR author field
    approver: ":approved_stamp:"        # required approvers field

  rules:
    - environments: [prod, staging]  # only notify these envs
    - stacks: ["prod/payments", "prod/auth"]
```

### Message lifecycle

reeve sends one message per PR and edits it in place as the run progresses.
The sidebar color and status field update at each stage:

| Stage | Trigger | Color |
| --- | --- | --- |
| Plan ready | `trigger: plan` - plan finishes | 🟡 yellow |
| Ready | `/reeve ready` or `auto_ready: true` on draft→ready with successful plan | 🟡 yellow |
| Approved | Preconditions passed, apply imminent | 🔵 blue |
| Applying | Apply loop started | 🟣 purple |
| Applied | Apply completes successfully | 🟢 green |
| Failed | Apply errors | 🔴 red |
| Blocked | Preconditions not met | 🟡 yellow |

**Error rule:** if no message exists yet and apply fails, no message is created.
Errors only update an existing message.

> The Approved update can also fire the moment a PR review is approved
> (`reeve run approved`), but only if the GitHub Action is configured with
> `run-on-approval: "true"` and the workflow subscribes to
> `pull_request_review` events. By default that dispatch is skipped - the
> apply gate re-checks approvals anyway - so Slack flips to approved at
> apply time instead.

**`/reeve apply` hint** only appears when status is `approved`. Pending-approval
states show "Waiting for approval." instead.

### Thread timeline

The first message opens a Slack thread. Each event appends a timestamped
timeline entry (planned, ready, approved, applying, applied, failed).
When a `timeline_slack` channel is enabled it takes over the thread with
richer entries (per-stack outcomes, per-run CI links) and these courtesy
entries are suppressed.

No plan output is sent to Slack. Full output is in the GitHub Actions run log.

Token expansion: `${env:NAME}` pulls from the process environment.

---

## `observability.yaml`

```yaml
version: 1
config_type: observability

otel:
  enabled: true
  endpoint: ${env:OTEL_EXPORTER_OTLP_ENDPOINT}
  service_name: reeve
  resource_attributes:
    team: platform
    repo: ${env:GITHUB_REPOSITORY}
  stack_cardinality: hash            # allow | hash (default) | drop
  headers:
    Authorization: ${env:OTEL_AUTH_HEADER}

annotations:
  - type: grafana
    url: https://grafana.mycompany.internal
    api_key: ${env:GRAFANA_API_KEY}
    events: [apply_started, apply_completed, apply_failed]

  - type: datadog
    url: https://api.datadoghq.com
    api_key: ${env:DATADOG_API_KEY}
    events: [apply_completed, apply_failed, drift_detected]

  - type: webhook
    url: https://hooks.mycompany.internal/reeve
    events: [apply_started, apply_completed]
```

- Fully opt-in. Without `observability.yaml`, reeve emits no telemetry.
- `stack_cardinality: hash` emits a stable 64-bit fingerprint of
  `{project}/{stack}` as the OTEL label - prevents cardinality blow-up on
  big monorepos. Use `allow` for small deployments, `drop` to omit the
  stack label entirely.

---

## `drift.yaml`

See [drift.md](drift.md). Minimal:

```yaml
version: 1
config_type: drift

scope:
  include_patterns: ["prod/*", "staging/*"]
  exclude_patterns: ["*/scratch"]

behavior:
  refresh_before_check: true
  max_parallel_stacks: 8
  state_bootstrap:
    mode: require_manual           # baseline | alert_all | require_manual
    baseline_max_age: 7d

schedules:
  critical:
    patterns: ["prod/payments", "prod/auth"]
  prod:
    patterns: ["prod/*"]
    exclude_patterns: ["prod/payments", "prod/auth"]

channels:
  - type: slack
    channel: "#infra-drift"
    on: [drift_detected, check_failed]

  - type: pagerduty
    integration_key: ${env:PD_CHANGE_EVENTS_KEY}
    on: [drift_detected]
    severity_map:
      prod: error
      staging: warning
```

---

## `user.yaml` (local only)

Location: `~/.config/reeve/user.yaml`. Never committed to a repo.

Reserved for local-only preferences that don't belong in team config.
v1 scope is minimal - most local overrides happen via CLI flags or env
vars. The schema exists as a forward-compatible slot.

```yaml
version: 1
config_type: user
```

---

## Token expansion

The following fields accept `${env:NAME}` references:

- `shared.yaml`: `bucket.*`, `locking.admin_override.*`
- `auth.yaml`: provider fields (see [auth.md](auth.md) for the full list)
- `notifications.yaml`: `slack.auth_token`
- `observability.yaml`: `otel.endpoint`, `otel.headers`, `annotations.*.api_key`, etc.
- `drift.yaml`: channel credentials

`${env:X}` expands at runtime via `os.Getenv("X")`. Missing env vars expand
to empty strings (not an error) - so token references safely degrade when
a feature is optional.

## Lint

```bash
reeve lint
```

Catches:

- Unknown top-level keys
- Unsupported `version` values
- Duplicate `config_type` (except `engine`)
- Missing required fields (`bucket.type`, at least one engine)
- Auth provider scope conflicts (see [auth.md](auth.md))
- `env_passthrough` without `i_understand_this_is_dangerous: true`

## Migration

When a schema bumps version (e.g. `shared: 1 → 2`):

```bash
reeve migrate-config --dry-run   # preview
reeve migrate-config             # writes; keeps *.bak backups
```

Per-file version bumps - migrations don't have to be in lockstep across
config types.
