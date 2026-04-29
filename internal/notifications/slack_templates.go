// Package notifications is PR-scoped human notifications. Slack only in v1.
// Runs last in the pipeline so upstream failures are captured accurately.
package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/slack"
)

// attachment sidebar colors.
const (
	colorPending  = "#f2c744" // plan ready / pending approval
	colorApproved = "#0066cc" // approved, ready to apply
	colorApplying = "#6f42c1" // applying in progress
	colorSuccess  = "#28a745" // applied successfully
	colorFailed   = "#dc3545" // failed or blocked hard
	colorBlocked  = "#f2c744" // soft block (missing approval, lock)
)

// SlackBackend handles the PR-scoped Slack message lifecycle.
type SlackBackend struct {
	Client    *slack.Client
	Channel   string
	Icons     *schemas.SlackIcons
	BlobStore blob.Store
}

// State is what we persist at notifications/pr-{n}/slack.json.
type State struct {
	Channel  string `json:"channel"`
	MainTS   string `json:"main_ts"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

// Event phases -- drive both the main message update and thread timeline.
type event string

const (
	eventPlanReady event = "plan_ready"
	eventReady     event = "ready"
	eventApproved  event = "approved"
	eventApplying  event = "applying"
	eventApplied   event = "applied"
	eventFailed    event = "failed"
	eventBlocked   event = "blocked"
)

// NotifyPlanReady sends or updates the Slack message when a plan finishes.
// Only creates a new message if trigger == "plan".
func (b *SlackBackend) NotifyPlanReady(ctx context.Context, in NotifyInput) error {
	if b == nil || b.Client == nil {
		return nil
	}
	state, _ := b.loadState(ctx, in.PR)
	// Only create on plan trigger; if message exists from a prior event, always update.
	if state.MainTS == "" && in.Trigger != schemas.SlackTriggerPlan {
		return nil
	}
	return b.sendOrUpdate(ctx, in, state, eventPlanReady, colorPending)
}

// NotifyReady sends or updates the Slack message when /reeve ready is run.
// Only creates a new message if trigger == "ready" or "plan".
func (b *SlackBackend) NotifyReady(ctx context.Context, in NotifyInput) error {
	if b == nil || b.Client == nil {
		return nil
	}
	state, _ := b.loadState(ctx, in.PR)
	if state.MainTS == "" && in.Trigger == schemas.SlackTriggerApply {
		return nil
	}
	return b.sendOrUpdate(ctx, in, state, eventReady, colorPending)
}

// NotifyApproved updates the message to approved state (preconditions passed, apply imminent).
func (b *SlackBackend) NotifyApproved(ctx context.Context, in NotifyInput) error {
	if b == nil || b.Client == nil {
		return nil
	}
	state, _ := b.loadState(ctx, in.PR)
	if state.MainTS == "" {
		return nil
	}
	return b.sendOrUpdate(ctx, in, state, eventApproved, colorApproved)
}

// NotifyApplying updates the message to "applying" state. Never creates.
func (b *SlackBackend) NotifyApplying(ctx context.Context, in NotifyInput) error {
	if b == nil || b.Client == nil {
		return nil
	}
	state, _ := b.loadState(ctx, in.PR)
	if state.MainTS == "" {
		// First message on apply trigger path.
		return b.sendOrUpdate(ctx, in, state, eventApplying, colorApplying)
	}
	return b.sendOrUpdate(ctx, in, state, eventApplying, colorApplying)
}

// NotifyApplied updates the message with final apply result. Never creates on error.
func (b *SlackBackend) NotifyApplied(ctx context.Context, in NotifyInput, blocked bool) error {
	if b == nil || b.Client == nil {
		return nil
	}
	state, _ := b.loadState(ctx, in.PR)
	hasErrors := anyErrors(in.Stacks)

	// Error: only update if message exists.
	if hasErrors && state.MainTS == "" {
		return nil
	}

	ev := eventApplied
	color := colorSuccess
	switch {
	case hasErrors:
		ev = eventFailed
		color = colorFailed
	case blocked:
		ev = eventBlocked
		color = colorBlocked
	}
	return b.sendOrUpdate(ctx, in, state, ev, color)
}

// NotifyInput bundles everything the backend needs for any notification call.
type NotifyInput struct {
	PR                int
	CommitSHA         string
	RunURL            string
	PRTitle           string
	PRAuthor          string
	RepoFull          string // "owner/repo" from GITHUB_REPOSITORY
	RequiredApprovers []string
	Trigger           schemas.SlackTrigger
	Stacks            []summary.StackSummary
}

func (b *SlackBackend) sendOrUpdate(ctx context.Context, in NotifyInput, state *State, ev event, color string) error {
	blocks := b.buildMainBlocks(in, ev)
	text := mainFallbackText(in.RepoFull, in.PR, ev)

	var res *slack.PostResult
	var err error
	if state.MainTS == "" {
		res, err = b.Client.Post(ctx, slack.Message{
			Channel:     b.Channel,
			Text:        text,
			Attachments: []slack.Attachment{{Color: color, Blocks: blocks}},
		})
	} else {
		res, err = b.Client.Update(ctx, slack.Message{
			Channel:     b.Channel,
			TS:          state.MainTS,
			Text:        text,
			Attachments: []slack.Attachment{{Color: color, Blocks: blocks}},
		})
	}
	if err != nil {
		return err
	}
	state.Channel = b.Channel
	state.MainTS = res.TS

	// Thread: first timeline entry initialises the thread; subsequent events append.
	timelineText := timelineEntry(ev, in.CommitSHA)
	tr, terr := b.Client.PostThread(ctx, b.Channel, res.TS, timelineText, nil)
	if terr == nil && state.ThreadTS == "" {
		state.ThreadTS = tr.TS
	}

	return b.saveState(ctx, in.PR, state)
}

// buildMainBlocks produces the attachment blocks for the main message.
func (b *SlackBackend) buildMainBlocks(in NotifyInput, ev event) []slack.Block {
	engineIcon := b.icon("engine", ":building_construction:")
	runnerIcon := b.icon("runner", ":runner:")
	authorIcon := b.icon("author", ":bust_in_silhouette:")
	approverIcon := b.icon("approver", ":approved_stamp:")

	blocks := []slack.Block{
		slack.Header(fmt.Sprintf("%s IAC Changes", engineIcon)),
		slack.Divider(),
	}

	// Status + Author row
	blocks = append(blocks, slack.Fields(
		slack.MrkdwnField(fmt.Sprintf("%s *Status:* %s", statusEmoji(ev), eventTitle(ev))),
		slack.MrkdwnField(fmt.Sprintf("%s *Author:* %s", authorIcon, in.PRAuthor)),
	))

	blocks = append(blocks, slack.Divider())

	// PR link
	prLink := fmt.Sprintf("<https://github.com/%s/pull/%d|#%d", in.RepoFull, in.PR, in.PR)
	if in.PRTitle != "" {
		prLink += " - " + in.PRTitle
	}
	prLink += ">"
	blocks = append(blocks, slack.Section(fmt.Sprintf(":writing_hand: *PR:* %s", prLink)))

	// Required approvers when pending
	if (ev == eventPlanReady || ev == eventReady) && len(in.RequiredApprovers) > 0 {
		blocks = append(blocks, slack.Section(
			fmt.Sprintf("%s *Needs approval from:* %s", approverIcon, strings.Join(in.RequiredApprovers, ", ")),
		))
	}

	// Stack list (changed stacks only)
	stackLines := stackSummaryLines(in.Stacks)
	if stackLines != "" {
		blocks = append(blocks, slack.Divider())
		blocks = append(blocks, slack.Section(stackLines))
	}

	// Footer: view run button, apply hint only when approved/pending
	blocks = append(blocks, slack.Divider())
	var footerText string
	switch ev {
	case eventApproved:
		footerText = "Run `/reeve apply` in the PR to apply."
	case eventPlanReady, eventReady:
		footerText = "Waiting for approval."
	case eventApplying:
		footerText = ":hourglass_flowing_sand: Apply in progress..."
	case eventApplied:
		footerText = ":white_check_mark: Applied successfully."
	case eventFailed:
		footerText = ":x: Apply failed. Check the run for details."
	case eventBlocked:
		footerText = ":lock: Apply blocked - preconditions not met."
	}

	if in.RunURL != "" {
		blocks = append(blocks, slack.SectionWithButton(
			footerText,
			fmt.Sprintf("%s View Run", runnerIcon),
			in.RunURL,
			"view_run",
		))
	} else if footerText != "" {
		blocks = append(blocks, slack.Section(footerText))
	}

	return blocks
}

func statusEmoji(ev event) string {
	switch ev {
	case eventPlanReady, eventReady:
		return ":large_orange_circle:"
	case eventApproved:
		return ":large_blue_circle:"
	case eventApplying:
		return ":hourglass_flowing_sand:"
	case eventApplied:
		return ":large_green_circle:"
	case eventFailed:
		return ":red_circle:"
	case eventBlocked:
		return ":large_yellow_circle:"
	}
	return ":white_circle:"
}

func stackSummaryLines(stacks []summary.StackSummary) string {
	var sb strings.Builder
	for _, s := range stacks {
		if s.Counts.Total() == 0 && s.Status != summary.StatusError {
			continue
		}
		fmt.Fprintf(&sb, "%s `%s` — +%d ~%d -%d ±%d\n",
			stackIcon(s.Status), s.Stack,
			s.Counts.Add, s.Counts.Change, s.Counts.Delete, s.Counts.Replace)
	}
	// Ellipsis hint if all stacks were no-ops (shouldn't normally trigger the block).
	return strings.TrimRight(sb.String(), "\n")
}

func (b *SlackBackend) icon(kind, defaultEmoji string) string {
	if b.Icons == nil {
		return defaultEmoji
	}
	switch kind {
	case "engine":
		if b.Icons.Engine != "" {
			return b.Icons.Engine
		}
	case "runner":
		if b.Icons.Runner != "" {
			return b.Icons.Runner
		}
	case "author":
		if b.Icons.Author != "" {
			return b.Icons.Author
		}
	case "approver":
		if b.Icons.Approver != "" {
			return b.Icons.Approver
		}
	}
	return defaultEmoji
}

func eventTitle(ev event) string {
	switch ev {
	case eventPlanReady:
		return "Planned - Pending Approval"
	case eventReady:
		return "Ready for Apply"
	case eventApproved:
		return "Approved - Ready to Apply"
	case eventApplying:
		return "Applying..."
	case eventApplied:
		return "Applied :white_check_mark:"
	case eventFailed:
		return "Failed :x:"
	case eventBlocked:
		return "Blocked :lock:"
	}
	return "Update"
}

func mainFallbackText(repoFull string, pr int, ev event) string {
	return fmt.Sprintf("%s %s - PR #%d", repoFull, ev, pr)
}

func timelineEntry(ev event, sha string) string {
	ts := time.Now().UTC().Format("15:04 UTC")
	commit := ""
	if sha != "" {
		commit = fmt.Sprintf(" · `%s`", shortSHA(sha))
	}
	switch ev {
	case eventPlanReady:
		return fmt.Sprintf(":clipboard: *Plan ready* · %s%s", ts, commit)
	case eventReady:
		return fmt.Sprintf(":white_check_mark: *Marked ready* · %s%s", ts, commit)
	case eventApproved:
		return fmt.Sprintf(":approved_stamp: *Approved* · %s%s", ts, commit)
	case eventApplying:
		return fmt.Sprintf(":hourglass_flowing_sand: *Applying* · %s%s", ts, commit)
	case eventApplied:
		return fmt.Sprintf(":white_check_mark: *Applied* · %s%s", ts, commit)
	case eventFailed:
		return fmt.Sprintf(":x: *Failed* · %s%s", ts, commit)
	case eventBlocked:
		return fmt.Sprintf(":lock: *Blocked* · %s%s", ts, commit)
	}
	return fmt.Sprintf("· %s%s", ts, commit)
}

func stackIcon(s summary.Status) string {
	switch s {
	case summary.StatusPlanned:
		return ":white_check_mark:"
	case summary.StatusBlocked:
		return ":lock:"
	case summary.StatusError:
		return ":x:"
	case summary.StatusNoOp:
		return ":dot:"
	}
	return ":grey_question:"
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

func anyErrors(stacks []summary.StackSummary) bool {
	for _, s := range stacks {
		if s.Status == summary.StatusError {
			return true
		}
	}
	return false
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
