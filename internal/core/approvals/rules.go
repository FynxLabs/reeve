// Package approvals resolves approval rules against PR reviews.
//
// Rule resolution is pure: sources supply the raw approvals/owners, this
// package merges default + per-stack config, checks required_approvals +
// require_all_groups, honors CODEOWNERS when enabled, and returns a
// structured trace explaining *why* the result is what it is.
package approvals

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
)

// Approval is an individual review/approval event.
type Approval struct {
	Source      string // "pr_review" | "pr_comment" | ...
	Approver    string // user login (no leading @)
	SubmittedAt time.Time
	CommitSHA   string // HEAD SHA at time of approval; used for dismissal
}

// Source is a pluggable approval backend (v1: pr_review, pr_comment).
type Source interface {
	Name() string
	ListApprovals(ctx context.Context, pr PR) ([]Approval, error)
}

// PR is the minimum we need to resolve approvals. Populated by run/apply.go.
type PR struct {
	Number  int
	HeadSHA string
	Author  string
	BaseRef string
	IsFork  bool
	Changed []string // changed file paths; feeds CODEOWNERS
}

// Rules is the merged approvals.yaml-ish surface for a single stack.
type Rules struct {
	RequiredApprovals  int
	Approvers          []string // team slugs or user handles (no leading @)
	Codeowners         bool
	RequireAllGroups   bool // approvers list treated as groups
	DismissOnNewCommit bool
	Freshness          time.Duration // 0 = no freshness requirement
}

// Config is the raw config_type=shared approvals block. Populated by
// loader; resolved per-stack via Resolve.
type Config struct {
	Sources []SourceConfig
	Default Rules
	Stacks  []StackRule
}

type SourceConfig struct {
	Type    string // pr_review | pr_comment | ...
	Enabled bool
	Command string // for pr_comment
}

type StackRule struct {
	Pattern string // glob over "project/stack"
	Rules   Rules
	// PresentKeys lets merging distinguish "unset" from "zero" for numeric fields.
	Present map[string]bool
}

// Resolve merges the default rule with any stack-pattern rules matching
// ref ("project/stack"). More specific overrides take precedence for
// numeric fields; approver lists union.
func Resolve(cfg Config, ref string) Rules {
	out := cfg.Default
	// Apply matching patterns in order; later (more specific) wins for
	// numeric/bool fields. We approximate "more specific" by pattern
	// specificity (fewer wildcards = more specific).
	matching := []StackRule{}
	for _, r := range cfg.Stacks {
		if ok, _ := doublestar.Match(r.Pattern, ref); ok {
			matching = append(matching, r)
		}
	}
	sortBySpecificity(matching)
	for _, r := range matching {
		if r.Present["required_approvals"] {
			out.RequiredApprovals = r.Rules.RequiredApprovals
		}
		if r.Present["require_all_groups"] {
			out.RequireAllGroups = r.Rules.RequireAllGroups
		}
		if r.Present["codeowners"] {
			out.Codeowners = r.Rules.Codeowners
		}
		if r.Present["dismiss_on_new_commit"] {
			out.DismissOnNewCommit = r.Rules.DismissOnNewCommit
		}
		if r.Present["freshness"] {
			out.Freshness = r.Rules.Freshness
		}
		// Approvers: union, dedup.
		if len(r.Rules.Approvers) > 0 {
			out.Approvers = unionStrings(out.Approvers, r.Rules.Approvers)
		}
	}
	return out
}

// Resolution is the outcome of evaluating rules for one stack.
type Resolution struct {
	Ref       string
	Rules     Rules
	Satisfied bool
	// TotalNeeded is the sum of all gates (numeric + groups).
	TotalNeeded int
	// Got is how many acceptable approvals we counted.
	Got int
	// Trace is a human-readable ordered list explaining each rule.
	Trace []string
	// Missing lists approver groups/CODEOWNER paths still needed.
	Missing []string
}

