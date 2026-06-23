# PR Flow

## Overview

On PR open or update, reeve runs **preview** for every stack touched. A single
PR comment is posted and edited in place on subsequent runs. On `/reeve apply`
comment (or merge, depending on config), reeve acquires locks and runs **apply**.

## Pipeline

1. PR opened / updated → `reeve run preview` for touched stacks.
2. Single PR comment posted, identified by hidden HTML marker, edited in place
   on subsequent runs. Help comment upserted separately.
3. Slack message posted/updated (if configured).
4. If `auto_ready: true`, when PR converts from draft to ready for review and plan has
   succeeded, reeve fires `/reeve ready` automatically. Otherwise author runs it manually.
5. Reviewers approve per configured rules.
6. On `/reeve apply` comment → acquire locks, evaluate preconditions, run apply.
6a. Apply posts a per-run timeline comment, starting with `🚀 apply starting` and appending each event (see rendering spec).
7. Results update PR comment and Slack message.
8. Audit log entry written to bucket.
9. Locks released, queue advanced.

## Requirements

- Preview runs in parallel across stacks; apply serializes per-stack via locks.
- Preview artifacts persist under `runs/pr-{n}/{run-id}/` for the PR lifetime.
- Apply uses the saved plan from the most recent successful preview on the
  current HEAD SHA.
- Apply on **fork PRs** is **deny by default**. Opt-in per repo with documented
  risk; fork PRs otherwise get dry-run-only credentials.
- Notifications run last in the pipeline so upstream failures are captured
  accurately in the authoritative "what happened" surface.
- SHA resolution: `apply`, `ready`, and `approved` commands resolve the commit
  SHA from the PR HEAD via the VCS API (`GetPR`), not from `GITHUB_SHA`. This
  ensures manifests and plan lookups use the branch tip SHA regardless of what
  the CI runner checked out.
- Stacks declared with `path: .` (repo root) are triggered by any changed file
  that survives `ignore_changes` filtering.

## Already-applied guard

A fully-clean apply (no failed/blocked stacks) writes `runs/pr-{n}/applied/{sha}.json`. A later run at the same commit:

- **apply** - skips work, posts the ⏭️ timeline notice, exits success.
- **preview** - renders the plan with an "already applied" banner.
- `--force` - bypasses the guard on both; re-runs all side effects.

## Out of scope (v1)

- Auto-apply on merge without `/reeve apply` command (configurable later).
- Multi-engine runs in one PR (v1 is Pulumi only).
