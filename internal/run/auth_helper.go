package run

import (
	"context"
	"fmt"

	"github.com/thefynx/reeve/internal/auth"
	"github.com/thefynx/reeve/internal/config/schemas"
)

// ResolveAuthEnv returns the merged env var map for a single stack +
// mode. If cfg is nil or has no bindings, returns empty env (relies on
// ambient creds / $GITHUB_TOKEN / etc).
func ResolveAuthEnv(ctx context.Context, cfg *schemas.Auth, registry *auth.Registry, stackRef string, mode auth.Mode) (map[string]string, error) {
	if cfg == nil || registry == nil {
		return nil, nil
	}
	bindings := make([]auth.Binding, 0, len(cfg.Bindings))
	for _, b := range cfg.Bindings {
		bindings = append(bindings, auth.Binding{
			StackPattern: b.Match.Stack,
			Mode:         auth.Mode(b.Match.Mode),
			Providers:    b.Providers,
			Override:     b.Override,
		})
	}
	names := auth.Resolve(bindings, stackRef, mode)
	if len(names) == 0 {
		return nil, nil
	}
	env, _, err := registry.AcquireAll(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("acquire creds for %s (%s): %w", stackRef, mode, err)
	}
	return env, nil
}
