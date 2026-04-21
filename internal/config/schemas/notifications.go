package schemas

// Notifications is .reeve/notifications.yaml.
type Notifications struct {
	Header `yaml:",inline"`
	Slack  *SlackConfig `yaml:"slack,omitempty"`
}

// SlackConfig wires the Slack notification backend.
type SlackConfig struct {
	Enabled   bool              `yaml:"enabled"`
	Channel   string            `yaml:"channel"`
	AuthToken string            `yaml:"auth_token"` // "${env:SLACK_BOT_TOKEN}"
	Rules     []SlackNotifyRule `yaml:"rules,omitempty"`
}

// SlackNotifyRule gates which stacks flow into Slack (e.g. environments: [prod]).
type SlackNotifyRule struct {
	Environments []string `yaml:"environments,omitempty"`
	Stacks       []string `yaml:"stacks,omitempty"` // glob patterns
}
