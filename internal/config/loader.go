package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/thefynx/reeve/internal/config/schemas"
)

// Config is the loaded, validated set of .reeve/*.yaml files.
type Config struct {
	Shared        *schemas.Shared
	Engines       []*schemas.Engine
	Auth          *schemas.Auth
	Notifications *schemas.Notifications
	Drift         *schemas.Drift
	Observability *schemas.Observability
}

// Load reads .reeve/ under root (or root itself if root points at a file),
// validates each file's header, and unmarshals strictly into the matching
// schema. Unknown keys are errors. Exactly one file per config_type except
// engine (unique engine.type per file).
func Load(root string) (*Config, error) {
	dir := filepath.Join(root, ".reeve")
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".yaml") && !strings.HasSuffix(n, ".yml") {
			continue
		}
		files = append(files, filepath.Join(dir, n))
	}
	sort.Strings(files)

	cfg := &Config{}
	seenType := map[string]string{}
	seenEngine := map[string]string{}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var hdr schemas.Header
		if err := strictDecode(data, &hdr); err != nil {
			// header pass is lenient on unknown keys (we re-decode strictly below)
			if err := nonStrictDecode(data, &hdr); err != nil {
				return nil, fmt.Errorf("%s: parse header: %w", f, err)
			}
		}
		if hdr.Version != 1 {
			return nil, fmt.Errorf("%s: unsupported version %d (want 1)", f, hdr.Version)
		}
		if hdr.ConfigType == "" {
			return nil, fmt.Errorf("%s: missing config_type", f)
		}

		switch hdr.ConfigType {
		case "shared":
			if prev, ok := seenType["shared"]; ok {
				return nil, fmt.Errorf("%s: duplicate config_type shared (also in %s)", f, prev)
			}
			seenType["shared"] = f
			var s schemas.Shared
			if err := strictDecode(data, &s); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			cfg.Shared = &s
		case "engine":
			var e schemas.Engine
			if err := strictDecode(data, &e); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			if e.Engine.Type == "" {
				return nil, fmt.Errorf("%s: engine.type is required", f)
			}
			if prev, ok := seenEngine[e.Engine.Type]; ok {
				return nil, fmt.Errorf("%s: duplicate engine.type %q (also in %s)", f, e.Engine.Type, prev)
			}
			seenEngine[e.Engine.Type] = f
			cfg.Engines = append(cfg.Engines, &e)
		case "auth":
			if prev, ok := seenType["auth"]; ok {
				return nil, fmt.Errorf("%s: duplicate config_type auth (also in %s)", f, prev)
			}
			seenType["auth"] = f
			var a schemas.Auth
			if err := strictDecode(data, &a); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			cfg.Auth = &a
		case "notifications":
			if prev, ok := seenType["notifications"]; ok {
				return nil, fmt.Errorf("%s: duplicate config_type notifications (also in %s)", f, prev)
			}
			seenType["notifications"] = f
			var n schemas.Notifications
			if err := strictDecode(data, &n); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			cfg.Notifications = &n
		case "drift":
			if prev, ok := seenType["drift"]; ok {
				return nil, fmt.Errorf("%s: duplicate config_type drift (also in %s)", f, prev)
			}
			seenType["drift"] = f
			var d schemas.Drift
			if err := strictDecode(data, &d); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			cfg.Drift = &d
		case "observability":
			if prev, ok := seenType["observability"]; ok {
				return nil, fmt.Errorf("%s: duplicate config_type observability (also in %s)", f, prev)
			}
			seenType["observability"] = f
			var o schemas.Observability
			if err := strictDecode(data, &o); err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			cfg.Observability = &o
		case "user":
			// Scaffold: accept header but do nothing. Later phases land
			// concrete schemas.
			if prev, ok := seenType[hdr.ConfigType]; ok {
				return nil, fmt.Errorf("%s: duplicate config_type %s (also in %s)", f, hdr.ConfigType, prev)
			}
			seenType[hdr.ConfigType] = f
		default:
			return nil, fmt.Errorf("%s: unknown config_type %q", f, hdr.ConfigType)
		}
	}

	return cfg, nil
}

// Validate runs cross-file checks. Phase 1: require at least one engine
// config and a bucket.
func (c *Config) Validate() error {
	if len(c.Engines) == 0 {
		return errors.New("no engine config found (e.g. .reeve/pulumi.yaml)")
	}
	if c.Shared == nil {
		return errors.New("no shared config found (.reeve/shared.yaml)")
	}
	if c.Shared.Bucket.Type == "" {
		return errors.New("shared.yaml: bucket.type is required")
	}
	return nil
}

func strictDecode(data []byte, out any) error {
	dec := yaml.NewDecoder(bytesReader(data))
	dec.KnownFields(true)
	return dec.Decode(out)
}

func nonStrictDecode(data []byte, out any) error {
	dec := yaml.NewDecoder(bytesReader(data))
	return dec.Decode(out)
}
