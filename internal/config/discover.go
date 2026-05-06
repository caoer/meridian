package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// configNames are the filenames to look for, in order of preference.
var configNames = []string{"meridian.yaml", ".meridian.yaml"}

// Discover walks up from startDir looking for a config file.
// MERIDIAN_CONFIG env var overrides the walk.
func Discover(startDir string) (string, error) {
	// Check env override first
	if envPath := os.Getenv("MERIDIAN_CONFIG"); envPath != "" {
		return ResolveConfigPath(envPath)
	}

	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		for _, name := range configNames {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return "", fmt.Errorf("no meridian.yaml found (searched up from %s)", startDir)
		}
		dir = parent
	}
}

// ResolveConfigPath validates a config path from JSON input.
// Must be a regular file with .yaml/.yml extension.
func ResolveConfigPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving config path: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(abs))
	if ext != ".yaml" && ext != ".yml" {
		return "", fmt.Errorf("config path must have .yaml or .yml extension: %q", path)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("config path not found: %q", path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("config path is not a regular file: %q", path)
	}

	return abs, nil
}
