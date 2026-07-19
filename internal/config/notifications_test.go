package config

import (
	"strings"
	"testing"

	"github.com/thefynx/reeve/internal/config/schemas"
)

const sharedYAMLMin = `version: 1
config_type: shared
bucket:
  type: filesystem
  name: ./.reeve-state
`

const engineYAMLMin = `version: 1
config_type: engine
engine:
  type: pulumi
  stacks:
    - project: api
      path: projects/api
      stacks: [dev]
`

const legacyNotifications = `version: 1
config_type: notifications
slack:
  enabled: true
  channel: "#infra-deploys"
  auth_token: xoxb-test
  trigger: plan
  events: [plan, applied, failed]
  icons:
    engine: ":pulumi:"
  rules:
    - environments: [prod]
`

func TestLegacyNotificationsV1StillLoads(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml":        sharedYAMLMin,
		"pulumi.yaml":        engineYAMLMin,
		"notifications.yaml": legacyNotifications,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	sinks := cfg.Notifications.EffectiveSinks()
	if len(sinks) != 1 {
		t.Fatalf("EffectiveSinks: %+v", sinks)
	}
	s := sinks[0]
	if s.Type != "slack" || !s.IsEnabled() || s.Channel != "#infra-deploys" ||
		s.AuthToken != "xoxb-test" || s.Trigger != schemas.SlackTriggerPlan {
		t.Fatalf("mapped sink: %+v", s)
	}
	if len(s.On) != 3 || s.On[0] != "plan" || s.On[2] != "failed" {
		t.Fatalf("events → on: %v", s.On)
	}
	if s.Icons == nil || s.Icons.Engine != ":pulumi:" {
		t.Fatalf("icons: %+v", s.Icons)
	}
	if len(s.Rules) != 1 || s.Rules[0].Environments[0] != "prod" {
		t.Fatalf("rules: %+v", s.Rules)
	}
}

func TestNotificationsV2SinksLoad(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": sharedYAMLMin,
		"pulumi.yaml": engineYAMLMin,
		"notifications.yaml": `version: 2
config_type: notifications
sinks:
  - type: slack
    channel: "#deploys"
    auth_token: xoxb-x
    on: [plan, applied, drift_detected]
  - type: webhook
    name: audit
    url: https://example.test/hook
    on: [applied, failed]
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	sinks := cfg.Notifications.EffectiveSinks()
	if len(sinks) != 2 {
		t.Fatalf("sinks: %+v", sinks)
	}
	if sinks[0].Type != "slack" || sinks[1].EffectiveName() != "audit" {
		t.Fatalf("sinks: %+v", sinks)
	}
}

func TestNotificationsV3Rejected(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": sharedYAMLMin,
		"pulumi.yaml": engineYAMLMin,
		"notifications.yaml": `version: 3
config_type: notifications
`,
	})
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "unsupported version 3 (want 1..2)") {
		t.Fatalf("want version error, got %v", err)
	}
}

func TestUnknownOnEventRejected(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": sharedYAMLMin,
		"pulumi.yaml": engineYAMLMin,
		"notifications.yaml": `version: 2
config_type: notifications
sinks:
  - type: webhook
    url: https://example.test/hook
    on: [aplied]
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	err = cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `unknown event "aplied"`) {
		t.Fatalf("want unknown-event error, got %v", err)
	}
	if !strings.Contains(err.Error(), "applied") {
		t.Fatalf("error should list valid events: %v", err)
	}
}

func TestUnknownOnEventInDriftRejected(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": sharedYAMLMin,
		"pulumi.yaml": engineYAMLMin,
		"drift.yaml": `version: 1
config_type: drift
sinks:
  - type: slack
    channel: "#drift"
    on: [drift_detcted]
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "drift.yaml") {
		t.Fatalf("want drift.yaml event error, got %v", err)
	}
}

func TestLegacySlackEventsValidated(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": sharedYAMLMin,
		"pulumi.yaml": engineYAMLMin,
		"notifications.yaml": `version: 1
config_type: notifications
slack:
  enabled: true
  channel: "#x"
  events: [aplied]
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "slack.events") {
		t.Fatalf("want slack.events error, got %v", err)
	}
}

func TestDisabledLegacySlackMapsToDisabledSink(t *testing.T) {
	n := &schemas.Notifications{Slack: &schemas.SlackConfig{Enabled: false, Channel: "#x"}}
	sinks := n.EffectiveSinks()
	if len(sinks) != 1 || sinks[0].IsEnabled() {
		t.Fatalf("disabled slack should map to a disabled sink: %+v", sinks)
	}
}
