package discovery

import (
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Stack is the normalized shape produced by the engine adapter. Core owns
// filtering and change-mapping over a flat []Stack.
type Stack struct {
	Project string // e.g. "api"
	Path    string // repo-relative, e.g. "projects/api"
	Name    string // stack name, e.g. "prod"
	Env     string // derived from Name or explicitly declared; used in rendering
}

// Ref returns "project/name" - the canonical identifier used in rules,
// comments, and bucket keys.
func (s Stack) Ref() string { return s.Project + "/" + s.Name }

// Declaration is one entry from engine config: either a literal
// (Project + Path) or a Pattern (glob over paths).
type Declaration struct {
	Project string
	Path    string
	Pattern string
	Stacks  []string // which stack names apply
}

// Filter is the exclude set from engine config.
type Filter struct {
	// PathPatterns exclude by stack path glob (doublestar).
	PathPatterns []string
	// StackPatterns exclude by "project/stack" glob.
	StackPatterns []string
}

// ChangeMapping drives which stacks are "affected" by a changed-files set.
type ChangeMapping struct {
	IgnoreChanges []string            // globs of paths to ignore entirely
	ExtraTriggers map[string][]string // project -> extra path patterns that trigger it
}

// Resolve expands declarations into concrete stacks and applies filters.
// enumerated is the flat []Stack the engine adapter produced. decls tells
// us which (project, stack-name) pairs we care about.
func Resolve(enumerated []Stack, decls []Declaration, filter Filter) []Stack {
	// Build declaration index: path -> stackNames, project -> stackNames.
	declaredByPath := map[string][]string{}
	declaredByPattern := []Declaration{}
	for _, d := range decls {
		if d.Pattern != "" {
			declaredByPattern = append(declaredByPattern, d)
			continue
		}
		if d.Path != "" {
			declaredByPath[d.Path] = append(declaredByPath[d.Path], d.Stacks...)
		}
	}

	keep := make([]Stack, 0, len(enumerated))
	for _, s := range enumerated {
		if !declared(s, declaredByPath, declaredByPattern) {
			continue
		}
		if excluded(s, filter) {
			continue
		}
		keep = append(keep, s)
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].Ref() < keep[j].Ref() })
	return keep
}

func declared(s Stack, byPath map[string][]string, patterns []Declaration) bool {
	if names, ok := byPath[s.Path]; ok && containsStack(names, s.Name) {
		return true
	}
	for _, d := range patterns {
		ok, _ := doublestar.Match(d.Pattern, s.Path)
		if ok && containsStack(d.Stacks, s.Name) {
			return true
		}
	}
	return false
}

func excluded(s Stack, f Filter) bool {
	for _, p := range f.PathPatterns {
		if ok, _ := doublestar.Match(p, s.Path); ok {
			return true
		}
	}
	for _, p := range f.StackPatterns {
		if ok, _ := doublestar.Match(p, s.Ref()); ok {
			return true
		}
	}
	return false
}

func containsStack(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

// Affected returns the subset of stacks whose paths (or ExtraTriggers)
// intersect the given changed files, honoring IgnoreChanges.
func Affected(stacks []Stack, changedFiles []string, cm ChangeMapping) []Stack {
	// Drop ignored files first.
	filtered := make([]string, 0, len(changedFiles))
	for _, f := range changedFiles {
		ignore := false
		for _, g := range cm.IgnoreChanges {
			if ok, _ := doublestar.PathMatch(g, f); ok {
				ignore = true
				break
			}
		}
		if !ignore {
			filtered = append(filtered, f)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	out := make([]Stack, 0, len(stacks))
	for _, s := range stacks {
		if intersectsPath(s.Path, filtered) {
			out = append(out, s)
			continue
		}
		if triggers, ok := cm.ExtraTriggers[s.Project]; ok && matchesAny(triggers, filtered) {
			out = append(out, s)
		}
	}
	return out
}

func intersectsPath(stackPath string, changed []string) bool {
	prefix := stackPath + "/"
	for _, f := range changed {
		if f == stackPath || strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}

func matchesAny(patterns, files []string) bool {
	for _, pat := range patterns {
		for _, f := range files {
			if ok, _ := doublestar.PathMatch(pat, f); ok {
				return true
			}
		}
	}
	return false
}
