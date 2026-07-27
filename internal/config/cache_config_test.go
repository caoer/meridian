package config

import "testing"

func TestCacheEnabled(t *testing.T) {
	tru, fls := true, false
	cases := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{"nil config", nil, true},
		{"no cache section", &Config{}, true},
		{"cache section, enabled unset", &Config{Cache: &CacheConfig{}}, true},
		{"explicit enabled true", &Config{Cache: &CacheConfig{Enabled: &tru}}, true},
		{"explicit enabled false", &Config{Cache: &CacheConfig{Enabled: &fls}}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.CacheEnabled(); got != tc.want {
			t.Errorf("%s: CacheEnabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestParseCacheSection(t *testing.T) {
	cfg, err := Parse([]byte("version: \"1\"\ncache:\n  enabled: false\n"), t.TempDir())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Cache == nil || cfg.Cache.Enabled == nil || *cfg.Cache.Enabled {
		t.Fatalf("cache.enabled=false not parsed: %+v", cfg.Cache)
	}
	if cfg.CacheEnabled() {
		t.Error("CacheEnabled() = true, want false")
	}

	// Absent cache section defaults to enabled.
	def, err := Parse([]byte("version: \"1\"\n"), t.TempDir())
	if err != nil {
		t.Fatalf("parse default: %v", err)
	}
	if !def.CacheEnabled() {
		t.Error("absent cache section: CacheEnabled() = false, want true (on by default)")
	}
}
