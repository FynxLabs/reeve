// Package schemas holds Go structs for each config_type. See
// openspec/specs/config for rules. Phase 1 implements shared + engine
// (pulumi); other config_types scaffold with version+config_type only.
package schemas

// Header is common to every config file.
type Header struct {
	Version    int    `yaml:"version"`
	ConfigType string `yaml:"config_type"`
}

// Shared is .reeve/shared.yaml. Extended in Phase 2 with locking,
// approvals, preconditions, freeze_windows, apply_command.
type Shared struct {
	Header        `yaml:",inline"`
	Bucket        BucketConfig       `yaml:"bucket"`
	Comments      CommentsConfig     `yaml:"comments"`
	Locking       LockingConfig      `yaml:"locking"`
	Approvals     ApprovalsYAML      `yaml:"approvals"`
	Preconditions PreconditionsYAML  `yaml:"preconditions"`
	FreezeWindows []FreezeWindowYAML `yaml:"freeze_windows"`
	Apply         ApplyConfig        `yaml:"apply"`
}

type LockingConfig struct {
	TTL            string        `yaml:"ttl"`             // e.g. "4h"
	Queue          string        `yaml:"queue"`           // fifo (v1)
	ReaperInterval string        `yaml:"reaper_interval"` // unused v1 (opportunistic)
	AdminOverride  AdminOverride `yaml:"admin_override"`
}

type AdminOverride struct {
	Allowed        []string `yaml:"allowed"`
	RequiresReason bool     `yaml:"requires_reason"`
}

type ApprovalsYAML struct {
	Sources []ApprovalSource            `yaml:"sources"`
	Default ApprovalRuleYAML            `yaml:"default"`
	Stacks  map[string]ApprovalRuleYAML `yaml:"stacks"`
}

type ApprovalSource struct {
	Type    string `yaml:"type"` // pr_review | pr_comment
	Enabled bool   `yaml:"enabled"`
	Command string `yaml:"command"` // for pr_comment
}

type ApprovalRuleYAML struct {
	RequiredApprovals  *int     `yaml:"required_approvals,omitempty"`
	Approvers          []string `yaml:"approvers"`
	Codeowners         *bool    `yaml:"codeowners,omitempty"`
	RequireAllGroups   *bool    `yaml:"require_all_groups,omitempty"`
	DismissOnNewCommit *bool    `yaml:"dismiss_on_new_commit,omitempty"`
	Freshness          string   `yaml:"freshness,omitempty"` // e.g. "24h"
}

type PreconditionsYAML struct {
	RequireUpToDate         *bool  `yaml:"require_up_to_date,omitempty"`
	RequireChecksPassing    *bool  `yaml:"require_checks_passing,omitempty"`
	PreviewFreshness        string `yaml:"preview_freshness,omitempty"` // e.g. "2h"
	PreviewMaxCommitsBehind int    `yaml:"preview_max_commits_behind"`
}

type FreezeWindowYAML struct {
	Name     string   `yaml:"name"`
	Cron     string   `yaml:"cron"`
	Duration string   `yaml:"duration"` // e.g. "65h"
	Stacks   []string `yaml:"stacks"`
}

type ApplyConfig struct {
	// Trigger: "comment" (default; requires /reeve apply) | "merge".
	Trigger string `yaml:"trigger"`
	// Command: the magic phrase in a PR comment that triggers apply.
	// Defaults to "/reeve apply".
	Command string `yaml:"command"`
	// AllowForkPRs: if true, apply runs on fork PRs with full creds.
	// Default false. Surfaces via preconditions GateFork.
	AllowForkPRs bool `yaml:"allow_fork_prs"`
	// AutoReady: if true, reeve automatically marks the PR ready (posts
	// comment + Slack notification) after a fully successful plan, and
	// also when the PR transitions from draft to ready_for_review.
	AutoReady bool `yaml:"auto_ready"`
}

type BucketConfig struct {
	Type   string `yaml:"type"`   // filesystem | s3 | gcs | azblob | r2
	Name   string `yaml:"name"`   // bucket name, or directory for filesystem
	Region string `yaml:"region"` // optional
	Prefix string `yaml:"prefix"` // optional sub-prefix
}

