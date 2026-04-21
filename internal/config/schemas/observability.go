package schemas

// Observability is .reeve/observability.yaml. Opt-in: off by default. If
// this file does not exist, reeve emits no telemetry at all.
type Observability struct {
	Header      `yaml:",inline"`
	OTEL        OTELConfig         `yaml:"otel"`
	Annotations []AnnotationConfig `yaml:"annotations"`
}

type OTELConfig struct {
	Enabled       bool              `yaml:"enabled"`
	Endpoint      string            `yaml:"endpoint"`
	ServiceName   string            `yaml:"service_name"`
	ResourceAttrs map[string]string `yaml:"resource_attributes"`
	// StackCardinality controls the stack label cardinality:
	//   allow | hash | drop (default: hash)
	StackCardinality string `yaml:"stack_cardinality"`
	// Headers are passed to the OTLP exporter (supports ${env:X}).
	Headers map[string]string `yaml:"headers"`
}

type AnnotationConfig struct {
	Type     string            `yaml:"type"` // grafana | dash0 | datadog | webhook
	URL      string            `yaml:"url"`
	Endpoint string            `yaml:"endpoint"` // some systems prefer "endpoint"
	APIKey   string            `yaml:"api_key"`
	Events   []string          `yaml:"events"`
	Headers  map[string]string `yaml:"headers,omitempty"`
}
