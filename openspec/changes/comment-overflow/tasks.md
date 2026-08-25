# Tasks

## Design gate

- [x] Approve part markers, the three split modes, and the delete capability.

## Implementation

- [x] Add `comments.overflow` to the schema: `mode`, `split`, `max_parts`.
- [x] Add `render.PartMarker(style, sha, part)`; part 1 byte-identical.
- [x] Render `part N of M` headers and part 1's forward link.
- [x] Implement the `divided`, `stack`, and `group` splits.
- [x] Fall back to the trim ladder for a single stack that cannot fit a part.
- [x] Add `DeleteCommentsByMarkerPrefix` to the VCS client and its interface.
- [x] Delete surplus parts when a run needs fewer than the last.
- [x] Wire preview, apply, and refresh through the split.
- [x] Add a CLI flag for every comments setting, not only the new ones.
- [x] Update the rendering spec, the config spec, and the docs.

## Verification

- [x] Test: a board that fits is byte-identical to today, one comment.
- [x] Test: part 1's marker is unchanged under both `replace` and `section`.
- [x] Test: every part is under the limit and well-formed, for each split mode.
- [x] Test: no stack's detail is split across two parts.
- [x] Test: `divided` balances rather than leaving a one-stack tail.
- [x] Test: `group` keeps a status group together, splitting it only if oversize.
- [x] Test: a single oversize stack trims without affecting other parts.
- [x] Test: a shrinking run deletes the surplus parts.
- [x] Test: a delete failure is logged and does not fail the run.
- [x] Test: `max_parts` caps the count and logs what it dropped.
- [x] Test: an unset flag leaves config alone; a set one overrides it.
- [x] Test: an unknown flag value is rejected by name.
- [x] Test: every board-rendering command carries every flag.
- [x] `mise run check` green.
