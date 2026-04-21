package run

import (
	"time"

	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/core/approvals"
	"github.com/thefynx/reeve/internal/core/freeze"
	"github.com/thefynx/reeve/internal/core/preconditions"
)

// toPreconditionsConfig extracts the preconditions gate config from shared.yaml.
func toPreconditionsConfig(s *schemas.Shared) preconditions.Config {
	if s == nil {
		return preconditions.Config{}
	}
	out := preconditions.Config{
		FailOnForkPRs:           !s.Apply.AllowForkPRs,
		PreviewMaxCommitsBehind: s.Preconditions.PreviewMaxCommitsBehind,
	}
	if s.Preconditions.RequireUpToDate != nil {
		out.RequireUpToDate = *s.Preconditions.RequireUpToDate
	}
	if s.Preconditions.RequireChecksPassing != nil {
		out.RequireChecksPassing = *s.Preconditions.RequireChecksPassing
	}
	if d, err := time.ParseDuration(s.Preconditions.PreviewFreshness); err == nil {
		out.PreviewFreshness = d
	}
	return out
}

// ApprovalsConfigFor is the exported extractor used by `reeve rules explain`.
func ApprovalsConfigFor(s *schemas.Shared) approvals.Config { return toApprovalsConfig(s) }

// toApprovalsConfig extracts the approvals config.
func toApprovalsConfig(s *schemas.Shared) approvals.Config {
	if s == nil {
		return approvals.Config{}
	}
	cfg := approvals.Config{Default: toApprovalRule(s.Approvals.Default, nil)}
	for _, src := range s.Approvals.Sources {
		cfg.Sources = append(cfg.Sources, approvals.SourceConfig{
			Type: src.Type, Enabled: src.Enabled, Command: src.Command,
		})
	}
	for pattern, r := range s.Approvals.Stacks {
		present := map[string]bool{}
		if r.RequiredApprovals != nil {
			present["required_approvals"] = true
		}
		if r.Codeowners != nil {
			present["codeowners"] = true
		}
		if r.RequireAllGroups != nil {
			present["require_all_groups"] = true
		}
		if r.DismissOnNewCommit != nil {
			present["dismiss_on_new_commit"] = true
		}
		if r.Freshness != "" {
			present["freshness"] = true
		}
		cfg.Stacks = append(cfg.Stacks, approvals.StackRule{
			Pattern: pattern,
			Rules:   toApprovalRule(r, present),
			Present: present,
		})
	}
	return cfg
}

func toApprovalRule(r schemas.ApprovalRuleYAML, present map[string]bool) approvals.Rules {
	out := approvals.Rules{Approvers: r.Approvers}
	if r.RequiredApprovals != nil {
		out.RequiredApprovals = *r.RequiredApprovals
	}
	if r.Codeowners != nil {
		out.Codeowners = *r.Codeowners
	}
	if r.RequireAllGroups != nil {
		out.RequireAllGroups = *r.RequireAllGroups
	}
	if r.DismissOnNewCommit != nil {
		out.DismissOnNewCommit = *r.DismissOnNewCommit
	}
	if r.Freshness != "" {
		if d, err := time.ParseDuration(r.Freshness); err == nil {
			out.Freshness = d
		}
	}
	_ = present
	return out
}

// toFreezeConfig extracts freeze windows.
func toFreezeConfig(s *schemas.Shared) freeze.Config {
	if s == nil {
		return freeze.Config{}
	}
	cfg := freeze.Config{}
	for _, w := range s.FreezeWindows {
		d, _ := time.ParseDuration(w.Duration)
		cfg.Windows = append(cfg.Windows, freeze.Window{
			Name: w.Name, Cron: w.Cron, Duration: d, Stacks: w.Stacks,
		})
	}
	return cfg
}

// lockTTL pulls the configured lock TTL (default 4h).
func lockTTL(s *schemas.Shared) time.Duration {
	if s == nil || s.Locking.TTL == "" {
		return 4 * time.Hour
	}
	if d, err := time.ParseDuration(s.Locking.TTL); err == nil {
		return d
	}
	return 4 * time.Hour
}
