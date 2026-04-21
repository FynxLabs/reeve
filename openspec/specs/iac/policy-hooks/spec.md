# Policy Hooks

Seeded from DESIGN.md §6.6, §8.9.

## Model

Generic command-execution hooks. reeve runs user-specified commands against
plan JSON and treats exit codes as pass/fail. Engine-agnostic; supports any
policy system (OPA, Conftest, CrossGuard, Sentinel, custom scripts).

## Config

Inside engine config:

```yaml
policy_hooks:
  - name: opa-compliance
    command: ["conftest", "test", "--policy", "policies/", "{{plan_json}}"]
    on_fail: block              # block | warn
    required: true

  - name: crossguard
    command: ["pulumi", "policy", "validate", "policies/aws-compliance"]
    on_fail: block
    required: false             # skip silently if command not present

  - name: cost-check
    command: ["./scripts/cost-gate.sh", "{{plan_json}}"]
    on_fail: warn
```

## Placeholders

- `{{plan_json}}` — path to the plan JSON reeve wrote.
- `{{stack_name}}`, `{{project}}`, `{{env}}` — current stack context.

## Exit code semantics

- `0` — pass.
- non-zero + `on_fail: block` — apply gate fails; stdout surfaces in PR
  comment.
- non-zero + `on_fail: warn` — warning in PR comment; apply proceeds.
- `required: false` + command not present — skip silently.

## Stdout safety

All stdout captured from hooks passes through `internal/core/redact` before
appearing in the PR comment, audit log, or any user-visible surface. No
redaction bypass.

## No dedicated config_type

Hooks live in engine config. Cross-engine OPA policies are templated into
multiple engine configs if needed. A dedicated `config_type: policy` is
not justified.
