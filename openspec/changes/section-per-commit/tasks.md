# Tasks

## Design gate

- [x] Approve the per-SHA marker and the preview/apply board sharing.

## Implementation

- [x] Add `render.DashboardMarker(style, sha)`: per-SHA under `section`,
      unchanged under `replace`.
- [x] Render the per-SHA marker in the preview and apply bodies.
- [x] Upsert against it from the preview and apply run paths.
- [x] Retire `render.ApplyMarker`.
- [x] Update `openspec/specs/core/rendering/spec.md` and
      `docs/configuration.md`.

- [x] Replace the byte truncate with a ladder that removes whole units:
      engine output, diff, summaries, clamped errors, sections, table rows.
- [x] Report every rung through `render.Trim`; log what it reports.
- [x] Account for dropped sections and rows in the comment body.

## Verification

- [x] Test: under `section`, preview and apply of one SHA hit one marker.
- [x] Test: under `section`, two SHAs get two markers.
- [x] Test: under `replace`, the marker is byte-identical to today's.
- [x] Test: the retired apply marker is never written.
- [x] Goldens unchanged: replace-style output is byte-identical.
- [x] Test: every renderer stays under the limit across pathological shapes.
- [x] Test: a trimmed body never leaves an unclosed fence or `<details>`.
- [x] Test: dropped sections and rows are named in the body.
- [x] Test: dropped diffs, clamped errors, and untabled stacks reach the log.
- [x] `mise run check` green.
