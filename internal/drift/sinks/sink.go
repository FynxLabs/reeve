// Package sinks defines the drift sink interface and houses shared
// event-filter helpers. Concrete sinks live in subpackages.
package sinks

import (
	"context"

	"github.com/thefynx/reeve/internal/drift"
)

// Payload is what every sink receives per event delivery.
type Payload struct {
	Event Event
	Item  drift.Item
	Run   *drift.RunOutput
}

// Event is a sink-flavored copy of drift.Event so sinks don't
// depend on drift.Event constants directly.
type Event = drift.Event

// Sink delivers drift events. Implementations are expected to be
// reentrant-safe and side-effect-only.
type Sink interface {
	Name() string
	// Subscribes returns the event types this sink wants to receive.
	Subscribes() []Event
	// Deliver publishes one event. Return error only for unrecoverable
	// failures; sinks swallow transient errors internally.
	Deliver(ctx context.Context, p Payload) error
}

// Dispatch iterates the runOutput, filtering events by subscription, and
// delivers to every configured sink. Errors are collected but do not
// abort the run.
func Dispatch(ctx context.Context, sinks []Sink, run *drift.RunOutput) []error {
	var errs []error
	for i, it := range run.Items {
		ev := run.Events[i]
		if ev == drift.EventNone {
			continue
		}
		for _, s := range sinks {
			if !subscribed(s, ev) {
				continue
			}
			if err := s.Deliver(ctx, Payload{Event: ev, Item: it, Run: run}); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errs
}

func subscribed(s Sink, ev Event) bool {
	for _, w := range s.Subscribes() {
		if w == ev {
			return true
		}
	}
	return false
}
