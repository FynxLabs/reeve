# Preconditions

## Evaluation

Ordered, fail-fast. Each gate emits a structured result consumed by the PR
comment renderer. All gates shown in comment regardless of which failed.

1. Branch up-to-date with base.
2. Required checks green.
3. Fresh preview exists for current HEAD SHA (within `preview_freshness`).
4. Preview succeeded (no errors).
5. Policy passed (all blocking policy hooks exit 0).
6. Approvals satisfied for this specific stack.
7. Lock acquirable.
8. Not in freeze window (if configured).

## Fork PR gate

If PR is from a fork, apply is denied unless the repo explicitly opts in.
Opt-in documented per-repo; otherwise dry-run-only credentials are used for
preview and apply is refused with a clear message in the PR comment.

## Configuration

Lives in `shared.yaml` `preconditions.*`:
- `require_up_to_date: true`
- `require_checks_passing: true`
- `preview_freshness: 2h`
- `preview_max_commits_behind: 5`
