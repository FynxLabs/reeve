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
	b.WriteString("| `/reeve ready` | Mark PR as ready for apply, notify Slack |\n")
	b.WriteString("| `/reeve help` | Show this help message |\n")
	b.WriteString("\n")
	if autoReady {
		b.WriteString("> `auto_ready` is enabled - reeve will automatically mark this PR ready after a successful plan.\n\n")
	}
	b.WriteString("_reeve runs automatically on PR open/push (preview) and on comment commands above._\n")
	return b.String()
}
