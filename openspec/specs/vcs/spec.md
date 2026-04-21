# VCS Abstraction

Seeded from DESIGN.md §6.5.

## Design target

GitHub first; moderate future-proofing for GitLab. Interfaces abstract
enough to extend without pressure-testing against GitLab until its adapter
lands.

## Key interfaces (use-site)

```go
type PRReader interface {
    GetPR(ctx context.Context, number int) (*PR, error)
    ListChangedFiles(ctx context.Context, number int) ([]string, error)
    ListOpenPRsTouchingPaths(ctx context.Context, paths []string) ([]PR, error) // drift
}

type CommentPoster interface {
    UpsertComment(ctx context.Context, number int, body string, marker string) error
    Capabilities() CommentCapabilities
}

type CommentCapabilities struct {
    SupportsEdit bool
}

type CodeownersProvider interface {
    ResolveOwners(ctx context.Context, changedFiles []string) (map[string][]string, error)
}
```

## Auth

- Phase 1: `GITHUB_TOKEN` from GitHub Actions.
- Phase 4: GitHub App (`github_app` auth provider); both remain supported.

## Capabilities

VCS-specific capabilities (e.g. `stale_review_dismissal`) exposed via
capability flags, never hard-coded in core.

## Fork PRs

`PR.IsFork` must be populated by every adapter. Downstream gates (apply,
credentialed runs) consume this.
