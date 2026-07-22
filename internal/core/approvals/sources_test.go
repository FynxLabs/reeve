package approvals

import (
	"testing"
	"time"
)

func TestSourceEnablement_DefaultIsPRReviewOnly(t *testing.T) {
	// No sources block: pr_review on, pr_comment off - identical to prior
	// behavior for existing configs.
	var cfg Config
	if !cfg.PRReviewEnabled() {
		t.Error("pr_review must be on by default")
	}
	if cfg.PRCommentEnabled() {
		t.Error("pr_comment must be off unless opted in")
	}
	if cfg.CommentCommand() != DefaultCommentCommand {
		t.Errorf("default comment command = %q, want %q", cfg.CommentCommand(), DefaultCommentCommand)
	}
}

func TestSourceEnablement_ExplicitToggles(t *testing.T) {
	cfg := Config{Sources: []SourceConfig{
		{Type: SourcePRReview, Enabled: false},
		{Type: SourcePRComment, Enabled: true, Command: "/reeve approve"},
	}}
	if cfg.PRReviewEnabled() {
		t.Error("explicit pr_review enabled:false must disable it")
	}
	if !cfg.PRCommentEnabled() {
		t.Error("explicit pr_comment enabled:true must enable it")
	}
	if cfg.CommentCommand() != "/reeve approve" {
		t.Errorf("comment command = %q", cfg.CommentCommand())
	}
}

func TestSourceEnablement_PRCommentListedButDisabled(t *testing.T) {
	cfg := Config{Sources: []SourceConfig{
		{Type: SourcePRComment, Enabled: false},
	}}
	if !cfg.PRReviewEnabled() {
		t.Error("pr_review not mentioned should remain on by default")
	}
	if cfg.PRCommentEnabled() {
		t.Error("pr_comment enabled:false must stay off")
	}
}

func TestMergeApprovals_Concatenates(t *testing.T) {
	a := []Approval{{Approver: "alice", Source: SourcePRReview}}
	b := []Approval{{Approver: "bob", Source: SourcePRComment}}
	got := MergeApprovals(a, b, nil)
	if len(got) != 2 {
		t.Fatalf("merge len = %d, want 2", len(got))
	}
}

// The union must count a human who approves via BOTH a review and a comment
// exactly once toward required_approvals.
func TestEvaluate_DedupAcrossSources(t *testing.T) {
	head := "sha1"
	pr := PR{Number: 1, HeadSHA: head, Author: "author", RepoPrivate: true}
	one := 1
	rules := Rules{RequiredApprovals: one}
	merged := MergeApprovals(
		[]Approval{{Source: SourcePRReview, Approver: "sameperson", CommitSHA: head, SubmittedAt: time.Now()}},
		[]Approval{{Source: SourcePRComment, Approver: "sameperson", CommitSHA: head, SubmittedAt: time.Now()}},
	)
	res := Evaluate(rules, merged, pr, nil, pr.Author, time.Now())
	if res.Got != 1 {
		t.Fatalf("same human via two sources must count once; Got=%d trace=%v", res.Got, res.Trace)
	}
	if !res.Satisfied {
		t.Fatalf("one distinct approver should satisfy required_approvals=1; trace=%v", res.Trace)
	}
}

// A `/reeve approve` from the PR author never self-approves - the non-author
// rule applies uniformly to comment approvals.
func TestEvaluate_CommentAuthorSelfApprovalIgnored(t *testing.T) {
	head := "sha1"
	pr := PR{Number: 1, HeadSHA: head, Author: "author"}
	rules := Rules{RequiredApprovals: 1}
	appr := []Approval{{Source: SourcePRComment, Approver: "author", CommitSHA: head, SubmittedAt: time.Now()}}
	res := Evaluate(rules, appr, pr, nil, pr.Author, time.Now())
	if res.Satisfied {
		t.Fatalf("author's own /reeve approve must not satisfy the gate; trace=%v", res.Trace)
	}
}

// dismiss_on_new_commit invalidates a comment approval stamped with an older
// commit, exactly as it does a stale review.
func TestEvaluate_DismissOnNewCommitAppliesToComments(t *testing.T) {
	pr := PR{Number: 1, HeadSHA: "sha2", Author: "author", RepoPrivate: true}
	rules := Rules{RequiredApprovals: 1, DismissOnNewCommit: true}
	appr := []Approval{{Source: SourcePRComment, Approver: "reviewer", CommitSHA: "sha1", SubmittedAt: time.Now()}}
	res := Evaluate(rules, appr, pr, nil, pr.Author, time.Now())
	if res.Satisfied {
		t.Fatalf("comment approval on old commit must be dismissed under dismiss_on_new_commit; trace=%v", res.Trace)
	}
	// Same approval on current HEAD counts.
	appr[0].CommitSHA = "sha2"
	res = Evaluate(rules, appr, pr, nil, pr.Author, time.Now())
	if !res.Satisfied {
		t.Fatalf("comment approval on current HEAD should count; trace=%v", res.Trace)
	}
}
