// Package approvals resolves approval rules against PR reviews and CODEOWNERS.
// Pure: each consumer defines the minimal ApprovalSource / reviewLister /
// teamResolver interfaces it needs. Layered rules merge per PLAN.md §5.3.
package approvals
