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
	r.AddSecret("abc")     // too short (3)
	r.AddSecret("1234567") // still too short (7, below MinSecretLength)
	got := r.Redact("abc abc abc 1234567")
	if got != "abc abc abc 1234567" {
		t.Fatalf("short secrets should not be redacted: %q", got)
	}
}

func TestSecretReplacementIsLongestFirst(t *testing.T) {
	// "tokenABCDEFGH" contains "tokenABCD" as a prefix. If replacement
	// order were random (map iteration), the shorter secret could fire
	// first and leave "EFGH" unredacted. Longest-first guarantees the
	// outer secret wins.
	r := New()
	r.AddSecret("tokenABCD")
	r.AddSecret("tokenABCDEFGH")
	got := r.Redact("found tokenABCDEFGH in logs")
	if contains(got, "tokenABCD") || contains(got, "EFGH") {
		t.Fatalf("longest-first replacement leaked: %q", got)
	}
}

func TestSecretReplacementIsDeterministic(t *testing.T) {
	// Two registered secrets, run Redact many times, all outputs must
	// match. Map iteration alone would fail this test.
	r := New()
	r.AddSecret("alphabetagamma")
	r.AddSecret("deltaepsilonzeta")
	want := r.Redact("alphabetagamma deltaepsilonzeta")
	for i := 0; i < 100; i++ {
		if got := r.Redact("alphabetagamma deltaepsilonzeta"); got != want {
			t.Fatalf("non-deterministic redaction: %q vs %q", got, want)
		}
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
