package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Migrator applies per-config_type version bumps. Each registered
// migration converts version N→N+1 for a single file.
type Migrator struct {
	registry map[migrationKey]migrationFn
}

type migrationKey struct {
	ConfigType string
	From       int
}

type migrationFn func(*yaml.Node) error

// NewMigrator returns a Migrator with the built-in migrations registered.
func NewMigrator() *Migrator {
	m := &Migrator{registry: map[migrationKey]migrationFn{}}
	m.registry[migrationKey{ConfigType: "notifications", From: 1}] = migrateNotificationsV1ToV2
	return m
}

// migrateNotificationsV1ToV2 rewrites the legacy `slack:` block into an
// entry of the generic `sinks:` list:
//
//	slack:                      sinks:
//	  enabled: true               - type: slack
//	  channel: "#x"        →        enabled: true
//	  events: [plan]                channel: "#x"
//	                                on: [plan]
//
// All other keys (auth_token, trigger, icons, rules) carry over verbatim;
// `events` is renamed to `on`. Value nodes are reused so comments and
// styles survive. A file without a `slack:` block only gets its version
// bumped. The runtime keeps accepting v1, so running this is optional.
func migrateNotificationsV1ToV2(root *yaml.Node) error {
	m := docMapping(root)
	if m == nil {
		return fmt.Errorf("notifications: expected a mapping document")
	}
	slackIdx := -1
	sinksIdx := -1
	for i := 0; i < len(m.Content); i += 2 {
		switch m.Content[i].Value {
		case "slack":
			slackIdx = i
		case "sinks":
			sinksIdx = i
		}
	}
	if slackIdx < 0 {
		return nil // nothing to rewrite; version bump only
	}
	slackVal := m.Content[slackIdx+1]
	if slackVal.Kind != yaml.MappingNode {
		return fmt.Errorf("notifications: slack: expected a mapping")
	}

	sink := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	sink.Content = append(sink.Content, scalarNode("type"), scalarNode("slack"))
	for i := 0; i < len(slackVal.Content); i += 2 {
		k, v := slackVal.Content[i], slackVal.Content[i+1]
		if k.Value == "events" {
			k = scalarNode("on")
			k.HeadComment = slackVal.Content[i].HeadComment
			k.LineComment = slackVal.Content[i].LineComment
		}
		sink.Content = append(sink.Content, k, v)
	}

	if sinksIdx >= 0 {
		// A sinks list already exists (mixed v1 file): append the converted
		// slack entry and drop the slack key.
		seq := m.Content[sinksIdx+1]
		if seq.Kind != yaml.SequenceNode {
			return fmt.Errorf("notifications: sinks: expected a sequence")
		}
		seq.Content = append(seq.Content, sink)
		m.Content = append(m.Content[:slackIdx], m.Content[slackIdx+2:]...)
		return nil
	}

	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{sink}}
	m.Content[slackIdx] = scalarNode("sinks")
	m.Content[slackIdx].HeadComment = slackVal.HeadComment
	m.Content[slackIdx+1] = seq
	return nil
}

// docMapping unwraps a document node to its top-level mapping.
func docMapping(root *yaml.Node) *yaml.Node {
	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	return n
}

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// MigrateDirectory walks .reeve/ and migrates each file to the latest
// supported version. dryRun=true skips writes; diffs are printed.
func (m *Migrator) MigrateDirectory(dir string, dryRun bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".yaml") && !strings.HasSuffix(n, ".yml") {
			continue
		}
		path := filepath.Join(dir, n)
		if err := m.migrateOne(path, dryRun); err != nil {
			return fmt.Errorf("%s: %w", n, err)
		}
	}
	return nil
}

func (m *Migrator) migrateOne(path string, dryRun bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Extract header without strict unmarshal so old files still parse.
	// Match the two keys independently so any order and interleaved comments
	// or blank lines are tolerated (the loader accepts them, so migrate must
	// too).
	vm := versionRE.FindSubmatch(data)
	ctm := configTypeRE.FindSubmatch(data)
	if vm == nil || ctm == nil {
		return fmt.Errorf("cannot find version + config_type header")
	}
	fromVer := 0
	if _, err := fmt.Sscanf(string(vm[1]), "%d", &fromVer); err != nil {
		return err
	}
	configType := string(ctm[1])

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}

	changed := false
	cur := fromVer
	for {
		fn, ok := m.registry[migrationKey{ConfigType: configType, From: cur}]
		if !ok {
			break
		}
		if err := fn(&root); err != nil {
			return err
		}
		if err := setScalarField(&root, "version", fmt.Sprintf("%d", cur+1)); err != nil {
			return err
		}
		cur++
		changed = true
	}

	if !changed {
		return nil
	}
	out, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Printf("--- would migrate %s (v%d → v%d) ---\n%s\n", path, fromVer, cur, string(out))
		return nil
	}
	backup := path + ".bak"
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return os.WriteFile(path, out, 0o600)
}

var (
	versionRE    = regexp.MustCompile(`(?m)^version:\s*(\d+)\s*$`)
	configTypeRE = regexp.MustCompile(`(?m)^config_type:\s*([a-z_]+)\s*$`)
)

func setScalarField(root *yaml.Node, key, value string) error {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		return setScalarField(root.Content[0], key, value)
	}
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping, got %v", root.Kind)
	}
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1].Value = value
			return nil
		}
	}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
	return nil
}
