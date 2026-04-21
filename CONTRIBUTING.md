# Contributing to reeve

## License

MIT. By contributing you agree your changes ship under MIT. We will never
relicense reeve. See `LICENSE`.

## Development workflow: OpenSpec

reeve is developed using [OpenSpec](https://openspec.dev/). Specs live in
`openspec/`; the original design doc (now retired) is at
`docs/design/DESIGN.md`.

### Small fixes

Typos, obvious bugs, minor refactors: PR directly with tests.

### Non-trivial changes

Every feature or behavior change goes through a proposal in
`openspec/changes/<name>/`:

```
openspec/changes/<name>/
├── proposal.md       # why, what, scope
├── design.md         # technical approach
├── tasks.md          # implementation checklist
└── specs/
    └── <capability>/spec.md   # ADDED / MODIFIED / REMOVED deltas
```

On merge, the proposal archives to
`openspec/changes/archive/YYYY-MM-DD-<name>/` and its delta specs fold
into `openspec/specs/`.

## Principles

1. **No control plane.** No server, no SaaS, no telemetry. Ever.
2. **Pure core, effectful edges.** `internal/core/*` imports only stdlib
   and sibling core packages. Enforced by `depguard` in `.golangci.yml`.
3. **Small interfaces at use-sites.** No giant central interface.
4. **Engine-agnostic core.** Capability detection only; never branch on
   `Engine.Name()`.
5. **Explicit over clever.** When a rule fires, the user can ask "why?"
   and get a clear trace.
6. **CLI / config parity.** Every runtime behavior has both a flag and a
   setting.
7. **Local-first testing.** Filesystem blob + injectable clock + fake VCS
   client — every CI behavior reproducible on a laptop in seconds.

## Code

- Go 1.23+ (ensure `go version` matches `go.mod`).
- `go test ./...` green.
- `go vet ./...` clean.
- `golangci-lint run` clean — see `.golangci.yml`.
- Golden-file tests for anything rendered (PR comments, drift reports,
  Slack blocks).
- AI-generated code is welcome per OpenSpec convention; note the agent
  and model in the PR description.
