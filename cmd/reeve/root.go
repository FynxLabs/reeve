// Package cli wires the reeve command tree. Phase 0 ships the command surface
// with `not implemented` stubs; later phases attach real runners.
package main

import "github.com/spf13/cobra"

const version = "0.0.0-dev"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "reeve",
		Short: "PR-native, self-hosted GitOps orchestrator for Pulumi",
		Long: `reeve is a single-binary GitOps orchestrator that runs inside your CI.
No control plane, no SaaS backend, no telemetry, no account. The user owns all state.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

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
