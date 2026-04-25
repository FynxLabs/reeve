// Package summary builds structured plan/apply summaries from engine JSON
// output: counts of add/change/delete/replace, resource-level diffs, error
// highlights. Engine-agnostic - consumes a normalized shape produced by
// internal/iac adapters.
package summary
