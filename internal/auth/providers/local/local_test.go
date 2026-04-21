package local

import (
	"context"
	"strings"
	"testing"
)

func TestAWSProfileRefusesInCI(t *testing.T) {
	t.Setenv("CI", "true")
	p := &AWSProfile{ProviderName: "aws-local", Profile: "dev"}
	_, err := p.Acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refuses") {
		t.Fatalf("expected CI refusal, got %v", err)
	}
}

func TestAWSProfileOKWhenNotCI(t *testing.T) {
	t.Setenv("CI", "")
	p := &AWSProfile{ProviderName: "aws-local", Profile: "dev", Region: "us-west-2"}
	c, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Env["AWS_PROFILE"] != "dev" || c.Env["AWS_REGION"] != "us-west-2" {
		t.Fatalf("env wrong: %+v", c.Env)
	}
}

func TestEnvPassthroughRefusesWithoutFlag(t *testing.T) {
	p := &EnvPassthrough{ProviderName: "leak"}
	_, err := p.Acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "dangerous") {
		t.Fatalf("expected refusal without i_understand: %v", err)
	}
}

func TestEnvPassthroughCopiesVars(t *testing.T) {
	t.Setenv("MY_SECRET", "hush")
	p := &EnvPassthrough{
		ProviderName: "passthrough",
		IUnderstand:  true,
		EnvVars:      map[string]string{"MY_SECRET_OUT": "MY_SECRET"},
	}
	c, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Env["MY_SECRET_OUT"] != "hush" {
		t.Fatalf("passthrough failed: %+v", c.Env)
	}
}
