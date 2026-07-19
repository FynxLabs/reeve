package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/config/scaffold"
	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/iac/pulumi"
)

// stdinIsTTY reports whether stdin is an interactive terminal. Package var so
// tests can inject either answer without a real TTY.
var stdinIsTTY = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold .reeve/ configuration for this repository",
		Long: `Scaffold a .reeve/ configuration directory with sane defaults.

reeve init inspects the repository, discovers Pulumi stacks (the same scan
as ` + "`reeve stacks discover`" + `), and writes strict-loader-clean YAML:

  .reeve/shared.yaml         bucket, approvals, preconditions, apply gates
  .reeve/pulumi.yaml         engine config + discovered stack declarations
  .reeve/notifications.yaml  only when a Slack channel is configured

Modes:

  interactive (default on a terminal): a short wizard walks through the
  optional gates - approvals (CODEOWNERS-based or an explicit approver list
  plus required count), a commented freeze-window example, a Slack
  notification channel, and an approval-freshness window. Gates you skip are
  written as commented best-practice examples, off by default.

  --non-interactive / -n (auto-selected when stdin is not a terminal, and
  the only mode in minimal builds): zero prompts. The engine is detected
  from repo files (Pulumi.yaml -> pulumi), stacks are scanned and
  pre-filled, and a safe baseline is written with every optional gate off.

Idempotency: an existing .reeve/ is never clobbered - init fills in only
the missing config types and leaves existing files untouched. Use --force
to regenerate everything (originals are kept as *.bak).

After init: review the files, run ` + "`reeve lint`" + `, commit .reeve/, and add
the GitHub Actions workflow printed at the end of the run.`,
		Args: cobra.NoArgs,
		RunE: runInit,
	}
	cmd.Flags().BoolP("non-interactive", "n", false,
		"No prompts: detect the engine, scan stacks, write safe baseline defaults")
	cmd.Flags().Bool("force", false,
		"Overwrite existing .reeve/ config files (originals are kept as *.bak)")
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	root, _ := os.Getwd()
	w := cmd.OutOrStdout()
	force := flagBool(cmd, "force")
	nonInteractive := flagBool(cmd, "non-interactive")
	interactive := !nonInteractive && stdinIsTTY()

	dir := filepath.Join(root, ".reeve")
	existing, err := scaffold.ExistingTypes(dir)
	if err != nil {
		return err
	}

	// Stack scan: filesystem walk for Pulumi.yaml projects (no pulumi binary
	// needed), clustered into suggested declarations - the same path as
	// `reeve stacks discover --write`.
	enum, scanErr := pulumi.New("").EnumerateStacks(context.Background(), root)
	if scanErr != nil {
		fmt.Fprintf(w, "warning: stack scan failed (%v); continuing with an empty stacks: block\n", scanErr)
	}
	decls := discovery.Cluster(enum)
	printDiscoveredStacks(w, enum, decls)

	var opts scaffold.Options
	if interactive {
		if len(existing) > 0 && !force {
			fmt.Fprintf(w, "Existing .reeve/ config found (%s): only missing config types will be written; use --force to regenerate.\n\n",
				strings.Join(existingSummary(existing), ", "))
		}
		opts, err = runInitWizard(decls)
		if err != nil {
			return err
		}
	} else {
		opts = scaffold.Options{EngineType: detectEngine(w, root, enum), Stacks: decls}
	}

	files, err := scaffold.Render(opts)
	if err != nil {
		return err
	}

	written, skipped, err := writeScaffold(dir, files, existing, force)
	if err != nil {
		return err
	}

	for _, s := range skipped {
		fmt.Fprintf(w, "kept    %s\n", s)
	}
	for _, name := range written {
		fmt.Fprintf(w, "wrote   %s\n", filepath.Join(".reeve", name))
	}
	if len(written) == 0 {
		fmt.Fprintln(w, "\nNothing to do - every config type already exists. Use --force to regenerate.")
		return nil
	}

	// Sanity: everything on disk (ours + pre-existing) must pass the strict
	// loader. A failure here can only come from pre-existing files.
	if cfg, err := config.Load(root); err != nil {
		fmt.Fprintf(w, "\nwarning: .reeve/ does not pass the strict loader: %v\n", err)
	} else if err := cfg.Validate(); err != nil {
		fmt.Fprintf(w, "\nwarning: .reeve/ does not validate: %v\n", err)
	}

	printNextSteps(w)
	return nil
}

