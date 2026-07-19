# Modularity contract — tasks

Documentation-only change; no code. On merge the delta folds into
`openspec/specs/architecture/spec.md`.

- [ ] Land the `architecture` capability spec (the contract).
- [ ] Adopt it as the review checklist for `notification-channels` and any new
      provider axis.
- [ ] Track the known current violations against their owning changes:
  - [ ] `auth/factory` + `blob/factory` static provider imports → `split-builds`.
  - [ ] PR-flow notifications off the channel abstraction → `notification-channels`.
  - [ ] drift `github_issue`/`factory` importing `go-github` directly →
        `notification-channels`.
