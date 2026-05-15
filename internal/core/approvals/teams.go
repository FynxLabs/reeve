package approvals

import (
	"context"
	"fmt"
	"log/slog"
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
		slog.Debug("team expansion: no team slugs in rules, skipping")
		return nil, nil
	}
	slog.Debug("team expansion: resolving slugs", "slugs", slugKeys(want))

	out := make(map[string][]string, len(want))
	var errs []string
	for slug := range want {
		members, err := expander.ListTeamMembers(ctx, slug)
		if err != nil {
			slog.Warn("team expansion failed for slug", "slug", slug, "err", err)
			errs = append(errs, fmt.Sprintf("%s: %v", slug, err))
			continue
		}
		slog.Debug("team expanded", "slug", slug, "member_count", len(members), "members", members)
		out[slug] = members
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("team expansion: %s", strings.Join(errs, "; "))
	}
	return out, nil
}

func slugKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
