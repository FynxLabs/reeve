# Security policy

## Reporting a vulnerability

**Do not open a public GitHub issue for security reports.**

Use GitHub's private vulnerability reporting:
<https://github.com/FynxLabs/reeve/security/advisories/new>.

Expect an acknowledgement within 72 hours. We aim to triage and respond
with a remediation plan within 7 days of acknowledgement.

## Scope

In scope:

- The `reeve` binary and every package under `internal/` and `cmd/`.
- The `action.yml` GitHub Action.
- Release tooling (goreleaser config, signing).
- Auth provider adapters (`internal/auth/providers/*`) — especially
  anything involving credential exchange, token handling, or privilege
  escalation.
- Redaction pipeline (`internal/core/redact`) — a bypass that leaks
  secrets through stdout, audit logs, or telemetry is in scope.
- Lock state machine (`internal/core/locks` + `internal/blob/locks`) —
  concurrent-write bugs that let two PRs hold the same lock are in scope.

Out of scope:

- Issues in third-party dependencies (report upstream; we'll track via
  Renovate / Dependabot and bump when fixes land).
- Social engineering, phishing, physical attacks.
- Denial-of-service against GitHub APIs via configured reeve behavior
  (configure rate limits appropriately).
- User-side misconfiguration that doesn't violate documented invariants
  (e.g. pointing reeve at a world-writable bucket, opting into
  `env_passthrough` with long-lived secrets).

## Our posture

- **Zero-trust auth by default.** Federated credentials (OIDC, WIF,
  Azure federated) acquire 1-hour tokens. Long-lived secrets are a
  flagged opt-in (`env_passthrough` with
  `i_understand_this_is_dangerous: true`).
- **No telemetry.** reeve emits OpenTelemetry traces/metrics only when
  `observability.yaml` is present and enabled, and only to endpoints
  the user configures. reeve never phones home.
- **Fork PR deny-by-default.** Fork PRs receive dry-run-only
  credentials. Opt-in via `shared.yaml: apply.allow_fork_prs: true` is
  an explicit, documented risk.
- **All user-visible output runs through `internal/core/redact`.**
  Credential literals are registered with the redactor at acquire time
  — leaks through engine stdout are scrubbed.
- **Audit log is write-once.** Entries are created with
  `If-None-Match` preconditions. Overwrites are rejected.

## Supply-chain controls

- **Release signing (planned, not shipped yet).** `.goreleaser.yaml`
  wires sigstore/cosign keyless signing for when the first release is
  cut. Pre-release, reeve is built from source only.
- **Pinned third-party GitHub Actions.** The `.github/workflows/security.yml`
  job `action-pin-check` fails CI if a third-party action is referenced
  by tag instead of SHA.
- **Vulnerability scanning on every PR:**
  - `govulncheck` — Go standard library + module CVE database.
  - `gosec` — static Go security analyzer (SARIF → GitHub code scanning).
  - CodeQL — `security-extended` and `security-and-quality` query packs.
  - `actions/dependency-review-action` — block PRs that introduce HIGH+
    CVEs in dependencies.
- **Renovate auto-updates** — weekly PRs for module + action bumps;
  vulnerability alerts get their own `security` label.
- **No external network calls at test time.** Core tests use an
  in-memory filesystem blob adapter and stubbed VCS clients.

## Local pre-commit / pre-push

reeve uses [hk](https://hk.jdx.dev/) for git hooks:

- `pre-commit`: `go fmt`, `go vet`, `golangci-lint run --fix`
- `pre-push`: `go test -race`, `govulncheck`, `gosec`

Install by running `mise install` (the `postinstall` hook wires hk).

Run the full gate manually:

```bash
mise run check          # fmt + vet + lint + vuln + sec + test
hk run check            # same, via hk
```

## Coordinated disclosure

We prefer coordinated disclosure with a 90-day default embargo. If the
issue is being actively exploited, we will ship a fix as soon as
practical and coordinate disclosure timing with the reporter.

We do not currently offer a bug bounty. Acknowledgement in release
notes and the security advisory is offered with the reporter's consent.