// detectEngine picks the engine for non-interactive mode from repo files.
// Pulumi.yaml projects -> pulumi. Terraform files draw a heads-up (the
// engine is not implemented yet) but still scaffold pulumi config, the only
// engine reeve can run today.
func detectEngine(w io.Writer, root string, enum []discovery.Stack) string {
	if len(enum) > 0 {
		return "pulumi"
	}
	if hasTerraformFiles(root) {
		fmt.Fprintln(w, "note: Terraform files detected, but terraform support is coming soon - scaffolding pulumi engine config (the only engine reeve can run today).")
	} else {
		fmt.Fprintln(w, "note: no Pulumi projects found - writing an empty stacks: block; re-run `reeve stacks discover --write` once projects exist.")
	}
	return "pulumi"
}

// hasTerraformFiles reports whether any *.tf file exists under root,
// skipping the usual noise directories.
func hasTerraformFiles(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort detection only
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "venv", ".venv", ".terraform":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".tf") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// writeScaffold writes the rendered files into dir, skipping any whose
// config_type already exists (unless force). With force, overwritten files
// are first copied to *.bak.
func writeScaffold(dir string, files []scaffold.File, existing map[string]string, force bool) (written, skipped []string, err error) {
	for _, f := range files {
		if prev, ok := existing[f.ConfigType]; ok && !force {
			skipped = append(skipped, fmt.Sprintf("%s (config_type %s already declared there)", filepath.Join(".reeve", prev), f.ConfigType))
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, nil, err
		}
		path := filepath.Join(dir, f.Name)
		// With --force, an existing file of the same config_type may live
		// under a different name; back up and replace the same-named file,
		// which is the conventional location.
		if old, readErr := os.ReadFile(path); readErr == nil {
			if err := os.WriteFile(path+".bak", old, 0o600); err != nil {
				return nil, nil, fmt.Errorf("backup %s: %w", path, err)
			}
		}
		if err := os.WriteFile(path, f.Content, 0o600); err != nil {
			return nil, nil, err
		}
		written = append(written, f.Name)
	}
	return written, skipped, nil
}

func existingSummary(existing map[string]string) []string {
	out := make([]string, 0, len(existing))
	for t, f := range existing {
		out = append(out, fmt.Sprintf("%s in %s", t, f))
	}
	// Deterministic order for output and tests.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func printDiscoveredStacks(w io.Writer, enum []discovery.Stack, decls []discovery.Declaration) {
	if len(enum) == 0 {
		return
	}
	fmt.Fprintf(w, "Discovered %d Pulumi stack(s) -> %d stack config entr%s:\n", len(enum), len(decls), plural(len(decls), "y", "ies"))
	for _, d := range decls {
		if d.Pattern != "" {
			fmt.Fprintf(w, "  pattern=%q stacks=%v\n", d.Pattern, d.Stacks)
		} else {
			fmt.Fprintf(w, "  project=%s path=%s stacks=%v\n", d.Project, d.Path, d.Stacks)
		}
	}
	fmt.Fprintln(w)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func printNextSteps(w io.Writer) {
	fmt.Fprint(w, `
Next steps:
  1. Review the generated files under .reeve/ (settings you skipped are
     included as comments), then commit the directory.
  2. Validate:            reeve lint
  3. Inspect stacks:      reeve stacks
  4. Dry-run the comment: reeve plan-run --sha $(git rev-parse HEAD) --run-number 1
  5. Add the GitHub Actions workflow (.github/workflows/reeve.yml):

       name: reeve
       on:
         pull_request:
           types: [opened, synchronize, reopened, ready_for_review]
         issue_comment:
           types: [created]
       permissions:
         contents: read
         pull-requests: write
         issues: write
         id-token: write
       concurrency:
         group: reeve-${{ github.event.pull_request.number || github.event.issue.number }}
         cancel-in-progress: false
       jobs:
         reeve:
           runs-on: ubuntu-latest
           steps:
             - uses: actions/checkout@v4
             - uses: FynxLabs/reeve@master
               with:
                 pulumi-version: latest

See docs/getting-started.md for the full walk-through.
`)
}
