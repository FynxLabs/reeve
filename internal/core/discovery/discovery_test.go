package discovery

import (
	"reflect"
	"testing"
)

func TestResolveLiteralAndPattern(t *testing.T) {
	enum := []Stack{
		{Project: "api", Path: "projects/api", Name: "dev"},
		{Project: "api", Path: "projects/api", Name: "prod"},
		{Project: "worker", Path: "services/worker", Name: "prod"},
		{Project: "scratch", Path: "projects/sandbox/scratch", Name: "dev"},
	}
	decls := []Declaration{
		{Project: "api", Path: "projects/api", Stacks: []string{"dev", "prod"}},
		{Pattern: "services/*", Stacks: []string{"prod"}},
	}
	filter := Filter{PathPatterns: []string{"projects/sandbox/**"}}

	got := Resolve(enum, decls, filter)
	want := []Stack{
		{Project: "api", Path: "projects/api", Name: "dev"},
		{Project: "api", Path: "projects/api", Name: "prod"},
		{Project: "worker", Path: "services/worker", Name: "prod"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolve mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestResolveStackPatternExclude(t *testing.T) {
	enum := []Stack{
		{Project: "api", Path: "projects/api", Name: "prod"},
		{Project: "api", Path: "projects/api", Name: "scratch"},
	}
	decls := []Declaration{{Project: "api", Path: "projects/api", Stacks: []string{"prod", "scratch"}}}
	filter := Filter{StackPatterns: []string{"*/scratch"}}
	got := Resolve(enum, decls, filter)
	if len(got) != 1 || got[0].Name != "prod" {
		t.Fatalf("expected only prod, got %+v", got)
	}
}

func TestAffectedByPath(t *testing.T) {
	stacks := []Stack{
		{Project: "api", Path: "projects/api", Name: "dev"},
		{Project: "worker", Path: "services/worker", Name: "prod"},
	}
	changed := []string{"projects/api/index.ts", "README.md"}
	cm := ChangeMapping{IgnoreChanges: []string{"**/*.md"}}
	got := Affected(stacks, changed, cm)
	if len(got) != 1 || got[0].Project != "api" {
		t.Fatalf("expected only api, got %+v", got)
	}
}

func TestAffectedByExtraTrigger(t *testing.T) {
	stacks := []Stack{
		{Project: "api", Path: "projects/api", Name: "prod"},
	}
	changed := []string{"shared/types/user.ts"}
	cm := ChangeMapping{ExtraTriggers: map[string][]string{"api": {"shared/types/**"}}}
	got := Affected(stacks, changed, cm)
	if len(got) != 1 {
		t.Fatalf("expected api to be triggered, got %+v", got)
	}
}

func TestAffectedNoChangesAllIgnored(t *testing.T) {
	stacks := []Stack{{Project: "api", Path: "projects/api", Name: "prod"}}
	changed := []string{"README.md", "docs/x.md"}
	cm := ChangeMapping{IgnoreChanges: []string{"**/*.md"}}
	got := Affected(stacks, changed, cm)
	if len(got) != 0 {
		t.Fatalf("expected no stacks, got %+v", got)
	}
}
