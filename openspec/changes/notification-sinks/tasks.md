# Notification sinks — tasks

- [x] Lift the sink framework into `internal/notify`: `Sink` interface,
      unified event model (PR flow + drift), registry with `init()`
      self-registration, factory resolving by config `type:` string.
- [x] Concurrent `Dispatch` with per-delivery timeout; shared HTTP client;
      bounded retry with backoff (`PostJSON`) for 5xx/network errors.
- [x] Migrate the five drift sinks onto the framework, behavior-identical.
- [x] Fix the `go-github` leaks: `github_issue` consumes a narrow
      `notify.IssueClient` implemented by `internal/vcs/github`.
- [x] PR flow as producer: run pipeline (`preview`, `apply`, `ready`,
      `approved`) publishes events via `notify.Dispatch`; Slack PR message
      lifecycle moves into the slack sink.
- [x] Slack hardening: propagate state-load errors (duplicate-post fix),
      compare-and-swap state writes via `blob.PutIfMatch`, mrkdwn escaping,
      fence-safe embedding, UTF-8-safe truncation.
- [x] Config: `notifications.yaml` v2 `sinks:` list; v1 `slack:` block maps
      onto the sink model internally; `on:` event validation at load/lint;
      warn on empty subscriptions.
- [x] `reeve migrate-config`: notifications v1 → v2 migration with tests.
- [x] Docs: docs/notifications.md (sink catalog, events, adding a
      destination), configuration.md notifications section, drift.md sink
      section points at the shared framework.
- [ ] Archive this change: fold the delta into
      `openspec/specs/notifications/spec.md` on merge.
