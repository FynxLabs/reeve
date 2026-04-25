package pulumi

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/thefynx/reeve/internal/core/discovery"
	"github.com/thefynx/reeve/internal/iac"
)

// DriftCheck runs `pulumi preview --expect-no-changes` with optional
// refresh. Returns the preview JSON parsed into counts + summary. Any
// non-zero count is interpreted as drift by the caller.
func (e *Engine) DriftCheck(ctx context.Context, stack discovery.Stack, opts iac.PreviewOpts, refreshFirst bool) (iac.PreviewResult, error) {
	cwd := opts.Cwd
	if cwd == "" {
		cwd = stack.Path
	}

	if refreshFirst {
		timeout := time.Duration(opts.TimeoutSec) * time.Second
		if timeout == 0 {
			timeout = 10 * time.Minute
		}
		refCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		refresh := exec.CommandContext(refCtx, e.Binary, "refresh", "--stack", stack.Name, "--yes", "--non-interactive")
		refresh.Dir = cwd
		if len(opts.Env) > 0 {
			refresh.Env = append(os.Environ(), flattenEnv(opts.Env)...)
		}
		var rstderr bytes.Buffer
		refresh.Stderr = &rstderr
		// Ignore refresh errors - treat as run-level failure in the caller
		// by returning the error. Successful refresh is silent.
		if err := refresh.Run(); err != nil {
			return iac.PreviewResult{
				Error:    rstderr.String(),
				FullPlan: rstderr.String(),
			}, nil
		}
	}

	// `preview --expect-no-changes` exits non-zero if drift exists - we
	// parse counts and let the caller classify.
	args := []string{"preview", "--stack", stack.Name, "--json", "--non-interactive", "--expect-no-changes"}
	args = append(args, opts.ExtraArgs...)

	timeout := time.Duration(opts.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, e.Binary, args...)
	cmd.Dir = cwd
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), flattenEnv(opts.Env)...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	_ = cmd.Run() // non-zero is OK - indicates drift
	out := stdout.Bytes()

	if len(bytes.TrimSpace(out)) > 0 && out[0] == '{' {
		counts, short, diagErr, parseErr := parsePreview(out)
		if parseErr == nil {
			return iac.PreviewResult{
				Counts:      counts,
				PlanSummary: short,
				FullPlan:    stderr.String() + string(out),
				Error:       diagErr,
			}, nil
		}
	}
	return iac.PreviewResult{
		Error:    stderr.String(),
		FullPlan: stderr.String() + string(out),
	}, nil
}
