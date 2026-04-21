package drift

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

func TestClassifyFirstDetection(t *testing.T) {
	prev := State{}
	cur := Result{Outcome: OutcomeDriftDetected, CheckedAt: t0, Fingerprint: "fp1"}
	ev, next := Classify(prev, cur)
	if ev != EventDriftDetected {
		t.Fatalf("expected drift_detected, got %s", ev)
	}
	if next.OngoingSince != t0 {
		t.Fatalf("ongoing_since should be set to t0: %v", next.OngoingSince)
	}
}

func TestClassifyOngoingSilent(t *testing.T) {
	prev := State{LastOutcome: OutcomeDriftDetected, OngoingSince: t0.Add(-time.Hour), Fingerprint: "fp1"}
	cur := Result{Outcome: OutcomeDriftDetected, CheckedAt: t0, Fingerprint: "fp1"}
	ev, next := Classify(prev, cur)
	if ev != EventDriftOngoing {
		t.Fatalf("expected ongoing, got %s", ev)
	}
	if next.OngoingSince != prev.OngoingSince {
		t.Fatalf("ongoing_since should persist: %v", next.OngoingSince)
	}
}

func TestClassifyResolved(t *testing.T) {
	prev := State{LastOutcome: OutcomeDriftDetected, OngoingSince: t0.Add(-time.Hour), Fingerprint: "fp1"}
	cur := Result{Outcome: OutcomeNoDrift, CheckedAt: t0}
	ev, next := Classify(prev, cur)
	if ev != EventDriftResolved {
		t.Fatalf("expected resolved, got %s", ev)
	}
	if !next.OngoingSince.IsZero() {
		t.Fatalf("ongoing_since should be cleared")
	}
	if next.Fingerprint != "" {
		t.Fatalf("fingerprint should be cleared")
	}
}

func TestClassifyFingerprintChangeRefires(t *testing.T) {
	prev := State{LastOutcome: OutcomeDriftDetected, OngoingSince: t0.Add(-time.Hour), Fingerprint: "fp1"}
	cur := Result{Outcome: OutcomeDriftDetected, CheckedAt: t0, Fingerprint: "fp2"}
	ev, _ := Classify(prev, cur)
	if ev != EventDriftDetected {
		t.Fatalf("fingerprint change should refire: %s", ev)
	}
}

func TestClassifyError(t *testing.T) {
	prev := State{ConsecutiveErrors: 1}
	cur := Result{Outcome: OutcomeError, CheckedAt: t0, ErrorMessage: "boom"}
	ev, next := Classify(prev, cur)
	if ev != EventCheckFailed {
		t.Fatalf("expected check_failed, got %s", ev)
	}
	if next.ConsecutiveErrors != 2 {
		t.Fatalf("consecutive errors: %d", next.ConsecutiveErrors)
	}
}

func TestFingerprintStable(t *testing.T) {
	a := Fingerprint([]string{"urn:b", "urn:a", "urn:c"})
	b := Fingerprint([]string{"urn:c", "urn:a", "urn:b"})
	if a != b {
		t.Fatalf("fingerprint not stable under reordering: %s vs %s", a, b)
	}
	if a == "" {
		t.Fatal("empty fingerprint")
	}
}

func TestFingerprintEmpty(t *testing.T) {
	if Fingerprint(nil) != "" {
		t.Fatal("empty list should produce empty fingerprint")
	}
}
