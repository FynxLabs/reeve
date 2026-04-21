package codeowners

import (
	"strings"
	"testing"
)

func TestParseSkipsCommentsAndBlanks(t *testing.T) {
	src := `# top-level owners
*           @org/platform

# specific
/internal/core/   @org/core-team
/internal/**/render   @frontend-lead
`
	rs := Parse(strings.NewReader(src))
	if len(rs) != 3 {
		t.Fatalf("expected 3 rules, got %d: %+v", len(rs), rs)
	}
}

func TestResolveLastMatchWins(t *testing.T) {
	src := `*       @default-team
*.md    @docs-team
/internal/core/rendering/*   @frontend-lead
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{
		"README.md",
		"internal/core/rendering/preview.go",
		"internal/core/locks/locks.go",
	})
	if !contains(got["README.md"], "@docs-team") {
		t.Fatalf("README.md: %v", got["README.md"])
	}
	if !contains(got["internal/core/rendering/preview.go"], "@frontend-lead") {
		t.Fatalf("rendering: %v", got["internal/core/rendering/preview.go"])
	}
	if !contains(got["internal/core/locks/locks.go"], "@default-team") {
		t.Fatalf("locks: %v", got["internal/core/locks/locks.go"])
	}
}

func TestDirectoryMatch(t *testing.T) {
	src := `/internal/vcs/   @vcs-team
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{"internal/vcs/github/client.go"})
	if !contains(got["internal/vcs/github/client.go"], "@vcs-team") {
		t.Fatalf("expected @vcs-team: %+v", got)
	}
}

func TestDoubleStar(t *testing.T) {
	src := `/internal/**/render/*   @render-team
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{"internal/core/render/preview.go"})
	if !contains(got["internal/core/render/preview.go"], "@render-team") {
		t.Fatalf("expected @render-team match: %+v", got)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
