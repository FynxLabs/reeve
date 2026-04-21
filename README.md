# reeve

**PR-native, self-hosted GitOps orchestrator for Pulumi.** No control plane,
no vendor backend, no telemetry, no account — you own everything.

> Named after the medieval reeve: an official empowered to enforce rules and
> manage an estate on behalf of those who own it. A tool whose entire job is
> to enforce approval policy, manage locks, and act on infrastructure on
> behalf of the team — while owning none of it.

---

## What reeve does

`reeve` is a single Go binary you drop into your CI. On a PR it:

1. Runs `pulumi preview` for every stack touched by the changed files.
2. Posts a single PR comment with per-stack change counts and a collapsible
   plan — edited in place on every push.
3. Gates `/reeve apply` behind approvals, CODEOWNERS, required checks,
   up-to-date base, preview freshness, policy hooks, per-stack FIFO locks,
   and freeze windows.
4. Writes locks, run artifacts, and audit entries to **your** bucket (S3 /
   GCS / Azure Blob / R2 / local filesystem).
5. Acquires **short-lived federated credentials** (AWS OIDC, GCP WIF, Azure
   federated, GitHub App) per stack — reeve never stores long-lived secrets.
6. Detects drift on a schedule, classifies events (new / ongoing / resolved),
   and routes to Slack, PagerDuty, webhook, GitHub issues.
7. Emits OpenTelemetry traces and metrics to **your** collector.

Every arrow leaves your trust boundary. `reeve` holds nothing.

## What reeve is not

- **Not a SaaS.** No hosted offering, ever.
- **No telemetry.** No phone-home. The code does not contain the feature.
- **No account.** No login. No registration.
- **MIT.** Not a pivot-later license. Full stop.

---

## Quickstart

```bash
# Install via Homebrew
brew install thefynx/tap/reeve

# Or grab a signed release binary
curl -fsSL https://github.com/thefynx/reeve/releases/latest/download/reeve_linux_amd64.tar.gz | tar xz

# Verify
reeve --version
```

In your repo, create `.reeve/`:

```yaml
# .reeve/shared.yaml
version: 1
config_type: shared
bucket:
  type: s3
  name: mycompany-reeve
  region: us-east-1
approvals:
  default:
    required_approvals: 1
    approvers: ["@org/infra-reviewers"]
preconditions:
  require_up_to_date: true
  preview_freshness: 2h
```

```yaml
# .reeve/pulumi.yaml
version: 1
config_type: engine
engine:
  type: pulumi
  stacks:
    - pattern: "projects/*"
      stacks: [dev, staging, prod]
```

Add a GitHub workflow:

```yaml
# .github/workflows/reeve.yml
name: reeve
on:
  pull_request:
  issue_comment:
    types: [created]
permissions:
  contents: read
  pull-requests: write
  id-token: write # for OIDC federation
jobs:
  preview:
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: thefynx/reeve@v1
        with:
          command: preview
          pulumi-version: "3.231.0"
  apply:
    if: |
      github.event_name == 'issue_comment' &&
      startsWith(github.event.comment.body, '/reeve apply')
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: thefynx/reeve@v1
        with:
          command: apply
          pulumi-version: "3.231.0"
```

That's it. Open a PR and reeve will comment with the plan. Approve and
comment `/reeve apply` to apply.

## Local development

reeve uses [mise](https://mise.jdx.dev/) to pin Go and tooling versions:

```bash
mise install           # installs go, goreleaser, golangci-lint, openspec, hk
mise run check         # fmt + vet + lint + test
mise run demo          # runs reeve against examples/toy-stack
mise run build         # bin/reeve
```

Available tasks:

```bash
mise tasks             # list all tasks
mise run test          # go test -race ./...
mise run lint          # golangci-lint (enforces internal/core/* purity)
mise run release-check # goreleaser config validation
```

## Documentation

- [Getting started](docs/getting-started.md) — zero-to-PR-comment in 10 minutes
- [Configuration reference](docs/configuration.md) — every config_type
- [Auth providers](docs/auth.md) — OIDC/WIF/federated/secret managers
- [Drift detection](docs/drift.md) — schedules, sinks, bootstrap modes
- [Policy hooks](docs/policy-hooks.md) — OPA, Conftest, CrossGuard, custom
- [Self-hosting](docs/self-hosting.md) — bucket choice, GH App, scope
- [Design doc (retired)](docs/design/DESIGN.md) — the original vision
- [Spec](openspec/specs/) — authoritative per-capability behavior

## Architecture at a glance

```text
┌──────────────────────────────────────────────────────────────────┐
│                        CI Runner (GH Actions)                    │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │                       reeve (binary)                       │  │
│  │                                                            │  │
│  │   ┌──────────────────────────────────────────────────┐    │  │
│  │   │                   Pure Core                      │    │  │
│  │   │  stack discovery · rule resolver · lock FSM      │    │  │
│  │   │  precondition eval · comment render · redact     │    │  │
│  │   └──────────────────────────────────────────────────┘    │  │
│  │                         │                                  │  │
│  │   ┌─────────┬─────────┬─────────┬─────────┬─────────┬──────┐│  │
│  │   ▼         ▼         ▼         ▼         ▼         ▼      ▼│  │
│  │ IaC      VCS       Blob     Notify     Obs       Auth   Policy│
│  │(Pulumi)(GitHub) (S3/GCS)   (Slack)   (OTEL)    (OIDC)  (hooks)│
│  └────│────────│────────│────────│──────────│─────────│────────┘  │
└──────│────────│────────│────────│──────────│─────────│──────────-┘
       │        │        │        │          │         │
       ▼        ▼        ▼        ▼          ▼         ▼
   pulumi   GitHub    user's   Slack      user's    Cloud IAM
    CLI      API     bucket     API       OTEL     (federated)
                                         collector
```

## Status

**v1 shipped.** See [CHANGELOG](https://github.com/thefynx/reeve/releases) for
release history. See [openspec/changes/](openspec/changes/) for proposed work.

## Contributing

`reeve` uses [OpenSpec](https://openspec.dev/) for non-trivial changes. See
[CONTRIBUTING.md](CONTRIBUTING.md) for the workflow.

## License

[MIT](LICENSE). Will stay MIT. If a fork happens later, the original repo
stays MIT under its existing maintainers.