type CommentsConfig struct {
	Sort              string `yaml:"sort"`               // status_grouped | alphabetical | env_priority
	CollapseThreshold int    `yaml:"collapse_threshold"` // collapse no-op stacks above N
	ShowGates         bool   `yaml:"show_gates"`
}

// Engine is .reeve/<engine>.yaml. Phase 1 supports Pulumi stack declarations
// and basic filters. Full surface (modules, stack_dependencies,
// policy_hooks, stack_overrides) lands in later phases.
type Engine struct {
	Header `yaml:",inline"`
	Engine EngineBody `yaml:"engine"`
}

type EngineBody struct {
	Type          string           `yaml:"type"` // pulumi | terraform | opentofu
	Binary        EngineBinary     `yaml:"binary"`
	State         EngineState      `yaml:"state,omitempty"`
	Stacks        []StackDecl      `yaml:"stacks"`
	Filters       EngineFilters    `yaml:"filters"`
	ChangeMapping ChangeMap        `yaml:"change_mapping"`
	Execution     Execution        `yaml:"execution"`
	PolicyHooks   []PolicyHookYAML `yaml:"policy_hooks,omitempty"`
}

// EngineState describes the engine's own state backend. reeve does not
// manage state - it configures the engine (e.g. `pulumi login`) before
// each run.
type EngineState struct {
	Backend         string                 `yaml:"backend"` // s3 | gcs | azblob | pulumi_cloud | file
	URL             string                 `yaml:"url,omitempty"`
	AuthProvider    string                 `yaml:"auth_provider,omitempty"` // refers to auth.yaml
	SecretsProvider EngineSecretsProvider  `yaml:"secrets_provider,omitempty"`
	StackOverrides  map[string]EngineState `yaml:"stack_overrides,omitempty"`
}

// EngineSecretsProvider is the engine's secret-encryption backend (Pulumi's
// awskms / gcpkms / passphrase / etc.). Separate from auth.yaml runtime
// creds - this encrypts Pulumi stack state at rest.
type EngineSecretsProvider struct {
	Type       string `yaml:"type"` // awskms | gcpkms | azurekeyvault | hashivault | passphrase
	Key        string `yaml:"key,omitempty"`
	Passphrase string `yaml:"passphrase,omitempty"`
}

// PolicyHookYAML is one entry in engine.policy_hooks.
type PolicyHookYAML struct {
	Name     string   `yaml:"name"`
	Command  []string `yaml:"command"`
	OnFail   string   `yaml:"on_fail"` // block | warn (default block)
	Required bool     `yaml:"required"`
}

type EngineBinary struct {
	Path    string `yaml:"path"`
	Version string `yaml:"version"` // optional pin
}

// StackDecl is either a literal project entry or a pattern.
type StackDecl struct {
	Project string   `yaml:"project"` // literal
	Path    string   `yaml:"path"`    // literal
	Pattern string   `yaml:"pattern"` // glob (re: prefix not supported in v1)
	Stacks  []string `yaml:"stacks"`  // which stack names apply
}

type EngineFilters struct {
	Exclude []ExcludeRule `yaml:"exclude"`
}

// ExcludeRule accepts either a plain string pattern or a {stack: pattern}
// form for stack-level filtering.
type ExcludeRule struct {
	Pattern string `yaml:"-"`
	Stack   string `yaml:"stack"`
}

type ChangeMap struct {
	IgnoreChanges []string       `yaml:"ignore_changes"`
	ExtraTriggers []ExtraTrigger `yaml:"extra_triggers"`
}

type ExtraTrigger struct {
	Project string   `yaml:"project"`
	Paths   []string `yaml:"paths"`
}

type Execution struct {
	MaxParallelStacks int    `yaml:"max_parallel_stacks"`
	PreviewTimeout    string `yaml:"preview_timeout"` // e.g. "10m"
	ApplyTimeout      string `yaml:"apply_timeout"`
}
