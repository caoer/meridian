package fix

import (
	"strings"
	"testing"
)

func TestPropertyFix_MissingFieldAdded(t *testing.T) {
	content := []byte("---\ntitle: Hello\n---\n# Page\n")
	params := map[string]any{
		"property": "source",
		"required": true,
	}

	changed, result, actions, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(actions))
	}
	if !strings.Contains(string(result), "source: \"\"") {
		t.Errorf("expected source field added, got:\n%s", result)
	}
}

func TestPropertyFix_MissingFieldMultiKey(t *testing.T) {
	content := []byte("---\ntitle: Hello\n---\n# Page\n")
	params := map[string]any{
		"property": []any{"source", "created"},
		"required": true,
	}

	changed, result, actions, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(actions) != 2 {
		t.Fatalf("want 2 actions, got %d", len(actions))
	}
	s := string(result)
	if !strings.Contains(s, "source: \"\"") {
		t.Errorf("missing source field:\n%s", s)
	}
	// created gets date default
	if !strings.Contains(s, "created: ") {
		t.Errorf("missing created field:\n%s", s)
	}
}

func TestPropertyFix_NoFrontmatter(t *testing.T) {
	content := []byte("# Page with no frontmatter\n")
	params := map[string]any{
		"property": "source",
		"required": true,
	}

	changed, result, _, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	s := string(result)
	if !strings.HasPrefix(s, "---\n") {
		t.Errorf("expected frontmatter block, got:\n%s", s)
	}
	if !strings.Contains(s, "source: \"\"") {
		t.Errorf("missing source field:\n%s", s)
	}
}

func TestPropertyFix_FieldAlreadyExists(t *testing.T) {
	content := []byte("---\nsource: \"[[page]]\"\n---\n# Page\n")
	params := map[string]any{
		"property": "source",
		"required": true,
	}

	changed, _, _, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if changed {
		t.Error("expected changed=false when field exists")
	}
}

func TestPropertyFix_LinkDisplayRewrite(t *testing.T) {
	content := []byte("---\nsource: \"[[projects/alpha|wrong-display]]\"\n---\n# Page\n")
	params := map[string]any{
		"property": "source",
		"required": false,
		"wikilink": map[string]any{
			"link_display": map[string]any{
				"template": []any{
					map[string]any{
						"when":   "projects/*",
						"format": "{{target}}",
					},
				},
			},
		},
	}

	changed, result, actions, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	s := string(result)
	if !strings.Contains(s, "[[projects/alpha|projects/alpha]]") {
		t.Errorf("expected rewritten display, got:\n%s", s)
	}
	if len(actions) == 0 {
		t.Error("expected at least one action")
	}
}

func TestPropertyFix_LinkDisplayAdded(t *testing.T) {
	content := []byte("---\nsource: \"[[projects/beta]]\"\n---\n# Page\n")
	params := map[string]any{
		"property": "source",
		"required": false,
		"wikilink": map[string]any{
			"link_display": map[string]any{
				"template": []any{
					map[string]any{
						"when":   "projects/*",
						"format": "{{target}}",
					},
				},
			},
		},
	}

	changed, result, _, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true for adding display")
	}
	s := string(result)
	if !strings.Contains(s, "[[projects/beta|projects/beta]]") {
		t.Errorf("expected display added, got:\n%s", s)
	}
}

func TestPropertyFix_Idempotent(t *testing.T) {
	content := []byte("---\nsource: \"[[projects/alpha|projects/alpha]]\"\n---\n# Page\n")
	params := map[string]any{
		"property": "source",
		"required": false,
		"wikilink": map[string]any{
			"link_display": map[string]any{
				"template": []any{
					map[string]any{
						"when":   "projects/*",
						"format": "{{target}}",
					},
				},
			},
		},
	}

	changed, _, _, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if changed {
		t.Error("expected changed=false when display already correct (idempotent)")
	}
}

func TestPropertyFix_NotRequired_NoChange(t *testing.T) {
	content := []byte("---\ntitle: Hello\n---\n# Page\n")
	params := map[string]any{
		"property": "source",
		"required": false,
	}

	changed, _, _, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if changed {
		t.Error("expected no change for non-required missing field")
	}
}

func TestPropertyFix_ParseError_NoChange(t *testing.T) {
	content := []byte("---\ntitle: Hello\n---\n# Page\n")
	params := map[string]any{
		// Missing "property" key
		"required": true,
	}

	changed, _, _, err := PropertyFix(content, params)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if changed {
		t.Error("expected no change on parse error")
	}
}
