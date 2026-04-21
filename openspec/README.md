# OpenSpec

Spec-driven development for reeve, per [openspec.dev](https://openspec.dev/).

## Layout

- `specs/` — source of truth per capability. Edit freely; reviewed like code.
- `changes/` — active proposals under discussion, one folder per change.
- `changes/archive/YYYY-MM-DD-<name>/` — archived on merge, dated.
- `config.yaml` — project config.

## Workflow

Every non-trivial feature or behavior change is a proposal in `changes/<name>/`:

```
changes/<name>/
├── proposal.md       # why, what, scope
├── design.md         # technical approach
├── tasks.md          # implementation checklist
└── specs/
    └── <capability>/spec.md   # ADDED / MODIFIED / REMOVED deltas
```

On merge, the proposal archives and its delta specs fold into `specs/`.

Small fixes (typos, obvious bugs) can skip the proposal.

## Seeded from DESIGN.md

`specs/` was seeded on 2026-04-20 from `docs/design/DESIGN.md` (the retired
design doc). See that file for the original full-v1 vision. Authoritative
per-capability behavior now lives here.
