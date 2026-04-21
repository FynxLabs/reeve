// Package slack is shared Slack infrastructure: auth, message lifecycle,
// Block Kit primitives. Consumed by both internal/notifications (PR flow)
// and internal/drift/sinks/slack (drift events). Templates live with
// consumers; this package owns the client. PLAN.md §6.1 "Why this layout".
package slack
