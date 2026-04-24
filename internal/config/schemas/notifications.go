package schemas

// Notifications is .reeve/notifications.yaml.
type Notifications struct {
	Header `yaml:",inline"`
	Slack  *SlackConfig `yaml:"slack,omitempty"`
}

// SlackTrigger controls when the first Slack message is created.
type SlackTrigger string

const (
	// SlackTriggerApply creates the message only when apply is invoked (default).
	SlackTriggerApply SlackTrigger = "apply"
	// SlackTriggerPlan creates the message when a plan finishes (status: pending approval).
	SlackTriggerPlan SlackTrigger = "plan"
	// SlackTriggerReady creates the message only when /reeve ready is run.
	SlackTriggerReady SlackTrigger = "ready"
)

// SlackIcons overrides the default emoji used in messages.
// All fields are optional -- unset fields use the built-in defaults.
type SlackIcons struct {
	// Engine is the icon shown next to the repo/project name.
	// Default: ":building_construction:"
	Engine string `yaml:"engine,omitempty"`
	// Runner is the icon for the CI runner / GitHub Actions.
	// Default: ":runner:"
	Runner string `yaml:"runner,omitempty"`
	// Author is the icon next to the PR author field.
	// Default: ":writing_hand:"
	Author string `yaml:"author,omitempty"`
	// Approver is the icon next to the approvers field.
	// Default: ":approved_stamp:"
	Approver string `yaml:"approver,omitempty"`
}

// SlackConfig wires the Slack notification backend.
type SlackConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Channel   string `yaml:"channel"`
	AuthToken string `yaml:"auth_token"` // "${env:SLACK_BOT_TOKEN}"
	// Trigger controls when the initial Slack message is created.
	// Subsequent events always update the existing message in place.
	// Default: "apply"
	Trigger SlackTrigger      `yaml:"trigger,omitempty"`
	Icons   *SlackIcons       `yaml:"icons,omitempty"`
	Rules   []SlackNotifyRule `yaml:"rules,omitempty"`
}

// SlackNotifyRule gates which stacks flow into Slack (e.g. environments: [prod]).
type SlackNotifyRule struct {
	Environments []string `yaml:"environments,omitempty"`
	Stacks       []string `yaml:"stacks,omitempty"` // glob patterns
}
