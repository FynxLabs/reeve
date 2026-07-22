package github

import (
	"context"
	"sort"
	"strings"
	"time"

	gh "github.com/google/go-github/v66/github"

	"github.com/FynxLabs/reeve/internal/core/approvals"
	"github.com/FynxLabs/reeve/internal/vcs"
)

// ListCommentApprovals implements the opt-in pr_comment approval source: an
// authorized, non-bot commenter posting `/reeve approve` on the PR counts as an
// approval. It reads historical issue comments directly from the API, so it
// re-enforces the SAME author_association gate action.yml applies to command
// dispatch - nothing upstream vouches for a past comment. The non-author rule,
// freshness, and dismiss_on_new_commit are applied downstream by
// approvals.Evaluate, uniformly with pr_review approvals.
//
// Each approval is stamped with the SHA that was HEAD when the comment was
// posted (mirroring how a pr_review carries its commit_id), so
// dismiss_on_new_commit invalidates a comment approval once a newer commit
// lands - exactly as it does for a stale review.
func (c *Client) ListCommentApprovals(ctx context.Context, pr approvals.PR, cfg vcs.CommentApprovalConfig) ([]approvals.Approval, error) {
	prefixes, verb := parseCommentTrigger(cfg)
	allowed := normalizeAssociations(cfg.AllowedAssociations)

	// Commit timeline: needed to map a comment's post time to the SHA that was
	// HEAD at the time, so dismiss_on_new_commit can be evaluated. Fail closed
	// on error - a swallowed error would leave CommitSHA empty and let a stale
	// comment approval slip past dismissal.
	commits, err := c.listCommitTimes(ctx, pr.Number)
	if err != nil {
		return nil, err
	}

	comments, err := c.listIssueComments(ctx, pr.Number)
	if err != nil {
		return nil, err
	}

	return commentApprovals(comments, commits, prefixes, verb, allowed, pr.HeadSHA), nil
}

// commentApprovals is the pure core of the pr_comment source: it turns issue
// comments into approvals, applying the prefix/verb parse, the bot guard, and
// the author_association gate, and stamping each with the HEAD-at-comment-time
// SHA. Kept side-effect-free so it can be tested without an HTTP mock. The
// non-author rule, freshness, and dismissal are left to approvals.Evaluate.
func commentApprovals(comments []*gh.IssueComment, commits []commitTime, prefixes []string, verb string, allowed map[string]struct{}, headSHA string) []approvals.Approval {
	var out []approvals.Approval
	// One approval per commenter, keeping the LATEST qualifying comment.
	// Comments arrive oldest-first; if we kept the first, a re-approval after
	// a new push (a fresh `/reeve approve` stamped with the new HEAD) would be
	// discarded in favor of the stale earlier one - which dismiss_on_new_commit
	// then invalidates, making the gate impossible to re-satisfy by comment.
	// Overwriting in place keeps output order stable at first appearance while
	// carrying the latest comment's timestamp + HEAD SHA.
	idx := map[string]int{} // login -> position in out
	for _, cm := range comments {
		login := cm.GetUser().GetLogin()
		if login == "" {
			continue
		}
		// Self-trigger guard: never treat reeve's own (or any bot's) comment as
		// an approval. Mirrors the action.yml dispatch guard.
		if cm.GetUser().GetType() == "Bot" || strings.HasSuffix(login, "[bot]") {
			continue
		}
		if !isApproveComment(cm.GetBody(), prefixes, verb) {
			continue
		}
		// Authorization gate: identical to the action.yml command gate. An
		// unauthorized commenter's `/reeve approve` MUST NOT count.
		if !associationAllowed(cm.GetAuthorAssociation(), allowed) {
			continue
		}
		created := cm.GetCreatedAt().Time
		ap := approvals.Approval{
			Source:      approvals.SourcePRComment,
			Approver:    login,
			SubmittedAt: created,
			CommitSHA:   headSHAAt(commits, created, headSHA),
		}
		if i, ok := idx[login]; ok {
			out[i] = ap // later comment supersedes the earlier one
			continue
		}
		idx[login] = len(out)
		out = append(out, ap)
	}
	return out
}

// commitTime pairs a commit SHA with the time it landed on the PR.
type commitTime struct {
	sha string
	at  time.Time
}

