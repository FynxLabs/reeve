// Package otel emits OpenTelemetry traces and metrics (PLAN.md §5.7).
// Opt-in: off unless observability.yaml exists. Honors standard
// OTEL_EXPORTER_OTLP_* env vars. Stack-name label cardinality is gated
// via observability.otel.stack_cardinality config (default: hash).
package otel
