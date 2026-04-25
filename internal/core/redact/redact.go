// Package redact is the central secret-redaction pipeline. Every
// user-visible output path (PR comment, audit log, run artifacts,
// policy hook stdout, telemetry) funnels through a Redactor. See
// openspec/specs/core/rendering and openspec/specs/iac/policy-hooks.
package redact

import (
	"regexp"
	"strings"
)

// Redactor wraps a set of rules. Immutable once constructed.
type Redactor struct {
	rules       []*regexp.Regexp
	replacement string
	// Known string values to replace (exact-match). Populated by
	// AddSecret - used when callers know the secret value (e.g. from
	// Pulumi's --show-secrets=false or from an auth credential).
	secrets map[string]struct{}
}

// New returns a Redactor with no rules - everything passes through.
func New() *Redactor { return &Redactor{replacement: "[redacted]", secrets: map[string]struct{}{}} }

// WithReplacement changes the replacement string. Default "[redacted]".
func (r *Redactor) WithReplacement(s string) *Redactor {
	r.replacement = s
	return r
}

// AddRule compiles a regex and adds it. Invalid regexes are skipped with
// no error - use CompileRules for validation.
func (r *Redactor) AddRule(pattern string) *Redactor {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return r
	}
	r.rules = append(r.rules, re)
	return r
}

// CompileRules compiles a list of regex strings. Returns any compile
// errors so callers can surface them at lint time.
func CompileRules(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

// NewFromRules returns a Redactor from pre-compiled regexes.
func NewFromRules(rules []*regexp.Regexp) *Redactor {
	return &Redactor{rules: rules, replacement: "[redacted]", secrets: map[string]struct{}{}}
}

// AddSecret registers a literal string to redact. Empty strings are
// ignored. Callers should register credential values after acquiring
// them so stdout leaks don't expose them.
func (r *Redactor) AddSecret(s string) {
	if len(s) < 4 {
		return // too short to safely redact - false positives would eat everything
	}
	r.secrets[s] = struct{}{}
}

// Redact returns s with all rules + known secrets replaced.
func (r *Redactor) Redact(s string) string {
	if r == nil {
		return s
	}
	out := s
	// Literal-secret replacement first.
	for sec := range r.secrets {
		out = strings.ReplaceAll(out, sec, r.replacement)
	}
	// Regex rules second.
	for _, re := range r.rules {
		out = re.ReplaceAllString(out, r.replacement)
	}
	// Pulumi [secret] markers - handled idempotently.
	out = pulumiSecretMarker.ReplaceAllString(out, r.replacement)
	return out
}

// Redactables passes any struct through Redact field-by-field.
// For now we only need Redact(string) - callers handle nested structs.

// pulumiSecretMarker catches the standard Pulumi marker for secret values
// that escape into engine output: `[secret]` or `<secret>`.
var pulumiSecretMarker = regexp.MustCompile(`\[secret\]|<secret>`)
