package github

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v66/github"

	"github.com/FynxLabs/reeve/internal/core/approvals"
	"github.com/FynxLabs/reeve/internal/vcs"
)

func issueComment(login, assoc, body, typ string, at time.Time) *gh.IssueComment {
	return &gh.IssueComment{
		User:              &gh.User{Login: gh.String(login), Type: gh.String(typ)},
		AuthorAssociation: gh.String(assoc),
		Body:              gh.String(body),
		CreatedAt:         &gh.Timestamp{Time: at},
	}
}

func defaultTrigger(t *testing.T) (prefixes []string, verb string, allowed map[string]struct{}) {
	t.Helper()
	cfg := vcs.CommentApprovalConfig{
		CommandPrefixes:     []string{"/reeve", "@reeve"},
		AllowedAssociations: []string{"OWNER", "MEMBER", "COLLABORATOR"},
	}
	p, v := parseCommentTrigger(cfg)
	return p, v, normalizeAssociations(cfg.AllowedAssociations)
}

func TestCommentApprovals_AuthorizedNonBotCounts(t *testing.T) {
	prefixes, verb, allowed := defaultTrigger(t)
	head := "sha1"
	commits := []commitTime{{sha: "sha1", at: time.Unix(1000, 0)}}
	comments := []*gh.IssueComment{
		issueComment("reviewer", "MEMBER", "/reeve approve", "User", time.Unix(2000, 0)),
	}
	out := commentApprovals(comments, commits, prefixes, verb, allowed, head)
	if len(out) != 1 {
		t.Fatalf("want 1 approval, got %d (%+v)", len(out), out)
	}
	if out[0].Approver != "reviewer" || out[0].Source != approvals.SourcePRComment {
		t.Fatalf("unexpected approval: %+v", out[0])
	}
	if out[0].CommitSHA != "sha1" {
		t.Fatalf("want CommitSHA sha1 (HEAD-at-comment-time), got %q", out[0].CommitSHA)
	}
}

func TestCommentApprovals_UnauthorizedAssociationRejected(t *testing.T) {
	prefixes, verb, allowed := defaultTrigger(t)
	commits := []commitTime{{sha: "sha1", at: time.Unix(1000, 0)}}
	comments := []*gh.IssueComment{
		// NONE / CONTRIBUTOR are not in the allowlist: must NOT count.
		issueComment("outsider", "NONE", "/reeve approve", "User", time.Unix(2000, 0)),
		issueComment("drive-by", "CONTRIBUTOR", "@reeve approve", "User", time.Unix(2001, 0)),
	}
	out := commentApprovals(comments, commits, prefixes, verb, allowed, "sha1")
	if len(out) != 0 {
		t.Fatalf("unauthorized commenters must not approve, got %+v", out)
	}
}

func TestCommentApprovals_BotRejected(t *testing.T) {
	prefixes, verb, allowed := defaultTrigger(t)
	commits := []commitTime{{sha: "sha1", at: time.Unix(1000, 0)}}
	comments := []*gh.IssueComment{
		issueComment("reeve[bot]", "MEMBER", "/reeve approve", "Bot", time.Unix(2000, 0)),
		issueComment("github-actions[bot]", "MEMBER", "/reeve approve", "User", time.Unix(2001, 0)),
	}
	out := commentApprovals(comments, commits, prefixes, verb, allowed, "sha1")
	if len(out) != 0 {
		t.Fatalf("bot comments must not approve (self-trigger guard), got %+v", out)
	}
}

func TestCommentApprovals_NonCommandIgnored(t *testing.T) {
	prefixes, verb, allowed := defaultTrigger(t)
	commits := []commitTime{{sha: "sha1", at: time.Unix(1000, 0)}}
	comments := []*gh.IssueComment{
		issueComment("a", "MEMBER", "looks good, approving", "User", time.Unix(2000, 0)),
		issueComment("b", "MEMBER", "/reeve preview", "User", time.Unix(2001, 0)),
		issueComment("c", "MEMBER", "please /reeve approve this", "User", time.Unix(2002, 0)), // prefix not first token
		issueComment("d", "MEMBER", "/reeve", "User", time.Unix(2003, 0)),                     // no verb
	}
	out := commentApprovals(comments, commits, prefixes, verb, allowed, "sha1")
	if len(out) != 0 {
		t.Fatalf("only a first-token `<prefix> approve` counts, got %+v", out)
	}
}

