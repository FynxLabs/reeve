// Package notify is the shared notification-sink framework. Both producers
// (the drift runner and the PR-flow pipeline) publish events through it; a
// destination implements Sink once and can subscribe to events from either
// producer. Concrete sinks live in subpackages under internal/notify/sinks
// and self-register via Register in their init(), so a build can compile in
// a subset (see the modularity contract in openspec/specs/architecture).
package notify

import (
	"context"

	"github.com/thefynx/reeve/internal/config/schemas"
)

// Event names a lifecycle event a sink can subscribe to. The string values
// are the `on:` names in config (schemas.ValidSinkEvents).
type Event string

const (
	// PR-flow events (produced by the run pipeline).
	EventPlanning Event = schemas.SinkEventPlanning // preview run started
	EventPlan     Event = schemas.SinkEventPlan     // preview finished, pending approval
	EventReady    Event = schemas.SinkEventReady    // /reeve ready (or auto_ready)
	EventApproved Event = schemas.SinkEventApproved // preconditions passed, apply imminent
	EventApplying Event = schemas.SinkEventApplying // apply loop started
	EventApplied  Event = schemas.SinkEventApplied  // apply finished successfully
	EventFailed   Event = schemas.SinkEventFailed   // apply errored
	EventBlocked  Event = schemas.SinkEventBlocked  // apply blocked (gates/locks)
	// EventBreakGlass fires when an emergency-override (break-glass) apply
	// is authorized; run.Apply emits it in place of EventApproved.
	EventBreakGlass Event = schemas.SinkEventBreakGlass

	// Drift events (produced by the drift runner).
	EventDriftDetected Event = schemas.SinkEventDriftDetected
	EventDriftOngoing  Event = schemas.SinkEventDriftOngoing
	EventDriftResolved Event = schemas.SinkEventDriftResolved
	EventCheckFailed   Event = schemas.SinkEventCheckFailed
)

// PREvents lists the core PR-flow lifecycle events in order. The legacy
// Slack trigger-onward default subscription derives from this list, so it
// deliberately EXCLUDES the timeline-only additions (planning, break_glass):
// adding them here would silently widen existing sinks' subscriptions.
func PREvents() []Event {
	return []Event{EventPlan, EventReady, EventApproved, EventApplying, EventApplied, EventFailed, EventBlocked}
}

// TimelinePREvents lists every PR-flow event the deployment timeline
// renders, in lifecycle order: the core set plus preview-started and the
// reserved break-glass event. Timeline sinks subscribe to this set by
// default.
func TimelinePREvents() []Event {
	return append(append([]Event{EventPlanning}, PREvents()...), EventBreakGlass)
}

// DriftEvents lists every drift event.
func DriftEvents() []Event {
	return []Event{EventDriftDetected, EventDriftOngoing, EventDriftResolved, EventCheckFailed}
}

// ParseEvents converts `on:` strings into Events, dropping unknown names.
// Unknown names are rejected earlier, at config load/lint time.
func ParseEvents(on []string) []Event {
	out := make([]Event, 0, len(on))
	for _, s := range on {
		if schemas.IsValidSinkEvent(s) {
			out = append(out, Event(s))
		}
	}
	return out
}

// Payload is what every sink receives per delivery. Exactly one of Drift or
// PR is non-nil, matching the event's producer.
type Payload struct {
	Event Event
	Drift *DriftPayload
	PR    *PRPayload
}

// DriftPayload is one stack's drift-check outcome, flattened from
// drift.RunOutput so sinks do not depend on the drift package.
type DriftPayload struct {
	Project     string
	Stack       string
	Env         string
	Outcome     string // no_drift | drift_detected | error | skipped_fresh
	Add         int
	Change      int
	Delete      int
	Replace     int
	PlanSummary string
	Fingerprint string
	Error       string
	RunID       string
}

// Ref returns "project/stack".
func (d DriftPayload) Ref() string { return d.Project + "/" + d.Stack }

// PRPayload is the PR-flow event context, flattened from the run pipeline.
type PRPayload struct {
	PR                int
	CommitSHA         string
	RunURL            string
	Title             string
	Author            string
	RepoFull          string // "owner/repo"
	RequiredApprovers []string
	Stacks            []StackResult
}

// StackResult is one stack's summary inside a PR-flow payload.
type StackResult struct {
	Project string
	Stack   string
	Env     string
	Status  string // planned | noop | blocked | error
	Add     int
	Change  int
	Delete  int
	Replace int
}

// Total is the change-count sum, used for no-op detection.
func (s StackResult) Total() int { return s.Add + s.Change + s.Delete + s.Replace }

// Sink delivers events. Implementations are expected to be reentrant-safe,
// side-effect-only, and to honor ctx cancellation (Dispatch enforces a
// per-delivery timeout through ctx).
type Sink interface {
	Name() string
	// Subscribes returns the event types this sink wants to receive.
	Subscribes() []Event
	// Deliver publishes one event. Return error only for unrecoverable
	// failures; sinks swallow transient errors internally (Deliver-level
	// retries for HTTP sinks live in PostJSON).
	Deliver(ctx context.Context, p Payload) error
}
