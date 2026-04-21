package freeze

import (
	"testing"
	"time"
)

func TestActiveForFridayAfternoon(t *testing.T) {
	cfg := Config{Windows: []Window{{
		Name: "friday-afternoon", Cron: "0 15 * * 5",
		Duration: 65 * time.Hour, // through monday morning
		Stacks:   []string{"prod/*"},
	}}}

	// Friday 2026-04-24 16:00 UTC: window fires at 15:00, still active.
	fri := time.Date(2026, 4, 24, 16, 0, 0, 0, time.UTC)
	name, active, err := ActiveFor(cfg, "prod/api", fri)
	if err != nil {
		t.Fatal(err)
	}
	if !active || name != "friday-afternoon" {
		t.Fatalf("expected friday-afternoon active, got %q active=%v", name, active)
	}

	// Tuesday afternoon: window has long expired.
	tue := time.Date(2026, 4, 28, 16, 0, 0, 0, time.UTC)
	_, active, err = ActiveFor(cfg, "prod/api", tue)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("expected tuesday to be outside freeze")
	}
}

func TestFreezeDoesNotApplyToNonMatchingStack(t *testing.T) {
	cfg := Config{Windows: []Window{{
		Name: "prod-only", Cron: "0 0 * * *", Duration: time.Hour, Stacks: []string{"prod/*"},
	}}}
	// Any time, dev stack should never be in freeze.
	now := time.Date(2026, 4, 24, 0, 30, 0, 0, time.UTC)
	_, active, err := ActiveFor(cfg, "dev/api", now)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("dev stack should not be in prod-only freeze")
	}
}

func TestInvalidCronBubblesError(t *testing.T) {
	cfg := Config{Windows: []Window{{Name: "bad", Cron: "nonsense"}}}
	_, _, err := ActiveFor(cfg, "prod/api", time.Now())
	if err == nil {
		t.Fatal("expected parse error")
	}
}
