package main

import (
	"os"

	// Register the default notification channel set (slack, webhook,
	// pagerduty, github_issue, otel_annotation). A build wanting a subset
	// imports individual channel packages instead.
	_ "github.com/thefynx/reeve/internal/notify/all"
)

func main() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
