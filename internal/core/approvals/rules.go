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
	Source      string // "pr_review" | ...
	Approver    string // user login (no leading @)
	SubmittedAt time.Time
	CommitSHA   string // HEAD SHA at time of approval; used for dismissal
}

// Source is a pluggable approval backend (v1: pr_review).
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
	Approvers          []string // team slugs ("org/team") or user handles (no leading @)
	Codeowners         bool
	RequireAllGroups   bool // approvers list treated as groups
	DismissOnNewCommit bool
	Freshness          time.Duration // 0 = no freshness requirement
	// TeamMembers is the optional pre-resolved team-slug → member-logins
	// map. Populated by the caller before Evaluate so a rule like
	// `approvers: [my-org/sre]` matches when an SRE team member approves.
	// nil or missing entries fall back to literal slug matching (legacy
	// behavior; safe but never matches a real approval).
	TeamMembers map[string][]string
}

// Config is the raw config_type=shared approvals block. Populated by
// loader; resolved per-stack via Resolve.
type Config struct {
	Default Rules
	Stacks  []StackRule
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
// CODEOWNERS resolution (path → owner slugs). The input approvals slice is
// not mutated - filtering writes into a fresh slice. now is the evaluation
// clock (injected so the package stays pure, matching core/freeze and
// core/locks); it only matters when rules.Freshness > 0.
func Evaluate(rules Rules, approvals []Approval, pr PR, codeowners map[string][]string, author string, now time.Time) Resolution {
	res := Resolution{Rules: rules}

	// Filter dismissed / self / stale approvals into a fresh slice. The
	// previous implementation aliased `approvals[:0]` and patched it back,
	// which silently corrupted the caller's slice header.
	effective := make([]Approval, 0, len(approvals))
	var stale []string
	for _, a := range approvals {
		if rules.DismissOnNewCommit && a.CommitSHA != "" && a.CommitSHA != pr.HeadSHA {
			res.Trace = append(res.Trace, fmt.Sprintf("dismissed %s (approval on %s, HEAD is %s)", a.Approver, short(a.CommitSHA), short(pr.HeadSHA)))
			continue
		}
		if a.Approver == author {
			res.Trace = append(res.Trace, fmt.Sprintf("ignored %s (self-approval)", a.Approver))
			continue
		}
		// Freshness: an approval older than the window at evaluation time
		// does not count. An approval with no timestamp cannot be shown
		// fresh, so it fails closed too.
		if rules.Freshness > 0 {
			if a.SubmittedAt.IsZero() {
				res.Trace = append(res.Trace, fmt.Sprintf("stale %s (no submission time; freshness window %s requires a timestamped approval)", a.Approver, rules.Freshness))
				stale = append(stale, strings.TrimPrefix(a.Approver, "@"))
				continue
			}
			if age := now.Sub(a.SubmittedAt); age > rules.Freshness {
				res.Trace = append(res.Trace, fmt.Sprintf("stale %s (approved %s ago, freshness window %s)", a.Approver, age.Round(time.Minute), rules.Freshness))
				stale = append(stale, strings.TrimPrefix(a.Approver, "@"))
				continue
			}
		}
		effective = append(effective, a)
	}

	approvers := names(effective)

	// Email owners in CODEOWNERS (e.g. docs@example.com) cannot be matched
	// to VCS logins - reeve has no commit-email lookup in the gate path.
	// Filter them out up front so a path owned only by emails never becomes
	// an unsatisfiable requirement, while the remaining login/team owners
	// still gate normally.
	effectiveOwners := codeowners
	if rules.Codeowners && len(codeowners) > 0 {
		effectiveOwners = make(map[string][]string, len(codeowners))
		for path, owners := range codeowners {
			var matchable []string
			for _, o := range owners {
				if isEmailOwner(o) {
					res.Trace = append(res.Trace, fmt.Sprintf("codeowners %s: owner %s is an email address, which reeve cannot match to a login; ignoring", path, o))
					continue
				}
				matchable = append(matchable, o)
			}
			if len(matchable) == 0 {
				res.Trace = append(res.Trace, fmt.Sprintf("codeowners %s: all owners are email addresses; skipping unenforceable requirement", path))
				continue
			}
			effectiveOwners[path] = matchable
		}
	}

	// Safety default: a stack with no effective gate still requires one
	// approval. Previously this path passed with zero approvals, so a repo
	// configured with only a bucket let anyone's `/reeve apply` run with live
	// cloud credentials and no human sign-off. Fail closed instead.
	noNumericGate := rules.RequiredApprovals == 0
	noGroupGate := !rules.RequireAllGroups || len(rules.Approvers) == 0
	noOwnerGate := !rules.Codeowners || len(effectiveOwners) == 0
	if noNumericGate && noGroupGate && noOwnerGate {
		rules.RequiredApprovals = 1
		res.Trace = append(res.Trace, "no approval policy configured; requiring 1 approval by default")
	}

	// Count simple required_approvals. With an allow list, only listed
	// approvers (or their team members) count; with no allow list, any
	// distinct non-author approver counts - GitHub's "require N approvals"
	// semantics, and never an unsatisfiable gate.
	if rules.RequiredApprovals > 0 {
		var hits []string
		if len(rules.Approvers) == 0 {
			hits = distinct(approvers)
		} else {
			hits = intersect(approvers, rules.Approvers, rules.TeamMembers)
		}
		res.Trace = append(res.Trace, fmt.Sprintf("required_approvals=%d, matched=%d (%s)",
			rules.RequiredApprovals, len(hits), strings.Join(hits, ",")))
		res.Got += len(hits)
		res.TotalNeeded += rules.RequiredApprovals
		if len(hits) < rules.RequiredApprovals {
			need := rules.RequiredApprovals - len(hits)
			if len(rules.Approvers) == 0 {
				res.Missing = append(res.Missing, fmt.Sprintf("%d more approval(s)", need))
			} else {
				res.Missing = append(res.Missing,
					fmt.Sprintf("%d more approval(s) from %s", need, strings.Join(rules.Approvers, "|")))
			}
		}
	}

	// Group semantics: each group in Approvers needs at least one approver.
	if rules.RequireAllGroups && len(rules.Approvers) > 0 {
		groupsSatisfied := 0
		for _, g := range rules.Approvers {
			if matchesOne(approvers, g, rules.TeamMembers) {
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
	if rules.Codeowners && len(effectiveOwners) > 0 {
		for path, owners := range effectiveOwners {
			found := false
			for _, o := range owners {
				if matchesOne(approvers, o, rules.TeamMembers) {
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

	// When the gate misses and fresh approvals were discarded for staleness,
	// say so in Missing too - `reeve rules` output should explain why an
	// apparently-approved PR is still blocked.
	if len(res.Missing) > 0 && len(stale) > 0 {
		res.Missing = append(res.Missing,
			fmt.Sprintf("re-approval from %s (stale: older than freshness window %s)", strings.Join(stale, "|"), rules.Freshness))
	}

	// A stack always has at least one gate here (the safety default above
	// injects required_approvals=1 when nothing else applies), so TotalNeeded
	// > 0 always holds and an unconfigured stack fails closed.
	res.Satisfied = len(res.Missing) == 0 && res.TotalNeeded > 0

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

// intersect returns the distinct approvers eligible under the allow-list:
// listed directly, or a member of any listed team slug. GitHub counts
// humans, not list entries - required_approvals=2 with a single team in
// approvers passes once two team members approve.
func intersect(approvers, required []string, teams map[string][]string) []string {
	eligible := map[string]struct{}{}
	for _, r := range required {
		r = strings.TrimPrefix(r, "@")
		if isTeamSlug(r) {
			for _, member := range teams[r] {
				eligible[strings.TrimPrefix(member, "@")] = struct{}{}
			}
			continue
		}
		eligible[r] = struct{}{}
	}
	seen := map[string]struct{}{}
	var hits []string
	for _, a := range approvers {
		a = strings.TrimPrefix(a, "@")
		if _, ok := eligible[a]; !ok {
			continue
		}
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		hits = append(hits, a)
	}
	return hits
}

// distinct returns the unique approver logins (leading @ trimmed). Used when
// required_approvals has no allow list, so any non-author approval counts.
func distinct(approvers []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, a := range approvers {
		a = strings.TrimPrefix(a, "@")
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}

// matchesOne returns true if any approver matches g, where g may be a user
// login or a team slug ("org/team"). Team slugs are resolved via the
// pre-populated teams map; an unknown slug falls back to literal match.
func matchesOne(approvers []string, g string, teams map[string][]string) bool {
	g = strings.TrimPrefix(g, "@")
	set := map[string]struct{}{}
	for _, a := range approvers {
		set[strings.TrimPrefix(a, "@")] = struct{}{}
	}
	if _, ok := set[g]; ok {
		return true
	}
	if isTeamSlug(g) {
		for _, member := range teams[g] {
			if _, ok := set[strings.TrimPrefix(member, "@")]; ok {
				return true
			}
		}
	}
	return false
}

// isTeamSlug reports whether s looks like a team handle ("org/team") rather
// than an individual login.
func isTeamSlug(s string) bool {
	return strings.Contains(s, "/")
}

// isEmailOwner reports whether a CODEOWNERS owner entry is an email address
// (an "@" after the optional leading "@" handle marker). GitHub allows
// email owners, but reeve has no commit-email → login resolution, so such
// entries are unenforceable.
func isEmailOwner(o string) bool {
	return strings.Contains(strings.TrimPrefix(o, "@"), "@")
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
// specific patterns last. Resolve applies matches in order and later wins,
// so the most-specific pattern must sort last to override. Specificity =
// count of non-wildcard chars (rough but good enough for v1).
func sortBySpecificity(rules []StackRule) {
	for i := 1; i < len(rules); i++ {
		for j := i; j > 0 && specificity(rules[j].Pattern) < specificity(rules[j-1].Pattern); j-- {
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
