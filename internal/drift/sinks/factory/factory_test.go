package factory

import (
	"context"
	"testing"

	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/drift"
)

func TestBuildParsesKnownEventsAndDropsUnknown(t *testing.T) {
	cfg := &schemas.Drift{
		Sinks: []schemas.DriftSinkYAML{
			{
				Type: "webhook",
				Name: "hook",
				URL:  "https://example.invalid/hook",
				On:   []string{"drift_detected", "not_an_event", "check_failed"},
			},
		},
	}
	out, err := Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 sink, got %d", len(out))
	}
	got := out[0].Subscribes()
	want := []drift.Event{drift.EventDriftDetected, drift.EventCheckFailed}
	if len(got) != len(want) {
		t.Fatalf("subscribes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("subscribes = %v, want %v", got, want)
		}
	}
}

func TestBuildEmptySubscriptionSinkNeverFires(t *testing.T) {
	// All-unknown (or empty) on: lists yield an empty subscription; Build
	// still constructs the sink (lint is the hard gate) but it must not be
	// subscribed to anything.
	cfg := &schemas.Drift{
		Sinks: []schemas.DriftSinkYAML{
			{Type: "webhook", Name: "hook", URL: "https://example.invalid/hook", On: []string{"typo_event"}},
		},
	}
	out, err := Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 sink, got %d", len(out))
	}
	if n := len(out[0].Subscribes()); n != 0 {
		t.Fatalf("expected empty subscription, got %d", n)
	}
}
