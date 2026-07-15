package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v66/github"

	"github.com/thefynx/reeve/internal/core/approvals"
	"github.com/thefynx/reeve/internal/vcs"
)

// ListApprovals returns APPROVED reviews for the PR. It filters out
// review comments and CHANGES_REQUESTED. Submission time and commit SHA
// are preserved so dismiss_on_new_commit can be evaluated.
func (c *Client) ListApprovals(ctx context.Context, pr approvals.PR) ([]approvals.Approval, error) {
	var out []approvals.Approval
	opt := &gh.ListOptions{PerPage: 100}
	for {
		revs, resp, err := c.gh.PullRequests.ListReviews(ctx, c.owner, c.repo, pr.Number, opt)
		if err != nil {
			return nil, err
		}
		for _, r := range revs {
			if r.GetState() != "APPROVED" {
				continue
			}
			out = append(out, approvals.Approval{
				Source:      "pr_review",
				Approver:    r.GetUser().GetLogin(),
				SubmittedAt: r.GetSubmittedAt().Time,
				CommitSHA:   r.GetCommitID(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}

func (*Client) Name() string { return "pr_review" }

// ListOpenPRsTouchingPaths powers drift-overlap surfacing and "blocked by
// PR #X" queries. Uses GitHub Search API: "is:pr is:open repo:owner/repo".
func (c *Client) ListOpenPRsTouchingPaths(ctx context.Context, paths []string) ([]approvals.PR, error) {
	// Simple implementation: list all open PRs, then per-PR ListFiles
	// intersect with paths. For repos with many open PRs this is slow -
	// Phase 7/8 can optimize with the GraphQL API if needed.
	var prs []*gh.PullRequest
	opt := &gh.PullRequestListOptions{State: "open", ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		page, resp, err := c.gh.PullRequests.List(ctx, c.owner, c.repo, opt)
		if err != nil {
			return nil, err
		}
		prs = append(prs, page...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	wanted := make(map[string]bool, len(paths))
	for _, p := range paths {
		wanted[p] = true
	}

	var out []approvals.PR
	for _, p := range prs {
		files, err := c.listFilesStrings(ctx, p.GetNumber())
		if err != nil {
			return nil, err
		}
		hit := false
		for _, f := range files {
			if wanted[f] || anyPrefixIn(f, paths) {
				hit = true
				break
			}
		}
		if hit {
			out = append(out, approvals.PR{
				Number:  p.GetNumber(),
				HeadSHA: p.GetHead().GetSHA(),
				Author:  p.GetUser().GetLogin(),
				BaseRef: p.GetBase().GetRef(),
				Changed: files,
			})
		}
	}
	return out, nil
}

func (c *Client) listFilesStrings(ctx context.Context, pr int) ([]string, error) {
	return c.ListChangedFiles(ctx, pr)
}

func anyPrefixIn(file string, paths []string) bool {
	for _, p := range paths {
		if file == p || strings.HasPrefix(file, p+"/") {
			return true
		}
	}
	return false
}

// ChecksGreen reports whether required status checks are passing for a
// commit. See vcs.ChecksGreenOpts for self-skip semantics.
func (c *Client) ChecksGreen(ctx context.Context, sha string, opts vcs.ChecksGreenOpts) (bool, []string, error) {
	if sha == "" {
		return false, nil, errors.New("sha required")
	}
	logger := slog.With("op", "checks_green", "sha", shortSHA(sha))

	ignoreURLFragment := ""
	if opts.IgnoreRunID > 0 {
		ignoreURLFragment = fmt.Sprintf("/runs/%d/", opts.IgnoreRunID)
	}
	ignoreNames := make(map[string]struct{}, len(opts.IgnoreNames))
	for _, n := range opts.IgnoreNames {
		if n == "" {
			continue
		}
		ignoreNames[n] = struct{}{}
	}

	var failing []string
	checkOpt := &gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		runs, resp, err := c.gh.Checks.ListCheckRunsForRef(ctx, c.owner, c.repo, sha, checkOpt)
		if err != nil {
			return false, nil, fmt.Errorf("list check runs: %w", err)
		}
		for _, r := range runs.CheckRuns {
			name, status, conclusion, url := r.GetName(), r.GetStatus(), r.GetConclusion(), r.GetDetailsURL()
			logger.Debug("check_run inspected",
				"name", name, "status", status, "conclusion", conclusion, "url", url)

			// Skip the current workflow run - it cannot be green while running.
			if ignoreURLFragment != "" && strings.Contains(url, ignoreURLFragment) {
				logger.Debug("check_run skipped: current run", "name", name)
				continue
			}
			// Skip reeve's own check_runs from prior workflow runs on this
			// SHA. Without this, a single failed apply pins the gate red
			// forever because the failed check_run lives on the SHA, not the
			// run.
			if _, self := ignoreNames[name]; self {
				logger.Debug("check_run skipped: self by name", "name", name)
				continue
			}
			// Skip all in-progress/queued/waiting checks - they haven't
			// finished yet, so they don't carry a verdict.
			if status == "in_progress" || status == "queued" || status == "waiting" {
				logger.Debug("check_run skipped: not yet concluded", "name", name, "status", status)
				continue
			}
			switch conclusion {
			case "success", "skipped", "neutral":
				continue
			case "":
				failing = append(failing, name+":pending")
			default:
				failing = append(failing, name+":"+conclusion)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		checkOpt.Page = resp.NextPage
	}

	// Commit statuses (legacy, separate from check runs). Only "failure" /
	// "error" combined states matter; "pending" reflects in-progress check
	// runs already filtered above.
	st, _, err := c.gh.Repositories.GetCombinedStatus(ctx, c.owner, c.repo, sha, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return false, nil, fmt.Errorf("combined status: %w", err)
	}
	logger.Debug("combined_status", "state", st.GetState(), "n", len(st.Statuses))
	if st.GetState() == "failure" || st.GetState() == "error" {
		for _, s := range st.Statuses {
			if s.GetState() != "success" && s.GetState() != "pending" {
				failing = append(failing, s.GetContext()+":"+s.GetState())
			}
		}
	}

	green := len(failing) == 0
	logger.Debug("verdict", "green", green, "failing", failing)
	return green, failing, nil
}

func shortSHA(sha string) string {
	if len(sha) < 7 {
		return sha
	}
	return sha[:7]
}

// CompareBranches reports how many commits HEAD is behind base.
func (c *Client) CompareBranches(ctx context.Context, base, head string) (int, error) {
	cmp, _, err := c.gh.Repositories.CompareCommits(ctx, c.owner, c.repo, base, head, &gh.ListOptions{PerPage: 1})
	if err != nil {
		return 0, err
	}
	// BehindBy: commits base has that head doesn't.
	return cmp.GetBehindBy(), nil
}

// FetchCodeowners returns the raw CODEOWNERS file contents from the
// repo's default branch. Returns "" and nil error if absent. Only a 404
// is treated as absence - auth/transport/rate-limit errors propagate so
// the codeowners gate fails closed instead of silently evaluating with no
// owners.
func (c *Client) FetchCodeowners(ctx context.Context) (string, error) {
	for _, path := range []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"} {
		f, _, resp, err := c.gh.Repositories.GetContents(ctx, c.owner, c.repo, path, nil)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				continue
			}
			return "", fmt.Errorf("get %s: %w", path, err)
		}
		if f == nil {
			continue
		}
		decoded, err := f.GetContent()
		if err != nil {
			return "", err
		}
		return decoded, nil
	}
	return "", nil
}

// ListTeamMembers expands a team slug "org/team" to member logins. Used
// by approval rules when the allow-list is a team rather than individuals.
func (c *Client) ListTeamMembers(ctx context.Context, slug string) ([]string, error) {
	slug = strings.TrimPrefix(slug, "@")
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 {
		return nil, errors.New("team slug must be org/team")
	}
	org, team := parts[0], parts[1]
	opt := &gh.TeamListTeamMembersOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	var out []string
	for {
		users, resp, err := c.gh.Teams.ListTeamMembersBySlug(ctx, org, team, opt)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			out = append(out, u.GetLogin())
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}
