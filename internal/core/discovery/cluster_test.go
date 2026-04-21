package discovery

import (
	"reflect"
	"testing"
)

func TestCluster_EmptyInput(t *testing.T) {
	if got := Cluster(nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestCluster_PatternFromSharedParent(t *testing.T) {
	enum := []Stack{
		{Project: "api", Path: "projects/api", Name: "dev"},
		{Project: "api", Path: "projects/api", Name: "prod"},
		{Project: "worker", Path: "projects/worker", Name: "dev"},
		{Project: "worker", Path: "projects/worker", Name: "prod"},
	}
	got := Cluster(enum)
	want := []Declaration{
		{Pattern: "projects/*", Stacks: []string{"dev", "prod"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCluster_MixedSignaturesSplit(t *testing.T) {
	enum := []Stack{
		{Project: "api", Path: "projects/api", Name: "dev"},
		{Project: "api", Path: "projects/api", Name: "prod"},
		{Project: "worker", Path: "projects/worker", Name: "prod"}, // only prod
	}
	got := Cluster(enum)
	// Two distinct signatures under projects/* → one literal per project.
	if len(got) != 2 {
		t.Fatalf("expected 2 declarations, got %+v", got)
	}
	byProject := map[string]Declaration{}
	for _, d := range got {
		byProject[d.Project] = d
	}
	if byProject["api"].Path != "projects/api" || len(byProject["api"].Stacks) != 2 {
		t.Fatalf("api entry wrong: %+v", byProject["api"])
	}
	if byProject["worker"].Path != "projects/worker" || byProject["worker"].Stacks[0] != "prod" {
		t.Fatalf("worker entry wrong: %+v", byProject["worker"])
	}
}

func TestCluster_SinglePathIsLiteral(t *testing.T) {
	enum := []Stack{
		{Project: "shared", Path: "services/shared", Name: "prod"},
	}
	got := Cluster(enum)
	if len(got) != 1 || got[0].Project != "shared" || got[0].Pattern != "" {
		t.Fatalf("expected literal for single path: %+v", got)
	}
}
