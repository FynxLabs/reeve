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

Every `comments` setting MUST have both a CLI flag and a config key. The flags
are `--comment-sort`, `--comment-stack-view`, `--comment-style`,
`--comment-show-gates`, `--comment-collapse-threshold`, `--comment-overflow`,
`--comment-overflow-split`, and `--comment-overflow-max-parts`.

A flag MUST override config only when it was actually given, so an untouched
flag cannot overwrite config with its own default. A value outside a flag's
allowed set MUST be rejected by name rather than silently treated as the
default. A flag given with no shared config to override MUST be an error, not
ignored.

Every command that renders a board MUST accept every flag.
