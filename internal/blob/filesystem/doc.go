// Package filesystem is the filesystem:// blob adapter - used for local
// tests and `reeve run preview --local`. Conditional writes via flock +
// fsync. First adapter implemented (PLAN.md §5.8).
package filesystem
