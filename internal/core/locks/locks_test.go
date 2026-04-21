package locks

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

func TestAcquireFree(t *testing.T) {
	l := NewLock("api", "prod", t0)
	got, ok, err := TryAcquire(l, Holder{PR: 1, CommitSHA: "aaa"}, time.Hour, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Holder == nil || got.Holder.PR != 1 {
		t.Fatalf("expected held by pr 1, got %+v", got)
	}
}

func TestAcquireAlreadyHeldEnqueues(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, ok, err := TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	if err != nil || ok {
		t.Fatalf("expected queued, got ok=%v err=%v", ok, err)
	}
	if len(l.Queue) != 1 || l.Queue[0].PR != 2 {
		t.Fatalf("queue wrong: %+v", l.Queue)
	}
}

func TestAcquireIdempotent(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, ok, err := TryAcquire(l, Holder{PR: 1, CommitSHA: "bbb"}, time.Hour, t0.Add(10*time.Minute))
	if !ok || !errors.Is(err, ErrAlreadyHolder) {
		t.Fatalf("expected ErrAlreadyHolder, got ok=%v err=%v", ok, err)
	}
	if l.Holder.CommitSHA != "bbb" {
		t.Fatalf("re-acquire should update commit sha")
	}
}

func TestReleasePromotesQueue(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 3}, time.Hour, t0)
	l, err := Release(l, 1, t0.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if l.Holder == nil || l.Holder.PR != 2 {
		t.Fatalf("expected pr 2 promoted, got %+v", l.Holder)
	}
	if len(l.Queue) != 1 || l.Queue[0].PR != 3 {
		t.Fatalf("queue wrong: %+v", l.Queue)
	}
}

func TestReleaseNotHolder(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	_, err := Release(l, 2, t0)
	if !errors.Is(err, ErrNotHolder) {
		t.Fatalf("expected ErrNotHolder, got %v", err)
	}
}

func TestReleaseFromQueueIsSilent(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	l, err := Release(l, 2, t0.Add(time.Minute))
	if err != nil {
		t.Fatalf("queued PR release should succeed silently: %v", err)
	}
	if len(l.Queue) != 0 {
		t.Fatalf("queue should be empty: %+v", l.Queue)
	}
}

func TestExpiredHolderEvictedOnAcquire(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	// 2 hours later, PR 2 shows up
	later := t0.Add(2 * time.Hour)
	l, ok, err := TryAcquire(l, Holder{PR: 2}, time.Hour, later)
	if err != nil || !ok {
		t.Fatalf("expected pr 2 to acquire after eviction, got ok=%v err=%v", ok, err)
	}
	if l.Holder.PR != 2 {
		t.Fatalf("holder should be pr 2: %+v", l.Holder)
	}
}

func TestReapEvictsAndPromotes(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	later := t0.Add(2 * time.Hour)
	l, evicted := Reap(l, later)
	if !evicted {
		t.Fatal("expected eviction")
	}
	if l.Holder == nil || l.Holder.PR != 2 {
		t.Fatalf("expected pr 2 promoted after reap: %+v", l.Holder)
	}
}

func TestLeaveRemovesAcrossHolderAndQueue(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 3}, time.Hour, t0)
	l = Leave(l, 2, t0.Add(time.Minute)) // queued PR leaves
	if len(l.Queue) != 1 || l.Queue[0].PR != 3 {
		t.Fatalf("expected queue to be [3], got %+v", l.Queue)
	}
	l = Leave(l, 1, t0.Add(2*time.Minute)) // holder leaves, promotes 3
	if l.Holder == nil || l.Holder.PR != 3 {
		t.Fatalf("expected pr 3 promoted: %+v", l.Holder)
	}
}

func TestStatus(t *testing.T) {
	l := NewLock("api", "prod", t0)
	if l.Status(t0) != StatusFree {
		t.Fatal("new lock should be free")
	}
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	if l.Status(t0) != StatusHeld {
		t.Fatal("should be held")
	}
	if l.Status(t0.Add(2*time.Hour)) != StatusExpired {
		t.Fatal("should be expired past ttl")
	}
}
