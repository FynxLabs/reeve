package notify

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/thefynx/reeve/internal/blob"
	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/observability/annotations"
)

// IssueClient is the narrow, consumer-defined VCS surface the github_issue
// sink needs. internal/vcs/github.Client satisfies it; sinks never import a
// VCS SDK directly (modularity contract).
type IssueClient interface {
	// FindIssueByMarker returns the number of the first open issue whose
	// body contains marker, or found=false if none.
	FindIssueByMarker(ctx context.Context, marker string) (number int, found bool, err error)
	// CreateIssue opens a new issue and returns its number.
	CreateIssue(ctx context.Context, title, body string, labels, assignees []string) (int, error)
	// UpdateIssue rewrites an existing issue's title and body.
	UpdateIssue(ctx context.Context, number int, title, body string) error
	// CloseIssue closes an issue, rewriting its body.
	CloseIssue(ctx context.Context, number int, body string) error
}

// CommentClient is the narrow PR-comment surface the timeline sink needs.
// internal/vcs/github.Client satisfies it; the run pipeline's comment poster
// does too. Marker-based upsert keeps one comment per key, edited in place.
type CommentClient interface {
	// UpsertComment finds the PR comment containing marker and rewrites it,
	// or creates it when absent.
	UpsertComment(ctx context.Context, number int, body, marker string) error
}

// Deps carries runtime dependencies sinks may need. Fields are optional; a
// constructor whose dependencies are missing returns (nil, nil) and the sink
// is skipped, matching the previous factory behavior.
type Deps struct {
	// HTTP is the shared client for outbound deliveries. Nil defaults to
	// SharedHTTPClient() (sane timeout).
	HTTP HTTPDoer
	// Blob persists sink state (e.g. the Slack per-PR message ID).
	Blob blob.Store
	// Issues backs the github_issue sink.
	Issues IssueClient
	// Comments backs the timeline_github sink (PR comment upserts).
	Comments CommentClient
	// Emitters back the otel_annotation sink.
	Emitters []annotations.Emitter
	// SlackToken is the fallback bot token when a sink config carries no
	// auth_token (drift.yaml slack sinks read SLACK_BOT_TOKEN).
	SlackToken string
	// RepoFull is "owner/repo" from the CI environment, used in payloads
	// that need a repo reference.
	RepoFull string
}

// Constructor builds a sink from its config entry plus runtime deps.
// Returning (nil, nil) skips the sink (unmet optional dependency).
type Constructor func(ctx context.Context, cfg schemas.SinkYAML, deps Deps) (Sink, error)

var (
	regMu    sync.RWMutex
	registry = map[string]Constructor{}
)

// Register makes a sink type available to Build. Sinks call it from init();
// importing a sink package (internal/notify/all imports the default set) is
// what compiles it in. Registering a duplicate type panics.
func Register(typ string, c Constructor) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[typ]; dup {
		panic(fmt.Sprintf("notify: duplicate sink type %q", typ))
	}
	registry[typ] = c
}

// Registered returns the sorted list of registered sink types.
func Registered() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Build resolves each config entry to its registered constructor, purely by
// the `type:` string. Disabled entries are skipped; constructors may skip
// themselves by returning (nil, nil). An unregistered type is an error.
func Build(ctx context.Context, cfgs []schemas.SinkYAML, deps Deps) ([]Sink, error) {
	if deps.HTTP == nil {
		deps.HTTP = SharedHTTPClient()
	}
	var out []Sink
	for _, cfg := range cfgs {
		if !cfg.IsEnabled() {
			continue
		}
		regMu.RLock()
		ctor, ok := registry[cfg.Type]
		regMu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("unknown notification sink type %q (registered: %v)", cfg.Type, Registered())
		}
		s, err := ctor(ctx, cfg, deps)
		if err != nil {
			return nil, fmt.Errorf("sink %s: %w", cfg.EffectiveName(), err)
		}
		if s != nil {
			out = append(out, s)
		}
	}
	return out, nil
}
