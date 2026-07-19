// Package slack is shared Slack infrastructure: auth, message lifecycle,
// Block Kit primitives, mrkdwn escaping helpers. Consumed by the slack
// notification channel (internal/notify/channels/slack), which handles both the
// PR flow and drift events. Templates live with consumers; this package
// owns the client. PLAN.md §6.1 "Why this layout".
package slack
