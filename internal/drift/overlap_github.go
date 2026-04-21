package drift

import (
	"context"
	"time"

	"github.com/thefynx/reeve/internal/core/approvals"
)

// ghOverlap wraps *github.Client into a PROverlapFinder.
type ghOverlap struct {
	client ghOverlapClient
}

// ghOverlapClient is the narrow interface we need from the VCS adapter —
// defined here so internal/drift doesn't take a hard dep on internal/vcs/github.
type ghOverlapClient interface {
	ListOpenPRsTouchingPaths(ctx context.Context, paths []string) ([]approvals.PR, error)
}

// NewGitHubPROverlap returns a PROverlapFinder backed by a client that
// can list open PRs. Usually that's *vcs/github.Client.
func NewGitHubPROverlap(client ghOverlapClient) PROverlapFinder {
	return &ghOverlap{client: client}
}

func (g *ghOverlap) FindOverlappingPRs(ctx context.Context, paths []string) ([]OverlappingPR, error) {
	prs, err := g.client.ListOpenPRsTouchingPaths(ctx, paths)
	if err != nil {
		return nil, err
	}
	out := make([]OverlappingPR, 0, len(prs))
	for _, p := range prs {
		opened, _ := time.Parse(time.RFC3339, "")
		out = append(out, OverlappingPR{
			Number:   p.Number,
			Author:   p.Author,
			HeadSHA:  p.HeadSHA,
			OpenedAt: opened, // VCS PR doesn't carry OpenedAt in core.PR yet — wire in Phase 7.x
			Paths:    p.Changed,
		})
	}
	return out, nil
}
