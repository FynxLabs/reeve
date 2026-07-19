package run

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/notify"
)

// PulumiLogin runs `pulumi login <backendURL>` if a backend URL is configured.
func PulumiLogin(ctx context.Context, cfg *schemas.Engine) error {
	if cfg == nil || cfg.Engine.State.URL == "" {
		return nil
	}
	binary := "pulumi"
	if cfg.Engine.Binary.Path != "" {
		binary = cfg.Engine.Binary.Path
	}
	cmd := exec.CommandContext(ctx, binary, "login", cfg.Engine.State.URL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pulumi login %s: %w", cfg.Engine.State.URL, err)
	}
	return nil
}

// BuildNotifyChannels resolves the configured notification channels
// (the `channels:` list) through the notify registry. Returns nil when
// nothing is configured. Build errors are logged, not fatal -
// notifications never abort a run.
func BuildNotifyChannels(ctx context.Context, cfg *schemas.Notifications, store blob.Store) []notify.Channel {
	var cfgs []schemas.ChannelYAML
	if cfg != nil {
		cfgs = cfg.Channels
	}
	if len(cfgs) == 0 {
		return nil
	}
	channels, err := notify.Build(ctx, cfgs, notify.Deps{
		Blob:       store,
		SlackToken: os.Getenv("SLACK_BOT_TOKEN"),
		RepoFull:   os.Getenv("GITHUB_REPOSITORY"),
	})
	if err != nil {
		slog.Warn("notification channel build failed", "err", err)
	}
	return channels
}

// PRNotifyInput bundles the PR-flow event context.
type PRNotifyInput struct {
	PR                int
	CommitSHA         string
	RunURL            string
	PRTitle           string
	PRAuthor          string
	RequiredApprovers []string
	Stacks            []summary.StackSummary
}

// NotifyPREvent publishes one PR-flow event to the configured channels. The
// PR-flow producer runs last in the pipeline so upstream failures are
// captured accurately; a delivery failure is returned (joined) for the
// caller to log, never to abort on.
func NotifyPREvent(ctx context.Context, channels []notify.Channel, ev notify.Event, in PRNotifyInput) error {
	if len(channels) == 0 {
		return nil
	}
	payload := notify.Payload{
		Event: ev,
		PR: &notify.PRPayload{
			PR:                in.PR,
			CommitSHA:         in.CommitSHA,
			RunURL:            in.RunURL,
			Title:             in.PRTitle,
			Author:            in.PRAuthor,
			RepoFull:          os.Getenv("GITHUB_REPOSITORY"),
			RequiredApprovers: in.RequiredApprovers,
			Stacks:            toStackResults(in.Stacks),
		},
	}
	return errors.Join(notify.Dispatch(ctx, channels, []notify.Payload{payload})...)
}

// ApplyOutcomeEvent picks the terminal apply event the same way the legacy
// Slack backend did: errors win over blocked, blocked over applied.
func ApplyOutcomeEvent(stacks []summary.StackSummary, blocked bool) notify.Event {
	for _, s := range stacks {
		if s.Status == summary.StatusError {
			return notify.EventFailed
		}
	}
	if blocked {
		return notify.EventBlocked
	}
	return notify.EventApplied
}

func toStackResults(ss []summary.StackSummary) []notify.StackResult {
	out := make([]notify.StackResult, 0, len(ss))
	for _, s := range ss {
		out = append(out, notify.StackResult{
			Project: s.Project,
			Stack:   s.Stack,
			Env:     s.Env,
			Status:  string(s.Status),
			Add:     s.Counts.Add,
			Change:  s.Counts.Change,
			Delete:  s.Counts.Delete,
			Replace: s.Counts.Replace,
		})
	}
	return out
}
