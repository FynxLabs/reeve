# Approvals

## Sources

Pluggable behind the `approvals.Source` interface. v1 ships:

- `pr_review` - reads PR reviews via the VCS adapter (always on).

Future (v2+): `pr_comment` (`/reeve approve`), `slack_interaction`,
`webhook`. Each is a new source implementation - no core changes. There is
no `approvals.sources:` config surface until a second source exists.

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
