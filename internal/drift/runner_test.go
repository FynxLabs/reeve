package drift

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/core/redact"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/iac"
)

// fakeEngine returns a canned DriftCheck result.
type fakeEngine struct {
	res iac.PreviewResult
	err error
}

func (fakeEngine) Name() string { return "fake" }
func (fakeEngine) EnumerateStacks(context.Context, string) ([]discovery.Stack, error) {
	return nil, nil
}
func (f fakeEngine) DriftCheck(context.Context, discovery.Stack, iac.PreviewOpts, bool) (iac.PreviewResult, error) {
	return f.res, f.err
}

func runOneWith(engine Engine, mode string) (Item, Event) {
	opts := Options{
		Engine:        engine,
		Redactor:      redact.New(),
		BootstrapMode: mode,
		// nil StateStore/SuppressionStore/AuthResolver: runOne handles nil.
	}
	item, ev, _, _ := runOne(context.Background(), opts, discovery.Stack{Project: "p", Name: "s", Path: "p/s"}, time.Now())
	return item, ev
}

// TestRunOneFailedCheckIsNotNoDrift is the regression for the critical
// false-resolve: a DriftCheck that fails (non-nil error, empty result) must
// classify as an error / check_failed, never as no_drift - otherwise an
// active drift alert silently resolves.
func TestRunOneFailedCheckIsNotNoDrift(t *testing.T) {
	// Empty result + non-nil error (e.g. timeout kill with no stderr).
	item, ev := runOneWith(fakeEngine{res: iac.PreviewResult{}, err: errors.New("context deadline exceeded")}, "")
	if item.Outcome != OutcomeError {
		t.Fatalf("failed check must be OutcomeError, got %s", item.Outcome)
	}
	if ev != EventCheckFailed {
		t.Fatalf("failed check must emit check_failed, got %s", ev)
	}

	// Error string set but no error return (refresh-style failure path).
	item, ev = runOneWith(fakeEngine{res: iac.PreviewResult{Error: "refresh failed"}}, "")
	if item.Outcome != OutcomeError || ev != EventCheckFailed {
		t.Fatalf("error-string result must be a failed check, got outcome=%s ev=%s", item.Outcome, ev)
	}
}

func TestRunOneNoDriftAndDrift(t *testing.T) {
	// Genuinely clean: empty counts, no error → no_drift, silent.
	item, ev := runOneWith(fakeEngine{res: iac.PreviewResult{}}, "")
	if item.Outcome != OutcomeNoDrift || ev != EventNone {
		t.Fatalf("clean check must be silent no_drift, got outcome=%s ev=%s", item.Outcome, ev)
	}

	// Drift with changed resources → drift_detected, fingerprint from URNs.
	res := iac.PreviewResult{
		Counts:      summary.Counts{Change: 1},
		DriftedURNs: []string{"urn:pulumi:prod::app::aws:s3/bucket:Bucket::data"},
	}
	item, ev = runOneWith(fakeEngine{res: res}, "")
	if item.Outcome != OutcomeDriftDetected || ev != EventDriftDetected {
		t.Fatalf("changed resources must detect drift, got outcome=%s ev=%s", item.Outcome, ev)
	}
	if item.Fingerprint == "" {
		t.Fatal("drift fingerprint should be set from DriftedURNs")
	}
}

// TestRunOneBaselineBootstrapSilent verifies baseline mode records
// pre-existing drift silently on the first run (no event) rather than firing.
func TestRunOneBaselineBootstrapSilent(t *testing.T) {
	res := iac.PreviewResult{
		Counts:      summary.Counts{Change: 1},
		DriftedURNs: []string{"urn:pulumi:prod::app::aws:s3/bucket:Bucket::data"},
	}
	item, ev := runOneWith(fakeEngine{res: res}, "baseline")
	if item.Outcome != OutcomeDriftDetected {
		t.Fatalf("baseline still records the drift outcome, got %s", item.Outcome)
	}
	if ev != EventNone {
		t.Fatalf("baseline first run must be silent, got %s", ev)
	}
	if item.Fingerprint == "" {
		t.Fatal("baseline must persist the drift fingerprint")
	}
}
