package terraform

import (
	"testing"

	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/iac"
)

// TestRegistryResolvesBothVariants: the one adapter package registers both
// engine.type values; each resolves to its own variant with the right
// default binary, and engine.binary.path overrides both.
func TestRegistryResolvesBothVariants(t *testing.T) {
	cases := []struct {
		typ, display, binary string
	}{
		{"terraform", "Terraform", "terraform"},
		{"tofu", "OpenTofu", "tofu"},
	}
	for _, c := range cases {
		e, err := iac.New(schemas.EngineBody{Type: c.typ})
		if err != nil {
			t.Fatalf("iac.New(%s): %v", c.typ, err)
		}
		if e.Name() != c.display {
			t.Errorf("%s: Name() = %q, want %q", c.typ, e.Name(), c.display)
		}
		te, ok := e.(*Engine)
		if !ok {
			t.Fatalf("%s: iac.New returned %T, want *terraform.Engine", c.typ, e)
		}
		if te.Binary != c.binary {
			t.Errorf("%s: Binary = %q, want %q", c.typ, te.Binary, c.binary)
		}

		custom, err := iac.New(schemas.EngineBody{
			Type:   c.typ,
			Binary: schemas.EngineBinary{Path: "/custom/bin"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if custom.(*Engine).Binary != "/custom/bin" {
			t.Errorf("%s: binary.path override ignored", c.typ)
		}
	}
}

func TestCapabilities(t *testing.T) {
	caps := New(Terraform, schemas.EngineBody{}).Capabilities()
	if !caps.SupportsSavedPlans {
		t.Error("saved plans: apply consumes the plan file its own plan step wrote")
	}
	if !caps.SupportsRefresh {
		t.Error("refresh: drift checks run plan -refresh-only")
	}
	if caps.SupportsPolicyNative {
		t.Error("no native policy engine")
	}
	if caps.SecretsProviderTypes != nil {
		t.Error("state encryption is backend-side; no engine secrets providers")
	}
}
