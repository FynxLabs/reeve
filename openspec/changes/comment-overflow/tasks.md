# Tasks

## Design gate

- [ ] Approve part markers, the three split modes, and the delete capability.

## Implementation

- [ ] Add `comments.overflow` to the schema: `mode`, `split`, `max_parts`.
- [ ] Add `render.PartMarker(style, sha, part)`; part 1 byte-identical.
- [ ] Render `part N of M` headers and part 1's forward link.
- [ ] Implement the `divided`, `stack`, and `group` splits.
- [ ] Fall back to the trim ladder for a single stack that cannot fit a part.
- [ ] Add `DeleteCommentsByMarkerPrefix` to the VCS client and its interface.
- [ ] Delete surplus parts when a run needs fewer than the last.
- [ ] Wire preview, apply, and refresh through the split.
- [ ] Update the rendering spec, the config spec, and the docs.

## Verification

- [ ] Test: a board that fits is byte-identical to today, one comment.
- [ ] Test: part 1's marker is unchanged under both `replace` and `section`.
- [ ] Test: every part is under the limit and well-formed, for each split mode.
- [ ] Test: no stack's detail is split across two parts.
- [ ] Test: `divided` balances rather than leaving a one-stack tail.
- [ ] Test: `group` keeps a status group together, splitting it only if oversize.
- [ ] Test: a single oversize stack trims without affecting other parts.
- [ ] Test: a shrinking run deletes the surplus parts.
- [ ] Test: a delete failure is logged and does not fail the run.
- [ ] Test: `max_parts` caps the count and logs what it dropped.
- [ ] `mise run check` green.
