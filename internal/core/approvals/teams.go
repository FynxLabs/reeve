package approvals

import (
	"context"
	"fmt"
	"strings"
)

// TeamExpander resolves an "org/team" slug to its member logins. Adapter
// implementations live in internal/vcs/*; the interface keeps this package
// pure.
type TeamExpander interface {
	ListTeamMembers(ctx context.Context, slug string) ([]string, error)
}

// ExpandTeams walks a Rules and resolves every team slug it references via
// the expander, returning the slug → members map ready to assign to
// Rules.TeamMembers. Slugs that fail to expand are logged via the returned
// error joined string but do not abort the run - the caller may decide to
// proceed with literal-only matching for those.
//
// The result is safe to share across stacks: team membership is per-org,
// not per-stack.
func ExpandTeams(ctx context.Context, expander TeamExpander, rules ...Rules) (map[string][]string, error) {
	if expander == nil {
		return nil, nil
	}
	want := map[string]struct{}{}
	for _, r := range rules {
		for _, a := range r.Approvers {
			a = strings.TrimPrefix(a, "@")
			if isTeamSlug(a) {
				want[a] = struct{}{}
			}
		}
	}
	if len(want) == 0 {
		return nil, nil
	}

	out := make(map[string][]string, len(want))
	var errs []string
	for slug := range want {
		members, err := expander.ListTeamMembers(ctx, slug)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", slug, err))
			continue
		}
		out[slug] = members
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("team expansion: %s", strings.Join(errs, "; "))
	}
	return out, nil
}
