// Package pagerduty delivers events via the PagerDuty Events API v2.
package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/notify"
)

// DefaultEndpoint is the PagerDuty Events API v2 enqueue URL.
const DefaultEndpoint = "https://events.pagerduty.com/v2/enqueue"

func init() {
	notify.Register("pagerduty", New)
}

// Sink sends a PagerDuty v2 event per notification event. Deliveries retry
// with backoff on 5xx/network errors (notify.PostJSON).
type Sink struct {
	name           string
	integrationKey string
	severityMap    map[string]string // env → severity (info|warning|error|critical)
	events         []notify.Event
	client         notify.HTTPDoer
	endpoint       string // overridable in tests; DefaultEndpoint otherwise
}

// New is the registered constructor.
func New(_ context.Context, cfg schemas.SinkYAML, deps notify.Deps) (notify.Sink, error) {
	sm := cfg.SeverityMap
	if sm == nil {
		sm = cfg.Payload.SeverityMap
	}
	return &Sink{
		name:           cfg.EffectiveName(),
		integrationKey: cfg.IntegrationKey,
		severityMap:    sm,
		events:         notify.ParseEvents(cfg.On),
		client:         deps.HTTP,
		endpoint:       DefaultEndpoint,
	}, nil
}

func (s *Sink) Name() string               { return s.name }
func (s *Sink) Subscribes() []notify.Event { return s.events }

func (s *Sink) Deliver(ctx context.Context, p notify.Payload) error {
	var event map[string]any
	switch {
	case p.Drift != nil:
		event = s.driftEvent(p)
	case p.PR != nil:
		event = s.prEvent(p)
	}
	if event == nil {
		return nil
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return notify.PostJSON(ctx, s.client, "pagerduty "+s.name, s.endpoint, nil, body)
}

func (s *Sink) driftEvent(p notify.Payload) map[string]any {
	d := p.Drift
	severity := s.severityMap[d.Env]
	if severity == "" {
		severity = "warning"
	}
	action := "trigger"
	if p.Event == notify.EventDriftResolved {
		action = "resolve"
	}
	return map[string]any{
		"routing_key":  s.integrationKey,
		"event_action": action,
		"dedup_key":    fmt.Sprintf("reeve-drift-%s", d.Ref()),
		"payload": map[string]any{
			"summary":  fmt.Sprintf("drift on %s (%s)", d.Ref(), p.Event),
			"source":   "reeve",
			"severity": severity,
			"custom_details": map[string]any{
				"project":     d.Project,
				"stack":       d.Stack,
				"env":         d.Env,
				"outcome":     d.Outcome,
				"fingerprint": d.Fingerprint,
				"add":         d.Add,
				"change":      d.Change,
				"delete":      d.Delete,
				"replace":     d.Replace,
				"run_id":      d.RunID,
			},
		},
	}
}

// prEvent maps the PR lifecycle onto PagerDuty incidents: failed/blocked
// applies trigger an incident, a subsequent successful apply resolves it.
// Intermediate lifecycle events are no-ops even when subscribed.
func (s *Sink) prEvent(p notify.Payload) map[string]any {
	pr := p.PR
	var action, severity string
	switch p.Event {
	case notify.EventFailed:
		action, severity = "trigger", "error"
	case notify.EventBlocked:
		action, severity = "trigger", "warning"
	case notify.EventApplied:
		action, severity = "resolve", "info"
	default:
		return nil
	}
	return map[string]any{
		"routing_key":  s.integrationKey,
		"event_action": action,
		"dedup_key":    fmt.Sprintf("reeve-pr-%s-%d", pr.RepoFull, pr.PR),
		"payload": map[string]any{
			"summary":  fmt.Sprintf("reeve apply %s on %s#%d", p.Event, pr.RepoFull, pr.PR),
			"source":   "reeve",
			"severity": severity,
			"custom_details": map[string]any{
				"pr":         pr.PR,
				"repo":       pr.RepoFull,
				"commit_sha": pr.CommitSHA,
				"run_url":    pr.RunURL,
			},
		},
	}
}
