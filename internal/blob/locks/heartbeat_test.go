package locks

import (
	"context"
	"testing"
	"time"

	"github.com/thefynx/reeve/internal/blob/filesystem"
	corelocks "github.com/thefynx/reeve/internal/core/locks"
)

// TestHeartbeatKeepsLongApplyAlive drives an apply that outlives the lock
// ttl: the heartbeat must extend the lease past the original expiry, and a
// reaper firing mid-apply must NOT evict the live holder. Once the
// heartbeat stops, the clock (injectable) jumps forward and the reaper
// evicts as usual.
func TestHeartbeatKeepsLongApplyAlive(t *testing.T) {
	ctx := context.Background()
	fs, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(fs) // real clock while the heartbeat runs

	const ttl = 1 * time.Second // heartbeat every ttl/3
	holder := corelocks.Holder{PR: 1, RunID: "long-run", Actor: "alice"}
	if _, ok, err := s.TryAcquire(ctx, "api", "prod", holder, ttl); err != nil || !ok {
		t.Fatalf("acquire: ok=%v err=%v", ok, err)
	}
	origExpiry := mustExpiry(t, s, ctx)

	stop := s.StartHeartbeat(ctx, "api", "prod", holder, ttl)
	defer stop()

	// Simulate a long apply: sleep well past the original 1s lease.
	time.Sleep(1500 * time.Millisecond)

	// A reaper during a heartbeat-kept lease must not evict.
	n, err := s.ReapAll(ctx, ttl)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("reaper evicted a live heartbeat-kept holder (%d evictions)", n)
	}
	l, _, err := s.Get(ctx, "api", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if l.Holder == nil || l.Holder.RunID != "long-run" {
		t.Fatalf("holder must survive past the original lease: %+v", l.Holder)
	}
	kept := mustExpiry(t, s, ctx)
	if !kept.After(origExpiry) {
		t.Fatalf("lease must have been extended: orig=%s kept=%s", origExpiry, kept)
	}

	// Stop is idempotent, and without the heartbeat the lease ages out.
	stop()
	stop()
	s.Now = func() time.Time { return time.Now().Add(time.Hour) }
	n, err = s.ReapAll(ctx, ttl)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("stopped heartbeat must let the reaper evict: %d evictions", n)
	}
}

// TestHeartbeatStopsOnContextCancel: a cancelled run context ends the
// heartbeat goroutine without needing stop().
func TestHeartbeatStopsOnContextCancel(t *testing.T) {
	fs, err := filesystem.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(fs)
	ctx, cancel := context.WithCancel(context.Background())
	const ttl = 300 * time.Millisecond
	holder := corelocks.Holder{PR: 1, RunID: "r1"}
	if _, ok, err := s.TryAcquire(ctx, "api", "prod", holder, ttl); err != nil || !ok {
		t.Fatalf("acquire: ok=%v err=%v", ok, err)
	}
	stop := s.StartHeartbeat(ctx, "api", "prod", holder, ttl)
	cancel()
	// stop must return promptly because the goroutine exited on ctx.Done.
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat did not stop on context cancel")
	}
}

func TestHeartbeatNilStoreOrZeroTTLIsNoop(t *testing.T) {
	var s *Store
	stop := s.StartHeartbeat(context.Background(), "api", "prod", corelocks.Holder{}, time.Hour)
	stop()
	fs, _ := filesystem.New(t.TempDir())
	stop = New(fs).StartHeartbeat(context.Background(), "api", "prod", corelocks.Holder{}, 0)
	stop()
}

func mustExpiry(t *testing.T, s *Store, ctx context.Context) time.Time {
	t.Helper()
	l, _, err := s.Get(ctx, "api", "prod")
	if err != nil || l.Holder == nil {
		t.Fatalf("get holder: %+v err=%v", l.Holder, err)
	}
	exp, err := time.Parse(time.RFC3339, l.Holder.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	return exp
}
