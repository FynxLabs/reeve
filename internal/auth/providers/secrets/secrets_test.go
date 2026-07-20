package secrets

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestApplyEnvMap(t *testing.T) {
	cases := []struct {
		name  string
		m     map[string]string
		value string
		want  map[string]string
	}{
		{
			name: "nil map yields empty env",
			m:    nil, value: "whatever",
			want: map[string]string{},
		},
		{
			name: "empty field takes whole value",
			m:    map[string]string{"API_KEY": ""}, value: "raw-value",
			want: map[string]string{"API_KEY": "raw-value"},
		},
		{
			name:  "json field extraction",
			m:     map[string]string{"API_KEY": "api_key", "OTHER": "other"},
			value: `{"api_key":"k-1","other":"o-1"}`,
			want:  map[string]string{"API_KEY": "k-1", "OTHER": "o-1"},
		},
		{
			name: "missing field falls back to whole value",
			m:    map[string]string{"X": "nope"}, value: `{"api_key":"k"}`,
			want: map[string]string{"X": `{"api_key":"k"}`},
		},
		{
			name: "non-json value falls back to whole value",
			m:    map[string]string{"X": "field"}, value: "plain-string",
			want: map[string]string{"X": "plain-string"},
		},
		{
			name: "non-string json field falls back to whole value",
			m:    map[string]string{"X": "n"}, value: `{"n":42}`,
			want: map[string]string{"X": `{"n":42}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyEnvMap(tc.m, tc.value); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("applyEnvMap = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractJSONField(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		field  string
		want   string
		wantOK bool
	}{
		{"simple", `{"a":"b"}`, "a", "b", true},
		{"escaped quotes", `{"a":"say \"hi\""}`, "a", `say "hi"`, true},
		{"unicode escape", `{"a":"é"}`, "a", "é", true},
		{"nested object is not a string", `{"a":{"b":"c"}}`, "a", "", false},
		{"missing field", `{"a":"b"}`, "z", "", false},
		{"not json", `nope`, "a", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractJSONField(tc.raw, tc.field)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("extractJSONField(%q, %q) = (%q, %v), want (%q, %v)",
					tc.raw, tc.field, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestBase64StdDecode(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"padded", "aHVzaA==", "hush", false},
		{"unpadded gets repadded", "aHVzaA", "hush", false},
		{"invalid", "!!!", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := base64StdDecode(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGitHubSecret(t *testing.T) {
	t.Run("resolves env var", func(t *testing.T) {
		t.Setenv("REEVE_TEST_SECRET", "hush")
		p := NewGitHubSecret(&GitHubSecret{Name: "gh", EnvVar: "REEVE_TEST_SECRET",
			EnvMap: map[string]string{"OUT": ""}})
		if p.Name() != "gh" || p.Type() != "github_secret" {
			t.Errorf("Name/Type = %q/%q", p.Name(), p.Type())
		}
		cred, err := p.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if cred.Env["OUT"] != "hush" || cred.Kind != "github-secret" || cred.Source != "gh" {
			t.Errorf("credential wrong: %+v", cred)
		}
	})
	t.Run("empty env var fails closed without leaking", func(t *testing.T) {
		t.Setenv("REEVE_TEST_SECRET", "")
		p := NewGitHubSecret(&GitHubSecret{Name: "gh", EnvVar: "REEVE_TEST_SECRET"})
		_, err := p.Acquire(context.Background())
		if err == nil || !strings.Contains(err.Error(), "REEVE_TEST_SECRET") {
			t.Fatalf("expected missing-secret error naming the env var, got %v", err)
		}
	})
}

func setSecretManagerBase(t *testing.T, url string) {
	t.Helper()
	orig := secretManagerBase
	secretManagerBase = url
	t.Cleanup(func() { secretManagerBase = orig })
}

func TestGCPSecretManagerRequiresToken(t *testing.T) {
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "")
	p := NewGCPSecretManager(&GCPSecretManager{Name: "g", SecretName: "projects/p/secrets/s/versions/latest"})
	_, err := p.Acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "bind a gcp_wif provider first") {
		t.Fatalf("expected missing-token guidance, got %v", err)
	}
}

// TestGCPSecretManagerRealResponseShape asserts the provider parses the
// REAL Secret Manager access response, where the base64 data lives
// nested at payload.data.
func TestGCPSecretManagerRealResponseShape(t *testing.T) {
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "gcp-token-do-not-leak")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"projects/p/secrets/s/versions/1","payload":{"data":"aHVzaA==","dataCrc32c":"0"}}`))
	}))
	defer srv.Close()
	setSecretManagerBase(t, srv.URL)

	p := NewGCPSecretManager(&GCPSecretManager{
		Name: "g", SecretName: "projects/p/secrets/s/versions/latest",
		EnvMap: map[string]string{"OUT": ""},
	})
	cred, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if cred.Env["OUT"] != "hush" {
		t.Fatalf("decoded secret = %q, want hush", cred.Env["OUT"])
	}
}

// TestGCPSecretManagerFlatPayloadRejected: a response without the nested
// payload.data (e.g. the old flattened fixture shape) is not a valid
// Secret Manager response and fails closed.
func TestGCPSecretManagerFlatPayloadRejected(t *testing.T) {
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "gcp-token-do-not-leak")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":"aHVzaA=="}`))
	}))
	defer srv.Close()
	setSecretManagerBase(t, srv.URL)

	p := NewGCPSecretManager(&GCPSecretManager{Name: "g", SecretName: "projects/p/secrets/s/versions/latest"})
	_, err := p.Acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected payload") {
		t.Fatalf("flat payload must be rejected; got %v", err)
	}
}

