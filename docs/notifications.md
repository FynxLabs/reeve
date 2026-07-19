# Notifications

reeve publishes lifecycle events through a single **notification-channel
framework** (`internal/notify`). Two producers feed it:

- the **PR flow** (plan → ready → approved → applying → applied/failed/blocked)
- the **drift runner** (drift_detected / drift_ongoing / drift_resolved / check_failed)

A destination is implemented once and can subscribe to events from either
producer: the same Slack channel that tracks a PR's apply lifecycle can also
receive drift alerts, and a webhook can be pointed at both.

## Declaring channels

Channels are declared in `.reeve/notifications.yaml` (v2 shape) as a generic
list — `type` picks the adapter, `on:` picks the events:

```yaml
version: 2
config_type: notifications

channels:
  - type: slack
    channel: "#infra-deploys"
    auth_token: ${env:SLACK_BOT_TOKEN}
    trigger: plan                     # when the per-PR message is created
    on: [plan, approved, applying, applied, failed, blocked]

  - type: slack
    name: drift-alerts
    channel: "#infra-drift"
    auth_token: ${env:SLACK_BOT_TOKEN}
    on: [drift_detected, check_failed]

  - type: webhook
    name: audit-feed
    url: https://example.internal/hooks/reeve
    on: [applied, failed, drift_detected]
    headers:
      Authorization: "Bearer ${env:HOOK_TOKEN}"
```

Drift-only channels can also live in `drift.yaml` under `channels:` (same shape,
same adapters) — that file remains fully supported. drift.yaml's pre-v0.3
spelling `sinks:` is still accepted as a deprecated alias (`reeve
migrate-config` rewrites it; declaring both keys is an error). See
[drift.md](drift.md#channels) for the drift-specific rendering of each type.

### Events

Valid `on:` values, in lifecycle order:

| Event | Producer | Meaning |
| --- | --- | --- |
| `plan` | PR flow | Preview finished; pending approval |
| `ready` | PR flow | `/reeve ready` (or `auto_ready`) |
| `approved` | PR flow | Preconditions passed; apply imminent |
| `applying` | PR flow | Apply loop started |
| `applied` | PR flow | Apply finished successfully |
| `failed` | PR flow | Apply errored |
| `blocked` | PR flow | Apply blocked (gates/locks) |
| `drift_detected` | drift | New drift on a stack |
| `drift_ongoing` | drift | Still drifted since the last run |
| `drift_resolved` | drift | Was drifted, now clean |
| `check_failed` | drift | Drift check errored |

Unknown names in `on:` fail `reeve lint` / config load. A channel with an
empty `on:` list draws a warning — it will never fire (exception: a Slack
channel defaults to every PR-flow event at or after its `trigger`, preserving
the legacy behavior).

### Channel types

| Type | Destination | Notes |
| --- | --- | --- |
| `slack` | Slack (Web API) | PR events drive one message per PR (upsert + thread timeline); drift events post standalone messages. `channel`, `auth_token`, `trigger`, `icons`, `rules` |
| `webhook` | Generic HTTP POST | Raw JSON payload; `url`, `headers` |
| `pagerduty` | PagerDuty Events API v2 | drift: trigger/resolve per stack; PR: `failed`/`blocked` trigger, `applied` resolves; `integration_key`, `severity_map` |
| `github_issue` | GitHub issue per drifted stack | Drift events only; `labels`, `assignees`. Requires `GITHUB_TOKEN` with `issues: write` |
| `otel_annotation` | Annotation emitters (Grafana/Datadog/Dash0) | Maps drift + apply lifecycle onto annotation events; configure emitters in `observability.yaml` |

Common fields on every channel: `type`, `name` (defaults to the type),
`enabled` (defaults to `true`), `on`.

### Delivery guarantees

- Channels receive events **concurrently** — one hung endpoint cannot starve
  the others. Each delivery is bounded by a timeout.
- HTTP channels (webhook, pagerduty) share an HTTP client with a sane
  timeout and retry transient failures (network errors, 5xx, 429) with
  bounded exponential backoff.
- Notification failures are logged, never fatal: they cannot abort a plan
  or apply.
- Notifications run last in the pipeline, so upstream failures are
  captured accurately.

## Legacy shape (v1)

The pre-v0.3 shape — a single `slack:` block — keeps working unchanged and
is mapped onto the channel model internally (`slack.events` becomes `on:`):

```yaml
version: 1
config_type: notifications

slack:
  enabled: true
  channel: "#infra-deploys"
  auth_token: ${env:SLACK_BOT_TOKEN}
  trigger: plan
  events: [plan, applied, failed]
```

Run `reeve migrate-config` to rewrite it to the v2 `channels:` shape
(originals are backed up as `*.bak`; `--dry-run` previews). Migration is
optional — v1 files load forever.

`comments.*` in `shared.yaml` (PR comment rendering) is unchanged and
unrelated to channels.

## The Slack PR message lifecycle

See [configuration.md](configuration.md#notificationsyaml) for the full
message lifecycle (colors, thread timeline, trigger semantics, icons,
rules).

## Adding a destination

One interface implementation serves both producers. In
`internal/notify/channels/<name>`:

```go
func init() { notify.Register("my_channel", New) }

func New(_ context.Context, cfg schemas.ChannelYAML, deps notify.Deps) (notify.Channel, error) {
    // return (nil, nil) to skip when an optional dependency is missing
}

func (s *Channel) Name() string                { ... }
func (s *Channel) Subscribes() []notify.Event  { ... } // usually notify.ParseEvents(cfg.On)
func (s *Channel) Deliver(ctx context.Context, p notify.Payload) error {
    // p.Drift != nil for drift events, p.PR != nil for PR-flow events
}
```

Then add the package to `internal/notify/all` (or import it directly in a
custom build). Channels self-register; the factory resolves purely by the
config `type:` string — no core code changes needed (see the modularity
contract in `openspec/specs/architecture`).
