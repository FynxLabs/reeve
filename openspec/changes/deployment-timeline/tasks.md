# Deployment timeline — tasks

- [x] Event model: add `planning` (preview started) and reserved
      `break_glass` to schemas + `internal/notify`; keep the legacy default
      subscription (`PREvents`) unwidened; add `TimelinePREvents`.
- [x] Producer: `run.Preview` emits `planning` at run start; sinks built
      once per preview and reused for the `plan` event.
- [x] `notify.Deps.Comments` (narrow `CommentClient` marker-upsert surface);
      plumbed from preview/apply/ready/approved call sites.
- [x] `timeline_github` sink: one comment per SHA under the new
      `reeve:timeline:v1:{shortsha}` marker namespace; entry history in
      per-PR blob state with CAS append; full re-render per event.
- [x] `timeline_slack` sink: thread replies under one PR-level anchor;
      shared slack blob state (exported `StateStore`/`PRState`), anchor
      reuse/creation with first-writer-wins conflict handling;
      `thread_owner` claim suppresses the dashboard's courtesy thread notes.
- [x] Config: new sink types default-off, validated events, empty-`on`
      warning exemption for types with default subscriptions.
- [x] Tests: entry rendering, SHA grouping, GitHub CAS conflict merge,
      Slack anchor create/reuse/race, dashboard thread suppression, default
      subscriptions, event-set parity, config load/validate.
- [x] Docs: docs/notifications.md timeline section (dashboard vs timeline,
      config example, sink catalog + events table updates).
- [ ] Producer for `break_glass` when the emergency-override flow lands.
- [ ] Archive this change: fold the delta into
      `openspec/specs/notifications/spec.md` on merge.
