package redact

import "testing"

func TestRedactKnownSecret(t *testing.T) {
	r := New()
	r.AddSecret("xoxb-super-secret-token")
	got := r.Redact("Authorization: Bearer xoxb-super-secret-token; trailing")
	if got == "Authorization: Bearer xoxb-super-secret-token; trailing" {
		t.Fatal("known secret not redacted")
	}
	if want := "[redacted]"; !contains(got, want) {
		t.Fatalf("replacement missing: %q", got)
	}
}

func TestRedactRegexRule(t *testing.T) {
	r := New().AddRule(`AKIA[0-9A-Z]{16}`)
	got := r.Redact("role AKIAIOSFODNN7EXAMPLE goes here")
	if contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("AKIA key not redacted: %s", got)
	}
}

func TestRedactPulumiMarker(t *testing.T) {
	r := New()
	got := r.Redact("password: [secret], api_key: <secret>")
	if contains(got, "[secret]") || contains(got, "<secret>") {
		t.Fatalf("pulumi markers not redacted: %s", got)
	}
}

func TestShortSecretsNotAdded(t *testing.T) {
	r := New()
	r.AddSecret("abc") // too short
	got := r.Redact("abc abc abc")
	if got != "abc abc abc" {
		t.Fatalf("short secret should not be redacted: %q", got)
	}
}

func TestNilRedactorPassThrough(t *testing.T) {
	var r *Redactor
	if r.Redact("x") != "x" {
		t.Fatal("nil redactor should pass through")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
