// Package codeowners holds a format-agnostic CODEOWNERS parser + resolver
// for GitHub-flavored syntax. Adapters in internal/vcs/<platform> fetch
// the file; this package parses it and resolves owners for a set of
// changed paths.
package codeowners

import (
	"bufio"
	"io"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Rule is one line from CODEOWNERS. A rule with no owners is meaningful:
// when it is the last match for a path, the path is un-owned (GitHub's
// carve-out idiom).
type Rule struct {
	Pattern string
	Owners  []string // handles as written, with leading @
}

// Parse returns rules in file order. Comments and blank lines are skipped.
// Pattern-only lines (no owners) are kept: GitHub uses them to un-own paths
// matched by earlier rules.
func Parse(r io.Reader) []Rule {
	var rules []Rule
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		rules = append(rules, Rule{
			Pattern: fields[0],
			Owners:  fields[1:],
		})
	}
	return rules
}

// Resolve returns path → owners for every changed file that is owned.
// GitHub semantics: the LAST matching rule wins exclusively - earlier
// matches contribute nothing. A last match with no owners un-owns the
// path, so it is omitted from the result.
func Resolve(rules []Rule, changedPaths []string) map[string][]string {
	out := map[string][]string{}
	for _, p := range changedPaths {
		for i := len(rules) - 1; i >= 0; i-- {
			if !match(rules[i].Pattern, p) {
				continue
			}
			if len(rules[i].Owners) > 0 {
				out[p] = rules[i].Owners
			}
			break
		}
	}
	return out
}

// match implements the gitignore-style glob CODEOWNERS uses:
//   - a pattern containing a non-trailing "/" is anchored to the repo root;
//     otherwise it matches at any depth ("docs/" matches "x/docs/y").
//   - a trailing "/" matches every descendant of a matching directory.
//   - a pattern without trailing "/" whose last segment has no wildcard also
//     owns descendants ("/docs" covers "docs/a/b"), while "docs/*" covers
//     direct children only - both per GitHub's documented examples.
//   - "!", and "[" / "]" ranges are unsupported by GitHub CODEOWNERS; such
//     patterns never match anything (GitHub's behavior).
func match(pattern, p string) bool {
	p = strings.TrimPrefix(p, "/")
	if strings.HasPrefix(pattern, "!") || strings.ContainsAny(pattern, "[]") {
		return false
	}

	dirOnly := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	anchored := strings.Contains(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	if pattern == "" {
		return false
	}
	if !anchored {
		pattern = "**/" + pattern
	}

	if dirOnly {
		return globMatch(pattern+"/**", p)
	}
	if globMatch(pattern, p) {
		return true
	}
	// A non-wildcard final segment may name a directory; a directory owns
	// its contents. Wildcard tails ("docs/*") stay direct-children-only.
	if last := pattern[strings.LastIndex(pattern, "/")+1:]; !strings.ContainsAny(last, "*?") {
		return globMatch(pattern+"/**", p)
	}
	return false
}

func globMatch(pattern, s string) bool {
	ok, err := doublestar.Match(pattern, s)
	return err == nil && ok
}
