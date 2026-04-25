// Package slack is the drift Slack sink. Dashboard-style: one message
// per run per channel (not per event - groups by channel).
package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/thefynx/reeve/internal/drift"
	"github.com/thefynx/reeve/internal/drift/sinks"
	"github.com/thefynx/reeve/internal/slack"
)

// Sink delivers drift events as Slack messages. Phase 8: one threaded
// message per item (no state tracking - drift runs are usually daily
// and idempotency on the bucket side covers re-runs).
type Sink struct {
	Name_    string
	Client   *slack.Client
	Channel  string
	Events   []sinks.Event
	Grouping string // "by_environment" | "one_per_stack"
}

func (s *Sink) Name() string              { return s.Name_ }
func (s *Sink) Subscribes() []sinks.Event { return s.Events }

func (s *Sink) Deliver(ctx context.Context, p sinks.Payload) error {
	text := buildMessage(p)
	blocks := []slack.Block{
		slack.Header(fmt.Sprintf("reeve · drift · %s", labelEvent(p.Event))),
		slack.Section(text),
	}
	_, err := s.Client.Upsert(ctx, s.Channel, "", text, blocks)
	return err
}

func buildMessage(p sinks.Payload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "*%s* - %s (+%d ~%d -%d ±%d)\n",
		p.Item.Ref(), labelEvent(p.Event),
		p.Item.Counts.Counts.Add, p.Item.Counts.Counts.Change,
		p.Item.Counts.Counts.Delete, p.Item.Counts.Counts.Replace,
	)
	if p.Item.Error != "" {
		fmt.Fprintf(&b, "_error:_ %s\n", truncate(p.Item.Error, 200))
	}
	if p.Item.Counts.PlanSummary != "" {
		fmt.Fprintf(&b, "```%s```\n", truncate(p.Item.Counts.PlanSummary, 500))
	}
	return b.String()
}

func labelEvent(e sinks.Event) string {
	switch e {
	case drift.EventDriftDetected:
		return "🆕 drift detected"
	case drift.EventDriftOngoing:
		return "🔁 ongoing"
	case drift.EventDriftResolved:
		return "✅ resolved"
	case drift.EventCheckFailed:
		return "💥 check failed"
	}
	return string(e)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
