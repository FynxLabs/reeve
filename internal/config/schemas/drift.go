package schemas

// Drift is .reeve/drift.yaml.
type Drift struct {
	Header    `yaml:",inline"`
	Scope     DriftScope          `yaml:"scope"`
	Behavior  DriftBehavior       `yaml:"behavior"`
	Freshness DriftFreshness      `yaml:"freshness"`
	Schedules map[string]Schedule `yaml:"schedules"`
	Channels  []ChannelYAML       `yaml:"channels"`
	// DeprecatedSinks is the original spelling of Channels (drift.yaml's
	// `sinks:` key shipped in v0.2.0). The loader REJECTS it with a hard
	// error pointing at `reeve migrate-config`, which rewrites `sinks:` to
	// `channels:`. The field exists only so the strict decoder can parse the
	// key and produce that error instead of a generic unknown-key failure.
	DeprecatedSinks []ChannelYAML `yaml:"sinks,omitempty"`
}

type DriftScope struct {
	IncludePatterns []string `yaml:"include_patterns"`
	ExcludePatterns []string `yaml:"exclude_patterns"`
}

type DriftBehavior struct {
	RefreshBeforeCheck bool           `yaml:"refresh_before_check"`
	MaxParallelStacks  int            `yaml:"max_parallel_stacks"`
	ExitOn             DriftExitOn    `yaml:"exit_on"`
	StateBootstrap     StateBootstrap `yaml:"state_bootstrap"`
}

type DriftExitOn struct {
	DriftDetected bool `yaml:"drift_detected"`
	DriftOngoing  bool `yaml:"drift_ongoing"`
	RunError      bool `yaml:"run_error"`
}

type StateBootstrap struct {
	Mode           string `yaml:"mode"` // baseline | alert_all | require_manual
	BaselineMaxAge string `yaml:"baseline_max_age"`
}

type DriftFreshness struct {
	Enabled         bool   `yaml:"enabled"`
	Window          string `yaml:"window"`
	RespectFailures bool   `yaml:"respect_failures"`
}

type Schedule struct {
	Patterns        []string `yaml:"patterns"`
	ExcludePatterns []string `yaml:"exclude_patterns"`
}

// DriftPayload tunes the webhook channel's payload shape.
type DriftPayload struct {
	Format      string            `yaml:"format,omitempty"`
	DedupeKey   string            `yaml:"dedupe_key,omitempty"`
	SeverityMap map[string]string `yaml:"severity_map,omitempty"`
}
