package auth

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Binding matches a stack ref and run mode to a set of provider names.
type Binding struct {
	StackPattern string // glob over "project/stack"
	Mode         Mode   // "" = all modes; otherwise preview|apply|drift
	Providers    []string
	Override     []string // replaces more-general providers of the same scope
}

// ProviderDecl declares a named provider. The Type drives which adapter
// is constructed. Fields is the raw YAML fields the adapter consumes.
type ProviderDecl struct {
	Name   string
	Type   string
	Fields map[string]any
}

// Resolve returns the ordered, deduplicated list of provider names to
// activate for (stackRef, mode). Override lists replace earlier entries
// of the same name; Providers lists union.
//
// Pure logic: actual credential acquisition lives in internal/auth/providers.
func Resolve(bindings []Binding, stackRef string, mode Mode) []string {
	// Sort bindings: general → specific. "More specific" = longer pattern
	// with fewer wildcards, plus mode-matched bindings override mode-agnostic.
	sorted := append([]Binding{}, bindings...)
	sortBindings(sorted)

	seen := map[string]bool{}
	var out []string
	for _, b := range sorted {
		if !matches(b, stackRef, mode) {
			continue
		}
		if len(b.Override) > 0 {
			// Override replaces everything from the same logical scope.
			// Phase 4 approximation: clear the entire set of providers
			// from the "scope" (same provider-name prefix).
			for _, repl := range b.Override {
				out, seen = replaceScope(out, seen, repl)
			}
		}
		for _, p := range b.Providers {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Validate checks bindings for conflicts. Two bindings matching the same
// stack with providers of identical logical scope and different names is
// an error (to avoid "which AWS role did I use?" ambiguity).
// Phase 4 approximation: same Type must not appear twice.
func Validate(bindings []Binding, declsByName map[string]ProviderDecl, stacks []string) error {
	for _, stack := range stacks {
		for _, mode := range []Mode{ModePreview, ModeApply, ModeDrift} {
			names := Resolve(bindings, stack, mode)
			typeSeen := map[string]string{}
			for _, n := range names {
				d, ok := declsByName[n]
				if !ok {
					return fmt.Errorf("binding references undeclared provider %q", n)
				}
				scope := scopeOfType(d.Type)
				if prev, exists := typeSeen[scope]; exists && prev != n {
					return fmt.Errorf("stack %s (%s): conflicting providers of scope %q: %s vs %s",
						stack, mode, scope, prev, n)
				}
				typeSeen[scope] = n
			}
		}
	}
	return nil
}

// scopeOfType groups providers by the credential "domain" they live in.
// Two providers of the same scope bound to the same stack is an error.
func scopeOfType(t string) string {
	switch t {
	case "aws_oidc", "aws_profile", "aws_sso":
		return "aws"
	case "gcp_wif", "gcloud_adc":
		return "gcp"
	case "azure_federated":
		return "azure"
	case "github_app":
		return "github-identity"
	}
	// Secret managers, vault, env_passthrough: allow multiple.
	return "other:" + t
}

func matches(b Binding, ref string, mode Mode) bool {
	if b.Mode != "" && b.Mode != mode {
		return false
	}
	if b.StackPattern == "" {
		return true
	}
	ok, _ := doublestar.Match(b.StackPattern, ref)
	return ok
}

// sortBindings: general → specific, and mode-agnostic → mode-scoped.
// Iteration order matters: Override can only replace earlier-visited
// providers, so general must come first.
func sortBindings(bs []Binding) {
	for i := 1; i < len(bs); i++ {
		for j := i; j > 0 && moreSpecific(bs[j-1], bs[j]); j-- {
			bs[j], bs[j-1] = bs[j-1], bs[j]
		}
	}
}

// moreSpecific reports whether a is more specific than b - if so, swap
// pushes a rightward, landing after b.
func moreSpecific(a, b Binding) bool {
	if (a.Mode == "") != (b.Mode == "") {
		return a.Mode != ""
	}
	return specificity(a.StackPattern) > specificity(b.StackPattern)
}

func specificity(p string) int {
	score := 0
	for _, r := range p {
		switch r {
		case '*', '?':
			score -= 2
		default:
			score++
		}
	}
	return score
}

func replaceScope(list []string, seen map[string]bool, repl string) ([]string, map[string]bool) {
	// Very coarse: remove any existing provider whose name shares the
	// first '-' segment with the replacement. e.g. aws-prod replaces
	// aws-prod-readonly but not cloudflare-token. This is good enough
	// for v1; auth.yaml schema will formalize scopes later.
	out := list[:0]
	for _, x := range list {
		if sameScope(x, repl) {
			delete(seen, x)
			continue
		}
		out = append(out, x)
	}
	if !seen[repl] {
		seen[repl] = true
		out = append(out, repl)
	}
	return out, seen
}

func sameScope(a, b string) bool {
	aRoot := a
	if idx := strings.Index(a, "-"); idx > 0 {
		aRoot = a[:idx]
	}
	bRoot := b
	if idx := strings.Index(b, "-"); idx > 0 {
		bRoot = b[:idx]
	}
	return aRoot == bRoot
}
