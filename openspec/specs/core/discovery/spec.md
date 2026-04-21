# Stack Discovery

Seeded from DESIGN.md §6.4, §8.3.

## Split

- **Engine owns:** `EnumerateStacks(ctx, root)` returning `(project, path, name, raw)` tuples; `ValidateStack(ctx, stack)`.
- **Core owns:** pattern matching (doublestar), include/exclude filtering,
  change mapping, module dependency resolution, the orchestrating pipeline,
  and `reeve stacks` / `reeve stacks discover` CLI logic.

## Runtime behavior

Always explicit. reeve acts only on stacks that are either declared
literally or match a declared pattern. There is no runtime auto-discovery.

## Pattern language

Doublestar glob. `re:` regex escape hatch is **not shipped in v1** (cut
per plan appendix). Revisit only if a user files a concrete need.

## Pipeline

1. **Declare** — literal project entries and `pattern:` globs from engine config.
2. **Include** — if any include rules exist, only matching entries pass.
3. **Exclude** — drop matching entries.
4. **Resolve** — engine verifies each remaining stack exists.
5. **Map to changes** — stack is "affected" if changed files intersect its
   paths or declared dependencies.

## `reeve stacks discover`

Dev-time tool (not runtime). Walks the repo, clusters discovered stacks by
shared-prefix, generates suggested pattern entries. With `--write`, mutates
engine config via a comment-preserving YAML writer. Same command supports
modules via `reeve stacks discover --kind modules`.

The `reeve <engine> find` grammar from DESIGN.md is **not shipped** —
`reeve stacks discover --engine <e>` is the canonical surface.
