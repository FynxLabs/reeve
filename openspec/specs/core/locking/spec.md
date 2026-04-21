# Locking

## Granularity

Per-stack. Acquired at apply, not preview — previews are parallel-safe.

## Storage

Single JSON object per stack: `locks/{project}/{stack}.json`. Atomic
transitions via conditional writes:
- S3: `If-Match` on ETag
- GCS: generation preconditions
- Azure Blob: `If-Match` on ETag
- Filesystem: `flock` + fsync + rename

## Queue

FIFO. Queue entries visible in PR comment and via `reeve locks list`.
"Blocked by PR #X" surfaces in comments on both the waiting PR and the
holder PR.

## TTL & Reaper

Default TTL: 4h. Configurable per `shared.yaml` `locking.ttl`.

**Reaper is opportunistic** — there is no daemon. Every `reeve` invocation
scans `locks/` for expired TTLs before acquiring. Quiet repos may run an
optional scheduled GH Actions workflow (`reeve locks reap`) to sweep.
No control plane.

## Release triggers

- PR merged → release all locks held for this PR.
- PR closed unmerged → release all locks held for this PR.
- TTL expiry (opportunistic reaper).
- Manual `/reeve unlock` (admin per `shared.yaml` `locking.admin_override`).

## Fairness

FIFO with TTL provides bounded wait. Load-test fixture (Phase 2) verifies
no starvation under churn.

## Failure modes

- Mid-apply blob unavailability: apply fails, lock state is indeterminate.
  Next invocation detects via TTL or explicit admin override.
- Clock skew: adapters prefer server-side timestamps (S3 `LastModified`,
  GCS `updated`) over local clock; filesystem adapter emits drift warning
  if local clock disagrees with lock `acquired_at` by > 60s.
