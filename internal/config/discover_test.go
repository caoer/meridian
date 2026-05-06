package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover_WalkUp(t *testing.T) {
	// Create: /tmp/root/sub/deep/ with meridian.yaml at root
	root := t.TempDir()
	sub := filepath.Join(root, "sub", "deep")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "meridian.yaml")
	if err := os.WriteFile(configPath, []byte("rule_packs:\n  - path: ./rules\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(sub)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if got != configPath {
		t.Errorf("Discover = %q, want %q", got, configPath)
	}
}

func TestDiscover_DotMeridian(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".meridian.yaml")
	if err := os.WriteFile(configPath, []byte("rule_packs:\n  - path: ./rules\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if got != configPath {
		t.Errorf("Discover = %q, want %q", got, configPath)
	}
}

func TestDiscover_EnvOverride(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "custom.yaml")
	if err := os.WriteFile(configPath, []byte("rule_packs:\n  - path: ./rules\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MERIDIAN_CONFIG", configPath)

	got, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if got != configPath {
		t.Errorf("Discover = %q, want %q", got, configPath)
	}
}

func TestDiscover_NotFound(t *testing.T) {
	root := t.TempDir()
	_, err := Discover(root)
	if err == nil {
		t.Fatal("expected error when no config found")
	}
}

func TestDiscover_JSONPathOverride(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "my-config.yaml")
	if err := os.WriteFile(configPath, []byte("rule_packs:\n  - path: ./rules\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveConfigPath(configPath)
	if err != nil {
		t.Fatalf("ResolveConfigPath error: %v", err)
	}
	if got != configPath {
		t.Errorf("ResolveConfigPath = %q, want %q", got, configPath)
	}
}

func TestDiscover_JSONPathInvalidExt(t *testing.T) {
	root := t.TempDir()
	badPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(badPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveConfigPath(badPath)
	if err == nil {
		t.Fatal("expected error for non-yaml extension")
	}
}

func TestDiscover_JSONPathMissing(t *testing.T) {
	_, err := ResolveConfigPath("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
