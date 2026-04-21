package filesystem

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/thefynx/reeve/internal/blob"
)

func TestPutGet(t *testing.T) {
	ctx := context.Background()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	md, err := s.Put(ctx, "runs/pr-1/manifest.json", strings.NewReader(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if md.ETag == "" {
		t.Fatal("expected ETag")
	}

	data, _, err := ReadBytes(ctx, s, "runs/pr-1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("roundtrip mismatch: %s", data)
	}
}

func TestPutIfMatchOnlyIfAbsent(t *testing.T) {
	ctx := context.Background()
	s, _ := New(t.TempDir())
	// empty ifMatch => only if absent
	if _, err := s.PutIfMatch(ctx, "locks/a.json", strings.NewReader("v1"), ""); err != nil {
		t.Fatalf("first put: %v", err)
	}
	_, err := s.PutIfMatch(ctx, "locks/a.json", strings.NewReader("v2"), "")
	if !errors.Is(err, blob.ErrPreconditionFailed) {
		t.Fatalf("expected ErrPreconditionFailed, got %v", err)
	}
}

func TestPutIfMatchWithETag(t *testing.T) {
	ctx := context.Background()
	s, _ := New(t.TempDir())
	md, err := s.Put(ctx, "locks/b.json", strings.NewReader("v1"))
	if err != nil {
		t.Fatal(err)
	}
	// wrong etag
	_, err = s.PutIfMatch(ctx, "locks/b.json", strings.NewReader("v2"), "wrong")
	if !errors.Is(err, blob.ErrPreconditionFailed) {
		t.Fatalf("expected mismatch rejection, got %v", err)
	}
	// right etag
	if _, err := s.PutIfMatch(ctx, "locks/b.json", strings.NewReader("v2"), md.ETag); err != nil {
		t.Fatalf("if-match with correct etag failed: %v", err)
	}
}

func TestGetMissing(t *testing.T) {
	ctx := context.Background()
	s, _ := New(t.TempDir())
	_, _, err := s.Get(ctx, "missing")
	if !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPathEscapeRejected(t *testing.T) {
	ctx := context.Background()
	s, _ := New(t.TempDir())
	_, err := s.Put(ctx, "../escape", strings.NewReader("nope"))
	if err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("expected path-escape rejection, got %v", err)
	}
}

func TestList(t *testing.T) {
	ctx := context.Background()
	s, _ := New(t.TempDir())
	for _, k := range []string{"runs/pr-1/a.json", "runs/pr-1/b.json", "runs/pr-2/c.json"} {
		if _, err := s.Put(ctx, k, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := s.List(ctx, "runs/pr-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys under pr-1, got %v", keys)
	}
}
