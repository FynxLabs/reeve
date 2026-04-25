# reeve documentation

| Guide | Covers |
|---|---|
| [Getting started](getting-started.md) | Zero-to-first-PR-comment walkthrough: install from source, create `.reeve/`, add the workflow, move to real storage, add OIDC, tighten approvals, enable drift. |
| [Configuration reference](configuration.md) | Every `.reeve/*.yaml` schema - `shared`, `engine`, `auth`, `notifications`, `observability`, `drift`, `user`. Token expansion, lint, migration. |
| [Auth providers](auth.md) | OIDC / WIF / federated / secret-manager / GitHub App / local-dev provider catalog, binding resolution, IAM setup recipes, fork-PR policy. |
| [Drift detection](drift.md) | Event lifecycle, bootstrap modes, schedules, sinks (Slack / webhook / PagerDuty / GitHub issues / OTEL annotation), metrics. |
| [Policy hooks](policy-hooks.md) | Generic command-hook model, OPA/Conftest/CrossGuard/custom recipes, exit-code semantics, redaction. |
| [Self-hosting](self-hosting.md) | Bucket provisioning (S3 / GCS / Azure / R2 / filesystem), GitHub Actions setup, GitHub App, distribution, failure modes. |

Start at [getting-started.md](getting-started.md) if you're new.

Authoritative per-capability behavior lives in [../openspec/specs/](../openspec/specs/) -
the docs here are user-facing, the specs are where implementation intent
is pinned.
