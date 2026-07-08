package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/caoer/meridian/internal/hooks"
	"go.yaml.in/yaml/v3"
)

// Config represents a parsed meridian.yaml.
type Config struct {
	Version        string                    `yaml:"version"`
	Role           string                    `yaml:"role"` // companion-repo role (C45); wikis declare wiki-role: in LLM_WIKI.md instead
	RulePacks      []RulePack                `yaml:"rule_packs"`
	Profiles       map[string]Profile        `yaml:"profiles"`
	DefaultProfile string                    `yaml:"default_profile"`
	Scan           ScanConfig                `yaml:"scan"`
	Watch          *WatchConfig              `yaml:"watch"`
	ForeignRoots   []string                  `yaml:"foreign_roots"` // root-relative dirs whose files resolve for link-checking but are never linted
	RuleParams     map[string]map[string]any `yaml:"rule_params"`
}

// WatchConfig controls the md watch daemon.
type WatchConfig struct {
	DebounceMs int                      `yaml:"debounce_ms"`
	Ignore     []string                 `yaml:"ignore"`
	Hooks      map[string]hooks.HookDef `yaml:"hooks"`

	// OnChangeTimeoutMs bounds each md-on-change task run (default 2m) —
	// a wedged block must not wedge the daemon loop.
	OnChangeTimeoutMs int `yaml:"on_change_timeout_ms"`
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
	MaxFileSize    int64    `yaml:"max_file_size"` // bytes; 0 = no limit
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

	// Normalize foreign roots: strip trailing slashes to prevent
	// silent prefix-match failures (root+"/" would produce "foreign//").
	for i, fr := range cfg.ForeignRoots {
		cfg.ForeignRoots[i] = strings.TrimRight(fr, "/")
	}

	// Validate watch config
	if cfg.Watch != nil {
		if cfg.Watch.DebounceMs < 0 {
			return nil, fmt.Errorf("watch: debounce_ms must be non-negative, got %d", cfg.Watch.DebounceMs)
		}
		if cfg.Watch.DebounceMs == 0 {
			cfg.Watch.DebounceMs = 6000
		}
		if cfg.Watch.OnChangeTimeoutMs < 0 {
			return nil, fmt.Errorf("watch: on_change_timeout_ms must be non-negative, got %d", cfg.Watch.OnChangeTimeoutMs)
		}
		if cfg.Watch.OnChangeTimeoutMs == 0 {
			cfg.Watch.OnChangeTimeoutMs = 120000
		}
		for _, pattern := range cfg.Watch.Ignore {
			if !doublestar.ValidatePattern(pattern) {
				return nil, fmt.Errorf("watch: invalid ignore glob %q", pattern)
			}
		}
		if _, err := hooks.ParseHooks(cfg.Watch.Hooks); err != nil {
			return nil, fmt.Errorf("watch: %w", err)
		}
	}

	return &cfg, nil
}
