// Package github implements the VCS adapter for GitHub.
//
// When Phase 1 lands, *Client satisfies these use-site interfaces:
//   - vcs.PRReader (core/discovery, run/*)
//   - vcs.CommentPoster (core/render, run/*)
//   - approvals.ReviewLister (core/approvals)
//   - approvals.TeamResolver (core/approvals)
//   - apply.ChecksReader (core/preconditions)
//   - codeowners.Provider (core/approvals)
//
// Phase 0 scope: package declaration only.
package github
