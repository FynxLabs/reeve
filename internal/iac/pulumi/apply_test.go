package pulumi

import "testing"

func TestParseApplyFromEventStream(t *testing.T) {
	stream := []byte(`
{"summaryEvent":{"resourceChanges":{"create":2,"update":1,"replace":1}}}
`)
	counts, errMsg := parseApply(stream)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if counts.Add != 2 || counts.Change != 1 || counts.Replace != 1 {
		t.Fatalf("counts off: %+v", counts)
	}
}

func TestParseApplyFallbackText(t *testing.T) {
	text := []byte(`Updating (prod)

Resources:
    + 2 created
    ~ 1 updated
    - 0 deleted
Duration: 14s
`)
	counts, _ := parseApply(text)
	if counts.Add != 2 || counts.Change != 1 {
		t.Fatalf("counts: %+v", counts)
	}
}
