# Getting started

Zero to PR-comment in ten minutes. This guide walks you through the minimum
setup: one Pulumi project, one stack, a filesystem bucket (no cloud yet),
and a GitHub Actions workflow that opens a PR and comments on it.

For cloud-native setups (S3 locks, OIDC federation, multi-stack monorepo)
see [configuration.md](configuration.md) and [auth.md](auth.md).

## Prerequisites

- A GitHub repo with a Pulumi project. The [`examples/toy-stack/`](../examples/toy-stack/)
  in this repo is a working one — fork and start from there if you want.
- GitHub Actions enabled.
- Optional: a running Pulumi backend. The toy stack uses the `random`
  provider so no cloud credentials are needed.

## 1. Install reeve locally

reeve is pre-release. No published binary, no Homebrew tap, no container
image. Build from source:

```bash
git clone https://github.com/FynxLabs/reeve
cd reeve
mise install           # go + tooling (go, golangci-lint, govulncheck, gosec, hk)
go build -o ./bin/reeve ./cmd/reeve
./bin/reeve --help
```

Put `./bin/reeve` on your `$PATH` or invoke it directly.

## 2. Create `.reeve/`

At the repo root, create two files.

**`.reeve/shared.yaml`** — bucket, approvals, preconditions:

```yaml
version: 1
config_type: shared

bucket:
  type: filesystem
  name: ./.reeve-state           # local dir for quick iteration

approvals:
  sources:
    - type: pr_review
      enabled: true
  default:
    required_approvals: 1
    approvers: ["@your-org/infra-reviewers"]
    dismiss_on_new_commit: true

preconditions:
  require_up_to_date: true
  require_checks_passing: true
  preview_freshness: 2h

apply:
  trigger: comment
  command: "/reeve apply"
  allow_fork_prs: false          # deny-by-default; flip with care
```

**`.reeve/pulumi.yaml`** — engine + stack declarations:

```yaml
version: 1
config_type: engine

engine:
  type: pulumi
  binary:
    path: pulumi

  stacks:
    - pattern: "projects/*"      # globs are doublestar
      stacks: [dev, staging, prod]

  change_mapping:
    ignore_changes:
      - "**/*.md"
      - "**/node_modules/**"

  execution:
    max_parallel_stacks: 2
    preview_timeout: 10m
```

## 3. Verify locally

```bash
reeve lint                    # strict YAML check + cross-file validation
reeve stacks                  # prints the declared-and-enumerated stacks
reeve rules explain prod/api  # shows merged approval rules for one stack
reeve plan-run --sha $(git rev-parse HEAD) --run-number 1
```

`plan-run` renders the PR comment to stdout. No cloud calls, no GitHub calls,
no external services. Filesystem artifacts land under `.reeve-state/`.

## 4. Add the GitHub Actions workflow

**`.github/workflows/reeve.yml`**:

```yaml
name: reeve

on:
  pull_request:
    types: [opened, synchronize, reopened]
  issue_comment:
    types: [created]

permissions:
  contents: read
  pull-requests: write
  issues: write
  id-token: write

concurrency:
  group: reeve-${{ github.event.pull_request.number || github.event.issue.number }}
  cancel-in-progress: false

jobs:
  reeve:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: FynxLabs/reeve@main
        with:
          pulumi-version: latest
          slack-token: ${{ secrets.SLACK_BOT_TOKEN }}
```

That's it. The action auto-detects the command from the event:

- `pull_request` event → `reeve run preview`
- `issue_comment` with `/reeve ready` → `reeve run ready`
- `issue_comment` with `/reeve apply` → `reeve run apply`
- Any other comment → silent no-op

Open a PR. reeve posts a comment within ~30 seconds showing the plan for
every stack touched by the changed files.

## 5. Move the bucket to real storage

Filesystem buckets work great for smoke tests but every CI run starts fresh,
so lock state is lost. Switch to S3 / GCS / Azure Blob before enabling
`apply`.

Change `.reeve/shared.yaml`:

```yaml
bucket:
  type: s3                 # or gcs | azblob | r2
  name: mycompany-reeve
  region: us-east-1
```

