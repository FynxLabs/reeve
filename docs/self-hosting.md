# Self-hosting

reeve is a single Go binary that runs inside your CI. There is no hosted
offering, no control plane, no "free tier with optional backend". This
guide covers what you need to stand up on your side of the trust
boundary.

## What you need

| Component | Purpose | Required? |
|---|---|---|
| Blob storage (S3 / GCS / Azure / R2) | locks, run artifacts, audit, drift state | yes |
| GitHub repo with Actions | reeve runs inside workflows | yes |
| IAM role trusting GitHub's OIDC provider | short-lived creds for IaC | strongly recommended |
| GitHub App | higher rate limits, cross-repo install | optional |
| Slack workspace + bot | PR-scoped notifications + drift sinks | optional |
| OTEL collector | traces + metrics | optional |
| PagerDuty / incident system | drift escalation | optional |

**What you never need:** a reeve server, a reeve SaaS account, a reeve
database, or any credential shared with the reeve maintainers. The
binary is hermetic.

## Scope of trust

Each arrow out of the reeve binary crosses **your** trust boundary:

```mermaid
flowchart LR
  Reeve["<b>reeve binary</b><br/><i>in your CI runner</i>"]

  Reeve -->|"same process"| Pulumi(["pulumi CLI"])
  Reeve -->|"GITHUB_TOKEN or GitHub App"| GitHub(["GitHub API"])
  Reeve -->|"your creds, your bucket"| Bucket[("S3 / GCS / Azure / R2")]
  Reeve -->|"your bot token"| Slack(["Slack API"])
  Reeve -->|"your endpoint"| OTEL(["OTEL collector"])
  Reeve -->|"federated, 1h max"| IAM(["Cloud IAM"])

  classDef reeve fill:#e0f2fe,stroke:#0369a1,stroke-width:2px,color:#000;
  classDef ext fill:#fafafa,stroke:#94a3b8,stroke-dasharray:3 3,color:#000;
  class Reeve reeve;
  class Pulumi,GitHub,Bucket,Slack,OTEL,IAM ext;
```

reeve never calls anything reeve-operated. There is nothing reeve-operated.

---

## Bucket provisioning

The bucket holds locks, run artifacts, audit entries, drift state, and
Slack message IDs. Typical lifetime cost is a few MB/month - this is
metadata, not plan bodies.

### AWS S3

```bash
aws s3api create-bucket \
  --bucket mycompany-reeve \
  --create-bucket-configuration LocationConstraint=us-east-1

aws s3api put-bucket-versioning \
  --bucket mycompany-reeve \
  --versioning-configuration Status=Enabled

aws s3api put-public-access-block \
  --bucket mycompany-reeve \
  --public-access-block-configuration \
  "BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true"

# Lifecycle for run artifacts (30d), audit (7y-ish)
aws s3api put-bucket-lifecycle-configuration \
  --bucket mycompany-reeve \
  --lifecycle-configuration file://lifecycle.json
```

`lifecycle.json`:

```json
{
  "Rules": [
    {
      "ID": "run-artifacts",
      "Status": "Enabled",
      "Filter": { "Prefix": "runs/" },
      "Expiration": { "Days": 30 },
      "NoncurrentVersionExpiration": { "NoncurrentDays": 7 }
    },
    {
      "ID": "drift-artifacts",
      "Status": "Enabled",
      "Filter": { "Prefix": "drift/runs/" },
      "Expiration": { "Days": 90 }
    },
    {
      "ID": "audit",
      "Status": "Enabled",
      "Filter": { "Prefix": "audit/" },
      "Transitions": [
        { "Days": 90, "StorageClass": "GLACIER" }
      ],
      "Expiration": { "Days": 2557 }
    }
  ]
}
```

IAM permissions for the reeve role:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "s3:GetObject", "s3:PutObject", "s3:DeleteObject",
      "s3:ListBucket", "s3:GetObjectVersion"
    ],
    "Resource": [
      "arn:aws:s3:::mycompany-reeve",
      "arn:aws:s3:::mycompany-reeve/*"
    ]
  }]
}
```

Config:

```yaml
# .reeve/shared.yaml
bucket:
  type: s3
  name: mycompany-reeve
  region: us-east-1
  prefix: reeve/         # optional - useful if you share a bucket
```

### GCS

```bash
gcloud storage buckets create gs://mycompany-reeve \
  --location=us --uniform-bucket-level-access
gcloud storage buckets update gs://mycompany-reeve --lifecycle-file=lifecycle.json
```

```yaml
bucket:
  type: gcs
  name: mycompany-reeve
```

### Azure Blob

```bash
az storage container create \
  --account-name mycompanyreeve \
  --name reeve \
  --auth-mode login
```

```yaml
bucket:
  type: azblob
  name: reeve                                              # container name
  region: https://mycompanyreeve.blob.core.windows.net     # service URL
```

### Cloudflare R2

R2 is S3-compatible; use `type: r2` (enables path-style + custom endpoint
via `AWS_ENDPOINT_URL_S3`):

```yaml
bucket:
  type: r2
  name: mycompany-reeve
```

Workflow:

```yaml
env:
  AWS_ACCESS_KEY_ID: ${{ secrets.R2_ACCESS_KEY_ID }}
  AWS_SECRET_ACCESS_KEY: ${{ secrets.R2_SECRET_ACCESS_KEY }}
  AWS_ENDPOINT_URL_S3: https://<account>.r2.cloudflarestorage.com
```

(R2 has no OIDC yet, so long-lived R2 keys are one of the few places
env vars are genuinely the only option. Lock them down to the reeve
bucket only.)

### Filesystem (dev only)

```yaml
bucket:
  type: filesystem
  name: ./.reeve-state
