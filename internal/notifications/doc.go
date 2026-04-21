// Package notifications is PR-scoped human-readable status. Slack first,
// Mattermost/Teams/webhook later. Owns PR-flow templates; reuses
// internal/slack client. Runs last in the pipeline so it captures
// upstream failures accurately (PLAN.md §5.6).
package notifications
