// Package slack is the Slack notification sink. One implementation serves
// both producers: drift events post dashboard-style messages; PR-flow events
// drive the per-PR message lifecycle (upsert + thread timeline) with state
// persisted in the blob store.
package slack

import (
	"context"
	"os"
	"strings"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/notify"
	"github.com/thefynx/reeve/internal/slack"
)

func init() {
	notify.Register("slack", New)
}

// Sink delivers events as Slack messages.
type Sink struct {
	name    string
	client  Client
	channel string
	events  []notify.Event
	trigger schemas.SlackTrigger
	icons   *schemas.SlackIcons
	rules   []schemas.SlackNotifyRule
	blob    blob.Store
}

// Client is the slack API surface the sink consumes; *slack.Client
// satisfies it. Narrow so tests can fake it.
type Client interface {
	Post(ctx context.Context, m slack.Message) (*slack.PostResult, error)
	Update(ctx context.Context, m slack.Message) (*slack.PostResult, error)
	Upsert(ctx context.Context, channel, ts, text string, blocks []slack.Block) (*slack.PostResult, error)
	PostThread(ctx context.Context, channel, parentTS, text string, blocks []slack.Block) (*slack.PostResult, error)
}

// New is the registered constructor. The token comes from the sink's
// auth_token (notifications.yaml) or falls back to Deps.SlackToken
// (drift.yaml sinks read SLACK_BOT_TOKEN); with no token the sink is
// skipped, matching the previous factory behavior.
func New(_ context.Context, cfg schemas.SinkYAML, deps notify.Deps) (notify.Sink, error) {
	token := ExpandEnvRef(cfg.AuthToken)
	if token == "" {
		token = deps.SlackToken
	}
	if token == "" {
		return nil, nil
	}
	events := notify.ParseEvents(cfg.On)
	if len(cfg.On) == 0 {
		events = defaultPREvents(cfg.Trigger)
	}
	return &Sink{
		name:    cfg.EffectiveName(),
		client:  slack.New(token),
		channel: cfg.Channel,
		events:  events,
		trigger: cfg.Trigger,
		icons:   cfg.Icons,
		rules:   cfg.Rules,
		blob:    deps.Blob,
	}, nil
}

func (s *Sink) Name() string               { return s.name }
func (s *Sink) Subscribes() []notify.Event { return s.events }

// Deliver routes by producer: drift payloads post standalone messages,
// PR payloads drive the per-PR message lifecycle.
func (s *Sink) Deliver(ctx context.Context, p notify.Payload) error {
	switch {
	case p.Drift != nil:
		return s.deliverDrift(ctx, p)
	case p.PR != nil:
		return s.deliverPR(ctx, p)
	}
	return nil
}

// defaultPREvents preserves the legacy default subscription: with no
// explicit events list, every lifecycle event at or after the trigger is
// enabled. `apply` (and empty) is not itself a lifecycle event, so it
// enables everything - exactly what schemas.SlackEventEnabled did.
func defaultPREvents(trigger schemas.SlackTrigger) []notify.Event {
	order := notify.PREvents()
	idx := 0
	for i, e := range order {
		if string(e) == string(trigger) {
			idx = i
			break
		}
	}
	return order[idx:]
}

// ExpandEnvRef unwraps "${env:NAME}"; other strings pass through. Config
// loading already expands these, but sinks may be built from configs that
// skipped that pass.
func ExpandEnvRef(s string) string {
	if strings.HasPrefix(s, "${env:") && strings.HasSuffix(s, "}") {
		return os.Getenv(strings.TrimSuffix(strings.TrimPrefix(s, "${env:"), "}"))
	}
	return s
}
