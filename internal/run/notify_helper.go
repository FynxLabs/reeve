package run

import (
	"context"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/summary"
	"github.com/thefynx/reeve/internal/notifications"
	"github.com/thefynx/reeve/internal/slack"
)

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

// NotifySlackPreview is a small wrapper callers invoke at the end of a
// preview run. ctx bounded by caller; errors bubble up but should not
// fail the overall run (notifications run last by design).
func NotifySlackPreview(ctx context.Context, backend *notifications.SlackBackend, filter *schemas.Notifications, pr int, sha, runURL string, stacks []summary.StackSummary) error {
	if backend == nil {
		return nil
	}
	filtered := FilterStacksForSlack(filter, stacks)
	return backend.NotifyPreview(ctx, pr, sha, runURL, "preview", filtered)
}

// NotifySlackApply wraps the apply-phase Slack update.
func NotifySlackApply(ctx context.Context, backend *notifications.SlackBackend, filter *schemas.Notifications, pr int, sha, runURL string, stacks []summary.StackSummary, blocked bool) error {
	if backend == nil {
		return nil
	}
	filtered := FilterStacksForSlack(filter, stacks)
	return backend.NotifyApply(ctx, pr, sha, runURL, filtered, blocked)
}
