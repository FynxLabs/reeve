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

func TestStackViewAndRetentionParse(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": `version: 1
config_type: shared
bucket:
  type: filesystem
  name: ./.reeve-state
comments:
  stack_view: changed
retention:
  max_age: 48h
`,
		"pulumi.yaml": minimalPulumi(),
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Shared.Comments.StackView != "changed" {
		t.Errorf("stack_view: got %q", cfg.Shared.Comments.StackView)
	}
	if cfg.Shared.Retention.MaxAge != "48h" {
		t.Errorf("retention.max_age: got %q", cfg.Shared.Retention.MaxAge)
	}
}

func TestChangeMappingScopeParse(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": `version: 1
config_type: shared
bucket: {type: filesystem, name: x}
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
      stacks: [prod]
  change_mapping:
    scope: pulumi_only
`,
	})
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Engines[0].Engine.ChangeMapping.Scope != "pulumi_only" {
		t.Errorf("scope: got %q", cfg.Engines[0].Engine.ChangeMapping.Scope)
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

func TestLogSettingsNilShared(t *testing.T) {
	// The panic guard: commands call LogSettings() before Validate(), so it
	// must not dereference a nil Shared (missing .reeve/shared.yaml).
	var c *Config
	if lvl, fmtt := c.LogSettings(); lvl != "" || fmtt != "" {
		t.Fatalf("nil Config should yield empty settings, got %q/%q", lvl, fmtt)
	}
	c2 := &Config{Shared: nil}
	if lvl, fmtt := c2.LogSettings(); lvl != "" || fmtt != "" {
		t.Fatalf("nil Shared should yield empty settings, got %q/%q", lvl, fmtt)
	}
}
