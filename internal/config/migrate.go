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
//
// v1 is the only supported version today, so the registry is empty.
// Adding a migration for e.g. shared v1→v2 looks like:
//
//	registry[migrationKey{ConfigType: "shared", From: 1}] = func(node *yaml.Node) error { ... }
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
	// Example placeholder - wired when v2 lands.
	// m.registry[migrationKey{"shared", 1}] = migrateSharedV1ToV2
	return m
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
	header := headerRE.FindSubmatch(data)
	if len(header) < 3 {
		return fmt.Errorf("cannot find version + config_type header")
	}
	// header submatches: 1=version, 2=config_type
	fromVer := 0
	if _, err := fmt.Sscanf(string(header[1]), "%d", &fromVer); err != nil {
		return err
	}
	configType := string(header[2])

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

var headerRE = regexp.MustCompile(`(?m)^version:\s*(\d+)\s*\n.*?^config_type:\s*([a-z]+)`)

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
