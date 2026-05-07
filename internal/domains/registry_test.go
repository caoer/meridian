package domains

import (
	"testing"
)

func TestNewRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if got := len(r.Prefixes()); got != 0 {
		t.Errorf("empty registry: Prefixes() length = %d, want 0", got)
	}
	if got := r.Tree(); len(got) != 0 {
		t.Errorf("empty registry: Tree() length = %d, want 0", len(got))
	}
}

func TestAdd_ExtractsPrefixValue(t *testing.T) {
	r := NewRegistry()
	r.Add([]string{"domain/locus", "type/reference"})

	prefs := r.Prefixes()
	if len(prefs) != 2 {
		t.Fatalf("Prefixes() = %v, want [domain type]", prefs)
	}
	if prefs[0] != "domain" || prefs[1] != "type" {
		t.Errorf("Prefixes() = %v, want [domain type]", prefs)
	}

	d := r.Get("domain")
	if d == nil {
		t.Fatal("Get(domain) = nil")
	}
	if len(d.Values) != 1 || d.Values[0] != "locus" {
		t.Errorf("domain.Values = %v, want [locus]", d.Values)
	}
	if d.Count["locus"] != 1 {
		t.Errorf("domain.Count[locus] = %d, want 1", d.Count["locus"])
	}
}

func TestAdd_CountsMultiplePages(t *testing.T) {
	r := NewRegistry()
	r.Add([]string{"domain/locus"})
	r.Add([]string{"domain/locus"})
	r.Add([]string{"domain/meridian"})

	d := r.Get("domain")
	if d == nil {
		t.Fatal("Get(domain) = nil")
	}
	if d.Count["locus"] != 2 {
		t.Errorf("Count[locus] = %d, want 2", d.Count["locus"])
	}
	if d.Count["meridian"] != 1 {
		t.Errorf("Count[meridian] = %d, want 1", d.Count["meridian"])
	}

	// Values sorted
	if len(d.Values) != 2 {
		t.Fatalf("Values length = %d, want 2", len(d.Values))
	}
	if d.Values[0] != "locus" || d.Values[1] != "meridian" {
		t.Errorf("Values = %v, want [locus meridian]", d.Values)
	}
}

func TestAdd_IgnoresTagsWithoutSlash(t *testing.T) {
	r := NewRegistry()
	r.Add([]string{"standalone", "no-prefix"})

	if got := len(r.Prefixes()); got != 0 {
		t.Errorf("tags without slash: Prefixes() length = %d, want 0", got)
	}
}

func TestAdd_IgnoresEmptyParts(t *testing.T) {
	r := NewRegistry()
	r.Add([]string{"/value", "prefix/", ""})

	if got := len(r.Prefixes()); got != 0 {
		t.Errorf("malformed tags: Prefixes() length = %d, want 0", got)
	}
}

func TestGet_UnknownPrefix(t *testing.T) {
	r := NewRegistry()
	if got := r.Get("nonexistent"); got != nil {
		t.Errorf("Get(nonexistent) = %v, want nil", got)
	}
}

func TestTree_ReturnsAllDomains(t *testing.T) {
	r := NewRegistry()
	r.Add([]string{"domain/locus", "type/reference", "status/active"})

	tree := r.Tree()
	if len(tree) != 3 {
		t.Fatalf("Tree() length = %d, want 3", len(tree))
	}

	for _, prefix := range []string{"domain", "type", "status"} {
		if _, ok := tree[prefix]; !ok {
			t.Errorf("Tree() missing prefix %q", prefix)
		}
	}
}

func TestPrefixes_Sorted(t *testing.T) {
	r := NewRegistry()
	r.Add([]string{"type/reference", "domain/locus", "area/wiki"})

	prefs := r.Prefixes()
	if len(prefs) != 3 {
		t.Fatalf("Prefixes() length = %d, want 3", len(prefs))
	}
	if prefs[0] != "area" || prefs[1] != "domain" || prefs[2] != "type" {
		t.Errorf("Prefixes() = %v, want [area domain type]", prefs)
	}
}

func TestAdd_MultiSlashUsesFirstSegment(t *testing.T) {
	r := NewRegistry()
	r.Add([]string{"domain/locus/sub"})

	d := r.Get("domain")
	if d == nil {
		t.Fatal("Get(domain) = nil")
	}
	// value should be "locus/sub" (everything after first slash)
	if len(d.Values) != 1 || d.Values[0] != "locus/sub" {
		t.Errorf("Values = %v, want [locus/sub]", d.Values)
	}
}
