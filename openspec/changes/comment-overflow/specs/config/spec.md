# Config delta

## ADDED Requirements

### Requirement: `comments.overflow`

`comments.overflow` controls what happens when a board exceeds GitHub's comment
size limit.

- `mode`: `drop` (default) trims content as today; `continue` spills the board
  into further comments.
- `split`: `divided` (default), `stack`, or `group`. Ignored under `drop`.
- `max_parts`: maximum comments one board may occupy. Ignored under `drop`.

`drop` MUST remain the default. Posting several comments on a pull request is a
visible behavior change and MUST be opted into.

Every setting MUST have both a CLI flag and a config key.
