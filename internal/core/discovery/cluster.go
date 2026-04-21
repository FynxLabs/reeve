package discovery

import (
	"path"
	"sort"
	"strings"
)

// Cluster groups enumerated stacks into suggested StackDecl entries. It
// prefers pattern entries where multiple projects share a parent prefix
// and the same stack-name set, and falls back to literals otherwise.
func Cluster(stacks []Stack) []Declaration {
	if len(stacks) == 0 {
		return nil
	}

	// stacks-per-project-path
	byPath := map[string][]string{} // path → sorted stack names
	projectByPath := map[string]string{}
	for _, s := range stacks {
		byPath[s.Path] = appendIfMissing(byPath[s.Path], s.Name)
		projectByPath[s.Path] = s.Project
	}
	for k := range byPath {
		sort.Strings(byPath[k])
	}

	// Group paths by (parent, stack-signature). Paths with a common parent
	// and identical stack-name lists collapse to a pattern.
	type bucketKey struct {
		parent    string
		signature string
	}
	buckets := map[bucketKey][]string{}
	for p, names := range byPath {
		parent := path.Dir(p)
		if parent == "." {
			parent = ""
		}
		sig := strings.Join(names, ",")
		k := bucketKey{parent, sig}
		buckets[k] = append(buckets[k], p)
	}

	// Deterministic iteration for stable output.
	keys := make([]bucketKey, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].parent != keys[j].parent {
			return keys[i].parent < keys[j].parent
		}
		return keys[i].signature < keys[j].signature
	})

	var out []Declaration
	for _, k := range keys {
		paths := buckets[k]
		sort.Strings(paths)
		stackNames := strings.Split(k.signature, ",")
		if len(paths) >= 2 {
			parent := k.parent
			pattern := parent + "/*"
			if parent == "" {
				pattern = "*"
			}
			out = append(out, Declaration{
				Pattern: pattern,
				Stacks:  stackNames,
			})
			continue
		}
		// Single path → literal declaration.
		p := paths[0]
		out = append(out, Declaration{
			Project: projectByPath[p],
			Path:    p,
			Stacks:  stackNames,
		})
	}
	return out
}

func appendIfMissing(list []string, v string) []string {
	for _, e := range list {
		if e == v {
			return list
		}
	}
	return append(list, v)
}
