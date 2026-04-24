package main

import (
	"context"
	"fmt"

	"github.com/thefynx/reeve/internal/blob"
	bloblocks "github.com/thefynx/reeve/internal/blob/locks"
	"github.com/thefynx/reeve/internal/config"
	"github.com/thefynx/reeve/internal/iac/pulumi"
	"github.com/thefynx/reeve/internal/run"
)

func buildEngine(_ context.Context, cfg *config.Config, _ string) (run.Engine, error) {
	if len(cfg.Engines) == 0 {
		return nil, fmt.Errorf("no engine config found")
	}
	e := cfg.Engines[0]
	switch e.Engine.Type {
	case "pulumi":
		binary := "pulumi"
		if e.Engine.Binary.Path != "" {
			binary = e.Engine.Binary.Path
		}
		return pulumi.New(binary), nil
	default:
		return nil, fmt.Errorf("unsupported engine type %q", e.Engine.Type)
	}
}

func buildApplyEngine(_ context.Context, cfg *config.Config, _ string) (*pulumi.Engine, error) {
	if len(cfg.Engines) == 0 {
		return nil, fmt.Errorf("no engine config found")
	}
	e := cfg.Engines[0]
	switch e.Engine.Type {
	case "pulumi":
		binary := "pulumi"
		if e.Engine.Binary.Path != "" {
			binary = e.Engine.Binary.Path
		}
		return pulumi.New(binary), nil
	default:
		return nil, fmt.Errorf("unsupported engine type %q", e.Engine.Type)
	}
}

func buildLockStore(store blob.Store) *bloblocks.Store {
	return bloblocks.New(store)
}
