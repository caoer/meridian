package config

import (
	"testing"

	"github.com/caoer/meridian/internal/rules"
)

func TestConfig_ParseMinimal(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(cfg.RulePacks) != 1 {
		t.Fatalf("RulePacks = %d, want 1", len(cfg.RulePacks))
	}
	if cfg.RulePacks[0].Path != "/project/rules" {
		t.Errorf("RulePacks[0].Path = %q, want /project/rules", cfg.RulePacks[0].Path)
	}
}

func TestConfig_ParseFull(t *testing.T) {
	raw := `
version: "0.1"
rule_packs:
  - path: "./rules/core"
  - path: "./rules/locus"
profiles:
  strict:
    include: ["*"]
    exclude: []
    override:
      backticked-wikilink:
        severity: error
  minimal:
    include: ["required-fields", "tag-format"]
default_profile: strict
scan:
  root: "."
  follow_symlinks: false
  skip:
    - ".git"
    - ".obsidian"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if cfg.Version != "0.1" {
		t.Errorf("Version = %q", cfg.Version)
	}
	if len(cfg.RulePacks) != 2 {
		t.Fatalf("RulePacks = %d, want 2", len(cfg.RulePacks))
	}
	if cfg.DefaultProfile != "strict" {
		t.Errorf("DefaultProfile = %q", cfg.DefaultProfile)
	}
	if len(cfg.Profiles) != 2 {
		t.Fatalf("Profiles = %d, want 2", len(cfg.Profiles))
	}
	if cfg.Scan.Root != "/project" {
		t.Errorf("Scan.Root = %q, want /project", cfg.Scan.Root)
	}
}

func TestConfig_Strict(t *testing.T) {
	cfg, err := Parse([]byte("strict: true\n"), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !cfg.Strict {
		t.Error("Strict = false, want true")
	}

	cfg, err = Parse([]byte("version: \"0.1\"\n"), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if cfg.Strict {
		t.Error("Strict = true, want false default")
	}
}

func TestConfig_Defaults(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// Default skip dirs
	if len(cfg.Scan.Skip) != 3 {
		t.Errorf("default skip = %v, want 3 items", cfg.Scan.Skip)
	}
	if cfg.Scan.FollowSymlinks {
		t.Error("follow_symlinks should default to false")
	}
}

// --- Profile resolution ---

func TestProfile_IncludeAll(t *testing.T) {
	profile := Profile{
		Include: []string{"*"},
	}
	allRules := []rules.Rule{
		{ID: "required-fields", Severity: rules.SeverityError},
		{ID: "tag-format", Severity: rules.SeverityError},
		{ID: "filename-check", Severity: rules.SeverityWarn},
	}

	result := profile.Resolve(allRules)
	if len(result) != 3 {
		t.Errorf("got %d rules, want 3", len(result))
	}
}

func TestProfile_IncludeGlob(t *testing.T) {
	profile := Profile{
		Include: []string{"required-*"},
	}
	allRules := []rules.Rule{
		{ID: "required-fields", Severity: rules.SeverityError},
		{ID: "tag-format", Severity: rules.SeverityError},
	}

	result := profile.Resolve(allRules)
	if len(result) != 1 {
		t.Fatalf("got %d rules, want 1", len(result))
	}
	if result[0].ID != "required-fields" {
		t.Errorf("ID = %q", result[0].ID)
	}
}

func TestProfile_ExcludeWins(t *testing.T) {
	profile := Profile{
		Include: []string{"*"},
		Exclude: []string{"tag-*"},
	}
	allRules := []rules.Rule{
		{ID: "required-fields", Severity: rules.SeverityError},
		{ID: "tag-format", Severity: rules.SeverityError},
	}

	result := profile.Resolve(allRules)
	if len(result) != 1 {
		t.Fatalf("got %d rules, want 1", len(result))
	}
	if result[0].ID != "required-fields" {
		t.Errorf("ID = %q", result[0].ID)
	}
}

func TestProfile_SeverityOverride(t *testing.T) {
	profile := Profile{
		Include: []string{"*"},
		Override: map[string]Override{
			"tag-format": {Severity: "warn"},
		},
	}
	allRules := []rules.Rule{
		{ID: "tag-format", Severity: rules.SeverityError},
	}

	result := profile.Resolve(allRules)
	if len(result) != 1 {
		t.Fatal("expected 1 rule")
	}
	if result[0].Severity != rules.SeverityWarn {
		t.Errorf("severity = %v, want warn", result[0].Severity)
	}
}

// --- Watch config ---

func TestConfig_WatchSection(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
watch:
  debounce_ms: 3000
  ignore: [".git/**", "*.tmp"]
  hooks:
    on-create:
      action: "check"
      scope: "{{.Path}}"
    on-modify:
      action: "check"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if cfg.Watch == nil {
		t.Fatal("Watch is nil")
	}
	if cfg.Watch.DebounceMs != 3000 {
		t.Errorf("DebounceMs = %d, want 3000", cfg.Watch.DebounceMs)
	}
	if len(cfg.Watch.Ignore) != 2 {
		t.Errorf("Ignore = %v, want 2 items", cfg.Watch.Ignore)
	}
	if len(cfg.Watch.Hooks) != 2 {
		t.Errorf("Hooks = %d, want 2", len(cfg.Watch.Hooks))
	}
}

func TestConfig_NoWatchSection(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if cfg.Watch != nil {
		t.Errorf("Watch should be nil, got %+v", cfg.Watch)
	}
}

func TestConfig_WatchNegativeDebounce(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
watch:
  debounce_ms: -1
  hooks:
    on-create:
      action: "check"
`
	_, err := Parse([]byte(raw), "/project")
	if err == nil {
		t.Fatal("expected error for negative debounce_ms")
	}
}

