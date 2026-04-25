package schemas

// Drift is .reeve/drift.yaml.
type Drift struct {
	Header                `yaml:",inline"`
	Scope                 DriftScope          `yaml:"scope"`
	Behavior              DriftBehavior       `yaml:"behavior"`
	Classification        DriftClassification `yaml:"classification"`
	Freshness             DriftFreshness      `yaml:"freshness"`
	Schedules             map[string]Schedule `yaml:"schedules"`
	PermanentSuppressions []SuppressionYAML   `yaml:"permanent_suppressions"`
	Sinks                 []DriftSinkYAML     `yaml:"sinks"`
}

type DriftScope struct {
	IncludePatterns []string `yaml:"include_patterns"`
	ExcludePatterns []string `yaml:"exclude_patterns"`
}

type DriftBehavior struct {
	RefreshBeforeCheck    bool           `yaml:"refresh_before_check"`
	MaxParallelStacks     int            `yaml:"max_parallel_stacks"`
	TimeoutPerStack       string         `yaml:"timeout_per_stack"`
	RetryOnTransientError int            `yaml:"retry_on_transient_error"`
	ExitOn                DriftExitOn    `yaml:"exit_on"`
	StateBootstrap        StateBootstrap `yaml:"state_bootstrap"`
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

type DriftClassification struct {
	IgnoreProperties []IgnoreProp `yaml:"ignore_properties"`
	IgnoreResources  []string     `yaml:"ignore_resources"`
	TreatAsDrift     TreatAsDrift `yaml:"treat_as_drift"`
}

type IgnoreProp struct {
	ResourceType string   `yaml:"resource_type"`
	Properties   []string `yaml:"properties"`
}

type TreatAsDrift struct {
	OrphanedState bool `yaml:"orphaned_state"`
	MissingState  bool `yaml:"missing_state"`
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

type SuppressionYAML struct {
	Stack  string `yaml:"stack"`
	Until  string `yaml:"until"`
	Reason string `yaml:"reason"`
}

// DriftSinkYAML is intentionally polymorphic - Type chooses the adapter.
type DriftSinkYAML struct {
	Type           string            `yaml:"type"`
	Name           string            `yaml:"name,omitempty"`
	On             []string          `yaml:"on"`
	Channel        string            `yaml:"channel,omitempty"`
	Grouping       string            `yaml:"grouping,omitempty"`
	URL            string            `yaml:"url,omitempty"`
	Headers        map[string]string `yaml:"headers,omitempty"`
	Payload        DriftPayload      `yaml:"payload,omitempty"`
	IntegrationKey string            `yaml:"integration_key,omitempty"`
	SeverityMap    map[string]string `yaml:"severity_map,omitempty"`
	Emitter        string            `yaml:"emitter,omitempty"`
	Labels         []string          `yaml:"labels,omitempty"`
	Assignees      []string          `yaml:"assignees,omitempty"`
}

type DriftPayload struct {
	Format      string            `yaml:"format,omitempty"`
	DedupeKey   string            `yaml:"dedupe_key,omitempty"`
	SeverityMap map[string]string `yaml:"severity_map,omitempty"`
}
