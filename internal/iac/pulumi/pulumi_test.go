package pulumi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnumerateStacks(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(p, body string) {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("projects/api/Pulumi.yaml", "name: api\nruntime: nodejs\n")
	mustWrite("projects/api/Pulumi.dev.yaml", "")
	mustWrite("projects/api/Pulumi.prod.yaml", "")
	mustWrite("projects/api/node_modules/junk/Pulumi.yaml", "name: junk\n")
	mustWrite("services/worker/Pulumi.yaml", "name: worker\nruntime: go\n")
	mustWrite("services/worker/Pulumi.prod.yaml", "")

	e := New("")
	got, err := e.EnumerateStacks(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 stacks, got %d: %+v", len(got), got)
	}

	expected := map[string]bool{
		"api/dev":     false,
		"api/prod":    false,
		"worker/prod": false,
	}
	for _, s := range got {
		if _, ok := expected[s.Ref()]; !ok {
			t.Fatalf("unexpected stack %s", s.Ref())
		}
		expected[s.Ref()] = true
	}
	for k, v := range expected {
		if !v {
			t.Fatalf("missing expected stack %s", k)
		}
	}
}

func TestParsePreviewFromFixture(t *testing.T) {
	json := []byte(`{
		"changeSummary": {"create": 2, "update": 1, "replace": 1},
		"steps": [
			{"op":"create","type":"aws:s3/bucket:Bucket","urn":"urn:pulumi:prod::api::aws:s3/bucket:Bucket::logs-2026"},
			{"op":"create","type":"aws:iam/role:Role","urn":"urn:pulumi:prod::api::aws:iam/role:Role::app-role"},
			{"op":"update","type":"aws:ec2/instance:Instance","urn":"urn:pulumi:prod::api::aws:ec2/instance:Instance::web"},
			{"op":"replace","type":"aws:rds/instance:Instance","urn":"urn:pulumi:prod::api::aws:rds/instance:Instance::db"}
		]
	}`)
	counts, short, diagErr, err := parsePreview(json)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Add != 2 || counts.Change != 1 || counts.Replace != 1 || counts.Delete != 0 {
		t.Fatalf("counts off: %+v", counts)
	}
	if diagErr != "" {
		t.Fatalf("unexpected diagnostic error: %s", diagErr)
	}
	if short == "" {
		t.Fatal("expected non-empty summary")
	}
	if want := "+ aws:s3/bucket:Bucket  logs-2026"; !contains(short, want) {
		t.Fatalf("summary missing %q: %s", want, short)
	}
}

func TestParsePreviewError(t *testing.T) {
	json := []byte(`{
		"changeSummary": {},
		"steps": [],
		"diagnostics": [{"severity":"error","message":"snake oil"}]
	}`)
	_, _, diagErr, err := parsePreview(json)
	if err != nil {
		t.Fatal(err)
	}
	if diagErr != "snake oil" {
		t.Fatalf("expected snake oil error, got %q", diagErr)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
