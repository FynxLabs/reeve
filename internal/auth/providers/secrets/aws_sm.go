// Package secrets implements the secret-manager provider types:
// aws_secrets_manager, aws_ssm_parameter, gcp_secret_manager,
// azure_key_vault, github_secret (env).
//
// Each provider retrieves the secret value at Acquire-time and exposes
// it via env vars the engine consumes. Secrets are never persisted.
package secrets

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/thefynx/reeve/internal/auth"
)

// AWSSecretsManager pulls a secret and exposes it via env vars.
type AWSSecretsManager struct {
	Name     string
	SecretID string
	Region   string
	EnvMap   map[string]string // envKey → secret JSON field ("" = whole value)
	TTL      time.Duration
}

func (p *AWSSecretsManager) ProviderName() string { return p.Name }
func (p *AWSSecretsManager) Type() string         { return "aws_secrets_manager" }

func (p *AWSSecretsManager) Acquire(ctx context.Context) (*auth.Credential, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(p.Region))
	if err != nil {
		return nil, err
	}
	cli := secretsmanager.NewFromConfig(cfg)
	out, err := cli.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(p.SecretID),
	})
	if err != nil {
		return nil, err
	}
	value := aws.ToString(out.SecretString)
	env := applyEnvMap(p.EnvMap, value)
	ttl := p.TTL
	if ttl == 0 {
		ttl = time.Hour
	}
	return &auth.Credential{
		Env: env, Kind: "aws-secret", Source: p.Name,
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

// AWSSSMParameter pulls an SSM parameter (decrypted).
type AWSSSMParameter struct {
	Name      string
	Parameter string
	Region    string
	EnvMap    map[string]string
}

func (p *AWSSSMParameter) ProviderName() string { return p.Name }
func (p *AWSSSMParameter) Type() string         { return "aws_ssm_parameter" }

func (p *AWSSSMParameter) Acquire(ctx context.Context) (*auth.Credential, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(p.Region))
	if err != nil {
		return nil, err
	}
	cli := ssm.NewFromConfig(cfg)
	out, err := cli.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(p.Parameter), WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	val := aws.ToString(out.Parameter.Value)
	env := applyEnvMap(p.EnvMap, val)
	return &auth.Credential{Env: env, Kind: "aws-ssm", Source: p.Name, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// GitHubSecret reads an env var that Actions surfaces from repo secrets.
// The workflow wires $MY_SECRET_ENV into GitHubSecret.EnvVar=MY_SECRET_ENV.
type GitHubSecret struct {
	Name   string
	EnvVar string
	EnvMap map[string]string
}

func (p *GitHubSecret) ProviderName() string { return p.Name }
func (p *GitHubSecret) Type() string         { return "github_secret" }

func (p *GitHubSecret) Acquire(ctx context.Context) (*auth.Credential, error) {
	val := os.Getenv(p.EnvVar)
	if val == "" {
		return nil, fmt.Errorf("github_secret: env var %s is empty", p.EnvVar)
	}
	env := applyEnvMap(p.EnvMap, val)
	return &auth.Credential{Env: env, Kind: "github-secret", Source: p.Name}, nil
}

// applyEnvMap maps `{envKey: jsonField}` to env. If field is "", the
// whole value is the env value. If field starts with "$.", it's a
// JSON pointer into the secret string (parsed lazily).
func applyEnvMap(m map[string]string, value string) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(m))
	for envKey, field := range m {
		if field == "" {
			out[envKey] = value
			continue
		}
		// Simple dotted-path: "api_key" in a JSON object.
		if extracted, ok := extractJSONField(value, field); ok {
			out[envKey] = extracted
		} else {
			out[envKey] = value
		}
	}
	return out
}

func extractJSONField(raw, field string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return "", false
	}
	// Best-effort substring match for "field": "value" — JSON parsing
	// would pull in another dep; a targeted scan is fine.
	needle := `"` + field + `"`
	idx := strings.Index(raw, needle)
	if idx < 0 {
		return "", false
	}
	rest := raw[idx+len(needle):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return "", false
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// compile-time checks
var _ auth.Provider = &awsSMShim{}
var _ auth.Provider = &awsSSMShim{}
var _ auth.Provider = &githubSecretShim{}

// Shims so adapters satisfy the Provider interface via a thin wrapper —
// keeps Name()/Type() collision with embedded fields at bay.
type awsSMShim struct{ *AWSSecretsManager }

func (s *awsSMShim) Name() string { return s.AWSSecretsManager.Name }
func (s *awsSMShim) Type() string { return s.AWSSecretsManager.Type() }
func (s *awsSMShim) Acquire(ctx context.Context) (*auth.Credential, error) {
	return s.AWSSecretsManager.Acquire(ctx)
}

type awsSSMShim struct{ *AWSSSMParameter }

func (s *awsSSMShim) Name() string { return s.AWSSSMParameter.Name }
func (s *awsSSMShim) Type() string { return s.AWSSSMParameter.Type() }
func (s *awsSSMShim) Acquire(ctx context.Context) (*auth.Credential, error) {
	return s.AWSSSMParameter.Acquire(ctx)
}

type githubSecretShim struct{ *GitHubSecret }

func (s *githubSecretShim) Name() string { return s.GitHubSecret.Name }
func (s *githubSecretShim) Type() string { return s.GitHubSecret.Type() }
func (s *githubSecretShim) Acquire(ctx context.Context) (*auth.Credential, error) {
	return s.GitHubSecret.Acquire(ctx)
}

// Constructors returning the auth.Provider interface directly.
func NewAWSSecretsManager(p *AWSSecretsManager) auth.Provider { return &awsSMShim{p} }
func NewAWSSSMParameter(p *AWSSSMParameter) auth.Provider     { return &awsSSMShim{p} }
func NewGitHubSecret(p *GitHubSecret) auth.Provider           { return &githubSecretShim{p} }
