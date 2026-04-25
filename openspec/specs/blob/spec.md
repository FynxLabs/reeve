# Blob Storage

## Responsibility

Locks, run artifacts, drift state, audit logs. The user owns the bucket -
reeve never sees the data after delivery.

## Adapters (v1)

S3, GCS, Azure Blob, R2, filesystem. Filesystem is also the test harness
for all core components.

## Layout

```
<bucket>/reeve/
├── locks/
│   └── {project}/{stack}.json
├── runs/
│   └── pr-{number}/{run-id}/
│       ├── manifest.json
│       ├── {project}-{stack}/
│       │   ├── preview.json
│       │   ├── plan.bin
│       │   ├── summary.json
│       │   └── stdout.log
│       └── latest -> {run-id}
├── drift/
│   ├── runs/{run-id}/
│   │   ├── manifest.json
│   │   ├── results/{project}-{stack}.json
│   │   └── report.md
│   ├── state/{project}/{stack}.json
│   └── suppressions/{project}/{stack}.json
├── notifications/pr-{number}/slack.json
└── audit/{year}/{month}/{day}/{run-id}.json
```

## Conditional writes

All adapters must implement atomic conditional writes (If-Match on ETag,
GCS generation preconditions, filesystem flock+rename). `ErrPreconditionFailed`
signals "someone else got there first" - lock state machine re-reads.

## Retention

Runs: 30d default (configurable). Audit: 7y default. Locks: indefinite.
On PR close/merge, run artifacts move to `closed/` prefix with shorter
retention.

## Failure modes

Each adapter's implementation specifies: behavior on mid-operation bucket
unavailability, recovery procedure, and manual intervention steps.
