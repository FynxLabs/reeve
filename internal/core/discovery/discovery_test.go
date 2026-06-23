package discovery

import (
	"reflect"
	"sort"
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

// Shared-dir layout: many stacks live in one directory, each with its own
// Pulumi.<name>.yaml. A change to one stack's config must not pull in siblings.
func sharedDirStacks() []Stack {
	return []Stack{
		{Project: "edge", Path: "projects/edge", Name: "alpha", Env: "alpha"},
		{Project: "edge", Path: "projects/edge", Name: "beta", Env: "beta"},
		{Project: "edge", Path: "projects/edge", Name: "gamma", Env: "gamma"},
	}
}

func names(got []Stack) []string {
	out := make([]string, 0, len(got))
	for _, s := range got {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

func TestAffectedSharedDirPerStackConfig(t *testing.T) {
	got := Affected(sharedDirStacks(),
		[]string{"projects/edge/Pulumi.beta.yaml"}, ChangeMapping{})
	if g := names(got); len(g) != 1 || g[0] != "beta" {
		t.Fatalf("expected only beta, got %v", g)
	}
}

func TestAffectedSharedDirSiblingConfigsDistinct(t *testing.T) {
	got := Affected(sharedDirStacks(),
		[]string{
			"projects/edge/Pulumi.alpha.yaml",
			"projects/edge/Pulumi.beta.yaml",
		}, ChangeMapping{})
	if g := names(got); len(g) != 2 || g[0] != "alpha" || g[1] != "beta" {
		t.Fatalf("expected alpha + beta, got %v", g)
	}
}

func TestAffectedSharedDirSharedProgramFile(t *testing.T) {
	// A non-config file in the dir is shared program code -> all stacks.
	got := Affected(sharedDirStacks(),
		[]string{"projects/edge/index.ts"}, ChangeMapping{})
	if g := names(got); len(g) != 3 {
		t.Fatalf("expected all 3 stacks, got %v", g)
	}
}

func TestAffectedSharedDirSharedPulumiYaml(t *testing.T) {
	// The shared project file affects every stack in the dir.
	got := Affected(sharedDirStacks(),
		[]string{"projects/edge/Pulumi.yaml"}, ChangeMapping{})
	if g := names(got); len(g) != 3 {
		t.Fatalf("expected all 3 stacks, got %v", g)
	}
}

func TestAffectedSharedDirNestedFile(t *testing.T) {
	// A file in a subdirectory is shared program code -> all stacks.
	got := Affected(sharedDirStacks(),
		[]string{"projects/edge/components/bucket.ts"}, ChangeMapping{})
	if g := names(got); len(g) != 3 {
		t.Fatalf("expected all 3 stacks, got %v", g)
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