func TestConfig_WatchDefaultDebounce(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
watch:
  hooks:
    on-create:
      action: "check"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if cfg.Watch.DebounceMs != 6000 {
		t.Errorf("DebounceMs = %d, want 6000 (default)", cfg.Watch.DebounceMs)
	}
}

func TestConfig_WatchInvalidIgnoreGlob(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
watch:
  ignore: ["[invalid"]
  hooks:
    on-create:
      action: "check"
`
	_, err := Parse([]byte(raw), "/project")
	if err == nil {
		t.Fatal("expected error for invalid glob in ignore")
	}
}

func TestConfig_ForeignRoots(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
foreign_roots:
  - "foreign"
  - "mirrors/team-wiki"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(cfg.ForeignRoots) != 2 {
		t.Fatalf("ForeignRoots = %d, want 2", len(cfg.ForeignRoots))
	}
	if cfg.ForeignRoots[0] != "foreign" {
		t.Errorf("ForeignRoots[0] = %q, want foreign", cfg.ForeignRoots[0])
	}
	if cfg.ForeignRoots[1] != "mirrors/team-wiki" {
		t.Errorf("ForeignRoots[1] = %q, want mirrors/team-wiki", cfg.ForeignRoots[1])
	}
}

func TestConfig_ForeignRoots_TrailingSlashNormalized(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
foreign_roots:
  - "foreign/"
  - "mirrors/team/"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if cfg.ForeignRoots[0] != "foreign" {
		t.Errorf("ForeignRoots[0] = %q, want trailing slash stripped to \"foreign\"", cfg.ForeignRoots[0])
	}
	if cfg.ForeignRoots[1] != "mirrors/team" {
		t.Errorf("ForeignRoots[1] = %q, want trailing slash stripped to \"mirrors/team\"", cfg.ForeignRoots[1])
	}
}

func TestConfig_ForeignRoots_DefaultEmpty(t *testing.T) {
	raw := `
rule_packs:
  - path: "./rules"
`
	cfg, err := Parse([]byte(raw), "/project")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(cfg.ForeignRoots) != 0 {
		t.Errorf("ForeignRoots should default to empty, got %v", cfg.ForeignRoots)
	}
}

func TestProfile_EmptyInclude_DefaultAll(t *testing.T) {
	// Empty include defaults to ["*"]
	profile := Profile{}
	allRules := []rules.Rule{
		{ID: "a", Severity: rules.SeverityWarn},
		{ID: "b", Severity: rules.SeverityError},
	}

	result := profile.Resolve(allRules)
	if len(result) != 2 {
		t.Errorf("got %d rules, want 2", len(result))
	}
}
