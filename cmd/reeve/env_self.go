package main

import (
	"os"
	"strings"
)

// selfCheckNames returns the list of GitHub check_run names that belong to
// reeve itself in the current CI environment. These are passed to
// vcs.ChecksGreenOpts.IgnoreNames so a previously failed reeve apply doesn't
// pin the gate red on the same SHA forever.
//
// In GitHub Actions the check_run name shown on a commit is one of:
//
//   - "{workflow_name}"               (single-job workflow)
//   - "{workflow_name} / {job_name}"  (multi-job workflow with explicit job
//     names)
//   - "{job_name}"                    (some matrix configurations)
//
// All three shapes are populated so the match works regardless of how the
// consumer's workflow is structured. Empty values are filtered out by the
// adapter. Operators with custom check_run names can extend the list via
// $REEVE_SELF_CHECK_NAMES (comma-separated).
func selfCheckNames() []string {
	workflow := os.Getenv("GITHUB_WORKFLOW")
	job := os.Getenv("GITHUB_JOB")

	out := []string{workflow, job}
	if workflow != "" && job != "" {
		out = append(out, workflow+" / "+job)
	}
	if extra := os.Getenv("REEVE_SELF_CHECK_NAMES"); extra != "" {
		for _, n := range strings.Split(extra, ",") {
			if n = strings.TrimSpace(n); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}
