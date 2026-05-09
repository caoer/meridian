package property

import (
	"strings"
	"testing"
)

func TestParse_SingleStringKey(t *testing.T) {
	params := map[string]any{
		"property": "title",
		"required": false,
	}
	c, err := Parse(params)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Keys) != 1 || c.Keys[0] != "title" {
		t.Errorf("Keys = %v, want [title]", c.Keys)
	}
}

func TestParse_ArrayKeys(t *testing.T) {
	params := map[string]any{
		"property": []any{"tags", "created", "author"},
		"required": false,
	}
	c, err := Parse(params)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Keys) != 3 {
		t.Fatalf("Keys len = %d, want 3", len(c.Keys))
	}
	want := []string{"tags", "created", "author"}
	for i, k := range c.Keys {
		if k != want[i] {
			t.Errorf("Keys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestParse_RequiredTrue(t *testing.T) {
	params := map[string]any{
		"property": "title",
		"required": true,
	}
	c, err := Parse(params)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Required {
		t.Error("Required = false, want true")
	}
}

func TestParse_RequiredOmitted(t *testing.T) {
	params := map[string]any{
		"property": "title",
	}
	c, err := Parse(params)
	if err != nil {
		t.Fatal(err)
	}
	if c.Required {
		t.Error("Required = true, want false (default)")
	}
}

func TestParse_TypeBlockDetected(t *testing.T) {
	params := map[string]any{
		"property": "source",
		"required": false,
		"wikilink": map[string]any{"resolve": true},
	}
	c, err := Parse(params)
	if err != nil {
		t.Fatal(err)
	}
	if c.TypeName != "wikilink" {
		t.Errorf("TypeName = %q, want wikilink", c.TypeName)
	}
	m, ok := c.TypeRaw.(map[string]any)
	if !ok {
		t.Fatalf("TypeRaw type = %T, want map", c.TypeRaw)
	}
	if m["resolve"] != true {
		t.Errorf("TypeRaw[resolve] = %v", m["resolve"])
	}
}

func TestParse_DateBoolTypeBlock(t *testing.T) {
	params := map[string]any{
		"property": "created",
		"required": false,
		"date":     true,
	}
	c, err := Parse(params)
	if err != nil {
		t.Fatal(err)
	}
	if c.TypeName != "date" {
		t.Errorf("TypeName = %q, want date", c.TypeName)
	}
	if c.TypeRaw != true {
		t.Errorf("TypeRaw = %v, want true", c.TypeRaw)
	}
}

func TestParse_NoTypeBlock(t *testing.T) {
	params := map[string]any{
		"property": "title",
		"required": true,
	}
	c, err := Parse(params)
	if err != nil {
		t.Fatal(err)
	}
	if c.TypeName != "" {
		t.Errorf("TypeName = %q, want empty (existence-only)", c.TypeName)
	}
}

func TestParse_DeterministicTypeBlock(t *testing.T) {
	params := map[string]any{
		"property": "title",
		"required": false,
	}
	c, err := Parse(params)
	if err != nil {
		t.Fatal(err)
	}
	if c.TypeName != "" {
		t.Errorf("TypeName = %q, want empty for no type block", c.TypeName)
	}
}

func TestParse_MissingProperty(t *testing.T) {
	params := map[string]any{
		"required": true,
	}
	_, err := Parse(params)
	if err == nil {
		t.Fatal("expected error for missing property")
	}
}

func TestParse_EmptyArray(t *testing.T) {
	params := map[string]any{
		"property": []any{},
	}
	_, err := Parse(params)
	if err == nil {
		t.Fatal("expected error for empty property array")
	}
}

func TestParse_InvalidPropertyType(t *testing.T) {
	params := map[string]any{
		"property": 42,
	}
	_, err := Parse(params)
	if err == nil {
		t.Fatal("expected error for non-string/non-array property")
	}
}

// --- ResolveType tests ---

// mockTypeBlock is a stub TypeBlock for testing resolution.
type mockTypeBlock struct {
	name string
}

func (m *mockTypeBlock) Evaluate(key string, value any, ctx *EvalContext) []Finding {
	return nil
}

func TestResolveType_WithParser(t *testing.T) {
	c := &Config{
		Keys:     []string{"source"},
		Required: false,
		TypeName: "wikilink",
		TypeRaw:  map[string]any{"resolve": true},
	}
	parsers := map[string]TypeBlockParser{
		"wikilink": func(raw any) (TypeBlock, error) {
			return &mockTypeBlock{name: "wikilink"}, nil
		},
	}
	rc, err := c.ResolveType(parsers)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Type == nil {
		t.Fatal("Type is nil after resolve")
	}
	if len(rc.Keys) != 1 || rc.Keys[0] != "source" {
		t.Errorf("Keys = %v", rc.Keys)
	}
}

func TestResolveType_ExistenceOnly(t *testing.T) {
	c := &Config{
		Keys:     []string{"title"},
		Required: true,
		TypeName: "",
	}
	rc, err := c.ResolveType(nil)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Type != nil {
		t.Error("Type should be nil for existence-only")
	}
	if !rc.Required {
		t.Error("Required not preserved")
	}
}

func TestResolveType_UnknownParser(t *testing.T) {
	c := &Config{
		Keys:     []string{"x"},
		TypeName: "wikilink",
		TypeRaw:  true,
	}
	// Empty parser registry
	_, err := c.ResolveType(map[string]TypeBlockParser{})
	if err == nil {
		t.Fatal("expected error for missing parser")
	}
}

func TestResolveType_ParserError(t *testing.T) {
	// When the parser rejects the raw config, ResolveType wraps the error
	// with the type block name so callers can identify which block failed.
	c := &Config{
		Keys:     []string{"x"},
		TypeName: "wikilink",
		TypeRaw:  42, // ParseWikilink rejects non-bool/map input
	}
	parsers := map[string]TypeBlockParser{
		"wikilink": ParseWikilink,
	}
	_, err := c.ResolveType(parsers)
	if err == nil {
		t.Fatal("expected error for invalid type block config")
	}
	// Error must reference the failing block name so it can be surfaced as
	// a "type block error" in callers (e.g., fix.fixLinkDisplay).
	msg := err.Error()
	if !strings.Contains(msg, "wikilink") {
		t.Errorf("error %q should reference type block name 'wikilink'", msg)
	}
	if !strings.Contains(msg, "parsing type block") {
		t.Errorf("error %q should contain 'parsing type block' prefix", msg)
	}
}
