package approvals

import (
	"fmt"
	"strconv"
	"strings"
)

// OverlapScanError reports a PARTIAL PR-overlap scan: some open PRs could
// not be checked (changed-file fetch failed, or the scan was capped). The
// result slice returned alongside this error still carries every PR that
// WAS checked, so callers must not treat the error as "no overlap" -
// degrade to a warning naming the unchecked PRs instead.
type OverlapScanError struct {
	// Unchecked lists the PR numbers whose changed files could not be
	// inspected.
	Unchecked []int
	// Err is the first underlying fetch error, kept for diagnostics.
	Err error
}

func (e *OverlapScanError) Error() string {
	nums := make([]string, len(e.Unchecked))
	for i, n := range e.Unchecked {
		nums[i] = "#" + strconv.Itoa(n)
	}
	msg := fmt.Sprintf("overlap scan incomplete: could not check open PR(s) %s", strings.Join(nums, ", "))
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *OverlapScanError) Unwrap() error { return e.Err }
