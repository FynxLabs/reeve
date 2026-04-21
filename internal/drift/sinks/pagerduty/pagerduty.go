// Package pagerduty delivers drift events via the PagerDuty Events API v2.
package pagerduty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/thefynx/reeve/internal/drift"
	"github.com/thefynx/reeve/internal/drift/sinks"
)

const endpoint = "https://events.pagerduty.com/v2/enqueue"

// Sink sends a PagerDuty v2 event per drift event.
type Sink struct {
	Name_          string
	IntegrationKey string
	SeverityMap    map[string]string // env → severity (info|warning|error|critical)
	Events         []sinks.Event
	Client         *http.Client
}

func (s *Sink) Name() string              { return s.Name_ }
func (s *Sink) Subscribes() []sinks.Event { return s.Events }

func (s *Sink) Deliver(ctx context.Context, p sinks.Payload) error {
	if s.Client == nil {
		s.Client = http.DefaultClient
	}
	severity := s.SeverityMap[p.Item.Env]
	if severity == "" {
		severity = "warning"
	}
	action := "trigger"
	if p.Event == drift.EventDriftResolved {
		action = "resolve"
	}
	dedupKey := fmt.Sprintf("reeve-drift-%s", p.Item.Ref())
	event := map[string]any{
		"routing_key":  s.IntegrationKey,
		"event_action": action,
		"dedup_key":    dedupKey,
		"payload": map[string]any{
			"summary":  fmt.Sprintf("drift on %s (%s)", p.Item.Ref(), p.Event),
			"source":   "reeve",
			"severity": severity,
			"custom_details": map[string]any{
				"project":     p.Item.Project,
				"stack":       p.Item.Stack,
				"env":         p.Item.Env,
				"outcome":     p.Item.Outcome,
				"fingerprint": p.Item.Fingerprint,
				"add":         p.Item.Counts.Counts.Add,
				"change":      p.Item.Counts.Counts.Change,
				"delete":      p.Item.Counts.Counts.Delete,
				"replace":     p.Item.Counts.Counts.Replace,
				"run_id":      p.Run.RunID,
			},
		},
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("pagerduty %d", resp.StatusCode)
	}
	return nil
}
