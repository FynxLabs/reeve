package notify

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DefaultDeliveryTimeout bounds a single Deliver call during Dispatch.
const DefaultDeliveryTimeout = 30 * time.Second

// DispatchOptions tunes Dispatch behavior.
type DispatchOptions struct {
	// DeliveryTimeout bounds each Deliver call. Zero uses
	// DefaultDeliveryTimeout.
	DeliveryTimeout time.Duration
}

// Dispatch delivers every subscribed payload to every sink. Sinks run
// concurrently (one hung endpoint cannot starve the others); within a sink,
// payloads deliver in order so stateful sinks (Slack message upserts) stay
// consistent. Each delivery is bounded by a timeout; a sink that overruns it
// is abandoned for the rest of the batch. Errors are collected, never fatal.
func Dispatch(ctx context.Context, sinks []Sink, payloads []Payload) []error {
	return DispatchWith(ctx, sinks, payloads, DispatchOptions{})
}

// DispatchWith is Dispatch with explicit options.
func DispatchWith(ctx context.Context, sinks []Sink, payloads []Payload, opts DispatchOptions) []error {
	timeout := opts.DeliveryTimeout
	if timeout <= 0 {
		timeout = DefaultDeliveryTimeout
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	record := func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}

	for _, s := range sinks {
		wg.Add(1)
		go func(s Sink) {
			defer wg.Done()
			subs := s.Subscribes()
			for _, p := range payloads {
				if !subscribed(subs, p.Event) {
					continue
				}
				if err := ctx.Err(); err != nil {
					record(fmt.Errorf("sink %s: %w", s.Name(), err))
					return
				}
				err, timedOut := deliverBounded(ctx, s, p, timeout)
				if timedOut {
					// The Deliver goroutine may still be running (a sink
					// that ignores ctx); abandon this sink rather than
					// racing further deliveries against it.
					record(fmt.Errorf("sink %s: event %s: delivery timed out after %s (skipping remaining deliveries)", s.Name(), p.Event, timeout))
					return
				}
				if err != nil {
					record(fmt.Errorf("sink %s: event %s: %w", s.Name(), p.Event, err))
				}
			}
		}(s)
	}
	wg.Wait()
	return errs
}

// deliverBounded runs Deliver under a per-delivery timeout. timedOut is true
// only when the sink failed to return by the deadline (i.e. it also ignored
// ctx cancellation for the grace we give it).
func deliverBounded(ctx context.Context, s Sink, p Payload, timeout time.Duration) (err error, timedOut bool) {
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- s.Deliver(dctx, p)
	}()
	select {
	case err := <-done:
		return err, false
	case <-dctx.Done():
		// Grace period: a well-behaved sink returns promptly once its ctx
		// is cancelled - prefer its real error over a bare timeout.
		select {
		case err := <-done:
			return err, false
		case <-time.After(2 * time.Second):
			return nil, true
		}
	}
}

func subscribed(subs []Event, ev Event) bool {
	for _, w := range subs {
		if w == ev {
			return true
		}
	}
	return false
}
