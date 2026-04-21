package approvals

import (
	"testing"
	"time"
)

func TestResolveDefaultAndPatternMerge(t *testing.T) {
	cfg := Config{
		Default: Rules{RequiredApprovals: 1, Approvers: []string{"@org/infra-reviewers"}, Codeowners: true},
		Stacks: []StackRule{
			{
				Pattern: "prod/*",
				Rules:   Rules{RequiredApprovals: 2, Approvers: []string{"@org/sre", "@org/security"}, RequireAllGroups: true},
				Present: map[string]bool{"required_approvals": true, "require_all_groups": true},
			},
			{
				Pattern: "prod/payments",
				Rules:   Rules{Approvers: []string{"@org/payments-leads"}},
				Present: map[string]bool{},
			},
		},
	}
	got := Resolve(cfg, "prod/payments")
	if got.RequiredApprovals != 2 {
		t.Fatalf("expected pattern rule to win required_approvals=2: %+v", got)
	}
	if !got.RequireAllGroups {
		t.Fatalf("require_all_groups should be true")
	}
	// payments-leads unioned in.
	var hasPayments bool
	for _, a := range got.Approvers {
		if a == "@org/payments-leads" {
			hasPayments = true
		}
	}
	if !hasPayments {
		t.Fatalf("approvers missing payments-leads: %v", got.Approvers)
	}
}

func TestEvaluateRequiredApprovals(t *testing.T) {
	rules := Rules{RequiredApprovals: 2, Approvers: []string{"alice", "bob", "carol"}}
	pr := PR{Number: 7, HeadSHA: "sha1", Author: "dave"}
	approvals := []Approval{
		{Approver: "alice", CommitSHA: "sha1"},
		{Approver: "bob", CommitSHA: "sha1"},
	}
	res := Evaluate(rules, approvals, pr, nil, pr.Author)
	if !res.Satisfied {
		t.Fatalf("expected satisfied: %+v", res)
	}
}

func TestEvaluateDismissOnNewCommit(t *testing.T) {
	rules := Rules{RequiredApprovals: 1, Approvers: []string{"alice"}, DismissOnNewCommit: true}
	pr := PR{Number: 7, HeadSHA: "sha2"}
	approvals := []Approval{{Approver: "alice", CommitSHA: "sha1"}}
	res := Evaluate(rules, approvals, pr, nil, "dave")
	if res.Satisfied {
		t.Fatalf("expected unsatisfied after dismissal: %+v", res)
	}
}

func TestEvaluateGroupSemantics(t *testing.T) {
	rules := Rules{
		Approvers:        []string{"org/sre", "org/security"},
		RequireAllGroups: true,
	}
	pr := PR{Number: 7, HeadSHA: "sha1"}
	approvals := []Approval{{Approver: "org/sre"}}
	res := Evaluate(rules, approvals, pr, nil, "dave")
	if res.Satisfied {
		t.Fatalf("expected unsatisfied (missing security): %+v", res)
	}
	approvals = append(approvals, Approval{Approver: "org/security"})
	res = Evaluate(rules, approvals, pr, nil, "dave")
	if !res.Satisfied {
		t.Fatalf("expected satisfied when both groups approve: %+v", res)
	}
}

func TestEvaluateCodeowners(t *testing.T) {
	rules := Rules{Codeowners: true}
	pr := PR{Number: 7, HeadSHA: "sha1"}
	co := map[string][]string{
		"internal/core/render": {"frontend-team"},
	}
	res := Evaluate(rules, []Approval{{Approver: "frontend-team"}}, pr, co, "dave")
	if !res.Satisfied {
		t.Fatalf("expected codeowners satisfied: %+v", res)
	}
	res = Evaluate(rules, []Approval{{Approver: "someone-else"}}, pr, co, "dave")
	if res.Satisfied {
		t.Fatalf("expected codeowners unsatisfied: %+v", res)
	}
}

func TestEvaluateSelfApprovalIgnored(t *testing.T) {
	rules := Rules{RequiredApprovals: 1, Approvers: []string{"alice"}}
	pr := PR{Number: 7, HeadSHA: "sha1", Author: "alice"}
	approvals := []Approval{{Approver: "alice", SubmittedAt: time.Now()}}
	res := Evaluate(rules, approvals, pr, nil, pr.Author)
	if res.Satisfied {
		t.Fatalf("self-approval should not count: %+v", res)
	}
}

func TestEvaluateNoGatesConfiguredPasses(t *testing.T) {
	// No rules configured → open PR repo, no gate requirements → pass.
	res := Evaluate(Rules{}, nil, PR{HeadSHA: "x"}, nil, "dave")
	if !res.Satisfied {
		t.Fatalf("expected no-gates to pass: %+v", res)
	}
}
