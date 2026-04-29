package github

import (
	"context"
	"errors"
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
// commit. Phase 2 approximation: all non-reeve check-runs and statuses
// on HEAD SHA must be SUCCESS. Users can refine via required-check list
// in shared.yaml in later phases.
func (c *Client) ChecksGreen(ctx context.Context, sha string, ignoreNames []string) (bool, []string, error) {
	if sha == "" {
		return false, nil, errors.New("sha required")
	}
	shouldIgnore := func(name string) bool {
		for _, n := range ignoreNames {
			if name == n || strings.HasPrefix(name, n+" ") || strings.HasPrefix(name, n+"/") {
				return true
			}
		}
		return false
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
			if shouldIgnore(r.GetName()) {
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
	// Combined statuses (legacy).
	st, _, err := c.gh.Repositories.GetCombinedStatus(ctx, c.owner, c.repo, sha, &gh.ListOptions{PerPage: 100})
	if err == nil && st.GetState() != "" && st.GetState() != "success" {
		for _, s := range st.Statuses {
			if shouldIgnore(s.GetContext()) {
				continue
			}
			if s.GetState() != "success" {
				failing = append(failing, s.GetContext()+":"+s.GetState())
			}
		}
	}
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
