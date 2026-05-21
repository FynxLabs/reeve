// Package codeowners holds a format-agnostic CODEOWNERS parser + resolver
// for GitHub-flavored syntax. Adapters in internal/vcs/<platform> fetch
// the file; this package parses it and resolves owners for a set of
// changed paths.
package codeowners

import (
	"bufio"
	"io"
	"path"
	"strings"
)

// Rule is one line from CODEOWNERS.
type Rule struct {
	Pattern string
	Owners  []string // handles as written, with leading @
}

// Parse returns rules in file order. Comments and blank lines are skipped.
// GitHub's CODEOWNERS uses the last-matching rule, so callers scan in
// reverse or use Resolve, which handles that correctly.
func Parse(r io.Reader) []Rule {
	var rules []Rule
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rules = append(rules, Rule{
			Pattern: fields[0],
			Owners:  fields[1:],
		})
	}
	return rules
}

// Resolve returns path → owners for every changed file with at least one
// matching rule. All matching rules contribute owners (union); files with
// no owner are omitted.
func Resolve(rules []Rule, changedPaths []string) map[string][]string {
	out := map[string][]string{}
	for _, p := range changedPaths {
		owners := allOwners(rules, p)
		if len(owners) > 0 {
			out[p] = owners
		}
	}
	return out
}

// allOwners collects the union of owners from every rule that matches p.
func allOwners(rules []Rule, p string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, r := range rules {
		if match(r.Pattern, p) {
			for _, o := range r.Owners {
				if _, ok := seen[o]; !ok {
					seen[o] = struct{}{}
					out = append(out, o)
				}
			}
		}
	}
	return out
}

// match implements a small subset of gitignore-style glob used by
// CODEOWNERS: leading "/" anchors to repo root, trailing "/" means any
// descendant, "*" is a single path segment, "**" is any number.
func match(pattern, p string) bool {
	// Normalize.
	p = strings.TrimPrefix(p, "/")
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")

	if strings.HasSuffix(pattern, "/") {
		// Directory match → pattern is prefix.
		prefix := pattern
		if !anchored {
			// Unanchored dir patterns can match any ancestor segment.
			for cur := p; cur != "" && cur != "."; cur = path.Dir(cur) {
				if strings.HasPrefix(cur+"/", prefix) {
					return true
				}
			}
			return false
		}
		return strings.HasPrefix(p, prefix)
	}

	// File / glob match.
	if !anchored {
		// Match against the basename segments from right to left.
		// Simpler: match against any suffix of the path.
		for i := 0; i < len(p); i++ {
			if p[i] == '/' {
				if ok, _ := globMatch(pattern, p[i+1:]); ok {
					return true
				}
			}
		}
		ok, _ := globMatch(pattern, p)
		return ok
	}
	ok, _ := globMatch(pattern, p)
	return ok
}

// globMatch applies GitHub CODEOWNERS glob. Uses path.Match for the
// single-segment case and a small "**" expansion otherwise.
func globMatch(pattern, s string) (bool, error) {
	if !strings.Contains(pattern, "**") {
		return path.Match(pattern, s)
	}
	// Expand ** iteratively: try every split point in s.
	parts := strings.Split(pattern, "**")
	if len(parts) == 2 {
		prefix, suffix := parts[0], parts[1]
		prefix = strings.TrimSuffix(prefix, "/")
		suffix = strings.TrimPrefix(suffix, "/")
		if prefix != "" && !strings.HasPrefix(s, prefix) {
			return false, nil
		}
		tail := s
		if prefix != "" {
			tail = strings.TrimPrefix(s, prefix+"/")
		}
		if suffix == "" {
			return true, nil
		}
		// Try every descendant boundary.
		for i := -1; i < len(tail); i++ {
			sub := tail
			if i >= 0 {
				if tail[i] != '/' {
					continue
				}
				sub = tail[i+1:]
			}
			if ok, _ := path.Match(suffix, sub); ok {
				return true, nil
			}
		}
		return false, nil
	}
	// Fallback: strip ** and do a prefix check.
	return path.Match(strings.ReplaceAll(pattern, "**", "*"), s)
}
