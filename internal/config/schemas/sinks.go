package schemas

// SinkYAML is one generic notification-sink declaration. It is shared by
// notifications.yaml (`sinks:`) and drift.yaml (`sinks:`). Type chooses the
// adapter; each adapter reads only the fields it cares about. `On` lists the
// events the sink subscribes to (see ValidSinkEvents).
type SinkYAML struct {
	Type    string   `yaml:"type"`
	Name    string   `yaml:"name,omitempty"`
	Enabled *bool    `yaml:"enabled,omitempty"` // nil == true
	On      []string `yaml:"on,omitempty"`

	// slack
	Channel   string            `yaml:"channel,omitempty"`
	AuthToken string            `yaml:"auth_token,omitempty"` // "${env:SLACK_BOT_TOKEN}"
	Trigger   SlackTrigger      `yaml:"trigger,omitempty"`
	Icons     *SlackIcons       `yaml:"icons,omitempty"`
	Rules     []SlackNotifyRule `yaml:"rules,omitempty"`
	Grouping  string            `yaml:"grouping,omitempty"`

	// webhook
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Payload DriftPayload      `yaml:"payload,omitempty"`

	// pagerduty
	IntegrationKey string            `yaml:"integration_key,omitempty"`
	SeverityMap    map[string]string `yaml:"severity_map,omitempty"`

	// otel_annotation
	Emitter string `yaml:"emitter,omitempty"`

	// github_issue
	Labels    []string `yaml:"labels,omitempty"`
	Assignees []string `yaml:"assignees,omitempty"`
}

// IsEnabled reports whether the sink should be built. Enabled defaults to
// true when omitted.
func (s SinkYAML) IsEnabled() bool { return s.Enabled == nil || *s.Enabled }

// EffectiveName returns Name, falling back to Type.
func (s SinkYAML) EffectiveName() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Type
}

// Sink event names. This is the canonical list validated at load/lint time.
// PR-flow events come from the run pipeline; drift events from the drift
// runner. The strings are shared with internal/notify's Event constants.
const (
	// PR-flow events.
	SinkEventPlanning = "planning" // preview run started
	SinkEventPlan     = "plan"     // preview finished, pending approval
	SinkEventReady    = "ready"    // /reeve ready (or auto_ready)
	SinkEventApproved = "approved" // preconditions passed, apply imminent
	SinkEventApplying = "applying" // apply loop started
	SinkEventApplied  = "applied"  // apply finished successfully
	SinkEventFailed   = "failed"   // apply errored
	SinkEventBlocked  = "blocked"  // apply blocked (gates/locks)
	// SinkEventBreakGlass fires when an emergency-override (break-glass)
	// apply is authorized; the run pipeline emits it in place of approved.
	SinkEventBreakGlass = "break_glass"

	// Drift events.
	SinkEventDriftDetected = "drift_detected"
	SinkEventDriftOngoing  = "drift_ongoing"
	SinkEventDriftResolved = "drift_resolved"
	SinkEventCheckFailed   = "check_failed"
)

// ValidSinkEvents enumerates every valid `on:` entry, in documentation order.
var ValidSinkEvents = []string{
	SinkEventPlanning,
	SinkEventPlan,
	SinkEventReady,
	SinkEventApproved,
	SinkEventApplying,
	SinkEventApplied,
	SinkEventFailed,
	SinkEventBlocked,
	SinkEventBreakGlass,
	SinkEventDriftDetected,
	SinkEventDriftOngoing,
	SinkEventDriftResolved,
	SinkEventCheckFailed,
}

// IsValidSinkEvent reports whether name is a known `on:` event.
func IsValidSinkEvent(name string) bool {
	for _, e := range ValidSinkEvents {
		if e == name {
			return true
		}
	}
	return false
}
