package config

import (
	"testing"

	"github.com/thefynx/reeve/internal/config/schemas"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("TEST_BUCKET", "resolved-bucket")
	t.Setenv("TEST_APPROVER", "@org/resolved")

	c := &Config{Shared: &schemas.Shared{}}
	c.Shared.Bucket.Name = "${env:TEST_BUCKET}" // struct field
	c.Shared.Bucket.Type = "gcs"                // literal, must not change
	c.Shared.Approvals.Stacks = map[string]schemas.ApprovalRuleYAML{
		"prod/*": {Approvers: []string{"${env:TEST_APPROVER}", "@org/literal"}}, // map→struct→slice→string
	}

	c.ExpandEnv()

	if c.Shared.Bucket.Name != "resolved-bucket" {
		t.Fatalf("bucket.name not expanded: %q", c.Shared.Bucket.Name)
	}
	if c.Shared.Bucket.Type != "gcs" {
		t.Fatalf("literal bucket.type mutated: %q", c.Shared.Bucket.Type)
	}
	got := c.Shared.Approvals.Stacks["prod/*"].Approvers
	if got[0] != "@org/resolved" || got[1] != "@org/literal" {
		t.Fatalf("map/slice env expansion wrong: %v", got)
	}
}

func TestExpandEnvMissingVarBecomesEmpty(t *testing.T) {
	// os.Getenv of an unset var is "" - matches the notify helper's behavior.
	c := &Config{Shared: &schemas.Shared{}}
	c.Shared.Bucket.Name = "${env:DEFINITELY_UNSET_XYZ}"
	c.ExpandEnv()
	if c.Shared.Bucket.Name != "" {
		t.Fatalf("unset env should expand to empty, got %q", c.Shared.Bucket.Name)
	}
}

func TestStrictDecodeRejectsMultiDoc(t *testing.T) {
	single := []byte("version: 1\nconfig_type: shared\n")
	if err := strictDecode(single, &schemas.Header{}); err != nil {
		t.Fatalf("single doc should decode: %v", err)
	}
	multi := []byte("version: 1\nconfig_type: shared\n---\nversion: 1\nconfig_type: auth\n")
	if err := strictDecode(multi, &schemas.Header{}); err == nil {
		t.Fatal("multi-doc must be rejected, not silently ignored")
	}
}

func TestMigrateHeaderOrderIndependent(t *testing.T) {
	// Reversed key order with an interleaved comment: the old single regex
	// failed; the split regexes must still find both.
	data := []byte("config_type: shared\n# a comment\nversion: 1\n")
	if m := versionRE.FindSubmatch(data); m == nil || string(m[1]) != "1" {
		t.Fatalf("versionRE failed on reversed+commented header: %v", m)
	}
	if m := configTypeRE.FindSubmatch(data); m == nil || string(m[1]) != "shared" {
		t.Fatalf("configTypeRE failed on reversed+commented header: %v", m)
	}
}
