package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/reeveops/reeve/internal/config/schemas"
	"github.com/reeveops/reeve/internal/core/render"
)

// Comment rendering flags. Every runtime behavior in reeve carries both a CLI
// flag and a config setting, so the whole comments section is flaggable rather
// than only the newest key.
//
// A flag wins over config only when it was actually typed: cobra reports a
// default as a set value, so an untouched flag would otherwise silently
// overwrite the operator's config with the flag's own default.

// addCommentFlags registers the comment rendering flags on a command that
// renders a PR board.
func addCommentFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("comment-sort", "",
		"Stack ordering in the comment table: status_grouped (default) | alphabetical | env_priority. Overrides comments.sort.")
	f.String("comment-stack-view", "",
		"Which stacks the table lists: all (default) | changed. Overrides comments.stack_view.")
	f.String("comment-style", "",
		"How boards are keyed: replace (default) | section | append. Overrides comments.style.")
	f.Bool("comment-show-gates", false,
		"Show per-stack gate results in the comment. Overrides comments.show_gates.")
	f.Int("comment-collapse-threshold", 0,
		"Collapse no-op stacks above this count. Overrides comments.collapse_threshold.")
	f.String("comment-overflow", "",
		"What happens when a board exceeds GitHub's size limit: drop (default) | continue. Overrides comments.overflow.mode.")
	f.String("comment-overflow-split", "",
		"How a continued board splits: divided (default) | stack | group. Overrides comments.overflow.split.")
	f.Int("comment-overflow-max-parts", 0,
		"Maximum comments one board may occupy. Overrides comments.overflow.max_parts.")
}

// applyCommentFlags folds the comment flags into the loaded shared config and
// validates them.
//
// It mutates shared in place because every command hands cfg.Shared straight to
// the run pipeline; returning a copy would leave callers free to pass the
// unmodified one and silently lose the flags.
func applyCommentFlags(cmd *cobra.Command, shared *schemas.Shared) error {
	if shared == nil {
		// No shared config loaded. A flag that cannot be honored must say so
		// rather than be ignored.
		for _, name := range commentFlagNames {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--%s needs a shared config to override; none was loaded", name)
			}
		}
		return nil
	}
	f := cmd.Flags()
	c := &shared.Comments

	if f.Changed("comment-sort") {
		v, _ := f.GetString("comment-sort")
		if err := oneOf("comment-sort", v, "status_grouped", "alphabetical", "env_priority"); err != nil {
			return err
		}
		c.Sort = v
	}
	if f.Changed("comment-stack-view") {
		v, _ := f.GetString("comment-stack-view")
		if err := oneOf("comment-stack-view", v, render.StackViewAll, render.StackViewChanged); err != nil {
			return err
		}
		c.StackView = v
	}
	if f.Changed("comment-style") {
		v, _ := f.GetString("comment-style")
		if err := oneOf("comment-style", v, render.StyleReplace, render.StyleSection, render.StyleAppend); err != nil {
			return err
		}
		c.Style = v
	}
	if f.Changed("comment-show-gates") {
		c.ShowGates, _ = f.GetBool("comment-show-gates")
	}
	if f.Changed("comment-collapse-threshold") {
		v, _ := f.GetInt("comment-collapse-threshold")
		if v < 0 {
			return fmt.Errorf("--comment-collapse-threshold must not be negative, got %d", v)
		}
		c.CollapseThreshold = v
	}
	if f.Changed("comment-overflow") {
		v, _ := f.GetString("comment-overflow")
		if err := oneOf("comment-overflow", v, render.OverflowDrop, render.OverflowContinue); err != nil {
			return err
		}
		c.Overflow.Mode = v
	}
	if f.Changed("comment-overflow-split") {
		v, _ := f.GetString("comment-overflow-split")
		if err := oneOf("comment-overflow-split", v, render.SplitDivided, render.SplitStack, render.SplitGroup); err != nil {
			return err
		}
		c.Overflow.Split = v
	}
	if f.Changed("comment-overflow-max-parts") {
		v, _ := f.GetInt("comment-overflow-max-parts")
		if v < 1 {
			return fmt.Errorf("--comment-overflow-max-parts must be at least 1, got %d", v)
		}
		c.Overflow.MaxParts = v
	}
	return nil
}

// commentFlagNames is every flag addCommentFlags registers, so the no-config
// path can report any of them rather than accepting them silently.
var commentFlagNames = []string{
	"comment-sort", "comment-stack-view", "comment-style", "comment-show-gates",
	"comment-collapse-threshold", "comment-overflow", "comment-overflow-split",
	"comment-overflow-max-parts",
}

// oneOf rejects a value outside the allowed set, naming what was allowed. A
// typo in a rendering flag is otherwise silently treated as the default, which
// looks like the flag being ignored.
func oneOf(flag, got string, allowed ...string) error {
	for _, a := range allowed {
		if got == a {
			return nil
		}
	}
	return fmt.Errorf("--%s: %q is not one of %v", flag, got, allowed)
}
