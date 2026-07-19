package drift

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/thefynx/reeve/internal/blob"
)

// Outcome classifies a single drift-check result.
type Outcome string

const (
	OutcomeNoDrift       Outcome = "no_drift"
	OutcomeDriftDetected Outcome = "drift_detected"
	OutcomeError         Outcome = "error"
	OutcomeSkipped       Outcome = "skipped_fresh"
)

// Event is what sinks consume. Determined by comparing current outcome
// against prior state.
type Event string

const (
	EventDriftDetected Event = "drift_detected"
	EventDriftOngoing  Event = "drift_ongoing"
	EventDriftResolved Event = "drift_resolved"
	EventCheckFailed   Event = "check_failed"
	EventNone          Event = "" // silent - no sink delivery
)

// KnownEventNames lists the event names a sink's `on:` list may subscribe
// to. Used by lint and the sink factory to reject/flag typos instead of
// silently dropping them.
func KnownEventNames() []string {
	return []string{
		string(EventDriftDetected),
		string(EventDriftOngoing),
		string(EventDriftResolved),
		string(EventCheckFailed),
	}
}

// ParseEventName maps a config string to an Event. ok is false for
// unknown names (including the empty string).
func ParseEventName(s string) (Event, bool) {
	switch Event(s) {
	case EventDriftDetected, EventDriftOngoing, EventDriftResolved, EventCheckFailed:
		return Event(s), true
	}
	return EventNone, false
}

// State is the per-stack persisted state at drift/state/{project}/{stack}.json.
type State struct {
	Project          string    `json:"project"`
	Stack            string    `json:"stack"`
	LastOutcome      Outcome   `json:"last_outcome"`
	LastCheckedAt    time.Time `json:"last_checked_at"`
	LastSuccessfulAt time.Time `json:"last_successful_at,omitempty"`
	OngoingSince     time.Time `json:"ongoing_since,omitempty"`
	Fingerprint      string    `json:"fingerprint,omitempty"`
	// ConsecutiveErrors supports retry / backoff heuristics.
	ConsecutiveErrors int `json:"consecutive_errors,omitempty"`
}

// Result is the current check output (passed into Classify).
type Result struct {
	Project      string
	Stack        string
	Outcome      Outcome
	Fingerprint  string // empty if Outcome != drift_detected
	CheckedAt    time.Time
	ErrorMessage string
}

// Classify compares cur to prev and returns the event + new state.
func Classify(prev State, cur Result) (Event, State) {
	next := State{
		Project:           cur.Project,
		Stack:             cur.Stack,
		LastOutcome:       cur.Outcome,
		LastCheckedAt:     cur.CheckedAt,
		LastSuccessfulAt:  prev.LastSuccessfulAt,
		OngoingSince:      prev.OngoingSince,
		Fingerprint:       prev.Fingerprint,
		ConsecutiveErrors: prev.ConsecutiveErrors,
	}

	switch cur.Outcome {
	case OutcomeError:
		// A check error is transient and must not overwrite the last decisive
		// outcome. Preserve prev.LastOutcome (and OngoingSince/Fingerprint) so
		// that after a blip the next real check transitions from where we
		// actually were: drift->error->drift stays "ongoing" (no duplicate
		// detection, drift age intact), and drift->error->no_drift still emits
		// resolved. Only the error bookkeeping advances.
		next.LastOutcome = prev.LastOutcome
		next.ConsecutiveErrors = prev.ConsecutiveErrors + 1
		return EventCheckFailed, next

	case OutcomeNoDrift:
		next.ConsecutiveErrors = 0
		next.LastSuccessfulAt = cur.CheckedAt
		next.Fingerprint = ""
		if prev.LastOutcome == OutcomeDriftDetected {
			next.OngoingSince = time.Time{}
			return EventDriftResolved, next
		}
		next.OngoingSince = time.Time{}
		return EventNone, next

	case OutcomeDriftDetected:
		next.ConsecutiveErrors = 0
		next.LastSuccessfulAt = cur.CheckedAt
		next.Fingerprint = cur.Fingerprint
		if prev.LastOutcome != OutcomeDriftDetected {
			next.OngoingSince = cur.CheckedAt
			return EventDriftDetected, next
		}
		// Still drifted.
		if prev.OngoingSince.IsZero() {
			next.OngoingSince = cur.CheckedAt
		}
		if cur.Fingerprint != prev.Fingerprint {
			// Drift shape changed - treat as a fresh detection.
			return EventDriftDetected, next
		}
		return EventDriftOngoing, next

	default:
		return EventNone, next
	}
}

// Fingerprint returns a stable hash of the drifted resource set. Caller
// supplies the list of URNs (or any stable identifier) that drifted;
// order is canonicalized.
func Fingerprint(urns []string) string {
	if len(urns) == 0 {
		return ""
	}
	sorted := append([]string(nil), urns...)
	// simple insertion sort to avoid pulling sort for a small list
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	h := sha256.New()
	for _, u := range sorted {
		h.Write([]byte(u))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// --- blob persistence ---

// StateStore wraps a blob.Store for drift state.
type StateStore struct{ Blob blob.Store }

func (s *StateStore) key(project, stack string) string {
	return fmt.Sprintf("drift/state/%s/%s.json", project, stack)
}

// Load reads a state file. Returns a zero-value State if absent (first
// run).
func (s *StateStore) Load(ctx context.Context, project, stack string) (State, error) {
	rc, _, err := s.Blob.Get(ctx, s.key(project, stack))
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			return State{Project: project, Stack: stack}, nil
		}
		return State{}, err
	}
	defer rc.Close()
	var st State
	if err := json.NewDecoder(rc).Decode(&st); err != nil {
		return State{}, err
	}
	return st, nil
}

// Save persists the state.
func (s *StateStore) Save(ctx context.Context, st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	_, err = s.Blob.Put(ctx, s.key(st.Project, st.Stack), bytes.NewReader(data))
	return err
}
