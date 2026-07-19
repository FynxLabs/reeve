package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/thefynx/reeve/internal/config/schemas"
)

type fakeSink struct {
	name   string
	events []Event
	fn     func(ctx context.Context, p Payload) error
}

func (f *fakeSink) Name() string        { return f.name }
func (f *fakeSink) Subscribes() []Event { return f.events }
func (f *fakeSink) Deliver(ctx context.Context, p Payload) error {
	if f.fn != nil {
		return f.fn(ctx, p)
	}
	return nil
}

func TestRegistryBuildResolvesByType(t *testing.T) {
	Register("test_reg_a", func(_ context.Context, cfg schemas.SinkYAML, _ Deps) (Sink, error) {
		return &fakeSink{name: cfg.EffectiveName(), events: ParseEvents(cfg.On)}, nil
	})
	sinks, err := Build(context.Background(), []schemas.SinkYAML{
		{Type: "test_reg_a", Name: "one", On: []string{"applied"}},
		{Type: "test_reg_a"}, // name falls back to type
	}, Deps{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(sinks) != 2 {
		t.Fatalf("want 2 sinks, got %d", len(sinks))
	}
	if sinks[0].Name() != "one" || sinks[1].Name() != "test_reg_a" {
		t.Fatalf("names: %q %q", sinks[0].Name(), sinks[1].Name())
	}
	if got := sinks[0].Subscribes(); len(got) != 1 || got[0] != EventApplied {
		t.Fatalf("subscriptions: %v", got)
	}
}

func TestRegistryUnknownTypeErrors(t *testing.T) {
	_, err := Build(context.Background(), []schemas.SinkYAML{{Type: "no_such_sink"}}, Deps{})
	if err == nil || !strings.Contains(err.Error(), `unknown notification sink type "no_such_sink"`) {
		t.Fatalf("want unknown-type error, got %v", err)
	}
}

func TestRegistrySkipsDisabledAndNilSinks(t *testing.T) {
	off := false
	Register("test_reg_skip", func(context.Context, schemas.SinkYAML, Deps) (Sink, error) {
		return nil, nil // unmet optional dependency
	})
	sinks, err := Build(context.Background(), []schemas.SinkYAML{
		{Type: "test_reg_skip"},
		{Type: "no_such_sink", Enabled: &off}, // disabled: type not even resolved
	}, Deps{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(sinks) != 0 {
		t.Fatalf("want 0 sinks, got %d", len(sinks))
	}
}

func TestRegistryConstructorErrorIsLabeled(t *testing.T) {
	boom := errors.New("boom")
	Register("test_reg_err", func(context.Context, schemas.SinkYAML, Deps) (Sink, error) {
		return nil, boom
	})
	_, err := Build(context.Background(), []schemas.SinkYAML{{Type: "test_reg_err", Name: "mine"}}, Deps{})
	if err == nil || !errors.Is(err, boom) || !strings.Contains(err.Error(), "mine") {
		t.Fatalf("want labeled constructor error, got %v", err)
	}
}

func TestParseEventsDropsUnknown(t *testing.T) {
	got := ParseEvents([]string{"applied", "bogus", "drift_detected"})
	if len(got) != 2 || got[0] != EventApplied || got[1] != EventDriftDetected {
		t.Fatalf("ParseEvents: %v", got)
	}
}

func TestEventNamesMatchSchemas(t *testing.T) {
	// TimelinePREvents is the full PR-flow set (core lifecycle + planning +
	// break_glass); with the drift events it must cover schemas exactly.
	all := append(TimelinePREvents(), DriftEvents()...)
	if len(all) != len(schemas.ValidSinkEvents) {
		t.Fatalf("event count mismatch: notify %d vs schemas %d", len(all), len(schemas.ValidSinkEvents))
	}
	for _, e := range all {
		if !schemas.IsValidSinkEvent(string(e)) {
			t.Fatalf("event %q missing from schemas.ValidSinkEvents", e)
		}
	}
}

func TestTimelinePREventsSupersetOfCore(t *testing.T) {
	// The legacy default set must stay exactly the core lifecycle: widening
	// it would silently change existing sinks' subscriptions.
	core := PREvents()
	if core[0] != EventPlan || len(core) != 7 {
		t.Fatalf("core lifecycle changed: %v", core)
	}
	tl := TimelinePREvents()
	if tl[0] != EventPlanning || tl[len(tl)-1] != EventBreakGlass || len(tl) != len(core)+2 {
		t.Fatalf("timeline events: %v", tl)
	}
}
