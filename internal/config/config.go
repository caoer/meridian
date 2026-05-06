package config

import (
	"fmt"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Config represents a parsed meridian.yaml.
type Config struct {
	Version        string             `yaml:"version"`
	RulePacks      []RulePack         `yaml:"rule_packs"`
	Profiles       map[string]Profile `yaml:"profiles"`
	DefaultProfile string             `yaml:"default_profile"`
	Scan           ScanConfig         `yaml:"scan"`
}

// RulePack is a directory containing rule YAML files.
type RulePack struct {
	Path string `yaml:"path"`
}

// ScanConfig controls filesystem scanning.
type ScanConfig struct {
	Root           string   `yaml:"root"`
	FollowSymlinks bool     `yaml:"follow_symlinks"`
	Skip           []string `yaml:"skip"`
}

// Parse reads meridian.yaml content and resolves paths relative to configDir.
func Parse(data []byte, configDir string) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Resolve rule pack paths relative to config dir
	for i := range cfg.RulePacks {
		p := cfg.RulePacks[i].Path
		if !filepath.IsAbs(p) {
			cfg.RulePacks[i].Path = filepath.Join(configDir, p)
		}
	}

	// Resolve scan root
	if cfg.Scan.Root == "" || cfg.Scan.Root == "." {
		cfg.Scan.Root = configDir
	} else if !filepath.IsAbs(cfg.Scan.Root) {
		cfg.Scan.Root = filepath.Join(configDir, cfg.Scan.Root)
	}

	// Apply default skip dirs
	if cfg.Scan.Skip == nil {
		cfg.Scan.Skip = []string{".git", ".obsidian", "node_modules"}
	}

	return &cfg, nil
}