func TestCommentApprovals_MentionPrefixAndCaseInsensitiveVerb(t *testing.T) {
	prefixes, verb, allowed := defaultTrigger(t)
	commits := []commitTime{{sha: "sha1", at: time.Unix(1000, 0)}}
	comments := []*gh.IssueComment{
		issueComment("m", "OWNER", "@reeve Approve", "User", time.Unix(2000, 0)),
	}
	out := commentApprovals(comments, commits, prefixes, verb, allowed, "sha1")
	if len(out) != 1 {
		t.Fatalf("@reeve Approve should count, got %+v", out)
	}
}

func TestCommentApprovals_DedupPerCommenter(t *testing.T) {
	prefixes, verb, allowed := defaultTrigger(t)
	commits := []commitTime{{sha: "sha1", at: time.Unix(1000, 0)}}
	comments := []*gh.IssueComment{
		issueComment("reviewer", "MEMBER", "/reeve approve", "User", time.Unix(2000, 0)),
		issueComment("reviewer", "MEMBER", "/reeve approve", "User", time.Unix(3000, 0)),
	}
	out := commentApprovals(comments, commits, prefixes, verb, allowed, "sha1")
	if len(out) != 1 {
		t.Fatalf("same commenter approving twice must yield one approval, got %+v", out)
	}
}

func TestCommentApprovals_HeadSHAAtCommentTime(t *testing.T) {
	// A comment posted between commit sha1 and sha2 approves sha1, not the
	// current HEAD sha2 - so dismiss_on_new_commit can invalidate it.
	commits := []commitTime{
		{sha: "sha1", at: time.Unix(1000, 0)},
		{sha: "sha2", at: time.Unix(3000, 0)},
	}
	if got := headSHAAt(commits, time.Unix(2000, 0), "sha2"); got != "sha1" {
		t.Fatalf("comment between sha1 and sha2 should map to sha1, got %q", got)
	}
	if got := headSHAAt(commits, time.Unix(4000, 0), "sha2"); got != "sha2" {
		t.Fatalf("comment after sha2 should map to sha2, got %q", got)
	}
	// Comment older than every commit falls back to the oldest commit (fail
	// closed under dismiss_on_new_commit once HEAD moves on).
	if got := headSHAAt(commits, time.Unix(500, 0), "sha2"); got != "sha1" {
		t.Fatalf("comment before all commits should map to oldest commit sha1, got %q", got)
	}
	// No commits: fall back to current HEAD.
	if got := headSHAAt(nil, time.Unix(500, 0), "sha9"); got != "sha9" {
		t.Fatalf("no commits should fall back to head sha9, got %q", got)
	}
}

func TestCommentApprovals_EmptyAllowlistDeniesAll(t *testing.T) {
	prefixes, verb, _ := defaultTrigger(t)
	commits := []commitTime{{sha: "sha1", at: time.Unix(1000, 0)}}
	comments := []*gh.IssueComment{
		issueComment("owner", "OWNER", "/reeve approve", "User", time.Unix(2000, 0)),
	}
	out := commentApprovals(comments, commits, prefixes, verb, map[string]struct{}{}, "sha1")
	if len(out) != 0 {
		t.Fatalf("empty allowlist must deny everything (fail closed), got %+v", out)
	}
}

func TestParseCommentTrigger_CustomCommand(t *testing.T) {
	cfg := vcs.CommentApprovalConfig{Command: "!bot ok", CommandPrefixes: []string{"/reeve"}}
	prefixes, verb := parseCommentTrigger(cfg)
	if verb != "ok" {
		t.Fatalf("verb from custom command = %q, want ok", verb)
	}
	got := map[string]bool{}
	for _, p := range prefixes {
		got[p] = true
	}
	if !got["!bot"] || !got["/reeve"] {
		t.Fatalf("prefixes should union command's first token and CommandPrefixes, got %v", prefixes)
	}
}
