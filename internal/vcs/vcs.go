// Package vcs defines the version-control system abstraction. Adapters
// (internal/vcs/github, future: gitlab) satisfy small use-site interfaces
// declared by their consumers. This file holds shared types referenced
// across adapters and core. See PLAN.md §6.5.
package vcs

// PR is the minimum normalized PR shape consumed across reeve.
type PR struct {
	Number   int
	HeadSHA  string
	BaseRef  string
	Author   string
	IsFork   bool
	OpenedAt string // RFC3339
	URL      string
}

// CommentCapabilities describes optional VCS features. Capability detection
// avoids hard-coding per-platform behavior in core.
type CommentCapabilities struct {
	SupportsEdit bool // GitHub + GitLab = true; false triggers append fallback
}
