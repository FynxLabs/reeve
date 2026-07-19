// Package github_issue opens or updates a GitHub issue per drifted stack.
// Issues are identified by a marker in the body; re-runs update instead of
// duplicating. GitHub access goes through notify.IssueClient (implemented
// by internal/vcs/github) - no VCS SDK is imported here (modularity
// contract).
package github_issue

import (
	"context"
	"fmt"
	"strings"

	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/notify"
)

func init() {
	notify.Register("github_issue", New)
}

// Channel manages one issue per drifted stack. PR-flow events are no-ops.
type Channel struct {
	name      string
	issues    notify.IssueClient
	labels    []string
	assignees []string
	events    []notify.Event
}

// New is the registered constructor. Without an issue client (no GitHub
// token/repo in the environment) the channel is skipped, matching the previous
// factory behavior.
func New(_ context.Context, cfg schemas.ChannelYAML, deps notify.Deps) (notify.Channel, error) {
	if deps.Issues == nil {
		return nil, nil
	}
	return &Channel{
		name:      cfg.EffectiveName(),
		issues:    deps.Issues,
		labels:    cfg.Labels,
		assignees: cfg.Assignees,
		events:    notify.ParseEvents(cfg.On),
	}, nil
}

func (s *Channel) Name() string               { return s.name }
func (s *Channel) Subscribes() []notify.Event { return s.events }

func (s *Channel) Deliver(ctx context.Context, p notify.Payload) error {
	if p.Drift == nil {
		return nil
	}
	marker := fmt.Sprintf("<!-- reeve:drift:%s -->", p.Drift.Ref())
	body := marker + "\n\n" + renderBody(p)

	number, found, err := s.issues.FindIssueByMarker(ctx, marker)
	if err != nil {
		return err
	}

	if p.Event == notify.EventDriftResolved {
		if found {
			return s.issues.CloseIssue(ctx, number, body)
		}
		return nil
	}

	title := fmt.Sprintf("drift: %s", p.Drift.Ref())
	if found {
		return s.issues.UpdateIssue(ctx, number, title, body)
	}
	_, err = s.issues.CreateIssue(ctx, title, body, s.labels, s.assignees)
	return err
}

func renderBody(p notify.Payload) string {
	d := p.Drift
	var b strings.Builder
	fmt.Fprintf(&b, "## Drift detected on `%s`\n\n", d.Ref())
	fmt.Fprintf(&b, "- **Env:** %s\n", d.Env)
	fmt.Fprintf(&b, "- **Event:** %s\n", p.Event)
	fmt.Fprintf(&b, "- **Changes:** +%d ~%d -%d ±%d\n",
		d.Add, d.Change, d.Delete, d.Replace)
	if d.PlanSummary != "" {
		fmt.Fprintf(&b, "\n```\n%s\n```\n", d.PlanSummary)
	}
	fmt.Fprintf(&b, "\n_run:_ `%s`\n", d.RunID)
	return b.String()
}
