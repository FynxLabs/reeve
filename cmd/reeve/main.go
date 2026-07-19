package main

import (
	"os"

	// Register the default notification sink set (slack, webhook,
	// pagerduty, github_issue, otel_annotation). A build wanting a subset
	// imports individual sink packages instead.
	_ "github.com/thefynx/reeve/internal/notify/all"
)

func main() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
