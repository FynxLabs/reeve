package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/thefynx/reeve/internal/blob/factory"
	blocks "github.com/thefynx/reeve/internal/blob/locks"
	"github.com/thefynx/reeve/internal/config"
)

func newLocksCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "locks", Short: "Inspect or manage stack locks"}

	list := &cobra.Command{
		Use:   "list",
		Short: "Read-only bucket lock inspection",
		RunE:  locksList,
	}

	explain := &cobra.Command{
		Use:   "explain <project/stack>",
		Short: "Show lock holder, queue, TTL",
		Args:  cobra.ExactArgs(1),
		RunE:  locksExplain,
	}

	reap := &cobra.Command{
		Use:   "reap",
		Short: "Evict expired locks across the bucket (opportunistic - also runs on every invocation)",
		RunE:  locksReap,
	}

	unlock := &cobra.Command{
		Use:   "unlock <project/stack>",
		Short: "Admin-override release: forcibly clear the holder, promote queue",
		Args:  cobra.ExactArgs(1),
		RunE:  locksUnlock,
	}
	unlock.Flags().String("reason", "", "Required reason for the override (surfaces in logs)")
	unlock.Flags().String("actor", "", "User performing the override (default: $GITHUB_ACTOR or $USER)")

	cmd.AddCommand(list, explain, reap, unlock)
	return cmd
}

func openLocks(cmd *cobra.Command) (*blocks.Store, error) {
	root, _ := os.Getwd()
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	store, err := factory.Open(context.Background(), cfg.Shared.Bucket, root)
	if err != nil {
		return nil, fmt.Errorf("open bucket: %w", err)
	}
	return blocks.New(store), nil
}

func locksList(cmd *cobra.Command, _ []string) error {
	s, err := openLocks(cmd)
	if err != nil {
		return err
	}
	locks, err := s.ListAll(context.Background())
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	if len(locks) == 0 {
		fmt.Fprintln(w, "no locks")
		return nil
	}
	now := time.Now()
	for _, l := range locks {
		st := l.Status(now)
		holder := "-"
		if l.Holder != nil {
			holder = fmt.Sprintf("PR #%d (expires %s)", l.Holder.PR, l.Holder.ExpiresAt)
		}
		fmt.Fprintf(w, "%s/%s  %s  holder=%s  queue=%d\n",
			l.Project, l.Stack, st, holder, len(l.Queue))
	}
	return nil
}

func locksExplain(cmd *cobra.Command, args []string) error {
	ref := args[0]
	parts := splitRef(ref)
	if parts == nil {
		return fmt.Errorf("expected project/stack, got %q", ref)
	}
	s, err := openLocks(cmd)
	if err != nil {
		return err
	}
	l, etag, err := s.Get(context.Background(), parts[0], parts[1])
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "lock: %s/%s\n", l.Project, l.Stack)
	fmt.Fprintf(w, "status: %s\n", l.Status(time.Now()))
	fmt.Fprintf(w, "etag: %s\n", shortEtag(etag))
	if l.Holder != nil {
		h := l.Holder
		fmt.Fprintf(w, "holder: PR #%d  actor=%s  run=%s  acquired=%s  expires=%s\n",
			h.PR, h.Actor, h.RunID, h.AcquiredAt, h.ExpiresAt)
	} else {
		fmt.Fprintln(w, "holder: (free)")
	}
	if len(l.Queue) > 0 {
		fmt.Fprintln(w, "queue:")
		for i, q := range l.Queue {
			fmt.Fprintf(w, "  %d. PR #%d  actor=%s  enqueued=%s\n", i+1, q.PR, q.Actor, q.EnqueuedAt)
		}
	}
	return nil
}

func locksReap(cmd *cobra.Command, _ []string) error {
	s, err := openLocks(cmd)
	if err != nil {
		return err
	}
	n, err := s.ReapAll(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "reaped %d expired lock(s)\n", n)
	return nil
}

func locksUnlock(cmd *cobra.Command, args []string) error {
	ref := args[0]
	parts := splitRef(ref)
	if parts == nil {
		return fmt.Errorf("expected project/stack, got %q", ref)
	}

	root, _ := os.Getwd()
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	reason := flagStringOrDefault(cmd, "reason", "")
	actor := flagStringOrEnv(cmd, "actor", "GITHUB_ACTOR")
	if actor == "" {
		actor = os.Getenv("USER")
	}

	// Admin gate: shared.yaml locking.admin_override
	admin := cfg.Shared.Locking.AdminOverride
	if admin.RequiresReason && reason == "" {
		return fmt.Errorf("locks unlock: --reason is required (shared.yaml locking.admin_override.requires_reason=true)")
	}
	if len(admin.Allowed) > 0 {
		if !actorAllowed(actor, admin.Allowed) {
			return fmt.Errorf("locks unlock: actor %q is not in locking.admin_override.allowed", actor)
		}
	}

	s, err := openLocks(cmd)
	if err != nil {
		return err
	}
	l, err := s.ForceUnlock(context.Background(), parts[0], parts[1])
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "unlocked %s/%s by actor=%s reason=%q\n",
		parts[0], parts[1], actor, reason)
	if l.Holder != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "promoted PR #%d from queue\n", l.Holder.PR)
	}
	return nil
}

func actorAllowed(actor string, allowed []string) bool {
	// allowed entries may be user logins (@alice) or team slugs (@org/team).
	// Phase 2 approximation: literal equality, with/without leading @. Team
	// expansion lands when the VCS adapter is wired here (Phase 4+).
	trimmed := actor
	if len(trimmed) > 0 && trimmed[0] == '@' {
		trimmed = trimmed[1:]
	}
	for _, a := range allowed {
		candidate := a
		if len(candidate) > 0 && candidate[0] == '@' {
			candidate = candidate[1:]
		}
		if candidate == trimmed {
			return true
		}
	}
	return false
}

func splitRef(ref string) []string {
	for i := 0; i < len(ref); i++ {
		if ref[i] == '/' {
			return []string{ref[:i], ref[i+1:]}
		}
	}
	return nil
}

func shortEtag(e string) string {
	if len(e) > 12 {
		return e[:12] + "…"
	}
	return e
}