Commit, push, and the next PR run will write locks and artifacts to the
real bucket. See [self-hosting.md](self-hosting.md) for bucket provisioning
recipes.

## 6. Add federated auth for the engine

When you move from the toy stack to real infrastructure, you need short-lived
cloud credentials for `pulumi apply` to run. See [auth.md](auth.md) —
the three-minute version:

**`.reeve/auth.yaml`**:

```yaml
version: 1
config_type: auth

providers:
  aws-prod:
    type: aws_oidc
    role_arn: arn:aws:iam::111111111111:role/reeve-prod
    region: us-east-1
    duration: 1h

bindings:
  - match: { stack: "prod/*" }
    providers: [aws-prod]
```

Set up the AWS IAM role to trust GitHub's OIDC provider for
`token.actions.githubusercontent.com` with `aud=sts.amazonaws.com` and a
sub-claim matching your repo. See [auth.md#aws-oidc](auth.md) for the
trust-policy template.

## 7. Add approvals and locks

Tighten approvals for production in `.reeve/shared.yaml`:

```yaml
approvals:
  default:
    required_approvals: 1
    approvers: ["@your-org/infra-reviewers"]
  stacks:
    "prod/*":
      required_approvals: 2
      approvers: ["@your-org/sre", "@your-org/security"]
      require_all_groups: true    # one from each group, not 2-of-any

locking:
  ttl: 4h                         # opportunistic reaper cleans up expired locks
  queue: fifo
```

`reeve locks list` inspects the live state. `reeve locks explain <stack>`
shows holder + queue. `reeve rules explain <stack>` shows the merged rule
resolution.

## 8. Turn on drift detection

Separate workflow, scheduled, uses a read-only IAM role:

**`.github/workflows/drift.yml`**:

```yaml
name: drift
on:
  schedule:
    - cron: "17 */4 * * *"   # every 4 hours, off the hour
  workflow_dispatch:

permissions:
  contents: read
  id-token: write            # OIDC for the read-only role
  issues: write              # for github_issue drift sink

jobs:
  drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with: { repository: FynxLabs/reeve, path: _reeve }
      - uses: actions/checkout@v6
        with: { path: _src }
      - uses: actions/setup-go@v6
        with: { go-version-file: _reeve/go.mod }
      - run: go build -o /usr/local/bin/reeve ./cmd/reeve
        working-directory: _reeve
      - uses: pulumi/actions@v6
        with: { pulumi-version: "3.231.0" }
      - run: reeve drift run --schedule prod
        working-directory: _src
        env:
          GITHUB_TOKEN: ${{ github.token }}
```

Configure schedules + sinks in `.reeve/drift.yaml` — see [drift.md](drift.md).

## Troubleshooting

- **`pulumi: executable file not found`** — install Pulumi via
  `pulumi/actions@v6` before running reeve in the same job.
- **Comment keeps duplicating instead of editing in place** — reeve finds
  its comment by the hidden HTML marker `<!-- reeve:pr-comment:v1 -->`. If
  someone manually edited the comment and stripped the marker, reeve will
  post a new one.
- **`apply` says "fork PR — apply denied"** — expected. Fork PRs get
  dry-run-only credentials by default. Opt in with
  `shared.yaml: apply.allow_fork_prs: true` if you've thought about the
  supply-chain risk.
- **OIDC token exchange fails locally** — `aws_oidc`/`gcp_wif`/
  `azure_federated` only work inside GitHub Actions (they need the
  `ACTIONS_ID_TOKEN_REQUEST_URL` env var). Use `aws_profile` / `aws_sso` /
  `gcloud_adc` for local development.

## Next steps

- [configuration.md](configuration.md) — full schema for every `.reeve/*.yaml` file
- [auth.md](auth.md) — every provider type, plus GitHub App setup
- [drift.md](drift.md) — schedules, event lifecycle, sink catalog
- [policy-hooks.md](policy-hooks.md) — wiring OPA / Conftest / CrossGuard
- [self-hosting.md](self-hosting.md) — bucket provisioning, scope, distribution
