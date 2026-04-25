package cli

import (
	"context"

	"github.com/thefynx/reeve/internal/drift"
	"github.com/thefynx/reeve/internal/drift/sinks"
)

func dispatchSinks(ctx context.Context, list []sinks.Sink, out *drift.RunOutput) []error {
	return sinks.Dispatch(ctx, list, out)
}