```

Good for `plan-run` and local smoke tests. **Not for CI** - Actions
runners start empty, so locks don't persist across runs.

---

## GitHub Actions setup

### Minimum permissions

```yaml
permissions:
  contents: read
  pull-requests: write      # upsert PR comment
  issues: write             # /reeve apply via issue_comment; github_issue drift sink
  id-token: write           # only when using aws_oidc / gcp_wif / azure_federated
```

### Event triggers

reeve expects these events:

- `pull_request` (`opened`, `synchronize`, `reopened`) - fires `preview`
- `issue_comment` (`created`, body starts with `/reeve apply`, `/reeve ready`, or `/reeve help`) - fires respective command
- `schedule` - fires `drift run`
- `workflow_dispatch` - manual re-runs

### GitHub App (optional but recommended for multi-repo)

- Rate limits: PATs/`GITHUB_TOKEN` cap at 5K req/hour. Apps get 15K per
  installation, independent of other workflows.
- Attribution: reeve's comments and audit entries show under the App's
  branded identity ("reeve-bot") instead of the workflow's implicit
  identity.
- Cross-repo: one App install covers many repos.

Register the App with:

- **Repository permissions:** Contents `read`, Issues `write`, Metadata
  `read`, Pull requests `write`, Checks `read`.
- **Subscribe to events:** Issue comment, Pull request.
- **Where can this App be installed?** Only on this account (keep it
  private unless you're publishing).

Wire in `.reeve/auth.yaml`:

```yaml
providers:
  github-app:
    type: github_app
    app_id: ${env:GITHUB_APP_ID}
    installation_id: ${env:GITHUB_APP_INSTALLATION_ID}
    private_key: ${env:GITHUB_APP_PRIVATE_KEY}
    permissions: ["contents:read", "issues:write", "pull_requests:write"]

bindings:
  - match: { stack: "**" }
    providers: [github-app]
```

The GitHub App provider emits `GITHUB_TOKEN` into the engine environment,
overriding the workflow's default token.

---

## Distribution

reeve has not cut a release. The only supported distribution today is
**build from source in CI** - clone `FynxLabs/reeve`, run
`go build ./cmd/reeve`, invoke the resulting binary.

`.goreleaser.yaml`, the Homebrew formula block, the GHCR image pipeline,
and the `cosign` signing steps are all wired up but haven't been run.
When the first release is cut, signed release tarballs + a container
image + a Homebrew tap will land; update this section at the same time.

---

## Upgrading

### Binary

Until releases exist: `git pull` the reeve repo and rebuild. CI jobs
that build from source pick up the new SHA on their next run.

### Config schema

Schemas are versioned per-file (not globally). When reeve ships a new
schema version:

```bash
reeve migrate-config --dry-run   # preview changes
reeve migrate-config             # writes + keeps *.bak backups
```

Only files whose `config_type` has a migration land are touched.

---

## Monitoring reeve itself

### Runs failing silently?

Every CI run writes a run manifest to `runs/pr-<n>/<run-id>/manifest.json`.
Tail them with whatever bucket-event tooling you have (S3 EventBridge,
GCS Pub/Sub). An absence of run manifests on expected PRs means the
workflow itself didn't fire - check Actions.

### Drift backlog growing?

```bash
reeve drift status                  # all stacks
reeve drift status --stack prod/*   # specific
```

Or watch the `reeve.drift.stacks_in_drift{env="prod"}` gauge if you've
wired OTEL.

### Lock contention

```bash
reeve locks list                    # shows holder + queue depth
reeve locks explain <project/stack> # detail for one stack
```

Long queue depths on a stack indicate apply contention - usually a
symptom of too-coarse stack granularity or PRs that take too long to
merge after `/reeve apply`.

### Audit trail

Every apply writes to `audit/<year>/<month>/<day>/<run-id>.json`,
write-once (If-None-Match on create). Ship these to your SIEM with
the same bucket-event tooling.

Schema: see [`internal/audit/audit.go`](../internal/audit/audit.go).
Stable within a major version.

---

## Failure modes

### Bucket unavailable mid-apply

reeve writes lock state → apply runs → writes result. If S3 goes away
between the first two steps, the lock may be held indefinitely from
reeve's perspective. The opportunistic reaper cleans up based on TTL;
wait the configured TTL (default 4h), or use `reeve locks explain` +
`reeve locks reap` once the bucket is back.

### Clock skew

Lock TTL uses server-side timestamps (S3 `LastModified`, GCS `updated`)
when available. The filesystem adapter uses local `time.Now()` and warns
if `acquired_at` drift exceeds 60s. For cloud adapters, TTL accuracy is
the bucket's clock accuracy.

### Fork PRs

Deny-by-default. See [auth.md](auth.md#fork-pr-policy) for the security
rationale and opt-in procedure.

### Supply chain

reeve depends on Go modules, the Pulumi CLI, and cloud SDKs. The
`go.sum` + release checksums pin exactly what goes into the binary.
Vendor the modules (`go mod vendor`) and pin the Pulumi CLI version
in your workflow if you need to cut the supply chain further.

---

## FAQ

**Why no control plane?** Because every "just a small control plane for
X" decision compounds into exactly what reeve is trying to avoid
becoming. Nothing hosted, ever - including a "free tier API".

**What if I want telemetry for usage analytics?** You can add it
yourself in a fork. The upstream code does not contain the feature,
not as a toggle and not as a hook point. If the Slack-style "opt-in
data sharing" ever gets proposed upstream, the proposal will be
rejected.

**What about support?** GitHub issues, best effort. No SLA, no paid
support tier. Pull requests with tests are the fastest path to seeing
fixes.

**Can I relicense my fork?** MIT lets you do anything, including
relicensing a fork. The upstream repo stays MIT under its existing
maintainers.
