// Package cli wires the reeve command tree. Each subcommand lives in its
// own file in this package; root only handles flag plumbing and global
// initialisation (logger setup happens here in PersistentPreRun).
package main

import (
	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/log"
)

const version = "0.0.0-dev"

// rootLogLevel and rootLogFormat hold the flag values from the root command
// so subcommands can re-apply logging after loading shared.yaml config.
var rootLogLevel, rootLogFormat string

// applyLogConfig re-initialises the logger using flag > env > config priority.
// Call this in each subcommand after config.Load so shared.yaml log_level
// takes effect when no flag or env override is set.
func applyLogConfig(cfgLevel, cfgFormat string) {
	log.FromConfig(rootLogLevel, rootLogFormat, cfgLevel, cfgFormat)
}

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "reeve",
		Short: "PR-native, self-hosted GitOps orchestrator for Pulumi",
		Long: `reeve is a single-binary GitOps orchestrator that runs inside your CI.
No control plane, no SaaS backend, no telemetry, no account. The user owns all state.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			log.FromEnv(rootLogLevel, rootLogFormat)
		},
	}

	root.PersistentFlags().StringVar(&rootLogLevel, "log-level", "",
		"log level: debug | info | warn | error (default info; env REEVE_LOG_LEVEL)")
	root.PersistentFlags().StringVar(&rootLogFormat, "log-format", "",
		"log format: text | json (default text; env REEVE_LOG_FORMAT)")

	root.AddCommand(
		newLintCmd(),
		newStacksCmd(),
		newRulesCmd(),
		newPlanRunCmd(),
		newRenderCmd(),
		newRunCmd(),
		newLocksCmd(),
		newDriftCmd(),
		newMigrateConfigCmd(),
	)

	return root
}
