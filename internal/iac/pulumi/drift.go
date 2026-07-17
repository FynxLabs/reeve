package pulumi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
		// A refresh failure is a check failure, not "no drift". Return a
		// non-nil error AND a non-empty Error so the caller classifies it as
		// a failed check rather than silently treating an empty result as
		// resolved drift.
		if err := refresh.Run(); err != nil {
			msg := failureMessage(rstderr.String(), err)
			return iac.PreviewResult{
				Error:    msg,
				FullPlan: rstderr.String(),
			}, fmt.Errorf("pulumi refresh failed: %s", msg)
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

	// `preview --expect-no-changes` exits non-zero when drift exists, which
	// is expected here - but a non-zero exit ALSO covers genuine failures
	// (timeout kill, missing binary, auth error, crash). The exit code alone
	// is ambiguous, so we treat parseable JSON on stdout as the authoritative
	// signal: JSON present => the check ran and its counts are trustworthy
	// regardless of exit code; no parseable JSON => the check failed.
	runErr := cmd.Run()
	out := stdout.Bytes()

	if len(bytes.TrimSpace(out)) > 0 && out[0] == '{' {
		counts, short, diagErr, parseErr := parsePreview(out)
		if parseErr == nil {
			return iac.PreviewResult{
				Counts:      counts,
				PlanSummary: short,
				FullPlan:    stderr.String() + string(out),
				Error:       diagErr,
				DriftedURNs: driftedURNsFromJSON(out),
			}, nil
		}
	}

	// No parseable plan: the check did not produce a verdict. Fail closed
	// with a non-empty Error and a non-nil error so the caller records a
	// failed check instead of misreading an empty result as "no drift"
	// (which would falsely resolve an active drift alert).
	msg := failureMessage(stderr.String(), runErr)
	return iac.PreviewResult{
		Error:    msg,
		FullPlan: stderr.String() + string(out),
	}, fmt.Errorf("pulumi drift check produced no plan: %s", msg)
}

// failureMessage builds a non-empty error string from stderr, falling back
// to the process error (e.g. a timeout kill or missing binary leaves stderr
// empty). Never returns "".
func failureMessage(stderr string, err error) string {
	stderr = strings.TrimSpace(stderr)
	switch {
	case stderr != "" && err != nil:
		return stderr + " (" + err.Error() + ")"
	case stderr != "":
		return stderr
	case err != nil:
		return err.Error()
	default:
		return "drift check failed with no output"
	}
}

// driftedURNsFromJSON returns the URNs of resources that actually changed,
// excluding unchanged "same" (and "read") steps. Used to fingerprint the
// drifted set. Best-effort: returns nil on unparseable input.
func driftedURNsFromJSON(stdout []byte) []string {
	var p previewJSON
	if err := json.Unmarshal(stdout, &p); err != nil {
		return nil
	}
	var urns []string
	for _, s := range p.Steps {
		switch s.Op {
		case "create", "import", "update", "delete",
			"replace", "create-replacement", "delete-replaced":
			if s.URN != "" {
				urns = append(urns, s.URN)
			}
		}
	}
	return urns
}
