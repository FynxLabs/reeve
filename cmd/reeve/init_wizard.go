//go:build !reeve_minimal

// The interactive `reeve init` wizard. huh (and its bubbletea/lipgloss
// dependency tree) is heavy, so this file is excluded from minimal builds
// via the reeve_minimal build tag - see init_wizard_minimal.go for the stub
// (modularity contract, openspec/specs/architecture: heavy dependencies are
// build-tag gated).

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"github.com/thefynx/reeve/internal/config/scaffold"
	"github.com/thefynx/reeve/internal/core/discovery"
)

// runInitWizard walks the optional gates and returns the scaffold options.
// decls is the clustered result of the stack scan (may be empty).
func runInitWizard(decls []discovery.Declaration) (scaffold.Options, error) {
	opts := scaffold.Options{EngineType: "pulumi"}

	// Engine + stack pre-fill.
	useStacks := len(decls) > 0
	engineGroup := []huh.Field{
		huh.NewSelect[string]().
			Title("IaC engine").
			Description("pulumi is the only engine reeve can run today.").
			Options(
				huh.NewOption("pulumi", "pulumi"),
				huh.NewOption("terraform (coming soon)", "terraform"),
			).
			Validate(func(v string) error {
				if v != "pulumi" {
					return errors.New("terraform support is coming soon - pick pulumi")
				}
				return nil
			}).
			Value(&opts.EngineType),
	}
	if len(decls) > 0 {
		engineGroup = append(engineGroup, huh.NewConfirm().
			Title(fmt.Sprintf("Pre-fill engine config with the %d discovered stack entr%s shown above?", len(decls), plural(len(decls), "y", "ies"))).
			Description("You can regenerate later with: reeve stacks discover --write").
			Value(&useStacks))
	}
	if err := huh.NewForm(huh.NewGroup(engineGroup...)).Run(); err != nil {
		return opts, err
	}
	if useStacks {
		opts.Stacks = decls
	}

	// Approvals.
	requiredStr := "1"
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Approvals: who may approve an apply?").
			Options(
				huh.NewOption("CODEOWNERS (approvers derive from CODEOWNERS entries)", scaffold.ApprovalCodeowners),
				huh.NewOption("Explicit approver list (teams/users)", scaffold.ApprovalApprovers),
				huh.NewOption("Skip - any PR review counts (add approvers later)", scaffold.ApprovalBaseline),
			).
			Value(&opts.ApprovalMode),
	)).Run(); err != nil {
		return opts, err
	}
	approvalFields := []huh.Field{}
	var approversStr string
	if opts.ApprovalMode == scaffold.ApprovalApprovers {
		approvalFields = append(approvalFields, huh.NewInput().
			Title("Approvers (comma-separated, e.g. @your-org/sre, @alice)").
			Validate(func(s string) error {
				if len(splitTrim(s)) == 0 {
					return errors.New("list at least one approver")
				}
				return nil
			}).
			Value(&approversStr))
	}
	if opts.ApprovalMode != scaffold.ApprovalBaseline {
		approvalFields = append(approvalFields, huh.NewInput().
			Title("Required approvals").
			Placeholder("1").
			Validate(validateCount).
			Value(&requiredStr))
	}
	if len(approvalFields) > 0 {
		if err := huh.NewForm(huh.NewGroup(approvalFields...)).Run(); err != nil {
			return opts, err
		}
	}
	opts.Approvers = splitTrim(approversStr)
	if n, err := strconv.Atoi(strings.TrimSpace(requiredStr)); err == nil {
		opts.RequiredApprovals = n
	}

	// Freeze windows, notifications, freshness.
	var slackEnabled bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Freeze windows: write a commented example window?").
			Description("Disabled until you uncomment it - e.g. freeze prod over the weekend.").
			Value(&opts.FreezeWindowExample),
		huh.NewConfirm().
			Title("Notifications: send run updates to Slack?").
			Description("Writes notifications.yaml with a v2 sinks: entry.").
			Value(&slackEnabled),
	)).Run(); err != nil {
		return opts, err
	}
	if slackEnabled {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Slack channel").
				Placeholder("#infra-deploys").
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("channel is required (or go back and skip Slack)")
					}
					return nil
				}).
				Value(&opts.SlackChannel),
		)).Run(); err != nil {
			return opts, err
		}
		opts.SlackChannel = strings.TrimSpace(opts.SlackChannel)
		if !strings.HasPrefix(opts.SlackChannel, "#") {
			opts.SlackChannel = "#" + opts.SlackChannel
		}
	}

	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Approval freshness window (empty to skip)").
			Description("Approvals older than this go stale, e.g. 24h. Leave empty for no expiry.").
			Placeholder("24h").
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return nil
				}
				if _, err := time.ParseDuration(s); err != nil {
					return errors.New("not a Go duration (e.g. 24h, 90m)")
				}
				return nil
			}).
			Value(&opts.Freshness),
	)).Run(); err != nil {
		return opts, err
	}
	opts.Freshness = strings.TrimSpace(opts.Freshness)

	return opts, nil
}

func validateCount(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return errors.New("enter a whole number >= 1")
	}
	return nil
}

func splitTrim(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
