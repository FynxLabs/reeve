package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/thefynx/reeve/internal/blob"
)

// prState is persisted at notifications/pr-{n}/slack.json.
type prState struct {
	Channel  string `json:"channel"`
	MainTS   string `json:"main_ts"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

func prStateKey(pr int) string { return fmt.Sprintf("notifications/pr-%d/slack.json", pr) }

// loadPRState reads the per-PR message state plus its ETag for the
// compare-and-swap on save. A missing object is a legitimately fresh state;
// any other failure is propagated - swallowing it here made the channel believe
// no message existed and post a duplicate.
func (s *Channel) loadPRState(ctx context.Context, pr int) (*prState, string, error) {
	rc, meta, err := s.blob.Get(ctx, prStateKey(pr))
	if errors.Is(err, blob.ErrNotFound) {
		return &prState{}, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("load slack state for pr %d: %w", pr, err)
	}
	defer rc.Close()
	var st prState
	if err := json.NewDecoder(rc).Decode(&st); err != nil {
		return nil, "", fmt.Errorf("decode slack state for pr %d: %w", pr, err)
	}
	etag := ""
	if meta != nil {
		etag = meta.ETag
	}
	return &st, etag, nil
}

// savePRState writes the state conditionally on the ETag observed at load
// (empty ETag = create-only). On a conflict, another run raced us: if it
// recorded the same main message, retry over its version (keeping its
// thread TS); if it recorded a different message, keep the first writer's
// state and report the conflict.
func (s *Channel) savePRState(ctx context.Context, pr int, st *prState, etag string) error {
	for attempt := 0; attempt < 3; attempt++ {
		data, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			return err
		}
		_, err = s.blob.PutIfMatch(ctx, prStateKey(pr), strings.NewReader(string(data)), etag)
		if err == nil {
			return nil
		}
		if !errors.Is(err, blob.ErrPreconditionFailed) {
			return fmt.Errorf("save slack state for pr %d: %w", pr, err)
		}
		remote, remoteETag, lerr := s.loadPRState(ctx, pr)
		if lerr != nil {
			return fmt.Errorf("save slack state for pr %d: conflict, then: %w", pr, lerr)
		}
		if remote.MainTS != "" && remote.MainTS != st.MainTS {
			// A concurrent run created its own message first. Keep its
			// state (first writer wins) and surface the duplicate.
			slog.Warn("slack state conflict: concurrent message exists, keeping it",
				"pr", pr, "ours", st.MainTS, "theirs", remote.MainTS)
			return fmt.Errorf("slack state for pr %d: concurrent update created message ts=%s (ours ts=%s discarded)", pr, remote.MainTS, st.MainTS)
		}
		if st.ThreadTS == "" {
			st.ThreadTS = remote.ThreadTS
		}
		etag = remoteETag
	}
	return fmt.Errorf("save slack state for pr %d: too many conflicts", pr)
}