func TestGCPSecretManagerAcquire(t *testing.T) {
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "gcp-token-do-not-leak")

	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"payload":{"data":"aHVzaA=="}}`))
	}))
	defer srv.Close()
	setSecretManagerBase(t, srv.URL)

	p := NewGCPSecretManager(&GCPSecretManager{
		Name: "g", SecretName: "projects/p/secrets/s/versions/latest",
		EnvMap: map[string]string{"OUT": ""},
	})
	cred, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if gotPath != "/v1/projects/p/secrets/s/versions/latest:access" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer gcp-token-do-not-leak" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if cred.Env["OUT"] != "hush" || cred.Kind != "gcp-secret" || cred.Source != "g" {
		t.Errorf("credential wrong: %+v", cred)
	}
}

func TestGCPSecretManagerHTTPError(t *testing.T) {
	t.Setenv("CLOUDSDK_AUTH_ACCESS_TOKEN", "gcp-token-do-not-leak")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":{"status":"PERMISSION_DENIED"}}`))
	}))
	defer srv.Close()
	setSecretManagerBase(t, srv.URL)

	p := NewGCPSecretManager(&GCPSecretManager{Name: "g", SecretName: "projects/p/secrets/s/versions/latest"})
	_, err := p.Acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "secretmanager 403") {
		t.Fatalf("error = %v", err)
	}
	// Redaction contract: the access token never appears in errors.
	if strings.Contains(err.Error(), "gcp-token-do-not-leak") {
		t.Errorf("error leaks the access token: %v", err)
	}
}

// isolateAWS points the AWS SDK at the given endpoint and keeps it away
// from host config, IMDS, and real endpoints.
func isolateAWS(t *testing.T, service, url string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "example-secret")
	t.Setenv("AWS_ENDPOINT_URL_"+service, url)
}

func TestAWSSecretsManagerAcquire(t *testing.T) {
	var gotTarget string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTarget = r.Header.Get("X-Amz-Target")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"ARN":"arn:aws:secretsmanager:us-east-1:000000000000:secret:app","Name":"app","SecretString":"{\"api_key\":\"k-1\"}"}`))
	}))
	defer srv.Close()
	isolateAWS(t, "SECRETS_MANAGER", srv.URL)

	before := time.Now()
	p := NewAWSSecretsManager(&AWSSecretsManager{
		Name: "sm", SecretID: "app", Region: "us-east-1",
		EnvMap: map[string]string{"API_KEY": "api_key"},
	})
	if p.Name() != "sm" || p.Type() != "aws_secrets_manager" {
		t.Errorf("Name/Type = %q/%q", p.Name(), p.Type())
	}
	cred, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if gotTarget != "secretsmanager.GetSecretValue" {
		t.Errorf("X-Amz-Target = %q", gotTarget)
	}
	if gotBody["SecretId"] != "app" {
		t.Errorf("SecretId = %v", gotBody["SecretId"])
	}
	if cred.Env["API_KEY"] != "k-1" || cred.Kind != "aws-secret" || cred.Source != "sm" {
		t.Errorf("credential wrong: %+v", cred)
	}
	// Default TTL is 1h.
	if cred.ExpiresAt.Before(before.Add(59*time.Minute)) || cred.ExpiresAt.After(time.Now().Add(61*time.Minute)) {
		t.Errorf("ExpiresAt = %v, want ~1h", cred.ExpiresAt)
	}
}

func TestAWSSecretsManagerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException","message":"Secrets Manager can't find the specified secret."}`))
	}))
	defer srv.Close()
	isolateAWS(t, "SECRETS_MANAGER", srv.URL)

	p := NewAWSSecretsManager(&AWSSecretsManager{Name: "sm", SecretID: "missing", Region: "us-east-1"})
	_, err := p.Acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ResourceNotFoundException") {
		t.Fatalf("error = %v", err)
	}
}

func TestAWSSSMParameterAcquire(t *testing.T) {
	var gotTarget string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTarget = r.Header.Get("X-Amz-Target")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"Parameter":{"Name":"/app/key","Type":"SecureString","Value":"hush"}}`))
	}))
	defer srv.Close()
	isolateAWS(t, "SSM", srv.URL)

	p := NewAWSSSMParameter(&AWSSSMParameter{
		Name: "ssm", Parameter: "/app/key", Region: "us-east-1",
		EnvMap: map[string]string{"OUT": ""},
	})
	if p.Name() != "ssm" || p.Type() != "aws_ssm_parameter" {
		t.Errorf("Name/Type = %q/%q", p.Name(), p.Type())
	}
	cred, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if gotTarget != "AmazonSSM.GetParameter" {
		t.Errorf("X-Amz-Target = %q", gotTarget)
	}
	if gotBody["Name"] != "/app/key" {
		t.Errorf("Name = %v", gotBody["Name"])
	}
	if dec, ok := gotBody["WithDecryption"].(bool); !ok || !dec {
		t.Errorf("WithDecryption = %v, want true", gotBody["WithDecryption"])
	}
	if cred.Env["OUT"] != "hush" || cred.Kind != "aws-ssm" || cred.Source != "ssm" {
		t.Errorf("credential wrong: %+v", cred)
	}
}

func TestAWSSSMParameterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"__type":"ParameterNotFound","message":""}`))
	}))
	defer srv.Close()
	isolateAWS(t, "SSM", srv.URL)

	p := NewAWSSSMParameter(&AWSSSMParameter{Name: "ssm", Parameter: "/missing", Region: "us-east-1"})
	_, err := p.Acquire(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ParameterNotFound") {
		t.Fatalf("error = %v", err)
	}
}
