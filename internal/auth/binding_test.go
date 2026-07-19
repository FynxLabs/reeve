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

func TestResolveWithDeclsOverrideReplacesSameType(t *testing.T) {
	// Override must replace earlier-matched providers of the same declared
	// credential scope even when their names share no prefix. The old
	// name-prefix approximation left "legacy-cloud" (an AWS provider)
	// active alongside the override - an over-privileged leak.
	bs := []Binding{
		{StackPattern: "prod/*", Providers: []string{"legacy-cloud", "cloudflare-token"}},
		{StackPattern: "prod/payments", Override: []string{"aws-payments-strict"}},
	}
	decls := map[string]ProviderDecl{
		"legacy-cloud":        {Name: "legacy-cloud", Type: "aws_oidc"},
		"cloudflare-token":    {Name: "cloudflare-token", Type: "aws_secrets_manager"},
		"aws-payments-strict": {Name: "aws-payments-strict", Type: "aws_oidc"},
	}
	got := ResolveWithDecls(bs, decls, "prod/payments", ModeApply)
	want := []string{"cloudflare-token", "aws-payments-strict"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestResolveWithDeclsOverrideKeepsOtherTypes(t *testing.T) {
	bs := []Binding{
		{StackPattern: "prod/*", Providers: []string{"aws-prod", "gcp-prod"}},
		{StackPattern: "prod/*", Mode: ModeDrift, Override: []string{"aws-prod-readonly"}},
	}
	decls := map[string]ProviderDecl{
		"aws-prod":          {Name: "aws-prod", Type: "aws_oidc"},
		"gcp-prod":          {Name: "gcp-prod", Type: "gcp_wif"},
		"aws-prod-readonly": {Name: "aws-prod-readonly", Type: "aws_oidc"},
	}
	got := ResolveWithDecls(bs, decls, "prod/api", ModeDrift)
	want := []string{"gcp-prod", "aws-prod-readonly"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("different-type provider must survive override: got %v want %v", got, want)
	}
}

func TestResolveWithDeclsUndeclaredFallsBackToPrefix(t *testing.T) {
	// Names missing from decls keep the historical name-prefix behavior so
	// partially-declared configs do not silently change meaning.
	bs := []Binding{
		{StackPattern: "prod/*", Providers: []string{"aws-prod", "cloudflare-token"}},
		{StackPattern: "prod/payments", Override: []string{"aws-payments-strict"}},
	}
	got := ResolveWithDecls(bs, nil, "prod/payments", ModeApply)
	want := []string{"cloudflare-token", "aws-payments-strict"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
