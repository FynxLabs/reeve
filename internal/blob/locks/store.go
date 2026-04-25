// Package locks is the blob-backed lock storage layer. Composes the pure
// lock state machine (internal/core/locks) with any blob.Store.
// Conditional writes via If-Match; on precondition failure the retry loop
// re-reads and reapplies.
package locks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/thefynx/reeve/internal/blob"
	corelocks "github.com/thefynx/reeve/internal/core/locks"
)

// Store wraps a blob.Store with lock-specific key conventions.
type Store struct {
	store blob.Store
	// MaxRetries bounds the optimistic-concurrency retry loop.
	MaxRetries int
	// Now is injectable for tests.
	Now func() time.Time
}

// New returns a Store. MaxRetries defaults to 5.
func New(s blob.Store) *Store {
	return &Store{store: s, MaxRetries: 5, Now: time.Now}
}

func (s *Store) key(project, stack string) string {
	return fmt.Sprintf("locks/%s/%s.json", project, stack)
}

// Get reads the current lock state for a stack. Returns a fresh free lock
// if none exists yet.
func (s *Store) Get(ctx context.Context, project, stack string) (corelocks.Lock, string, error) {
	key := s.key(project, stack)
	rc, md, err := s.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			return corelocks.NewLock(project, stack, s.Now()), "", nil
		}
		return corelocks.Lock{}, "", err
	}
	defer rc.Close()
	var l corelocks.Lock
	if err := json.NewDecoder(rc).Decode(&l); err != nil {
		return corelocks.Lock{}, "", fmt.Errorf("decode lock: %w", err)
	}
	return l, md.ETag, nil
}

// TryAcquire runs the acquire transition with optimistic concurrency.
// Returns acquired=true if the caller holds the lock after the call.
// acquired=false means the caller is queued. The updated lock is always
// returned.
func (s *Store) TryAcquire(ctx context.Context, project, stack string, applicant corelocks.Holder, ttl time.Duration) (corelocks.Lock, bool, error) {
	return s.mutate(ctx, project, stack, func(cur corelocks.Lock) (corelocks.Lock, bool, error) {
		next, ok, err := corelocks.TryAcquire(cur, applicant, ttl, s.Now())
		if errors.Is(err, corelocks.ErrAlreadyHolder) {
			return next, true, nil // idempotent success
		}
		return next, ok, err
	})
}

// Release releases the lock. If pr is the holder, the next queued
// applicant is promoted.
func (s *Store) Release(ctx context.Context, project, stack string, pr int) (corelocks.Lock, error) {
	l, _, err := s.mutate(ctx, project, stack, func(cur corelocks.Lock) (corelocks.Lock, bool, error) {
		next, err := corelocks.Release(cur, pr, s.Now())
		return next, false, err
	})
	return l, err
}

// Leave removes pr from holder or queue across a stack. Silent if absent.
// Intended for PR merge/close cleanup.
func (s *Store) Leave(ctx context.Context, project, stack string, pr int) (corelocks.Lock, error) {
	l, _, err := s.mutate(ctx, project, stack, func(cur corelocks.Lock) (corelocks.Lock, bool, error) {
		return corelocks.Leave(cur, pr, s.Now()), false, nil
	})
	return l, err
}

// ForceUnlock is the admin-override release. It clears the holder
// regardless of PR, promotes the queue. Callers verify admin auth
// before invoking (via shared.yaml locking.admin_override.allowed).
func (s *Store) ForceUnlock(ctx context.Context, project, stack string) (corelocks.Lock, error) {
	l, _, err := s.mutate(ctx, project, stack, func(cur corelocks.Lock) (corelocks.Lock, bool, error) {
		cur.Holder = nil
		cur = forcePromoteQueue(cur, s.Now(), time.Hour*4)
		cur.UpdatedAt = s.Now().UTC().Format(time.RFC3339)
		return cur, false, nil
	})
	return l, err
}

func forcePromoteQueue(l corelocks.Lock, now time.Time, ttl time.Duration) corelocks.Lock {
	if l.Holder != nil || len(l.Queue) == 0 {
		return l
	}
	next := l.Queue[0]
	l.Queue = l.Queue[1:]
	l.Holder = &corelocks.Holder{
		PR:         next.PR,
		CommitSHA:  next.CommitSHA,
		RunID:      next.RunID,
		Actor:      next.Actor,
		AcquiredAt: now.UTC().Format(time.RFC3339),
		ExpiresAt:  now.Add(ttl).UTC().Format(time.RFC3339),
	}
	return l
}

// Reap evicts an expired holder. Returns (lock, evicted).
func (s *Store) Reap(ctx context.Context, project, stack string) (corelocks.Lock, bool, error) {
	var evicted bool
	l, _, err := s.mutate(ctx, project, stack, func(cur corelocks.Lock) (corelocks.Lock, bool, error) {
		next, ev := corelocks.Reap(cur, s.Now())
		evicted = ev
		return next, false, nil
	})
	return l, evicted, err
}

// ReapAll walks locks/ and reaps expired holders across every stack.
// Called opportunistically by reeve invocations and by `reeve locks reap`.
func (s *Store) ReapAll(ctx context.Context) (int, error) {
	keys, err := s.store.List(ctx, "locks")
	if err != nil {
		return 0, err
	}
	var n int
	for _, k := range keys {
		if !strings.HasSuffix(k, ".json") {
			continue
		}
		proj, stack, ok := parseLockKey(k)
		if !ok {
			continue
		}
		_, evicted, err := s.Reap(ctx, proj, stack)
		if err != nil {
			return n, err
		}
		if evicted {
			n++
		}
	}
	return n, nil
}

// ListAll returns every lock currently stored.
func (s *Store) ListAll(ctx context.Context) ([]corelocks.Lock, error) {
	keys, err := s.store.List(ctx, "locks")
	if err != nil {
		return nil, err
	}
	var out []corelocks.Lock
	for _, k := range keys {
		if !strings.HasSuffix(k, ".json") {
			continue
		}
		proj, stack, ok := parseLockKey(k)
		if !ok {
			continue
		}
		l, _, err := s.Get(ctx, proj, stack)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// mutate is the conditional-write retry loop.
func (s *Store) mutate(
	ctx context.Context, project, stack string,
	fn func(corelocks.Lock) (corelocks.Lock, bool, error),
) (corelocks.Lock, bool, error) {
	key := s.key(project, stack)
	for attempt := 0; attempt <= s.MaxRetries; attempt++ {
		cur, etag, err := s.Get(ctx, project, stack)
		if err != nil {
			return corelocks.Lock{}, false, err
		}
		next, flag, fnErr := fn(cur)
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			return corelocks.Lock{}, false, err
		}
		_, putErr := s.store.PutIfMatch(ctx, key, bytes.NewReader(data), etag)
		if putErr == nil {
			return next, flag, fnErr
		}
		if !errors.Is(putErr, blob.ErrPreconditionFailed) {
			return corelocks.Lock{}, false, putErr
		}
		// Lost the race - retry.
	}
	return corelocks.Lock{}, false, fmt.Errorf("lock %s/%s: exceeded %d retries", project, stack, s.MaxRetries)
}

// parseLockKey decodes "locks/<project>/<stack>.json".
func parseLockKey(k string) (string, string, bool) {
	k = strings.TrimPrefix(k, "locks/")
	k = strings.TrimSuffix(k, ".json")
	parts := strings.SplitN(k, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
