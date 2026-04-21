# Configuration

Seeded from DESIGN.md §8.

## Layout

```
.reeve/
├── shared.yaml           # approvals, locking, bucket, freeze, comments
├── auth.yaml             # credential providers and bindings
├── notifications.yaml
├── observability.yaml
├── drift.yaml
├── pulumi.yaml           # engine: pulumi
└── terraform.yaml        # engine: terraform (future)
```

Single-file `reeve.yaml` at repo root supported for simple cases. If
`.reeve/` exists, root `reeve.yaml` is ignored (ambiguity error in lint).

## File convention

Every file starts with:

```yaml
version: 1
config_type: <type>
```

`config_type` values (v1): `shared`, `engine`, `auth`, `notifications`,
`observability`, `drift`, `user`.

Exactly one file per `config_type`, except `engine` (multiple engines
allowed, each with unique `engine.type`).

## Validation

- Strict unmarshal — unknown keys are errors.
- Schema validation per `config_type` against Go structs in
  `internal/config/schemas/`.
- `version` is per-file — breaking changes to any schema bump only that
  file's version. Migration handled by `reeve migrate-config` (Phase 10).

## User config

`~/.config/reeve/*.yaml` holds local-only preferences (`config_type: user`).
No overlap with repo config fields. CLI flags override user config overrides
defaults, per parity rule (DESIGN.md §2.8).

`user.yaml` in v1 carries only rendering and local-auth preferences.
Single-field concerns are kept to env vars.

## CLI / config parity

Every runtime behavior has both a CLI flag and a config setting.
Flag-only exceptions are genuinely ephemeral (`--dry-run`, `--verbose`,
`--explain`). No config-only behaviors.
