package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseDurationExtended parses a Go duration string, additionally accepting
// day and week units that time.ParseDuration rejects: "7d" = 7*24h,
// "2w" = 14*24h (fractions like "1.5d" work too). Plain Go durations pass
// through unchanged.
func parseDurationExtended(s string) (time.Duration, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	var unit time.Duration
	switch {
	case strings.HasSuffix(s, "d"):
		unit = 24 * time.Hour
	case strings.HasSuffix(s, "w"):
		unit = 7 * 24 * time.Hour
	default:
		return 0, fmt.Errorf("invalid duration %q (Go duration like 24h, or day/week units like 7d, 2w)", s)
	}
	n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(s, "d"), "w"), 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid duration %q (Go duration like 24h, or day/week units like 7d, 2w)", s)
	}
	return time.Duration(n * float64(unit)), nil
}
