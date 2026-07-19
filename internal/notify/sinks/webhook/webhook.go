// Package webhook is the generic webhook sink. Ships `raw` format only
// (event payload as JSON). Named presets are out-of-scope until a user
// provides a real target payload.
package webhook

import (
	"context"
	"encoding/json"

	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/notify"
)

func init() {
	notify.Register("webhook", New)
}

// Sink publishes events as HTTP POST with an optional header map.
// Deliveries retry with backoff on 5xx/network errors (notify.PostJSON).
type Sink struct {
	name    string
	url     string
	headers map[string]string
	events  []notify.Event
	format  string // "raw" (default) - future: "incident_io" | "opsgenie" | ...
	client  notify.HTTPDoer
}

// New is the registered constructor.
func New(_ context.Context, cfg schemas.SinkYAML, deps notify.Deps) (notify.Sink, error) {
	return &Sink{
		name:    cfg.EffectiveName(),
		url:     cfg.URL,
		headers: cfg.Headers,
		events:  notify.ParseEvents(cfg.On),
		format:  cfg.Payload.Format,
		client:  deps.HTTP,
	}, nil
}

func (s *Sink) Name() string               { return s.name }
func (s *Sink) Subscribes() []notify.Event { return s.events }

func (s *Sink) Deliver(ctx context.Context, p notify.Payload) error {
	body, err := json.Marshal(payloadJSON(p))
	if err != nil {
		return err
	}
	return notify.PostJSON(ctx, s.client, "webhook "+s.name, s.url, s.headers, body)
}

// payloadJSON keeps the drift wire format byte-for-byte compatible with the
// previous drift-only sink; PR-flow events get an analogous raw shape.
func payloadJSON(p notify.Payload) map[string]any {
	if p.Drift != nil {
		d := p.Drift
		return map[string]any{
			"event":   p.Event,
			"project": d.Project,
			"stack":   d.Stack,
			"env":     d.Env,
			"outcome": d.Outcome,
			"counts": map[string]int{
				"add":     d.Add,
				"change":  d.Change,
				"delete":  d.Delete,
				"replace": d.Replace,
			},
			"fingerprint": d.Fingerprint,
			"error":       d.Error,
			"run_id":      d.RunID,
		}
	}
	pr := p.PR
	stacks := make([]map[string]any, 0, len(pr.Stacks))
	for _, st := range pr.Stacks {
		stacks = append(stacks, map[string]any{
			"project": st.Project,
			"stack":   st.Stack,
			"env":     st.Env,
			"status":  st.Status,
			"counts": map[string]int{
				"add":     st.Add,
				"change":  st.Change,
				"delete":  st.Delete,
				"replace": st.Replace,
			},
		})
	}
	return map[string]any{
		"event":      p.Event,
		"pr":         pr.PR,
		"repo":       pr.RepoFull,
		"title":      pr.Title,
		"author":     pr.Author,
		"commit_sha": pr.CommitSHA,
		"run_url":    pr.RunURL,
		"stacks":     stacks,
	}
}
