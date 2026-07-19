package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateNotificationsV1ToV2(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": sharedYAMLMin,
		"pulumi.yaml": engineYAMLMin,
		"notifications.yaml": `version: 1
config_type: notifications
slack:
  enabled: true
  channel: "#infra-deploys"
  auth_token: xoxb-test
  trigger: plan
  events: [plan, applied, failed]
  icons:
    engine: ":pulumi:"
  rules:
    - environments: [prod]
`,
	})
	dir := filepath.Join(root, ".reeve")

	if err := NewMigrator().MigrateDirectory(dir, false); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	migrated, err := os.ReadFile(filepath.Join(dir, "notifications.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(migrated)
	if !strings.Contains(text, "version: 2") {
		t.Fatalf("version not bumped:\n%s", text)
	}
	if strings.Contains(text, "slack:") {
		t.Fatalf("legacy slack key survived:\n%s", text)
	}
	if !strings.Contains(text, "sinks:") || !strings.Contains(text, "type: slack") {
		t.Fatalf("sinks list missing:\n%s", text)
	}
	if strings.Contains(text, "events:") || !strings.Contains(text, "on:") {
		t.Fatalf("events not renamed to on:\n%s", text)
	}
	// Backup of the original is kept.
	if _, err := os.Stat(filepath.Join(dir, "notifications.yaml.bak")); err != nil {
		t.Fatalf("backup missing: %v", err)
	}

	// The migrated tree loads, validates, and produces the same effective
	// sink as the legacy shape did.
	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load after migrate: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate after migrate: %v", err)
	}
	sinks := cfg.Notifications.EffectiveSinks()
	if len(sinks) != 1 {
		t.Fatalf("sinks: %+v", sinks)
	}
	s := sinks[0]
	if s.Type != "slack" || !s.IsEnabled() || s.Channel != "#infra-deploys" ||
		s.AuthToken != "xoxb-test" || string(s.Trigger) != "plan" {
		t.Fatalf("migrated sink: %+v", s)
	}
	if len(s.On) != 3 || s.On[0] != "plan" || s.On[2] != "failed" {
		t.Fatalf("on: %v", s.On)
	}
	if s.Icons == nil || s.Icons.Engine != ":pulumi:" || len(s.Rules) != 1 {
		t.Fatalf("icons/rules lost: %+v", s)
	}
}

func TestMigrateNotificationsWithoutSlackBumpsVersionOnly(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"notifications.yaml": `version: 1
config_type: notifications
sinks:
  - type: webhook
    url: https://example.test/hook
    on: [applied]
`,
	})
	dir := filepath.Join(root, ".reeve")
	if err := NewMigrator().MigrateDirectory(dir, false); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	text, _ := os.ReadFile(filepath.Join(dir, "notifications.yaml"))
	if !strings.Contains(string(text), "version: 2") {
		t.Fatalf("version not bumped:\n%s", text)
	}
	if !strings.Contains(string(text), "type: webhook") {
		t.Fatalf("existing sinks lost:\n%s", text)
	}
}

func TestMigrateMergesSlackIntoExistingSinks(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"notifications.yaml": `version: 1
config_type: notifications
slack:
  enabled: true
  channel: "#x"
sinks:
  - type: webhook
    url: https://example.test/hook
    on: [applied]
`,
	})
	dir := filepath.Join(root, ".reeve")
	if err := NewMigrator().MigrateDirectory(dir, false); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	text, _ := os.ReadFile(filepath.Join(dir, "notifications.yaml"))
	s := string(text)
	if strings.Contains(s, "slack:\n") {
		t.Fatalf("slack key survived:\n%s", s)
	}
	if !strings.Contains(s, "type: webhook") || !strings.Contains(s, "type: slack") {
		t.Fatalf("merged sinks wrong:\n%s", s)
	}
	if strings.Count(s, "sinks:") != 1 {
		t.Fatalf("duplicate sinks key:\n%s", s)
	}
}

func TestMigrateDryRunWritesNothing(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"notifications.yaml": `version: 1
config_type: notifications
slack:
  enabled: true
  channel: "#x"
`,
	})
	dir := filepath.Join(root, ".reeve")
	before, _ := os.ReadFile(filepath.Join(dir, "notifications.yaml"))
	if err := NewMigrator().MigrateDirectory(dir, true); err != nil {
		t.Fatalf("migrate --dry-run: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "notifications.yaml"))
	if string(before) != string(after) {
		t.Fatal("dry-run modified the file")
	}
	if _, err := os.Stat(filepath.Join(dir, "notifications.yaml.bak")); err == nil {
		t.Fatal("dry-run created a backup")
	}
}

func TestMigrateLeavesOtherConfigTypesAlone(t *testing.T) {
	root := writeReeve(t, map[string]string{
		"shared.yaml": sharedYAMLMin,
	})
	dir := filepath.Join(root, ".reeve")
	before, _ := os.ReadFile(filepath.Join(dir, "shared.yaml"))
	if err := NewMigrator().MigrateDirectory(dir, false); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "shared.yaml"))
	if string(before) != string(after) {
		t.Fatal("shared.yaml should be untouched")
	}
}
