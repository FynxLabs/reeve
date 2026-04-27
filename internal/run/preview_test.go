package run

import (
	"context"
	"strings"
	"testing"

	"github.com/thefynx/reeve/internal/blob/filesystem"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/iac"
)

type fakeEngine struct {
	enum    []discovery.Stack
	results map[string]iac.PreviewResult
}

func (f *fakeEngine) Name() string                   { return "fake" }
func (f *fakeEngine) Capabilities() iac.Capabilities { return iac.Capabilities{} }
func (f *fakeEngine) EnumerateStacks(ctx context.Context, root string) ([]discovery.Stack, error) {
	return f.enum, nil
}
func (f *fakeEngine) Preview(ctx context.Context, s discovery.Stack, opts iac.PreviewOpts) (iac.PreviewResult, error) {
	if r, ok := f.results[s.Ref()]; ok {
		return r, nil
	}
	return iac.PreviewResult{}, nil
}

type fakeVCS struct {
	changed []string
	posted  string
}

func (f *fakeVCS) ListChangedFiles(ctx context.Context, _ int) ([]string, error) {
	return f.changed, nil
}
func (f *fakeVCS) UpsertComment(ctx context.Context, _ int, body, _ string) error {
	f.posted = body
	return nil
}

func TestPreviewEndToEnd(t *testing.T) {
	ctx := context.Background()
	engine := &fakeEngine{
		enum: []discovery.Stack{
			{Project: "api", Path: "projects/api", Name: "dev", Env: "dev"},
			{Project: "api", Path: "projects/api", Name: "prod", Env: "prod"},
			{Project: "worker", Path: "services/worker", Name: "prod", Env: "prod"},
		},
		results: map[string]iac.PreviewResult{
			"api/prod":    {Counts: summary.Counts{Add: 2, Change: 1}, PlanSummary: "+ s3 bucket"},
			"worker/prod": {Counts: summary.Counts{Replace: 1}, PlanSummary: "± rds"},
		},
	}
	vcs := &fakeVCS{changed: []string{"projects/api/index.ts", "services/worker/go.mod"}}
	store, _ := filesystem.New(t.TempDir())

	out, err := Preview(ctx, PreviewInput{
		PRNumber:  42,
		CommitSHA: "abc12345xyz",
		RunNumber: 1,
		RepoRoot:  "/nope",
		Engine:    engine,
		Config: &schemas.Engine{Engine: schemas.EngineBody{
			Type: "pulumi",
			Stacks: []schemas.StackDecl{
				{Project: "api", Path: "projects/api", Stacks: []string{"dev", "prod"}},
				{Pattern: "services/*", Stacks: []string{"prod"}},
			},
		}},
		Shared:   &schemas.Shared{},
		Blob:     store,
		VCS:      vcs,
		Comments: vcs,
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	// Both api stacks share projects/api; worker shares services/worker.
	// 3 affected: api/dev (no-op), api/prod (ready), worker/prod (ready).
	if len(out.Stacks) != 3 {
		t.Fatalf("expected 3 affected stacks, got %d: %+v", len(out.Stacks), out.Stacks)
	}
	if !strings.Contains(vcs.posted, "reeve") {
		t.Fatalf("expected comment posted via fakeVCS, got %q", vcs.posted)
	}
	if !strings.Contains(out.CommentBody, "api/prod") {
		t.Fatalf("comment missing api/prod: %s", out.CommentBody)
	}
}

func TestPlanSucceeded(t *testing.T) {
	tests := []struct {
		name   string
		stacks []summary.StackSummary
		want   bool
	}{
		{"empty", nil, false},
		{"all planned", []summary.StackSummary{
			{Status: summary.StatusPlanned},
			{Status: summary.StatusNoOp},
		}, true},
		{"one error", []summary.StackSummary{
			{Status: summary.StatusPlanned},
			{Status: summary.StatusError},
		}, false},
	}
	for _, tt := range tests {
		if got := planSucceeded(tt.stacks); got != tt.want {
			t.Errorf("%s: planSucceeded = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPreviewLocalIgnoresChangedFiles(t *testing.T) {
	ctx := context.Background()
	engine := &fakeEngine{
		enum: []discovery.Stack{{Project: "api", Path: "projects/api", Name: "dev", Env: "dev"}},
	}
	store, _ := filesystem.New(t.TempDir())
	out, err := Preview(ctx, PreviewInput{
		Local:  true,
		Engine: engine,
		Config: &schemas.Engine{Engine: schemas.EngineBody{
			Stacks: []schemas.StackDecl{{Project: "api", Path: "projects/api", Stacks: []string{"dev"}}},
		}},
		Shared: &schemas.Shared{},
		Blob:   store,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Stacks) != 1 {
		t.Fatalf("local mode should run all declared stacks, got %d", len(out.Stacks))
	}
}
