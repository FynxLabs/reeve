package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	authfac "github.com/thefynx/reeve/internal/auth/factory"
	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/iac/pulumi"
)

func newLintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Static check across all .reeve/*.yaml files",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := os.Getwd()
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			// Auth lint: surfaces conflicts and dangerous providers.
			if cfg.Auth != nil {
				// Collect declared stack refs for the conflict check.
				var stacks []string
				engineCfg := cfg.Engines[0]
				engine := pulumi.New(engineCfg.Engine.Binary.Path)
				enum, _ := engine.EnumerateStacks(context.Background(), root)
				decls := make([]discovery.Declaration, 0, len(engineCfg.Engine.Stacks))
				for _, s := range engineCfg.Engine.Stacks {
					decls = append(decls, discovery.Declaration{
						Project: s.Project, Path: s.Path, Pattern: s.Pattern, Stacks: s.Stacks,
					})
				}
				resolved := discovery.Resolve(enum, decls, discovery.Filter{})
				for _, s := range resolved {
					stacks = append(stacks, s.Ref())
				}
				if err := authfac.ValidateLint(cfg.Auth, stacks); err != nil {
					return fmt.Errorf("auth lint: %w", err)
				}
			}
			authN := 0
			if cfg.Auth != nil {
				authN = len(cfg.Auth.Providers)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK - %d engine config(s) loaded, bucket=%s, auth_providers=%d\n",
				len(cfg.Engines), cfg.Shared.Bucket.Type, authN)
			return nil
		},
	}
	return cmd
}
