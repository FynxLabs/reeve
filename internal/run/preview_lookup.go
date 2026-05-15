package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/blob/filesystem"
	"github.com/thefynx/reeve/internal/core/summary"
)

// PreviewStatus is what apply needs to know about a prior preview for a
// given (PR, commit SHA, stack ref). Filled from the most recent matching
// run manifest in the bucket.
type PreviewStatus struct {
	Found        bool
	Age          time.Duration
	Succeeded    bool // false if the stack's preview errored
	HasChanges   bool
	ErrorMessage string
	RunID        string
}

// PlanSucceededForPR returns true if the most recent preview manifest for the
// given PR and commit SHA exists and has no stacks in error state.
func PlanSucceededForPR(ctx context.Context, store blob.Store, prNumber int, commitSHA string) bool {
	if store == nil || prNumber == 0 {
		return false
	}
	prefix := fmt.Sprintf("runs/pr-%d/", prNumber)
	keys, err := store.List(ctx, prefix)
	if err != nil {
		return false
	}
	var manifests []string
	for _, k := range keys {
		if strings.HasSuffix(k, "/manifest.json") {
			manifests = append(manifests, k)
		}
	}
	var best *manifest
	for _, k := range manifests {
		data, _, err := filesystem.ReadBytes(ctx, store, k)
		if err != nil {
			continue
		}
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Op != "preview" || m.CommitSHA != commitSHA {
			continue
		}
		if best == nil || m.CreatedAt > best.CreatedAt {
			c := m
			best = &c
		}
	}
	if best == nil {
		return false
	}
	for _, ss := range best.Stacks {
		if ss.Status == summary.StatusError {
			return false
		}
	}
	return len(best.Stacks) > 0
}

// FindPreviewForStack scans runs/pr-{n}/ for manifests, picks the most
// recent one whose commit_sha + op=preview matches, and reports whether
// the named stack was present and successful there.
func FindPreviewForStack(ctx context.Context, store blob.Store, prNumber int, commitSHA, stackRef string) (PreviewStatus, error) {
	if store == nil || prNumber == 0 {
		return PreviewStatus{}, nil
	}
	prefix := fmt.Sprintf("runs/pr-%d/", prNumber)
	keys, err := store.List(ctx, prefix)
	if err != nil {
		return PreviewStatus{}, err
	}
	// Keep only manifest.json entries.
	var manifests []string
	for _, k := range keys {
		if strings.HasSuffix(k, "/manifest.json") {
			manifests = append(manifests, k)
		}
	}
	slog.Debug("preview lookup: scanning manifests", "pr", prNumber, "sha", commitSHA, "stack", stackRef, "manifest_count", len(manifests))
	if len(manifests) == 0 {
		slog.Debug("preview lookup: no manifests found", "pr", prNumber, "sha", commitSHA)
		return PreviewStatus{}, nil
	}

	// Decode each, filter to preview + matching SHA, track newest by
	// CreatedAt.
	var best *manifest
	for _, k := range manifests {
		data, _, err := filesystem.ReadBytes(ctx, store, k)
		if err != nil {
			if errors.Is(err, blob.ErrNotFound) {
				continue
			}
			return PreviewStatus{}, err
		}
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue // skip malformed
		}
		slog.Debug("preview lookup: manifest scanned", "key", k, "op", m.Op, "manifest_sha", m.CommitSHA, "want_sha", commitSHA, "match", m.Op == "preview" && m.CommitSHA == commitSHA)
		if m.Op != "preview" || m.CommitSHA != commitSHA {
			continue
		}
		if best == nil || m.CreatedAt > best.CreatedAt {
			c := m
			best = &c
		}
	}
	if best == nil {
		slog.Debug("preview lookup: no matching preview manifest for sha", "pr", prNumber, "sha", commitSHA)
		return PreviewStatus{}, nil
	}
	slog.Debug("preview lookup: best manifest", "run_id", best.RunID, "created_at", best.CreatedAt, "stack_count", len(best.Stacks))

	createdAt, err := time.Parse(time.RFC3339, best.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}
	st := PreviewStatus{
		Found:     true,
		Age:       time.Since(createdAt),
		Succeeded: true,
		RunID:     best.RunID,
	}
	// Look for the specific stack.
	for _, ss := range best.Stacks {
		if ss.Ref() != stackRef {
			continue
		}
		if ss.Status == summary.StatusError {
			st.Succeeded = false
			st.ErrorMessage = ss.Error
		}
		if ss.Counts.Total() > 0 {
			st.HasChanges = true
		}
		return st, nil
	}
	// Manifest exists for this SHA but doesn't cover this stack - treat as
	// "no fresh preview for this stack".
	return PreviewStatus{Found: false}, nil
}
