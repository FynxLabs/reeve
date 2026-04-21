// Package factory builds a slice of configured Sinks from drift.yaml.
package factory

import (
	"context"
	"fmt"
	"net/http"

	gh "github.com/google/go-github/v66/github"
	"golang.org/x/oauth2"

	"github.com/thefynx/reeve/internal/config/schemas"
	"github.com/thefynx/reeve/internal/drift"
	"github.com/thefynx/reeve/internal/drift/sinks"
	"github.com/thefynx/reeve/internal/drift/sinks/github_issue"
	otelsink "github.com/thefynx/reeve/internal/drift/sinks/otel"
	"github.com/thefynx/reeve/internal/drift/sinks/pagerduty"
	slacksink "github.com/thefynx/reeve/internal/drift/sinks/slack"
	"github.com/thefynx/reeve/internal/drift/sinks/webhook"
	"github.com/thefynx/reeve/internal/observability/annotations"
	"github.com/thefynx/reeve/internal/slack"
)

// Options carries dependencies needed to construct sinks.
type Options struct {
	SlackToken         string
	GitHubToken        string
	GitHubOwner        string
	GitHubRepo         string
	AnnotationEmitters []annotations.Emitter // for otel_annotation sink
}

// Build returns the ordered list of Sinks.
func Build(ctx context.Context, cfg *schemas.Drift, opts Options) ([]sinks.Sink, error) {
	if cfg == nil {
		return nil, nil
	}
	var out []sinks.Sink
	for _, sk := range cfg.Sinks {
		events := parseEvents(sk.On)
		switch sk.Type {
		case "slack":
			if opts.SlackToken == "" {
				continue
			}
			out = append(out, &slacksink.Sink{
				Name_:   nameOr(sk.Name, "slack"),
				Client:  slack.New(opts.SlackToken),
				Channel: sk.Channel,
				Events:  events,
			})
		case "webhook":
			out = append(out, &webhook.Sink{
				Name_:   nameOr(sk.Name, "webhook"),
				URL:     sk.URL,
				Headers: sk.Headers,
				Events:  events,
				Format:  sk.Payload.Format,
			})
		case "pagerduty":
			out = append(out, &pagerduty.Sink{
				Name_:          nameOr(sk.Name, "pagerduty"),
				IntegrationKey: sk.IntegrationKey,
				SeverityMap:    sk.SeverityMap,
				Events:         events,
			})
		case "github_issue":
			if opts.GitHubToken == "" || opts.GitHubOwner == "" || opts.GitHubRepo == "" {
				continue
			}
			client := newGHClient(ctx, opts.GitHubToken)
			out = append(out, &github_issue.Sink{
				Name_:     nameOr(sk.Name, "github_issue"),
				Client:    client,
				Owner:     opts.GitHubOwner,
				Repo:      opts.GitHubRepo,
				Labels:    sk.Labels,
				Assignees: sk.Assignees,
				Events:    events,
			})
		case "otel_annotation":
			if len(opts.AnnotationEmitters) == 0 {
				continue
			}
			out = append(out, &otelsink.Sink{
				Name_:    nameOr(sk.Name, "otel_annotation"),
				Events:   events,
				Emitters: opts.AnnotationEmitters,
			})
		default:
			return nil, fmt.Errorf("unknown drift sink type %q", sk.Type)
		}
	}
	return out, nil
}

func parseEvents(on []string) []sinks.Event {
	out := make([]sinks.Event, 0, len(on))
	for _, s := range on {
		switch s {
		case "drift_detected":
			out = append(out, drift.EventDriftDetected)
		case "drift_ongoing":
			out = append(out, drift.EventDriftOngoing)
		case "drift_resolved":
			out = append(out, drift.EventDriftResolved)
		case "check_failed":
			out = append(out, drift.EventCheckFailed)
		}
	}
	return out
}

func nameOr(n, fallback string) string {
	if n != "" {
		return n
	}
	return fallback
}

func newGHClient(ctx context.Context, token string) *gh.Client {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return gh.NewClient(oauth2.NewClient(ctx, src))
}

// httpClient is retained for symmetry with other factories.
var _ = http.DefaultClient
