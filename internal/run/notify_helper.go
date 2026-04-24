package run

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/notifications"
	"github.com/thefynx/reeve/internal/slack"
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

// BuildSlackBackend constructs a SlackBackend if configured. Returns nil
// when disabled or missing token. Tokens of the form ${env:NAME} expand
// from the environment; bare literals pass through untouched.
func BuildSlackBackend(cfg *schemas.Notifications, store blob.Store) *notifications.SlackBackend {
	if cfg == nil || cfg.Slack == nil || !cfg.Slack.Enabled {
		return nil
	}
	token := expandEnvRef(cfg.Slack.AuthToken)
	if token == "" {
		return nil
	}
	return &notifications.SlackBackend{
		Client:    slack.New(token),
		Channel:   cfg.Slack.Channel,
		Icons:     cfg.Slack.Icons,
		BlobStore: store,
	}
}

// FilterStacksForSlack applies the rule list (environments + patterns) to
// the summary list. Empty rules = notify all.
func FilterStacksForSlack(cfg *schemas.Notifications, ss []summary.StackSummary) []summary.StackSummary {
	if cfg == nil || cfg.Slack == nil || len(cfg.Slack.Rules) == 0 {
		return ss
	}
	out := ss[:0]
	for _, s := range ss {
		if stackMatchesAnyRule(cfg.Slack.Rules, s) {
			out = append(out, s)
		}
	}
	return out
}

func stackMatchesAnyRule(rules []schemas.SlackNotifyRule, s summary.StackSummary) bool {
	for _, r := range rules {
		if len(r.Environments) > 0 {
			for _, e := range r.Environments {
				if e == s.Env {
					return true
				}
			}
		}
		for _, pat := range r.Stacks {
			if ok, _ := doublestar.Match(pat, s.Project+"/"+s.Stack); ok {
				return true
			}
		}
	}
	return false
}

// expandEnvRef unwraps "${env:NAME}" and returns os.Getenv(NAME);
// otherwise returns s unchanged.
func expandEnvRef(s string) string {
	if strings.HasPrefix(s, "${env:") && strings.HasSuffix(s, "}") {
		return os.Getenv(strings.TrimSuffix(strings.TrimPrefix(s, "${env:"), "}"))
	}
	return s
}

func slackTrigger(cfg *schemas.Notifications) schemas.SlackTrigger {
	if cfg == nil || cfg.Slack == nil || cfg.Slack.Trigger == "" {
		return schemas.SlackTriggerApply
	}
	return cfg.Slack.Trigger
}

// NotifySlackPlanReady is called at the end of a preview run.
// Creates a Slack message only when trigger == "plan".
func NotifySlackPlanReady(ctx context.Context, backend *notifications.SlackBackend, cfg *schemas.Notifications, pr int, sha, runURL, prTitle, prAuthor string, requiredApprovers []string, stacks []summary.StackSummary) error {
	if backend == nil {
		return nil
	}
	return backend.NotifyPlanReady(ctx, notifications.NotifyInput{
		PR:                pr,
		CommitSHA:         sha,
		RunURL:            runURL,
		PRTitle:           prTitle,
		PRAuthor:          prAuthor,
		RepoFull:          os.Getenv("GITHUB_REPOSITORY"),
		RequiredApprovers: requiredApprovers,
		Trigger:           slackTrigger(cfg),
		Stacks:            FilterStacksForSlack(cfg, stacks),
	})
}

// NotifySlackReady is called when /reeve ready is run.
func NotifySlackReady(ctx context.Context, backend *notifications.SlackBackend, cfg *schemas.Notifications, pr int, sha, runURL, prTitle, prAuthor string, requiredApprovers []string, stacks []summary.StackSummary) error {
	if backend == nil {
		return nil
	}
	return backend.NotifyReady(ctx, notifications.NotifyInput{
		PR:                pr,
		CommitSHA:         sha,
		RunURL:            runURL,
		PRTitle:           prTitle,
		PRAuthor:          prAuthor,
		RepoFull:          os.Getenv("GITHUB_REPOSITORY"),
		RequiredApprovers: requiredApprovers,
		Trigger:           slackTrigger(cfg),
		Stacks:            FilterStacksForSlack(cfg, stacks),
	})
}

// NotifySlackApproved is called after preconditions pass, before apply starts.
func NotifySlackApproved(ctx context.Context, backend *notifications.SlackBackend, cfg *schemas.Notifications, pr int, sha, runURL, prTitle, prAuthor string, stacks []summary.StackSummary) error {
	if backend == nil {
		return nil
	}
	return backend.NotifyApproved(ctx, notifications.NotifyInput{
		PR:        pr,
		CommitSHA: sha,
		RunURL:    runURL,
		PRTitle:   prTitle,
		PRAuthor:  prAuthor,
		RepoFull:  os.Getenv("GITHUB_REPOSITORY"),
		Trigger:   slackTrigger(cfg),
		Stacks:    FilterStacksForSlack(cfg, stacks),
	})
}

// NotifySlackApplying is called immediately before the apply loop starts.
func NotifySlackApplying(ctx context.Context, backend *notifications.SlackBackend, cfg *schemas.Notifications, pr int, sha, runURL, prTitle, prAuthor string, stacks []summary.StackSummary) error {
	if backend == nil {
		return nil
	}
	return backend.NotifyApplying(ctx, notifications.NotifyInput{
		PR:        pr,
		CommitSHA: sha,
		RunURL:    runURL,
		PRTitle:   prTitle,
		PRAuthor:  prAuthor,
		RepoFull:  os.Getenv("GITHUB_REPOSITORY"),
		Trigger:   slackTrigger(cfg),
		Stacks:    FilterStacksForSlack(cfg, stacks),
	})
}

// NotifySlackApplied is called after the apply loop completes.
func NotifySlackApplied(ctx context.Context, backend *notifications.SlackBackend, cfg *schemas.Notifications, pr int, sha, runURL, prTitle, prAuthor string, stacks []summary.StackSummary, blocked bool) error {
	if backend == nil {
		return nil
	}
	return backend.NotifyApplied(ctx, notifications.NotifyInput{
		PR:        pr,
		CommitSHA: sha,
		RunURL:    runURL,
		PRTitle:   prTitle,
		PRAuthor:  prAuthor,
		RepoFull:  os.Getenv("GITHUB_REPOSITORY"),
		Trigger:   slackTrigger(cfg),
		Stacks:    FilterStacksForSlack(cfg, stacks),
	}, blocked)
}
