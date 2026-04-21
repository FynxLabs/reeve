// Package notifications is PR-scoped human notifications. Phase 5 ships
// Slack; Mattermost / Teams / webhook are later. Runs last in the
// pipeline so upstream failures are captured accurately.
package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/slack"
)

// SlackBackend handles the PR-scoped Slack message lifecycle.
type SlackBackend struct {
	Client  *slack.Client
	Channel string
	// BlobStore persists the message TS per-PR so subsequent runs
	// upsert instead of duplicating.
	BlobStore blob.Store
}

// State is what we persist at notifications/pr-{n}/slack.json.
type State struct {
	Channel  string `json:"channel"`
	MainTS   string `json:"main_ts"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

// NotifyPreview publishes or updates the preview status message.
func (b *SlackBackend) NotifyPreview(ctx context.Context, pr int, commitSHA, runURL, op string, stacks []summary.StackSummary) error {
	if b == nil || b.Client == nil || b.Channel == "" {
		return nil
	}
	state, _ := b.loadState(ctx, pr)
	main := buildMainBlocks(pr, commitSHA, runURL, op, stacks)
	text := fmt.Sprintf("reeve %s for PR #%d (%d stacks)", op, pr, len(stacks))
	res, err := b.Client.Upsert(ctx, b.Channel, state.MainTS, text, main)
	if err != nil {
		return err
	}
	state.Channel = b.Channel
	state.MainTS = res.TS
	// Thread: one reply per stack with short summary (first run only —
	// subsequent runs leave the thread untouched to avoid spam).
	if state.ThreadTS == "" && len(stacks) > 0 {
		threadText := fmt.Sprintf("Per-stack summaries for PR #%d", pr)
		threadRes, err := b.Client.PostThread(ctx, b.Channel, res.TS, threadText, buildThreadBlocks(stacks))
		if err == nil {
			state.ThreadTS = threadRes.TS
		}
	}
	return b.saveState(ctx, pr, state)
}

// NotifyApply updates the existing main message with apply outcome.
func (b *SlackBackend) NotifyApply(ctx context.Context, pr int, commitSHA, runURL string, stacks []summary.StackSummary, blocked bool) error {
	if b == nil || b.Client == nil {
		return nil
	}
	state, _ := b.loadState(ctx, pr)
	op := "applied"
	if blocked {
		op = "apply blocked"
	}
	if hasErrors(stacks) {
		op = "apply failed"
	}
	main := buildMainBlocks(pr, commitSHA, runURL, op, stacks)
	text := fmt.Sprintf("reeve %s for PR #%d", op, pr)
	res, err := b.Client.Upsert(ctx, b.Channel, state.MainTS, text, main)
	if err != nil {
		return err
	}
	state.Channel = b.Channel
	state.MainTS = res.TS
	return b.saveState(ctx, pr, state)
}

func (b *SlackBackend) loadState(ctx context.Context, pr int) (*State, error) {
	key := fmt.Sprintf("notifications/pr-%d/slack.json", pr)
	rc, _, err := b.BlobStore.Get(ctx, key)
	if err != nil {
		return &State{}, err
	}
	defer rc.Close()
	var s State
	if err := json.NewDecoder(rc).Decode(&s); err != nil {
		return &State{}, err
	}
	return &s, nil
}

func (b *SlackBackend) saveState(ctx context.Context, pr int, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	key := fmt.Sprintf("notifications/pr-%d/slack.json", pr)
	_, err = b.BlobStore.Put(ctx, key, strings.NewReader(string(data)))
	return err
}

func buildMainBlocks(pr int, sha, runURL, op string, stacks []summary.StackSummary) []slack.Block {
	add, change, del, repl := totals(stacks)
	status := overallIcon(stacks)
	header := fmt.Sprintf("%s reeve · %s · PR #%d", status, op, pr)

	summaryLine := fmt.Sprintf("*%d stack(s)* · +%d ~%d -%d ±%d · commit `%s`",
		len(stacks), add, change, del, repl, short(sha))

	blocks := []slack.Block{
		slack.Header(header),
		slack.Section(summaryLine),
		slack.Divider(),
		slack.Section(stacksTable(stacks)),
	}
	if runURL != "" {
		blocks = append(blocks, slack.LinkButton("View run", runURL))
	}
	return blocks
}

func buildThreadBlocks(stacks []summary.StackSummary) []slack.Block {
	var bs []slack.Block
	for _, s := range stacks {
		body := fmt.Sprintf("*%s* · %s\n+%d ~%d -%d ±%d",
			s.Ref(), statusShort(s.Status), s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace)
		if s.Error != "" {
			body += "\n_error:_ " + truncate(s.Error, 200)
		}
		bs = append(bs, slack.Section(body))
	}
	return bs
}

func stacksTable(stacks []summary.StackSummary) string {
	if len(stacks) == 0 {
		return "_No stacks affected._"
	}
	var b strings.Builder
	for _, s := range stacks {
		fmt.Fprintf(&b, "• `%s` %s — +%d ~%d -%d ±%d\n",
			s.Ref(), statusShort(s.Status),
			s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace)
	}
	return b.String()
}

func totals(stacks []summary.StackSummary) (int, int, int, int) {
	var a, c, d, r int
	for _, s := range stacks {
		a += s.Counts.Add
		c += s.Counts.Change
		d += s.Counts.Delete
		r += s.Counts.Replace
	}
	return a, c, d, r
}

func overallIcon(stacks []summary.StackSummary) string {
	errored, blocked, changed := false, false, false
	for _, s := range stacks {
		switch s.Status {
		case summary.StatusError:
			errored = true
		case summary.StatusBlocked:
			blocked = true
		case summary.StatusReady:
			if s.Counts.Total() > 0 {
				changed = true
			}
		}
	}
	switch {
	case errored:
		return "🔴"
	case blocked:
		return "🟡"
	case changed:
		return "🟢"
	default:
		return "⚪"
	}
}

func statusShort(st summary.Status) string {
	switch st {
	case summary.StatusReady:
		return "✅"
	case summary.StatusBlocked:
		return "🔒"
	case summary.StatusError:
		return "🔴"
	case summary.StatusNoOp:
		return "·"
	}
	return "?"
}

func hasErrors(stacks []summary.StackSummary) bool {
	for _, s := range stacks {
		if s.Status == summary.StatusError {
			return true
		}
	}
	return false
}

func short(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
