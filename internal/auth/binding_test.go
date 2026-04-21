package auth

import (
	"reflect"
	"testing"
)

func TestResolveGeneralToSpecific(t *testing.T) {
	bs := []Binding{
		{StackPattern: "staging/*", Providers: []string{"aws-stage"}},
		{StackPattern: "prod/*", Providers: []string{"aws-prod", "gcp-prod"}},
		{StackPattern: "prod/payments", Providers: []string{"github-app"}},
	}
	got := Resolve(bs, "prod/payments", ModeApply)
	want := []string{"aws-prod", "gcp-prod", "github-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestResolveModeScoped(t *testing.T) {
	bs := []Binding{
		{StackPattern: "prod/*", Providers: []string{"aws-prod"}},
		{StackPattern: "prod/*", Mode: ModeDrift, Providers: []string{"aws-prod-readonly"}},
	}
	gotApply := Resolve(bs, "prod/api", ModeApply)
	if !reflect.DeepEqual(gotApply, []string{"aws-prod"}) {
		t.Fatalf("apply got %v", gotApply)
	}
	gotDrift := Resolve(bs, "prod/api", ModeDrift)
	if !contains(gotDrift, "aws-prod-readonly") {
		t.Fatalf("drift should include aws-prod-readonly: %v", gotDrift)
	}
}

func TestResolveOverrideReplacesScope(t *testing.T) {
	bs := []Binding{
		{StackPattern: "prod/*", Providers: []string{"aws-prod", "cloudflare-token"}},
		{StackPattern: "prod/payments", Override: []string{"aws-payments-strict"}, Providers: []string{"github-app"}},
	}
	got := Resolve(bs, "prod/payments", ModeApply)
	// aws-prod should be replaced by aws-payments-strict; cloudflare-token
	// untouched; github-app appended.
	want := []string{"cloudflare-token", "aws-payments-strict", "github-app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestValidateConflictSameScope(t *testing.T) {
	bs := []Binding{
		{StackPattern: "prod/*", Providers: []string{"aws-a", "aws-b"}},
	}
	decls := map[string]ProviderDecl{
		"aws-a": {Name: "aws-a", Type: "aws_oidc"},
		"aws-b": {Name: "aws-b", Type: "aws_oidc"},
	}
	err := Validate(bs, decls, []string{"prod/api"})
	if err == nil {
		t.Fatal("expected conflict error")
	}
}

func TestValidateAllowsDifferentScopes(t *testing.T) {
	bs := []Binding{
		{StackPattern: "prod/*", Providers: []string{"aws-prod", "gcp-prod", "cloudflare-token"}},
	}
	decls := map[string]ProviderDecl{
		"aws-prod":         {Name: "aws-prod", Type: "aws_oidc"},
		"gcp-prod":         {Name: "gcp-prod", Type: "gcp_wif"},
		"cloudflare-token": {Name: "cloudflare-token", Type: "aws_secrets_manager"},
	}
	if err := Validate(bs, decls, []string{"prod/api"}); err != nil {
		t.Fatalf("should validate: %v", err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
