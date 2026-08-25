# Design

## The marker

`section` renders `<!-- reeve:pr-comment:v1:{shortsha} -->`. It extends the
dashboard marker rather than opening a new namespace, so a reader scanning raw
comment source sees one family.

`replace` keeps `<!-- reeve:pr-comment:v1 -->` byte-identical. That matters: a
PR that has been running under `replace` must keep having its board edited, not
orphaned by a marker change.

Short SHA, matching the timeline. Seven characters is what the header already
prints, so the marker and the visible header agree.

## Why the SHA and not the run

A retried CI run is the same intent against the same code, and its board should
correct the failed attempt rather than sit beside it. Keying on the run number
would leave a flaky retry as two boards for one commit, which is the reading
problem this change exists to remove.

An explicit re-plan of an unchanged SHA does share a board under this rule. The
timeline already mints a series for that case and is the right place for it; the
dashboard's job is current state per commit.

## Preview and apply share the board

Under the old rule apply had its own global marker, so the apply result never
touched the preview board. Under a per-SHA board they are the same comment: the
apply overwrites its own commit's board with the apply render.

That is the intent - the board is the current state of that commit - and it is
why the previous commit's board must stay untouched. The plan is preserved by
the SHA boundary, not by the operation boundary.

## Callers

`render.Preview`, `render.Apply`, and the run pipeline already carry the commit
SHA and the style. A `render.DashboardMarker(style, sha)` helper keys both
callers off one place, so preview and apply cannot drift onto different markers
for the same commit.

## Alternatives rejected

Keep the operation split and stop apply from overwriting preview. Does not help:
the preview board is still overwritten by the next preview, and the reader still
holds two boards for a PR with ten runs.

Post a fresh comment per run (`append`'s behavior) under `section`. Creating a
comment fires an `issue_comment` webhook where editing is silent, which is what
drove the timeline to consolidate per commit.
