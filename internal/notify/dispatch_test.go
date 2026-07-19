package notify

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDispatchFiltersBySubscription(t *testing.T) {
	var mu sync.Mutex
	var got []Event
	s := &fakeSink{name: "a", events: []Event{EventApplied}, fn: func(_ context.Context, p Payload) error {
		mu.Lock()
		got = append(got, p.Event)
		mu.Unlock()
		return nil
	}}
	errs := Dispatch(context.Background(), []Sink{s}, []Payload{
		{Event: EventPlan, PR: &PRPayload{}},
		{Event: EventApplied, PR: &PRPayload{}},
		{Event: EventDriftDetected, Drift: &DriftPayload{}},
	})
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if len(got) != 1 || got[0] != EventApplied {
		t.Fatalf("delivered: %v", got)
	}
}

func TestDispatchDeliversConcurrentlyAcrossSinks(t *testing.T) {
	// Both sinks block until the other has started - only concurrent
	// delivery lets this finish.
	arrived := make(chan struct{}, 2)
	proceed := make(chan struct{})
	go func() {
		<-arrived
		<-arrived
		close(proceed)
	}()
	wait := func(_ context.Context, _ Payload) error {
		arrived <- struct{}{}
		select {
		case <-proceed:
			return nil
		case <-time.After(5 * time.Second):
			return errors.New("no concurrency")
		}
	}
	a := &fakeSink{name: "a", events: []Event{EventApplied}, fn: wait}
	b := &fakeSink{name: "b", events: []Event{EventApplied}, fn: wait}
	errs := Dispatch(context.Background(), []Sink{a, b}, []Payload{{Event: EventApplied, PR: &PRPayload{}}})
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
}

func TestDispatchOrderedWithinSink(t *testing.T) {
	var got []Event
	s := &fakeSink{name: "a", events: []Event{EventApproved, EventApplying}, fn: func(_ context.Context, p Payload) error {
		got = append(got, p.Event) // single sink goroutine: no race
		return nil
	}}
	Dispatch(context.Background(), []Sink{s}, []Payload{
		{Event: EventApproved, PR: &PRPayload{}},
		{Event: EventApplying, PR: &PRPayload{}},
	})
	if len(got) != 2 || got[0] != EventApproved || got[1] != EventApplying {
		t.Fatalf("order: %v", got)
	}
}

func TestDispatchTimesOutHungSink(t *testing.T) {
	hung := &fakeSink{name: "hung", events: []Event{EventApplied}, fn: func(ctx context.Context, _ Payload) error {
		<-ctx.Done() // honors ctx: returns on cancellation
		return ctx.Err()
	}}
	fast := &fakeSink{name: "fast", events: []Event{EventApplied}}
	start := time.Now()
	errs := DispatchWith(context.Background(), []Sink{hung, fast},
		[]Payload{{Event: EventApplied, PR: &PRPayload{}}},
		DispatchOptions{DeliveryTimeout: 50 * time.Millisecond})
	if time.Since(start) > 3*time.Second {
		t.Fatalf("dispatch blocked too long")
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "hung") {
		t.Fatalf("want one error from hung sink, got %v", errs)
	}
}

func TestDispatchAbandonsSinkIgnoringContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	stuck := &fakeSink{name: "stuck", events: []Event{EventApplied, EventFailed}, fn: func(context.Context, Payload) error {
		<-release // ignores ctx entirely
		return nil
	}}
	errs := DispatchWith(context.Background(), []Sink{stuck},
		[]Payload{{Event: EventApplied, PR: &PRPayload{}}, {Event: EventFailed, PR: &PRPayload{}}},
		DispatchOptions{DeliveryTimeout: 30 * time.Millisecond})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "timed out") {
		t.Fatalf("want single timeout error (remaining deliveries skipped), got %v", errs)
	}
}

func TestDispatchCollectsErrors(t *testing.T) {
	boom := errors.New("boom")
	bad := &fakeSink{name: "bad", events: []Event{EventApplied}, fn: func(context.Context, Payload) error { return boom }}
	good := &fakeSink{name: "good", events: []Event{EventApplied}}
	errs := Dispatch(context.Background(), []Sink{bad, good}, []Payload{{Event: EventApplied, PR: &PRPayload{}}})
	if len(errs) != 1 || !errors.Is(errs[0], boom) || !strings.Contains(errs[0].Error(), "bad") {
		t.Fatalf("errs: %v", errs)
	}
}
