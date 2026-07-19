package config

import (
	"errors"
	"fmt"
	"log/slog"
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
		if hdr.ConfigType == "" {
			return nil, fmt.Errorf("%s: missing config_type", f)
		}
		if max := maxSchemaVersion(hdr.ConfigType); hdr.Version < 1 || hdr.Version > max {
			if max == 1 {
				return nil, fmt.Errorf("%s: unsupported version %d (want 1)", f, hdr.Version)
			}
			return nil, fmt.Errorf("%s: unsupported version %d (want 1..%d)", f, hdr.Version, max)
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
			// `sinks:` shipped in v0.2.0 and remains a deprecated alias for
			// `channels:`. Prefer `channels:`; both at once is ambiguous.
			if len(d.DeprecatedSinks) > 0 {
				if len(d.Channels) > 0 {
					return nil, fmt.Errorf("%s: both channels: and sinks: are set; sinks: is a deprecated alias for channels: - merge the entries under channels:", f)
				}
				slog.Warn("drift.yaml sinks: is deprecated; rename it to channels: (or run `reeve migrate-config`)", "file", f)
				d.Channels = d.DeprecatedSinks
				d.DeprecatedSinks = nil
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

	// Resolve ${env:NAME} references across every field before use, so
	// credentials and endpoints can be supplied via environment variables.
	cfg.ExpandEnv()

	if cfg.Shared != nil {
		slog.Info("config loaded", "dir", dir, "log_level", cfg.Shared.LogLevel, "log_format", cfg.Shared.LogFormat)
	}
	return cfg, nil
}

// maxSchemaVersion returns the highest supported schema version for a
// config_type. notifications gained v2 (generic `channels:` list) - v1 (the
// legacy `slack:` block) remains fully supported and is mapped onto the
// channel model internally. `reeve migrate-config` rewrites v1 to v2.
func maxSchemaVersion(configType string) int {
	if configType == "notifications" {
		return 2
	}
	return 1
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
	if err := c.validateChannels(); err != nil {
		return err
	}
	return nil
}

// validateChannels checks every channel declaration (notifications.yaml `channels:`
// and drift.yaml `channels:`): `on:` entries must be known event names, and an
// empty subscription list draws a warning because the channel will never fire.
// (The legacy notifications `slack:` block is exempt from the empty-`on`
// warning - it defaults to all events at or after its trigger.)
func (c *Config) validateChannels() error {
	check := func(file string, channels []schemas.ChannelYAML) error {
		for _, s := range channels {
			if s.Type == "" {
				return fmt.Errorf("%s: channel %q: type is required", file, s.EffectiveName())
			}
			for _, ev := range s.On {
				if !schemas.IsValidChannelEvent(ev) {
					return fmt.Errorf("%s: channel %q: unknown event %q in on: (valid: %s)",
						file, s.EffectiveName(), ev, strings.Join(schemas.ValidChannelEvents, ", "))
				}
			}
			if len(s.On) == 0 && s.Type != "slack" {
				slog.Warn("notification channel subscribes to no events and will never fire; set on:",
					"file", file, "channel", s.EffectiveName(), "type", s.Type)
			}
		}
		return nil
	}
	if c.Notifications != nil {
		if err := check("notifications.yaml", c.Notifications.Channels); err != nil {
			return err
		}
		if c.Notifications.Slack != nil {
			for _, ev := range c.Notifications.Slack.Events {
				if !schemas.IsValidChannelEvent(string(ev)) {
					return fmt.Errorf("notifications.yaml: slack.events: unknown event %q (valid: %s)",
						ev, strings.Join(schemas.ValidChannelEvents, ", "))
				}
			}
		}
	}
	if c.Drift != nil {
		if err := check("drift.yaml", c.Drift.Channels); err != nil {
			return err
		}
	}
	return nil
}

// LogSettings returns the configured log level and format, safely handling a
// missing shared config (nil Shared). Commands call this before Validate to
// initialise logging without risking a nil-pointer panic when .reeve/ lacks
// shared.yaml.
func (c *Config) LogSettings() (level, format string) {
	if c == nil || c.Shared == nil {
		return "", ""
	}
	return c.Shared.LogLevel, c.Shared.LogFormat
}

func strictDecode(data []byte, out any) error {
	dec := yaml.NewDecoder(bytesReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	// A second YAML document ("---") was silently ignored before, so any keys
	// in it never took effect - a real footgun for a supposedly strict loader.
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("multiple YAML documents in one file are not supported (found a second document)")
	}
	return nil
}

func nonStrictDecode(data []byte, out any) error {
	dec := yaml.NewDecoder(bytesReader(data))
	return dec.Decode(out)
}
