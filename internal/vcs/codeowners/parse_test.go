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

func TestParseKeepsOwnerlessRules(t *testing.T) {
	src := `* @org/platform
/apps/github
`
	rs := Parse(strings.NewReader(src))
	if len(rs) != 2 {
		t.Fatalf("expected 2 rules (ownerless kept), got %d: %+v", len(rs), rs)
	}
	if len(rs[1].Owners) != 0 {
		t.Fatalf("expected empty owners: %+v", rs[1])
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
	// Last matching rule wins exclusively - earlier matches contribute nothing.
	assertOwners(t, got, "README.md", "@docs-team")
	assertOwners(t, got, "internal/core/rendering/preview.go", "@frontend-lead")
	assertOwners(t, got, "internal/core/locks/locks.go", "@default-team")
}

func TestResolveOwnerlessLastMatchUnowns(t *testing.T) {
	src := `/apps/ @octocat
/apps/github
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{
		"apps/web/main.go",
		"apps/github/hook.go",
	})
	assertOwners(t, got, "apps/web/main.go", "@octocat")
	if _, ok := got["apps/github/hook.go"]; ok {
		t.Fatalf("apps/github must be un-owned by the ownerless carve-out: %v", got)
	}
}

func TestDirectoryMatch(t *testing.T) {
	src := `/internal/vcs/   @vcs-team
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{"internal/vcs/github/client.go"})
	assertOwners(t, got, "internal/vcs/github/client.go", "@vcs-team")
}

func TestUnanchoredDirectoryMatchesAnyDepth(t *testing.T) {
	src := `docs/ @octocat
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{
		"docs/a.md",
		"projects/web/docs/nested.md",
		"docs-other/a.md",
	})
	assertOwners(t, got, "docs/a.md", "@octocat")
	assertOwners(t, got, "projects/web/docs/nested.md", "@octocat")
	if _, ok := got["docs-other/a.md"]; ok {
		t.Fatalf("docs-other must not match docs/: %v", got)
	}
}

func TestSlashPatternIsAnchored(t *testing.T) {
	src := `docs/* docs@example.com
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{
		"docs/getting-started.md",
		"docs/build-app/troubleshooting.md", // GitHub: direct children only
		"src/docs/other.md",                 // interior slash anchors to root
	})
	assertOwners(t, got, "docs/getting-started.md", "docs@example.com")
	if _, ok := got["docs/build-app/troubleshooting.md"]; ok {
		t.Fatalf("docs/* must not match nested files: %v", got)
	}
	if _, ok := got["src/docs/other.md"]; ok {
		t.Fatalf("docs/* is root-anchored, must not match src/docs: %v", got)
	}
}

func TestAnchoredBareDirectoryOwnsContents(t *testing.T) {
	src := `/build @ci-team
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{"build/logs/out.txt"})
	assertOwners(t, got, "build/logs/out.txt", "@ci-team")
}

func TestUnanchoredExtensionMatchesAnyDepth(t *testing.T) {
	src := `*.go @go-team
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{"main.go", "internal/deep/nested/x.go", "README.md"})
	assertOwners(t, got, "main.go", "@go-team")
	assertOwners(t, got, "internal/deep/nested/x.go", "@go-team")
	if _, ok := got["README.md"]; ok {
		t.Fatalf("README.md must not match *.go: %v", got)
	}
}

func TestDoubleStar(t *testing.T) {
	src := `/internal/**/render/*   @render-team
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{"internal/core/render/preview.go"})
	assertOwners(t, got, "internal/core/render/preview.go", "@render-team")
}

func TestUnsupportedPatternsNeverMatch(t *testing.T) {
	src := `* @default-team
!negated.md @nobody
file[0-9].txt @nobody
`
	rules := Parse(strings.NewReader(src))
	got := Resolve(rules, []string{"negated.md", "file1.txt"})
	// GitHub: "!" and "[]" are unsupported and never match, so the
	// catch-all stays the last match.
	assertOwners(t, got, "negated.md", "@default-team")
	assertOwners(t, got, "file1.txt", "@default-team")
}

func assertOwners(t *testing.T, got map[string][]string, path string, owners ...string) {
	t.Helper()
	have := got[path]
	if len(have) != len(owners) {
		t.Fatalf("%s: expected owners %v, got %v", path, owners, have)
	}
	for i := range owners {
		if have[i] != owners[i] {
			t.Fatalf("%s: expected owners %v, got %v", path, owners, have)
		}
	}
}
