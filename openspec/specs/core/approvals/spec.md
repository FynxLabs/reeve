# Approvals

## Sources

Pluggable. v1 ships:

- `pr_review` - default; reads PR reviews via the VCS adapter. On unless an
  explicit `sources` entry lists `pr_review` with `enabled: false`. When no
  `sources` block is configured at all, this is the only active source -
  identical to reeve's original behavior.
- `pr_comment` - opt-in; parses `/reeve approve` in PR comments. Off unless an
  explicit `sources` entry lists `pr_comment` with `enabled: true`.

Future (v2+): `slack_interaction`, `webhook`. Each is a new source
implementation - no core changes.

Source ordering in `shared.yaml` matters only for tie-breaking attribution.

### Union across sources

Enabled sources are gathered independently and **unioned**. Deduplication is
by approver identity: a human who approves via *both* a review and a
`/reeve approve` comment counts **once** toward `required_approvals`. Every
per-approval rule below (the non-author rule, freshness, dismiss-on-new-commit)
is applied **uniformly** across sources before that identity dedup.

### `pr_comment` authorization (fail-closed)

A `/reeve approve` comment only counts when **all** hold:

- The comment's first line is `<prefix> approve`, where `<prefix>` exactly
  matches a configured command prefix (default `/reeve`, `@reeve`) - parsed the
  same way every other `/reeve` command is.
- The commenter's `author_association` is in the same allowlist that gates
  command dispatch (default `OWNER`, `MEMBER`, `COLLABORATOR`). The source
  re-checks this itself because it reads historical comments directly, not the
  dispatched event; an unauthorized commenter's `/reeve approve` is ignored.
  An empty allowlist denies everyone.
- The commenter is not a bot (mirrors the self-trigger guard) and is **not the
  PR author** (the non-author rule - an author never self-approves, via review
  or comment).

A comment approval is stamped with the SHA that was HEAD when the comment was
posted (the newest commit at or before the comment's creation time), mirroring
how a review carries its `commit_id`. Under `dismiss_on_new_commit` (default
on) a comment approval is therefore **dismissed when a newer commit lands**,
exactly like a stale review. Where the intended commit is ambiguous (a comment
predating every commit), the source picks the oldest commit so dismissal still
fires - the fail-closed choice.

## Rule resolution

Layered, not either/or:

- `default` baseline applies to all stacks.
- `stacks.<pattern>` rules **merge** with default: approver lists union,
  more specific overrides numeric fields (e.g. `required_approvals`).
- `require_all_groups: true` means one approval from each listed group,
  not N-of-any.
- CODEOWNERS integration optional; when enabled, honored alongside team
  rules.
- Stale reviews dismissed on new commits (configurable, GitHub-only
  capability - declared via VCS capability flag).

`reeve rules explain <stack>` shows full resolution trace.

## Out of scope (v1)

- `break_glass` with `requires_incident_link` - dropped until a user asks.
  No runtime validation of incident links is specified.
