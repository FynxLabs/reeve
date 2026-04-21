// Package freeze evaluates freeze windows. A window is a cron expression
// + duration + stack pattern set. A stack is in freeze if *any* window
// matching it has fired within the last `duration` at now.
package freeze

import (
	"fmt"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/robfig/cron/v3"
)

// Window is a single freeze entry.
type Window struct {
	Name     string
	Cron     string        // e.g. "0 15 * * 5"
	Duration time.Duration // how long the freeze lasts after firing
	Stacks   []string      // glob patterns over "project/stack"; empty = all stacks
}

// Config is the full set from shared.yaml.
type Config struct {
	Windows []Window
}

// ActiveFor returns the first window currently freezing the given stack
// ref, or "" if none is active.
func ActiveFor(cfg Config, ref string, now time.Time) (string, bool, error) {
	for _, w := range cfg.Windows {
		match := len(w.Stacks) == 0
		for _, pat := range w.Stacks {
			if ok, _ := doublestar.Match(pat, ref); ok {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		firing, err := mostRecentFire(w.Cron, now)
		if err != nil {
			return "", false, fmt.Errorf("freeze %q: %w", w.Name, err)
		}
		if firing.IsZero() {
			continue
		}
		if now.Before(firing.Add(w.Duration)) {
			return w.Name, true, nil
		}
	}
	return "", false, nil
}

// mostRecentFire walks back at most 2*MaxFreezeWindow to find the
// most recent scheduled fire for the cron expression.
func mostRecentFire(expr string, now time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, err
	}
	// Robfig cron's Next takes "from t, return next occurrence after t".
	// We want the last occurrence at-or-before now. Strategy: scan Next()
	// starting from `now - window size * 2` until we overshoot now.
	start := now.Add(-14 * 24 * time.Hour) // two weeks window — sufficient for typical freezes
	last := time.Time{}
	cur := start
	for i := 0; i < 10_000; i++ {
		next := sched.Next(cur)
		if next.After(now) {
			break
		}
		last = next
		cur = next
	}
	return last, nil
}