// listCommitTimes returns the PR's commits ordered oldest-first with their
// committer timestamps.
func (c *Client) listCommitTimes(ctx context.Context, number int) ([]commitTime, error) {
	var out []commitTime
	opt := &gh.ListOptions{PerPage: 100}
	for {
		page, resp, err := c.gh.PullRequests.ListCommits(ctx, c.owner, c.repo, number, opt)
		if err != nil {
			return nil, err
		}
		for _, rc := range page {
			ct := commitTime{sha: rc.GetSHA()}
			// Prefer committer date (when the commit landed) over author date.
			if commit := rc.GetCommit(); commit != nil {
				if committer := commit.GetCommitter(); committer != nil {
					ct.at = committer.GetDate().Time
				}
				if ct.at.IsZero() && commit.GetAuthor() != nil {
					ct.at = commit.GetAuthor().GetDate().Time
				}
			}
			out = append(out, ct)
		}
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out, nil
}

// listIssueComments returns all issue comments on the PR (paginated).
func (c *Client) listIssueComments(ctx context.Context, number int) ([]*gh.IssueComment, error) {
	var out []*gh.IssueComment
	opt := &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{PerPage: 100}}
	for {
		page, resp, err := c.gh.Issues.ListComments(ctx, c.owner, c.repo, number, opt)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}

// headSHAAt returns the SHA that was HEAD when a comment was posted at t: the
// newest commit with a timestamp <= t. This is the code the commenter was
// approving. When no commit predates the comment (a comment older than every
// commit - e.g. an approval edited into an ancient comment), it falls back to
// the oldest commit so dismiss_on_new_commit still invalidates it once HEAD
// moves on (fail closed). With no commits at all, it falls back to fallback
// (the current PR HEAD).
func headSHAAt(commits []commitTime, t time.Time, fallback string) string {
	if len(commits) == 0 {
		return fallback
	}
	sha := commits[0].sha
	for _, ct := range commits {
		if ct.at.After(t) {
			break
		}
		sha = ct.sha
	}
	return sha
}

// parseCommentTrigger derives the accepted command prefixes and the verb from
// the source config. The verb is the second token of cfg.Command (default
// "/reeve approve" -> verb "approve"); the first token of cfg.Command is added
// to the configured prefix list.
func parseCommentTrigger(cfg vcs.CommentApprovalConfig) (prefixes []string, verb string) {
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		command = approvals.DefaultCommentCommand
	}
	fields := strings.Fields(command)
	verb = "approve"
	if len(fields) >= 2 {
		verb = strings.ToLower(fields[1])
	}
	set := map[string]struct{}{}
	if len(fields) >= 1 {
		set[fields[0]] = struct{}{}
	}
	for _, p := range cfg.CommandPrefixes {
		p = strings.TrimSpace(p)
		if p != "" {
			set[p] = struct{}{}
		}
	}
	if len(set) == 0 {
		set["/reeve"] = struct{}{}
		set["@reeve"] = struct{}{}
	}
	for p := range set {
		prefixes = append(prefixes, p)
	}
	return prefixes, verb
}

// isApproveComment reports whether the first line of a comment body is a
// `<prefix> <verb>` command, parsed the same way action.yml parses commands:
// the first whitespace-delimited token must exactly match an accepted prefix
// and the second token must be the approve verb. Trailing tokens are ignored.
func isApproveComment(body string, prefixes []string, verb string) bool {
	line := body
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return false
	}
	prefixOK := false
	for _, p := range prefixes {
		if fields[0] == p {
			prefixOK = true
			break
		}
	}
	if !prefixOK {
		return false
	}
	return strings.EqualFold(fields[1], verb)
}

// normalizeAssociations upper-cases and trims the allowlist for comparison.
func normalizeAssociations(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, a := range in {
		a = strings.ToUpper(strings.TrimSpace(a))
		if a != "" {
			out[a] = struct{}{}
		}
	}
	return out
}

// associationAllowed reports whether a comment's author_association is in the
// allowlist. An empty allowlist denies everything (fail closed): if no
// association is authorized, no comment can approve.
func associationAllowed(assoc string, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return false
	}
	_, ok := allowed[strings.ToUpper(strings.TrimSpace(assoc))]
	return ok
}
