// Package webhook is the generic webhook drift sink. Phase 8 ships
// `raw` format only (event payload as JSON). Named presets are
// out-of-scope until a user provides a real target payload.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/thefynx/reeve/internal/drift/sinks"
)

// Sink publishes drift events as HTTP POST with an optional header map.
type Sink struct {
	Name_   string
	URL     string
	Headers map[string]string
	Events  []sinks.Event
	Format  string // "raw" (default) - future: "incident_io" | "opsgenie" | ...
	Client  *http.Client
}

func (s *Sink) Name() string              { return s.Name_ }
func (s *Sink) Subscribes() []sinks.Event { return s.Events }

func (s *Sink) Deliver(ctx context.Context, p sinks.Payload) error {
	if s.Client == nil {
		s.Client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]any{
		"event":   p.Event,
		"project": p.Item.Project,
		"stack":   p.Item.Stack,
		"env":     p.Item.Env,
		"outcome": p.Item.Outcome,
		"counts": map[string]int{
			"add":     p.Item.Counts.Counts.Add,
			"change":  p.Item.Counts.Counts.Change,
			"delete":  p.Item.Counts.Counts.Delete,
			"replace": p.Item.Counts.Counts.Replace,
		},
		"fingerprint": p.Item.Fingerprint,
		"error":       p.Item.Error,
		"run_id":      p.Run.RunID,
	})
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("webhook %s: HTTP %d", s.Name_, resp.StatusCode)
	}
	return nil
}
