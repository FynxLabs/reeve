package render

import "strings"

const HelpMarker = "<!-- reeve:help -->"

func BuildHelpComment(autoReady bool) string {
	var b strings.Builder
	b.WriteString(HelpMarker + "\n")
	b.WriteString("## reeve commands\n\n")
	b.WriteString("| Command | Description |\n")
	b.WriteString("|---|---|\n")
	b.WriteString("| `/reeve apply` | Apply all planned stacks for this PR |\n")
	b.WriteString("| `/reeve ready` | Mark PR as ready for approval, notify Slack |\n")
	b.WriteString("| `/reeve help` | Show this help message |\n")
	b.WriteString("\n")
	if autoReady {
		b.WriteString("> `auto_ready` is enabled - when this PR is converted from draft to ready for review and a plan has succeeded, reeve will automatically notify for approval.\n\n")
	}
	b.WriteString("_reeve runs automatically on PR open/push (preview) and on comment commands above._\n")
	return b.String()
}
