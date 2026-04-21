package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReeve(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".reeve")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLoadMinimal(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": `version: 1
config_type: shared
bucket:
  type: filesystem
  name: ./.reeve-state
comments:
  sort: status_grouped
  show_gates: true
`,
		"pulumi.yaml": `version: 1
config_type: engine
engine:
  type: pulumi
  binary:
    path: pulumi
  stacks:
    - project: api
      path: projects/api
      stacks: [dev, prod]
    - pattern: "services/*"
      stacks: [prod]
  filters:
    exclude:
      - "projects/sandbox/*"
      - stack: "*/scratch"
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Shared.Bucket.Type != "filesystem" {
		t.Fatalf("bucket type: %s", cfg.Shared.Bucket.Type)
	}
	if len(cfg.Engines) != 1 || cfg.Engines[0].Engine.Type != "pulumi" {
		t.Fatalf("engine not loaded: %+v", cfg.Engines)
	}
	if len(cfg.Engines[0].Engine.Filters.Exclude) != 2 {
		t.Fatalf("expected 2 exclude rules, got %d", len(cfg.Engines[0].Engine.Filters.Exclude))
	}
	if cfg.Engines[0].Engine.Filters.Exclude[0].Pattern != "projects/sandbox/*" {
		t.Fatalf("first exclude should be plain pattern: %+v", cfg.Engines[0].Engine.Filters.Exclude[0])
	}
	if cfg.Engines[0].Engine.Filters.Exclude[1].Stack != "*/scratch" {
		t.Fatalf("second exclude should be stack form: %+v", cfg.Engines[0].Engine.Filters.Exclude[1])
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": `version: 1
config_type: shared
bucket: {type: filesystem, name: x}
bogus_field: 42
`,
		"pulumi.yaml": minimalPulumi(),
	})
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "bogus_field") {
		t.Fatalf("expected strict unmarshal to reject unknown field, got %v", err)
	}
}

func TestDuplicateEngineTypeRejected(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml":  minimalShared(),
		"pulumi.yaml":  minimalPulumi(),
		"pulumi2.yaml": minimalPulumi(),
	})
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "duplicate engine.type") {
		t.Fatalf("expected duplicate engine.type error, got %v", err)
	}
}

func TestMissingEngineRejected(t *testing.T) {
	root := writeReeve(t, map[string]string{"shared.yaml": minimalShared()})
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "engine") {
		t.Fatalf("expected validate to require engine, got %v", err)
	}
}

func TestUnsupportedVersionRejected(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": `version: 2
config_type: shared
bucket: {type: filesystem, name: x}
`,
	})
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("expected version rejection, got %v", err)
	}
}

func minimalShared() string {
	return `version: 1
config_type: shared
bucket: {type: filesystem, name: ./x}
`
}

func minimalPulumi() string {
	return `version: 1
config_type: engine
engine:
  type: pulumi
  binary: {path: pulumi}
`
}
