// Package locks is the pure lock state machine. No I/O — the storage
// interface (blob.Store with If-Match writes) is injected. See
// openspec/specs/core/locking.
package locks

import (
	"errors"
	"time"
)

// Status describes the current state of a lock on disk.
type Status string

const (
	StatusFree    Status = "free"
	StatusHeld    Status = "held"
	StatusExpired Status = "expired"
)

// Lock is the serialized shape written to blob. One file per stack:
// locks/{project}/{stack}.json.
type Lock struct {
	Project   string      `json:"project"`
	Stack     string      `json:"stack"`
	Holder    *Holder     `json:"holder,omitempty"` // nil if free
	Queue     []QueueItem `json:"queue"`            // FIFO; holder not included
	UpdatedAt string      `json:"updated_at"`       // RFC3339
}

// Holder is who currently holds the lock.
type Holder struct {
	PR         int    `json:"pr"`
	CommitSHA  string `json:"commit_sha"`
	RunID      string `json:"run_id"`
	Actor      string `json:"actor"`
	AcquiredAt string `json:"acquired_at"` // RFC3339
	ExpiresAt  string `json:"expires_at"`  // RFC3339
}

// QueueItem is a waiting applicant.
type QueueItem struct {
	PR         int    `json:"pr"`
	CommitSHA  string `json:"commit_sha"`
	RunID      string `json:"run_id"`
	Actor      string `json:"actor"`
	EnqueuedAt string `json:"enqueued_at"`
}

// ErrAlreadyHolder signals the caller is already the holder (idempotent
// re-acquire; callers typically treat as success).
var ErrAlreadyHolder = errors.New("already holding lock")

// ErrAlreadyQueued signals the caller is already waiting in the queue.
var ErrAlreadyQueued = errors.New("already queued")

// ErrNotHolder is returned by Release when the caller does not currently
// hold the lock.
var ErrNotHolder = errors.New("not the current holder")

// NewLock returns a fresh free lock.
func NewLock(project, stack string, now time.Time) Lock {
	return Lock{
		Project:   project,
		Stack:     stack,
		UpdatedAt: now.UTC().Format(time.RFC3339),
	}
}

// TryAcquire transitions the lock. Returns the new state plus either
// - acquired=true: caller is now the holder
// - acquired=false: caller is queued (or already queued)
//
// Expired holders are evicted. Holder idempotency: re-acquire by the same
// PR refreshes the expires_at.
func TryAcquire(l Lock, applicant Holder, ttl time.Duration, now time.Time) (Lock, bool, error) {
	l = evictIfExpired(l, now)

	// Idempotent re-acquire?
	if l.Holder != nil && l.Holder.PR == applicant.PR {
		refreshed := *l.Holder
		refreshed.ExpiresAt = now.Add(ttl).UTC().Format(time.RFC3339)
		refreshed.CommitSHA = applicant.CommitSHA
		refreshed.RunID = applicant.RunID
		refreshed.Actor = applicant.Actor
		l.Holder = &refreshed
		l.UpdatedAt = now.UTC().Format(time.RFC3339)
		return l, true, ErrAlreadyHolder
	}

	if l.Holder == nil {
		applicant.AcquiredAt = now.UTC().Format(time.RFC3339)
		applicant.ExpiresAt = now.Add(ttl).UTC().Format(time.RFC3339)
		l.Holder = &applicant
		// If applicant was also queued (race), remove from queue.
		l.Queue = removePR(l.Queue, applicant.PR)
		l.UpdatedAt = now.UTC().Format(time.RFC3339)
		return l, true, nil
	}

	// Already held by someone else — enqueue.
	for _, q := range l.Queue {
		if q.PR == applicant.PR {
			return l, false, ErrAlreadyQueued
		}
	}
	l.Queue = append(l.Queue, QueueItem{
		PR:         applicant.PR,
		CommitSHA:  applicant.CommitSHA,
		RunID:      applicant.RunID,
		Actor:      applicant.Actor,
		EnqueuedAt: now.UTC().Format(time.RFC3339),
	})
	l.UpdatedAt = now.UTC().Format(time.RFC3339)
	return l, false, nil
}

// Release transfers the lock to the next queued applicant (or frees it).
// Only the current holder can release by pr match.
func Release(l Lock, pr int, now time.Time) (Lock, error) {
	l = evictIfExpired(l, now)
	if l.Holder == nil || l.Holder.PR != pr {
		// Removing from queue is always safe.
		before := len(l.Queue)
		l.Queue = removePR(l.Queue, pr)
		if len(l.Queue) != before {
			l.UpdatedAt = now.UTC().Format(time.RFC3339)
			return l, nil
		}
		return l, ErrNotHolder
	}
	l.Holder = nil
	l = promoteNext(l, now, defaultPromoteTTL)
	l.UpdatedAt = now.UTC().Format(time.RFC3339)
	return l, nil
}

// Leave removes pr from holder or queue without erroring if absent.
// Used for PR closed / merged cleanup across all stacks.
func Leave(l Lock, pr int, now time.Time) Lock {
	if l.Holder != nil && l.Holder.PR == pr {
		l.Holder = nil
		l = promoteNext(l, now, defaultPromoteTTL)
	}
	l.Queue = removePR(l.Queue, pr)
	l.UpdatedAt = now.UTC().Format(time.RFC3339)
	return l
}

// Reap evicts an expired holder if present. Returns (lock, evicted).
func Reap(l Lock, now time.Time) (Lock, bool) {
	if l.Holder == nil {
		return l, false
	}
	if expired(l.Holder, now) {
		l.Holder = nil
		l = promoteNext(l, now, defaultPromoteTTL)
		l.UpdatedAt = now.UTC().Format(time.RFC3339)
		return l, true
	}
	return l, false
}

// Status returns the current status as seen at now.
func (l Lock) Status(now time.Time) Status {
	if l.Holder == nil {
		return StatusFree
	}
	if expired(l.Holder, now) {
		return StatusExpired
	}
	return StatusHeld
}

// --- helpers ---

const defaultPromoteTTL = 4 * time.Hour

func evictIfExpired(l Lock, now time.Time) Lock {
	if l.Holder != nil && expired(l.Holder, now) {
		l.Holder = nil
		l = promoteNext(l, now, defaultPromoteTTL)
	}
	return l
}

func promoteNext(l Lock, now time.Time, ttl time.Duration) Lock {
	if l.Holder != nil || len(l.Queue) == 0 {
		return l
	}
	next := l.Queue[0]
	l.Queue = l.Queue[1:]
	l.Holder = &Holder{
		PR:         next.PR,
		CommitSHA:  next.CommitSHA,
		RunID:      next.RunID,
		Actor:      next.Actor,
		AcquiredAt: now.UTC().Format(time.RFC3339),
		ExpiresAt:  now.Add(ttl).UTC().Format(time.RFC3339),
	}
	return l
}

func expired(h *Holder, now time.Time) bool {
	if h == nil || h.ExpiresAt == "" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, h.ExpiresAt)
	if err != nil {
		return false
	}
	return now.After(exp)
}

func removePR(q []QueueItem, pr int) []QueueItem {
	out := q[:0]
	for _, item := range q {
		if item.PR != pr {
			out = append(out, item)
		}
	}
	return out
}