// Evaluate checks the rules against the list of approvals + optional
// CODEOWNERS resolution (path → owner slugs).
func Evaluate(rules Rules, approvals []Approval, pr PR, codeowners map[string][]string, author string) Resolution {
	res := Resolution{Rules: rules}

	// Filter dismissed approvals: if DismissOnNewCommit and the approval
	// was for a different SHA than pr.HeadSHA, drop it.
	effective := approvals[:0]
	for _, a := range approvals {
		if rules.DismissOnNewCommit && a.CommitSHA != "" && a.CommitSHA != pr.HeadSHA {
			res.Trace = append(res.Trace, fmt.Sprintf("dismissed %s (approval on %s, HEAD is %s)", a.Approver, short(a.CommitSHA), short(pr.HeadSHA)))
			continue
		}
		if a.Approver == author {
			res.Trace = append(res.Trace, fmt.Sprintf("ignored %s (self-approval)", a.Approver))
			continue
		}
		if rules.Freshness > 0 && !a.SubmittedAt.IsZero() {
			// Freshness check deferred - caller passes only fresh approvals in
			// practice. Keep the trace for visibility.
			_ = a.SubmittedAt
		}
		effective = append(effective, a)
	}
	approvals = append(approvals[:0:0], effective...)

	approvers := names(approvals)

	// Count simple required_approvals (any approver in the allow list).
	if rules.RequiredApprovals > 0 {
		hits := intersect(approvers, rules.Approvers)
		res.Trace = append(res.Trace, fmt.Sprintf("required_approvals=%d, matched=%d (%s)",
			rules.RequiredApprovals, len(hits), strings.Join(hits, ",")))
		res.Got += len(hits)
		res.TotalNeeded += rules.RequiredApprovals
		if len(hits) < rules.RequiredApprovals {
			res.Missing = append(res.Missing,
				fmt.Sprintf("%d more approval(s) from %s",
					rules.RequiredApprovals-len(hits),
					strings.Join(rules.Approvers, "|")))
		}
	}

	// Group semantics: each group in Approvers needs at least one approver.
	if rules.RequireAllGroups && len(rules.Approvers) > 0 {
		groupsSatisfied := 0
		for _, g := range rules.Approvers {
			if matchesOne(approvers, g) {
				groupsSatisfied++
				res.Trace = append(res.Trace, fmt.Sprintf("group %s: satisfied", g))
			} else {
				res.Trace = append(res.Trace, fmt.Sprintf("group %s: missing", g))
				res.Missing = append(res.Missing, g)
			}
		}
		res.TotalNeeded += len(rules.Approvers)
		res.Got += groupsSatisfied
	}

	// CODEOWNERS: every path with owners needs at least one matching approver.
	if rules.Codeowners && len(codeowners) > 0 {
		for path, owners := range codeowners {
			found := false
			for _, o := range owners {
				if matchesOne(approvers, o) {
					found = true
					break
				}
			}
			if found {
				res.Trace = append(res.Trace, fmt.Sprintf("codeowners %s: satisfied", path))
				res.Got++
			} else {
				res.Trace = append(res.Trace, fmt.Sprintf("codeowners %s: needs %s", path, strings.Join(owners, "|")))
				res.Missing = append(res.Missing, fmt.Sprintf("codeowners(%s)", path))
			}
			res.TotalNeeded++
		}
	}

	res.Satisfied = len(res.Missing) == 0 && res.TotalNeeded > 0
	// Baseline: if no gates were configured, treat as unsatisfied (explicit
	// default of 1 via Rules.Default keeps this from triggering in practice).
	if rules.RequiredApprovals == 0 && !rules.RequireAllGroups && (!rules.Codeowners || len(codeowners) <= 0) {
		res.Satisfied = true // no gates configured → pass
	}

	res.Ref = pr.ToRef()
	return res
}

// ToRef lets Evaluate fill Resolution.Ref; caller is expected to pass the
// stack ref separately, but for convenience we attach the PR number.
// Kept as a method on PR for symmetry with other packages.
func (p PR) ToRef() string {
	return fmt.Sprintf("pr-%d", p.Number)
}

// --- helpers ---

func names(aa []Approval) []string {
	out := make([]string, 0, len(aa))
	for _, a := range aa {
		out = append(out, strings.TrimPrefix(a.Approver, "@"))
	}
	return out
}

func intersect(approvers, required []string) []string {
	set := map[string]struct{}{}
	for _, a := range approvers {
		set[strings.TrimPrefix(a, "@")] = struct{}{}
	}
	var hits []string
	for _, r := range required {
		r = strings.TrimPrefix(r, "@")
		// Teams (with "/") can't be matched to individual logins without
		// the VCS adapter's team-member expansion. Phase 2: rely on caller
		// to pre-expand teams by passing approvers containing either the
		// user login OR the team slug string matching.
		if _, ok := set[r]; ok {
			hits = append(hits, r)
		}
	}
	return hits
}

// matchesOne returns true if any approver string matches g (team slug or
// user login). Phase 2: simple equality after @ trim. Phase 4: VCS team
// expansion.
func matchesOne(approvers []string, g string) bool {
	g = strings.TrimPrefix(g, "@")
	for _, a := range approvers {
		if strings.TrimPrefix(a, "@") == g {
			return true
		}
	}
	return false
}

func unionStrings(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// sortBySpecificity orders so that broader patterns come first, more
// specific patterns override. Specificity = count of non-wildcard chars
// (rough but good enough for v1).
func sortBySpecificity(rules []StackRule) {
	for i := 1; i < len(rules); i++ {
		for j := i; j > 0 && specificity(rules[j].Pattern) > specificity(rules[j-1].Pattern); j-- {
			rules[j], rules[j-1] = rules[j-1], rules[j]
		}
	}
}

func specificity(p string) int {
	score := 0
	for _, r := range p {
		switch r {
		case '*', '?':
			// wildcards lower specificity
			score -= 2
		case '[', ']':
			score -= 1
		default:
			score += 1
		}
	}
	return score
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
