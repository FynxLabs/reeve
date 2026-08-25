# PR Comment Rendering

## Single comment, edited in place

Identified by hidden HTML marker (`<!-- reeve:pr-comment:v1 -->`). VCS
adapter's `UpsertComment` handles find-or-create via marker match. If the
VCS adapter reports `CommentCapabilities.SupportsEdit == false`, append
fallback kicks in - out of scope until a non-GitHub adapter ships.

## Layout

Header line: `## <status-icon> reeve · <op> · run #<n> · [commit <sha>]`
followed by total counts, duration, and a link to the CI run.

Table summarizing all affected stacks with columns:
`Stack | Env | Add | Change | Delete | Replace | Status`.

Per-stack sections below the table - status-grouped sort order (blocked,
ready, no-op last), each with: required approvers (if any), then collapsed
`<details>` for Summary and Full plan output. No-op stacks collapse to a
single table line with no section.

A help comment is upserted separately under marker `<!-- reeve:help -->`,
listing available commands. A ready comment is upserted under
`<!-- reeve:ready -->` when `/reeve ready` is triggered (manually or via `auto_ready`).

Apply comment mirrors preview structure, adds durations, floats failures
to top. Apply writes the same marker preview wrote for that commit, so a
commit has one board.

## `comments.style`

Controls how reeve posts dashboard comments. Three modes:

- `replace` (default) upserts one comment per PR under
  `<!-- reeve:pr-comment:v1 -->`. Every operation edits it.
- `section` upserts one comment per commit SHA, under
  `<!-- reeve:pr-comment:v1:<short-sha> -->`. Preview and apply of one SHA share
  that comment; a new SHA mints a new one, and a previous SHA's comment is never
  edited again, so the plan it recorded stays readable.
- `append` posts a new comment every run without editing the previous one.

`section` does not split by operation. The marker `<!-- reeve:apply:v1 -->` is
retired; comments already posted under it are left in place.

## `comments.stack_view`

Controls which stacks the table lists:

- `all` (default) - every declared stack, no-ops included.
- `changed` - only stacks with planned/applied changes.

Per-stack sections always skip no-ops regardless of view.

## Apply timeline

Each commit owns one comment, pinned by a per-commit marker
(`<!-- reeve:apply-timeline:<short-sha> -->`). Every run of that commit - the
first apply, a retry, a `--force` re-apply - appends to the same thread and
edits the comment in place rather than posting a new one. Entries are persisted
per commit (compare-and-swap) so concurrent runs never lose each other's
history, and the header shows the latest run to touch the commit. Because
editing a comment is silent while creating one fires an `issue_comment` webhook,
consolidating per commit also stops reeve from spawning a fresh (self-trigger
guard-skipped) workflow run for every progress update.

```
### 🚀 reeve · apply · [run #N](<url>) · [commit <sha>]
- 🚀 **apply starting**
- ✅ **applied**: 2 stack(s): api/prod, worker/prod
```

- 🚀 `apply starting` - posted before any stack runs.
- ✅ `applied` - changed stack refs.
- 🔴 `failed` - failing stack refs.
- 🔒 `blocked` - gate reason.
- ⏭️ `skipped` - commit already applied, or docs/asset-only changes.
- 📡 `scope broadened` - unmapped files; applying all stacks.

Separate from the replace-style dashboard comment.

## Size-limit trimming

A comment over GitHub's 65,536-char limit drops its heaviest per-stack content:
full engine output first, then the per-stack diff. Dropping full preview output
alone is silent, because the diff reviewers read is intact.

Once content a reviewer reads is dropped, the comment carries a note pointing at
the CI run, and the dropped content is written to the run log. The note must not
name an output that does not hold it.

## Safety rails

- Secrets marked by Pulumi `[secret]` are redacted before render.
- All rendered output funnels through `internal/core/redact` - no output
  path bypasses redaction.
- Replacement counts > 0 trigger a prominent warning block.

## Sort orders

- `status_grouped` (default): blocked → ready → no-op.
- `alphabetical`: by `{project}/{stack}`.
- `env_priority`: configured priority order (e.g. `prod > staging > dev`).

## Testing

Golden files. Every rendering change requires a new golden file + diff
review.
