package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/iac/pulumi"
)

func newStacksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stacks",
		Short: "List declared and matched stacks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, _ := os.Getwd()
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			// Phase 1: only pulumi engine.
			var engineCfg = cfg.Engines[0]
			e := pulumi.New(engineCfg.Engine.Binary.Path)
			enum, err := e.EnumerateStacks(context.Background(), root)
			if err != nil {
				return err
			}
			decls := make([]discovery.Declaration, 0, len(engineCfg.Engine.Stacks))
			for _, s := range engineCfg.Engine.Stacks {
				decls = append(decls, discovery.Declaration{
					Project: s.Project, Path: s.Path, Pattern: s.Pattern, Stacks: s.Stacks,
				})
			}
			var filter discovery.Filter
			for _, ex := range engineCfg.Engine.Filters.Exclude {
				if ex.Stack != "" {
					filter.StackPatterns = append(filter.StackPatterns, ex.Stack)
				}
				if ex.Pattern != "" {
					filter.PathPatterns = append(filter.PathPatterns, ex.Pattern)
				}
			}
			stacks := discovery.Resolve(enum, decls, filter)
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%d stack(s):\n", len(stacks))
			for _, s := range stacks {
				fmt.Fprintf(w, "  %s  (path=%s env=%s)\n", s.Ref(), s.Path, s.Env)
			}
			return nil
		},
	}
	discover := &cobra.Command{
		Use:   "discover",
		Short: "Walk the repo and suggest pattern entries for engine config",
		RunE:  stacksDiscover,
	}
	discover.Flags().String("engine", "pulumi", "Engine (pulumi | terraform | opentofu)")
	discover.Flags().Bool("write", false, "Rewrite the engine config's stacks: block (keeps a *.bak)")
	discover.Flags().Bool("diff", false, "Print the unified diff that --write would produce")
	cmd.AddCommand(discover)
	return cmd
}

func stacksDiscover(cmd *cobra.Command, _ []string) error {
	engineType := flagStringOrDefault(cmd, "engine", "pulumi")
	write := flagBool(cmd, "write")
	diff := flagBool(cmd, "diff")

	root, _ := os.Getwd()
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	// Enumerate using the requested engine. Phase 1: pulumi only.
	if engineType != "pulumi" {
		return fmt.Errorf("stacks discover: engine %q not supported yet (pulumi only in v1)", engineType)
	}
	bin := "pulumi"
	var enginePath string
	for _, e := range cfg.Engines {
		if e.Engine.Type == engineType {
			if e.Engine.Binary.Path != "" {
				bin = e.Engine.Binary.Path
			}
			// Locate the engine config file path - it's whichever .reeve/*.yaml
			// carries this config_type + engine.type. Simpler: fixed by convention.
			enginePath = filepath.Join(root, ".reeve", engineType+".yaml")
		}
	}
	e := pulumi.New(bin)
	enum, err := e.EnumerateStacks(context.Background(), root)
	if err != nil {
		return err
	}
	decls := discovery.Cluster(enum)

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Discovered %d stacks → %d suggested entries:\n\n", len(enum), len(decls))
	for _, d := range decls {
		if d.Pattern != "" {
			fmt.Fprintf(w, "  pattern=%q stacks=%v\n", d.Pattern, d.Stacks)
		} else {
			fmt.Fprintf(w, "  project=%s path=%s stacks=%v\n", d.Project, d.Path, d.Stacks)
		}
	}

	if !write && !diff {
		fmt.Fprintln(w, "\n(re-run with --diff to preview, --write to apply)")
		return nil
	}
	if _, err := os.Stat(enginePath); err != nil {
		return fmt.Errorf("engine config %s not found (looked for .reeve/%s.yaml)", enginePath, engineType)
	}

	if diff && !write {
		out, err := config.WriteClusteredStacks(enginePath, decls, true)
		if err != nil {
			return err
		}
		d, err := config.DryRunDiff(enginePath, out)
		if err != nil {
			return err
		}
		if d == "" {
			fmt.Fprintln(w, "no changes")
		} else {
			fmt.Fprintln(w, d)
		}
		return nil
	}

	out, err := config.WriteClusteredStacks(enginePath, decls, false)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nwrote %s (%d bytes); original saved to %s.bak\n", enginePath, len(out), enginePath)
	return nil
}
