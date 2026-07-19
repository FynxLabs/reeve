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
	l, _, _ = TryAcquire(l, Holder{PR: 1, RunID: "run-a"}, time.Hour, t0)
	l, ok, err := TryAcquire(l, Holder{PR: 1, RunID: "run-a", CommitSHA: "bbb"}, time.Hour, t0.Add(10*time.Minute))
	if !ok || !errors.Is(err, ErrAlreadyHolder) {
		t.Fatalf("expected ErrAlreadyHolder, got ok=%v err=%v", ok, err)
	}
	if l.Holder.CommitSHA != "bbb" {
		t.Fatalf("re-acquire should update commit sha")
	}
}

func TestAcquireSamePRDifferentRunRefused(t *testing.T) {
	// A double `/reeve apply` (or workflow re-run) on the same PR must not
	// run pulumi up concurrently: the second run is refused, not queued.
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1, RunID: "run-a"}, time.Hour, t0)
	got, ok, err := TryAcquire(l, Holder{PR: 1, RunID: "run-b"}, time.Hour, t0.Add(time.Minute))
	if ok || !errors.Is(err, ErrHeldBySamePR) {
		t.Fatalf("expected ErrHeldBySamePR, got ok=%v err=%v", ok, err)
	}
	if got.Holder == nil || got.Holder.RunID != "run-a" {
		t.Fatalf("holder must stay run-a: %+v", got.Holder)
	}
	if len(got.Queue) != 0 {
		t.Fatalf("same PR must not queue behind itself: %+v", got.Queue)
	}
}

func TestAcquireSamePRDifferentRunTakesOverExpired(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1, RunID: "run-a"}, time.Hour, t0)
	// run-a's lease expired: normal eviction/takeover applies.
	l, ok, err := TryAcquire(l, Holder{PR: 1, RunID: "run-b"}, time.Hour, t0.Add(2*time.Hour))
	if err != nil || !ok {
		t.Fatalf("expected takeover after expiry, got ok=%v err=%v", ok, err)
	}
	if l.Holder == nil || l.Holder.RunID != "run-b" {
		t.Fatalf("holder should be run-b: %+v", l.Holder)
	}
}

func TestReleasePromotesQueue(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1, RunID: "run-1"}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 3}, time.Hour, t0)
	l, err := Release(l, 1, "run-1", time.Hour, t0.Add(time.Minute))
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
	_, err := Release(l, 2, "", time.Hour, t0)
	if !errors.Is(err, ErrNotHolder) {
		t.Fatalf("expected ErrNotHolder, got %v", err)
	}
}

func TestReleaseFromQueueIsSilent(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	l, err := Release(l, 2, "", time.Hour, t0.Add(time.Minute))
	if err != nil {
		t.Fatalf("queued PR release should succeed silently: %v", err)
	}
	if len(l.Queue) != 0 {
		t.Fatalf("queue should be empty: %+v", l.Queue)
	}
}

func TestReleaseWrongRunIDDoesNotFree(t *testing.T) {
	// A stale or duplicate run of the same PR finishing must not free the
	// lock out from under the run that actually holds it.
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1, RunID: "run-a"}, time.Hour, t0)
	got, err := Release(l, 1, "run-b", time.Hour, t0.Add(time.Minute))
	if !errors.Is(err, ErrNotHolder) {
		t.Fatalf("expected ErrNotHolder, got %v", err)
	}
	if got.Holder == nil || got.Holder.RunID != "run-a" {
		t.Fatalf("holder must survive wrong-run release: %+v", got.Holder)
	}
}

func TestReleasePromotesWithConfiguredTTL(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1, RunID: "run-1"}, 30*time.Minute, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, 30*time.Minute, t0)
	rel := t0.Add(time.Minute)
	l, err := Release(l, 1, "run-1", 30*time.Minute, rel)
	if err != nil {
		t.Fatal(err)
	}
	want := rel.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	if l.Holder == nil || l.Holder.ExpiresAt != want {
		t.Fatalf("promoted holder must get the configured ttl: want expires=%s got %+v", want, l.Holder)
	}
}

func TestPromoteTTLFallsBackToDefault(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1, RunID: "run-1"}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	rel := t0.Add(time.Minute)
	l, err := Release(l, 1, "run-1", 0, rel)
	if err != nil {
		t.Fatal(err)
	}
	want := rel.Add(defaultPromoteTTL).UTC().Format(time.RFC3339)
	if l.Holder == nil || l.Holder.ExpiresAt != want {
		t.Fatalf("ttl<=0 must fall back to default: want expires=%s got %+v", want, l.Holder)
	}
}

func TestReapPromotesWithConfiguredTTL(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	later := t0.Add(2 * time.Hour)
	l, evicted := Reap(l, 30*time.Minute, later)
	if !evicted {
		t.Fatal("expected eviction")
	}
	want := later.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	if l.Holder == nil || l.Holder.ExpiresAt != want {
		t.Fatalf("reap-promoted holder must get the configured ttl: want expires=%s got %+v", want, l.Holder)
	}
}

func TestLeaveRunScopedKeepsOtherRunsHolder(t *testing.T) {
	// A finishing run leaving its PR everywhere must not evict a different
	// live run of the same PR that holds another stack's lock.
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1, RunID: "run-a"}, time.Hour, t0)
	l = Leave(l, 1, "run-b", time.Hour, t0.Add(time.Minute))
	if l.Holder == nil || l.Holder.RunID != "run-a" {
		t.Fatalf("run-scoped leave must not evict a different live run: %+v", l.Holder)
	}
	// runID "" (admin / PR-close cleanup) removes any run of the PR.
	l = Leave(l, 1, "", time.Hour, t0.Add(2*time.Minute))
	if l.Holder != nil {
		t.Fatalf("unscoped leave should clear the holder: %+v", l.Holder)
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

func TestCorruptedExpiresAtTreatedAsExpired(t *testing.T) {
	// A lock blob with a malformed ExpiresAt would otherwise be immortal -
	// the eviction check returned false on parse failure. The reaper must
	// be able to clear it.
	corrupted := &Holder{PR: 1, ExpiresAt: "not-a-timestamp"}
	if !expired(corrupted, t0) {
		t.Fatal("expected corrupted ExpiresAt to be treated as expired")
	}
}

func TestReapEvictsAndPromotes(t *testing.T) {
	l := NewLock("api", "prod", t0)
	l, _, _ = TryAcquire(l, Holder{PR: 1}, time.Hour, t0)
	l, _, _ = TryAcquire(l, Holder{PR: 2}, time.Hour, t0)
	later := t0.Add(2 * time.Hour)
	l, evicted := Reap(l, time.Hour, later)
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
	l = Leave(l, 2, "", time.Hour, t0.Add(time.Minute)) // queued PR leaves
	if len(l.Queue) != 1 || l.Queue[0].PR != 3 {
		t.Fatalf("expected queue to be [3], got %+v", l.Queue)
	}
	l = Leave(l, 1, "", time.Hour, t0.Add(2*time.Minute)) // holder leaves, promotes 3
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
