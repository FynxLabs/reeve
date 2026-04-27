package github

import "testing"

func TestAnyPrefixIn(t *testing.T) {
	tests := []struct {
		file  string
		paths []string
		want  bool
	}{
		// exact match
		{"infra/foo", []string{"infra/foo"}, true},
		// child path
		{"infra/foo/bar.ts", []string{"infra/foo"}, true},
		// unrelated
		{"infra/bar", []string{"infra/foo"}, false},
		// prefix substring that isn't a path boundary
		{"infra/foobar", []string{"infra/foo"}, false},
		// multiple paths, one matches
		{"a/b/c.go", []string{"x/y", "a/b"}, true},
		// empty paths
		{"infra/foo", []string{}, false},
	}
	for _, tt := range tests {
		got := anyPrefixIn(tt.file, tt.paths)
		if got != tt.want {
			t.Errorf("anyPrefixIn(%q, %v) = %v, want %v", tt.file, tt.paths, got, tt.want)
		}
	}
}
