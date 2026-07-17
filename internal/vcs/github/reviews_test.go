package github

import (
	"testing"

	gh "github.com/google/go-github/v66/github"
)

func review(login, state, commit string) *gh.PullRequestReview {
	return &gh.PullRequestReview{
		User:     &gh.User{Login: gh.String(login)},
		State:    gh.String(state),
		CommitID: gh.String(commit),
	}
}

func approverSet(t *testing.T, revs []*gh.PullRequestReview) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	for _, a := range latestApprovals(revs) {
		got[a.Approver] = true
	}
	return got
}

func TestLatestApprovals(t *testing.T) {
	// Reviews arrive oldest-first (GitHub's order).
	revs := []*gh.PullRequestReview{
		review("alice", "APPROVED", "sha1"),
		// Bob approves then later requests changes: his stance is withdrawn.
		review("bob", "APPROVED", "sha1"),
		review("bob", "CHANGES_REQUESTED", "sha1"),
		// Carol requests changes then later approves: her stance is approved.
		review("carol", "CHANGES_REQUESTED", "sha1"),
		review("carol", "APPROVED", "sha2"),
		// Dave's approval was dismissed.
		review("dave", "APPROVED", "sha1"),
		review("dave", "DISMISSED", "sha1"),
		// Comments never change a stance.
		review("alice", "COMMENTED", "sha2"),
	}
	got := approverSet(t, revs)
	want := map[string]bool{"alice": true, "carol": true}
	if len(got) != len(want) {
		t.Fatalf("approver set = %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("expected %s to be a current approver; got %v", k, got)
		}
	}
	if got["bob"] {
		t.Error("bob withdrew his approval via CHANGES_REQUESTED; must not count")
	}
	if got["dave"] {
		t.Error("dave's approval was dismissed; must not count")
	}
}

func TestLatestApprovalsPreservesCommitSHA(t *testing.T) {
	// The counted approval must carry the latest review's commit SHA so
	// dismiss_on_new_commit evaluates against the right commit.
	revs := []*gh.PullRequestReview{
		review("carol", "CHANGES_REQUESTED", "sha1"),
		review("carol", "APPROVED", "sha2"),
	}
	out := latestApprovals(revs)
	if len(out) != 1 || out[0].CommitSHA != "sha2" {
		t.Fatalf("expected one approval on sha2, got %+v", out)
	}
}

func TestAnyPrefixIn(t *testing.T) {
	tests := []struct {
		file  string
		paths []string
		want  bool
	}{
		// exact match
		{"infra/foo", []string{"infra/foo"}, true},
		// child path
		{"infra/foo/bar.ts", []string{"infra/foo"}, true},
		// unrelated
		{"infra/bar", []string{"infra/foo"}, false},
		// prefix substring that isn't a path boundary
		{"infra/foobar", []string{"infra/foo"}, false},
		// multiple paths, one matches
		{"a/b/c.go", []string{"x/y", "a/b"}, true},
		// empty paths
		{"infra/foo", []string{}, false},
	}
	for _, tt := range tests {
		got := anyPrefixIn(tt.file, tt.paths)
		if got != tt.want {
			t.Errorf("anyPrefixIn(%q, %v) = %v, want %v", tt.file, tt.paths, got, tt.want)
		}
	}
}
