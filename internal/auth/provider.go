package auth

import (
	"context"
	"fmt"
	"time"
)

// Credential is a resolved credential for a run. Providers return these;
// the run layer materializes them into env vars for the IaC engine.
type Credential struct {
	// Env vars to set for the engine process.
	Env map[string]string
	// Kind describes the credential shape for display / lint.
	Kind string
	// Source is the provider name that produced this credential.
	Source string
	// ExpiresAt is zero if the credential has no expiry (e.g. env passthrough).
	ExpiresAt time.Time
}

// Provider is the interface every auth adapter satisfies. Implementations
// live under internal/auth/providers/<type>/.
type Provider interface {
	Name() string
	Type() string
	// Acquire resolves a credential for the given run context. ctx is
	// bounded by the run timeout; providers honor it.
	Acquire(ctx context.Context) (*Credential, error)
}

// Registry wires names → constructed Provider instances.
type Registry struct {
	byName map[string]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{byName: map[string]Provider{}} }

// Register adds a provider. Duplicate names error.
func (r *Registry) Register(p Provider) error {
	if _, ok := r.byName[p.Name()]; ok {
		return fmt.Errorf("duplicate provider name %q", p.Name())
	}
	r.byName[p.Name()] = p
	return nil
}

// Get returns a provider by name.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// AcquireAll resolves the full credential set for a (stack, mode) pair by
// walking the binding list. Later providers' env vars override earlier
// ones on key conflict; callers can detect this by inspecting Credential.Env
// individually.
func (r *Registry) AcquireAll(ctx context.Context, names []string) (map[string]string, []*Credential, error) {
	merged := map[string]string{}
	creds := make([]*Credential, 0, len(names))
	for _, n := range names {
		p, ok := r.Get(n)
		if !ok {
			return nil, nil, fmt.Errorf("provider %q not registered", n)
		}
		c, err := p.Acquire(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("acquire %s (%s): %w", n, p.Type(), err)
		}
		for k, v := range c.Env {
			merged[k] = v
		}
		creds = append(creds, c)
	}
	return merged, creds, nil
}

// RefuseLocalInCI returns an error if the given provider type is a
// local-dev type and CI=true is set in the environment. Called by local
// provider Acquire methods. See openspec/specs/auth.
func RefuseLocalInCI(providerType, envCI string) error {
	local := map[string]bool{"aws_profile": true, "aws_sso": true, "gcloud_adc": true}
	if local[providerType] && (envCI == "true" || envCI == "1") {
		return fmt.Errorf("provider type %q refuses to run under CI=true", providerType)
	}
	return nil
}
