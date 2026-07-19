// Package all compiles in the default sink set via blank imports. Commands
// import it once; a build that wants a subset imports the sink packages it
// needs instead (modularity contract: the factory itself never statically
// imports concrete sinks).
package all

import (
	_ "github.com/thefynx/reeve/internal/notify/sinks/github_issue"
	_ "github.com/thefynx/reeve/internal/notify/sinks/otel"
	_ "github.com/thefynx/reeve/internal/notify/sinks/pagerduty"
	_ "github.com/thefynx/reeve/internal/notify/sinks/slack"
	_ "github.com/thefynx/reeve/internal/notify/sinks/timeline"
	_ "github.com/thefynx/reeve/internal/notify/sinks/webhook"
)
