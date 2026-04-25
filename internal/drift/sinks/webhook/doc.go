// Package webhook delivers drift events as generic HTTP POST with
// payload templating. Phase 8 ships `raw` format only; named presets
// (incident_io, rootly, opsgenie) are out of scope until a user provides
// a real payload - see plan appendix.
package webhook
