# Notifications

## Scope

PR-scoped human-readable status. Slack first. Runs **last** in the
pipeline so upstream failures are captured accurately.

## Slack

- One message per PR, tracked by message ID in `notifications/pr-{n}/slack.json`.
- Main message: high-level status (planned → applying → applied/failed/closed-unmerged).
- Thread: timestamped timeline entries only (one per event: planned, ready, approved, applying, applied, failed).
- Block Kit layout.
- Rule-gated (e.g. `environment: prod` only).
- Always links back to the PR.
- `trigger` controls which event **creates** the initial message (`plan` | `ready` | `apply`).
- `events` controls which lifecycle events emit at all. Default: all events at or after the trigger.
  Valid values: `plan`, `ready`, `approved`, `applying`, `applied`, `failed`, `blocked`.

## Client sharing

The Slack API client (auth, message lifecycle, Block Kit primitives) lives
in `internal/slack` and is shared with drift sinks. PR-flow templates live
here; drift-flow templates live in `internal/drift/sinks/slack`.

## Future sinks (out of scope for v1)

Mattermost, Rocket.Chat, Teams, generic webhook. Each is a new adapter in
this module.
