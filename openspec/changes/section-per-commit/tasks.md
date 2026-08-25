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

## Verification

- [x] Test: under `section`, preview and apply of one SHA hit one marker.
- [x] Test: under `section`, two SHAs get two markers.
- [x] Test: under `replace`, the marker is byte-identical to today's.
- [x] Test: the retired apply marker is never written.
- [x] Goldens unchanged: replace-style output is byte-identical.
- [x] `mise run check` green.
