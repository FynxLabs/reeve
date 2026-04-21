// Package github_issue opens or updates a GitHub issue per drifted stack.
// Issues are identified by a marker in the body; re-runs update instead
// of duplicating.
package github_issue

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v66/github"

	"github.com/thefynx/reeve/internal/drift"
	"github.com/thefynx/reeve/internal/drift/sinks"
)

// Sink manages one issue per drifted stack.
type Sink struct {
	Name_     string
	Client    *gh.Client
	Owner     string
	Repo      string
	Labels    []string
	Assignees []string
	Events    []sinks.Event
}

func (s *Sink) Name() string              { return s.Name_ }
func (s *Sink) Subscribes() []sinks.Event { return s.Events }

func (s *Sink) Deliver(ctx context.Context, p sinks.Payload) error {
	marker := fmt.Sprintf("<!-- reeve:drift:%s -->", p.Item.Ref())
	body := marker + "\n\n" + renderBody(p)

	// Find existing open issue by marker.
	existing, err := s.findByMarker(ctx, marker)
	if err != nil {
		return err
	}

	if p.Event == drift.EventDriftResolved {
		if existing != nil {
			_, _, err := s.Client.Issues.Edit(ctx, s.Owner, s.Repo, existing.GetNumber(), &gh.IssueRequest{
				State: gh.String("closed"),
				Body:  gh.String(body),
			})
			return err
		}
		return nil
	}

	title := fmt.Sprintf("drift: %s", p.Item.Ref())
	if existing != nil {
		_, _, err := s.Client.Issues.Edit(ctx, s.Owner, s.Repo, existing.GetNumber(), &gh.IssueRequest{
			Title: gh.String(title),
			Body:  gh.String(body),
		})
		return err
	}
	req := &gh.IssueRequest{
		Title:     gh.String(title),
		Body:      gh.String(body),
		Labels:    &s.Labels,
		Assignees: &s.Assignees,
	}
	_, _, err = s.Client.Issues.Create(ctx, s.Owner, s.Repo, req)
	return err
}

func (s *Sink) findByMarker(ctx context.Context, marker string) (*gh.Issue, error) {
	opt := &gh.IssueListByRepoOptions{State: "open", ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		issues, resp, err := s.Client.Issues.ListByRepo(ctx, s.Owner, s.Repo, opt)
		if err != nil {
			return nil, err
		}
		for _, i := range issues {
			if strings.Contains(i.GetBody(), marker) {
				return i, nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return nil, nil
}

func renderBody(p sinks.Payload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Drift detected on `%s`\n\n", p.Item.Ref())
	fmt.Fprintf(&b, "- **Env:** %s\n", p.Item.Env)
	fmt.Fprintf(&b, "- **Event:** %s\n", p.Event)
	fmt.Fprintf(&b, "- **Changes:** +%d ~%d -%d ±%d\n",
		p.Item.Counts.Counts.Add, p.Item.Counts.Counts.Change,
		p.Item.Counts.Counts.Delete, p.Item.Counts.Counts.Replace)
	if p.Item.Counts.PlanSummary != "" {
		fmt.Fprintf(&b, "\n```\n%s\n```\n", p.Item.Counts.PlanSummary)
	}
	fmt.Fprintf(&b, "\n_run:_ `%s`\n", p.Run.RunID)
	return b.String()
}
