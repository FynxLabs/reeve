// Package audit writes append-only audit log entries. One JSON file per
// run at audit/{year}/{month}/{day}/{run-id}.json. Write-once: entries
// are created with If-None-Match so the same run-id can never overwrite.
// Retention is configured in shared.yaml (default 7y).
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/thefynx/reeve/internal/blob"
)

// Schema version lives in every entry. Bumping requires a migration.
const SchemaVersion = 1

// Entry is one audit record. All fields are JSON-serializable; never
// contains plan bodies or secrets.
type Entry struct {
	SchemaVersion int     `json:"schema_version"`
	RunID         string  `json:"run_id"`
	Op            string  `json:"op"` // preview | apply | drift
	StartedAt     string  `json:"started_at"`
	FinishedAt    string  `json:"finished_at"`
	Actor         string  `json:"actor"` // user login triggering the run
	PR            int     `json:"pr,omitempty"`
	CommitSHA     string  `json:"commit_sha,omitempty"`
	Repo          string  `json:"repo,omitempty"` // "owner/name"
	Outcome       string  `json:"outcome"`        // "success" | "failed" | "blocked"
	Stacks        []Stack `json:"stacks"`
	// DurationMS is FinishedAt - StartedAt in milliseconds.
	DurationMS int64 `json:"duration_ms"`
}

// Stack is the per-stack record inside an entry. Counts only; no plan bodies.
type Stack struct {
	Ref        string `json:"ref"` // "project/stack"
	Env        string `json:"env"`
	Status     string `json:"status"`
	Add        int    `json:"add"`
	Change     int    `json:"change"`
	Delete     int    `json:"delete"`
	Replace    int    `json:"replace"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Error      string `json:"error,omitempty"` // short error message only
}

// Writer persists entries to blob.
type Writer struct {
	store blob.Store
	Now   func() time.Time
}

// NewWriter returns a Writer.
func NewWriter(s blob.Store) *Writer {
	return &Writer{store: s, Now: time.Now}
}

// Write stores an entry write-once at the audit/YYYY/MM/DD/run-id.json
// key. If the key already exists (another writer beat us), returns
// blob.ErrPreconditionFailed.
func (w *Writer) Write(ctx context.Context, e Entry) error {
	if e.SchemaVersion == 0 {
		e.SchemaVersion = SchemaVersion
	}
	started, err := time.Parse(time.RFC3339, e.StartedAt)
	if err != nil {
		return fmt.Errorf("audit: invalid started_at %q: %w", e.StartedAt, err)
	}
	key := fmt.Sprintf("audit/%04d/%02d/%02d/%s.json",
		started.Year(), int(started.Month()), started.Day(), e.RunID)

	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.store.PutIfMatch(ctx, key, bytes.NewReader(data), "") // "" = absent
	return err
}
