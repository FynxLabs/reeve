package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/thefynx/reeve/internal/config/schemas"
)

type fakeChannel struct {
	name   string
	events []Event
	fn     func(ctx context.Context, p Payload) error
}

func (f *fakeChannel) Name() string        { return f.name }
func (f *fakeChannel) Subscribes() []Event { return f.events }
func (f *fakeChannel) Deliver(ctx context.Context, p Payload) error {
	if f.fn != nil {
		return f.fn(ctx, p)
	}
	return nil
}

func TestRegistryBuildResolvesByType(t *testing.T) {
	Register("test_reg_a", func(_ context.Context, cfg schemas.ChannelYAML, _ Deps) (Channel, error) {
		return &fakeChannel{name: cfg.EffectiveName(), events: ParseEvents(cfg.On)}, nil
	})
	channels, err := Build(context.Background(), []schemas.ChannelYAML{
		{Type: "test_reg_a", Name: "one", On: []string{"applied"}},
		{Type: "test_reg_a"}, // name falls back to type
	}, Deps{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("want 2 channels, got %d", len(channels))
	}
	if channels[0].Name() != "one" || channels[1].Name() != "test_reg_a" {
		t.Fatalf("names: %q %q", channels[0].Name(), channels[1].Name())
	}
	if got := channels[0].Subscribes(); len(got) != 1 || got[0] != EventApplied {
		t.Fatalf("subscriptions: %v", got)
	}
}

func TestRegistryUnknownTypeErrors(t *testing.T) {
	_, err := Build(context.Background(), []schemas.ChannelYAML{{Type: "no_such_channel"}}, Deps{})
	if err == nil || !strings.Contains(err.Error(), `unknown notification channel type "no_such_channel"`) {
		t.Fatalf("want unknown-type error, got %v", err)
	}
}

func TestRegistrySkipsDisabledAndNilChannels(t *testing.T) {
	off := false
	Register("test_reg_skip", func(context.Context, schemas.ChannelYAML, Deps) (Channel, error) {
		return nil, nil // unmet optional dependency
	})
	channels, err := Build(context.Background(), []schemas.ChannelYAML{
		{Type: "test_reg_skip"},
		{Type: "no_such_channel", Enabled: &off}, // disabled: type not even resolved
	}, Deps{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(channels) != 0 {
		t.Fatalf("want 0 channels, got %d", len(channels))
	}
}

func TestRegistryConstructorErrorIsLabeled(t *testing.T) {
	boom := errors.New("boom")
	Register("test_reg_err", func(context.Context, schemas.ChannelYAML, Deps) (Channel, error) {
		return nil, boom
	})
	_, err := Build(context.Background(), []schemas.ChannelYAML{{Type: "test_reg_err", Name: "mine"}}, Deps{})
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
	all := append(PREvents(), DriftEvents()...)
	if len(all) != len(schemas.ValidChannelEvents) {
		t.Fatalf("event count mismatch: notify %d vs schemas %d", len(all), len(schemas.ValidChannelEvents))
	}
	for _, e := range all {
		if !schemas.IsValidChannelEvent(string(e)) {
			t.Fatalf("event %q missing from schemas.ValidChannelEvents", e)
		}
	}
}
