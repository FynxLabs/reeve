// Package otel delivers drift events as annotation events to the
// observability.annotations emitters (Grafana / Datadog / Dash0 / webhook).
package otel

import (
	"context"
	"time"

	"github.com/thefynx/reeve/internal/drift"
	"github.com/thefynx/reeve/internal/drift/sinks"
	"github.com/thefynx/reeve/internal/observability/annotations"
)

// Sink fans drift events out to a list of annotation emitters.
type Sink struct {
	Name_    string
	Events   []sinks.Event
	Emitters []annotations.Emitter
}

func (s *Sink) Name() string              { return s.Name_ }
func (s *Sink) Subscribes() []sinks.Event { return s.Events }

func (s *Sink) Deliver(ctx context.Context, p sinks.Payload) error {
	t := eventType(p.Event)
	if t == "" {
		return nil
	}
	annotations.Dispatch(ctx, s.Emitters, annotations.Event{
		Type:    t,
		When:    time.Now(),
		Project: p.Item.Project,
		Stack:   p.Item.Stack,
		Env:     p.Item.Env,
		Outcome: string(p.Item.Outcome),
		Message: p.Item.Error,
		Tags: map[string]string{
			"fingerprint": p.Item.Fingerprint,
			"run_id":      p.Run.RunID,
		},
	})
	return nil
}

func eventType(e sinks.Event) annotations.EventType {
	switch e {
	case drift.EventDriftDetected:
		return annotations.EventDriftDetected
	case drift.EventDriftResolved:
		return annotations.EventDriftResolved
	}
	return ""
}
