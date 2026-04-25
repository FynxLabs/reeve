package locks

import (
	"context"
	"testing"
	"time"

	"github.com/thefynx/reeve/internal/blob/filesystem"
	corelocks "github.com/thefynx/reeve/internal/core/locks"
)

func newStore(t *testing.T, now time.Time) *Store {
	t.Helper()
	fs, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(fs)
	s.Now = func() time.Time { return now }
	return s
}

func TestAcquireReleaseFlow(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	s := newStore(t, now)

	l, ok, err := s.TryAcquire(ctx, "api", "prod", corelocks.Holder{PR: 1, RunID: "r1"}, time.Hour)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	if l.Holder == nil || l.Holder.PR != 1 {
		t.Fatalf("lock not held by 1: %+v", l)
	}

	l, ok, err = s.TryAcquire(ctx, "api", "prod", corelocks.Holder{PR: 2, RunID: "r2"}, time.Hour)
	if err != nil || ok {
		t.Fatalf("second acquire should queue: ok=%v err=%v", ok, err)
	}
	if len(l.Queue) != 1 {
		t.Fatalf("queue len: %d", len(l.Queue))
	}

	l, err = s.Release(ctx, "api", "prod", 1)
	if err != nil {
		t.Fatal(err)
	}
	if l.Holder == nil || l.Holder.PR != 2 {
		t.Fatalf("expected pr 2 promoted: %+v", l.Holder)
	}
}

func TestListAllAndReapAll(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	s := newStore(t, now)

	_, _, _ = s.TryAcquire(ctx, "api", "prod", corelocks.Holder{PR: 1}, time.Hour)
	_, _, _ = s.TryAcquire(ctx, "worker", "prod", corelocks.Holder{PR: 5}, time.Hour)

	got, err := s.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 locks listed, got %d", len(got))
	}

	// Advance time past TTL.
	s.Now = func() time.Time { return now.Add(2 * time.Hour) }
	n, err := s.ReapAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 reaped, got %d", n)
	}
}

func TestConditionalWriteRetryOnRace(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	s := newStore(t, now)

	// First attempt: two calls in sequence, each read/write independently.
	// Interleave them: both read free lock, both write - second should
	// get a precondition failure and retry.
	// Simulate by just calling TryAcquire twice; the second will observe
	// the first's write via re-read.
	_, ok, err := s.TryAcquire(ctx, "api", "prod", corelocks.Holder{PR: 1}, time.Hour)
	if err != nil || !ok {
		t.Fatal(err)
	}
	_, ok, err = s.TryAcquire(ctx, "api", "prod", corelocks.Holder{PR: 2}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second acquire should have queued, not acquired")
	}
}
