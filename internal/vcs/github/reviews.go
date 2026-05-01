package github

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v66/github"

	"github.com/thefynx/reeve/internal/core/approvals"
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
// commit. ignoreRunID is the GitHub Actions run ID of the current workflow
// run (GITHUB_RUN_ID) - check runs belonging to that run are skipped so
// reeve does not block itself.
func (c *Client) ChecksGreen(ctx context.Context, sha string, ignoreRunID int64) (bool, []string, error) {
	if sha == "" {
		return false, nil, errors.New("sha required")
	}
	ignoreURLFragment := ""
	if ignoreRunID > 0 {
		ignoreURLFragment = fmt.Sprintf("/runs/%d/", ignoreRunID)
	}
	// Check-runs (the modern shape).
	var failing []string
	checkOpt := &gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		runs, resp, err := c.gh.Checks.ListCheckRunsForRef(ctx, c.owner, c.repo, sha, checkOpt)
		if err != nil {
			return false, nil, err
		}
		for _, r := range runs.CheckRuns {
			fmt.Printf("check_run name=%q status=%q conclusion=%q url=%q\n", r.GetName(), r.GetStatus(), r.GetConclusion(), r.GetDetailsURL())
			// Skip the current workflow run - it cannot be green while running.
			if ignoreURLFragment != "" && strings.Contains(r.GetDetailsURL(), ignoreURLFragment) {
				fmt.Printf("check_run skipped (current run)\n")
				continue
			}
			// Skip all in-progress/queued/waiting checks - they haven't finished yet.
			if r.GetStatus() == "in_progress" || r.GetStatus() == "queued" || r.GetStatus() == "waiting" {
				fmt.Printf("check_run skipped (in_progress/queued/waiting)\n")
				continue
			}
			switch r.GetConclusion() {
			case "success", "skipped", "neutral":
				continue
			case "":
				failing = append(failing, r.GetName()+":pending")
			default:
				failing = append(failing, r.GetName()+":"+r.GetConclusion())
			}
		}
		if resp.NextPage == 0 {
			break
		}
		checkOpt.Page = resp.NextPage
	}
	// Commit statuses (legacy, separate from check runs).
	// Only check failure/error - "pending" reflects in-progress check runs which we already skip above.
	st, _, err := c.gh.Repositories.GetCombinedStatus(ctx, c.owner, c.repo, sha, &gh.ListOptions{PerPage: 100})
	fmt.Printf("combined_status state=%q statuses=%d\n", st.GetState(), len(st.Statuses))
	if err == nil && (st.GetState() == "failure" || st.GetState() == "error") {
		for _, s := range st.Statuses {
			if s.GetState() != "success" && s.GetState() != "pending" {
				failing = append(failing, s.GetContext()+":"+s.GetState())
			}
		}
	}
	fmt.Printf("checks_green=%v failing=%v\n", len(failing) == 0, failing)
	return len(failing) == 0, failing, nil
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
// repo's default branch. Returns "" and nil error if absent.
func (c *Client) FetchCodeowners(ctx context.Context) (string, error) {
	for _, path := range []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"} {
		f, _, _, err := c.gh.Repositories.GetContents(ctx, c.owner, c.repo, path, nil)
		if err != nil {
			continue
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
