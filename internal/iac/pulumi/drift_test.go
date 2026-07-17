package pulumi

import (
	"errors"
	"testing"
)

func TestDriftedURNsFromJSONExcludesSame(t *testing.T) {
	// Two "same" (unchanged) steps and one "update" (drift). Fingerprint
	// input must contain only the drifted URN, so drift in a different
	// resource changes the fingerprint.
	js := []byte(`{"steps":[
		{"op":"same","urn":"urn:pulumi:prod::app::a::x"},
		{"op":"update","urn":"urn:pulumi:prod::app::b::y"},
		{"op":"same","urn":"urn:pulumi:prod::app::c::z"}
	]}`)
	got := driftedURNsFromJSON(js)
	if len(got) != 1 || got[0] != "urn:pulumi:prod::app::b::y" {
		t.Fatalf("expected only the updated URN, got %v", got)
	}
}

func TestDriftedURNsFromJSONBadInput(t *testing.T) {
	if got := driftedURNsFromJSON([]byte("not json")); got != nil {
		t.Fatalf("bad input should yield nil, got %v", got)
	}
}

func TestFailureMessageNeverEmpty(t *testing.T) {
	if got := failureMessage("", nil); got == "" {
		t.Fatal("failureMessage must never return empty (this is the false-resolve guard)")
	}
	if got := failureMessage("", errors.New("killed")); got != "killed" {
		t.Fatalf("expected process error fallback, got %q", got)
	}
	if got := failureMessage("boom", nil); got != "boom" {
		t.Fatalf("expected stderr, got %q", got)
	}
}
