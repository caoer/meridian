package fix

import (
	"testing"
	"time"
)

func TestFieldExistsFix_AddToExistingFrontmatter(t *testing.T) {
	content := []byte("---\ncreated: 2026-05-05\n---\n# Page\n")
	params := map[string]any{
		"frontmatter": []any{"tags"},
	}

	changed, newContent, actions, err := FieldExistsFix(content, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}

	got := string(newContent)
	// Must contain tags field in frontmatter
	if !containsLine(got, "tags: []") {
		t.Errorf("expected 'tags: []' in output:\n%s", got)
	}
	// Must preserve existing field
	if !containsLine(got, "created: 2026-05-05") {
		t.Errorf("expected 'created: 2026-05-05' preserved:\n%s", got)
	}
	// Must preserve body
	if !containsLine(got, "# Page") {
		t.Errorf("expected body '# Page' preserved:\n%s", got)
	}
}

func TestFieldExistsFix_CreateFrontmatter(t *testing.T) {
	content := []byte("# No Frontmatter\n\nSome body text.\n")
	params := map[string]any{
		"frontmatter": []any{"tags", "created"},
	}

	changed, newContent, actions, err := FieldExistsFix(content, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}

	got := string(newContent)
	// Must start with frontmatter
	if got[:4] != "---\n" {
		t.Errorf("expected output to start with '---\\n', got: %q", got[:4])
	}
	if !containsLine(got, "tags: []") {
		t.Errorf("expected 'tags: []':\n%s", got)
	}
	// created should get today's date
	today := time.Now().Format("2006-01-02")
	if !containsLine(got, "created: "+today) {
		t.Errorf("expected 'created: %s':\n%s", today, got)
	}
	// Body preserved after frontmatter
	if !containsLine(got, "# No Frontmatter") {
		t.Errorf("expected body preserved:\n%s", got)
	}
}

func TestFieldExistsFix_AllFieldsPresent(t *testing.T) {
	content := []byte("---\ntags: [foo]\ncreated: 2026-01-01\n---\n# Done\n")
	params := map[string]any{
		"frontmatter": []any{"tags", "created"},
	}

	changed, _, actions, err := FieldExistsFix(content, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false when all fields present")
	}
	if len(actions) != 0 {
		t.Fatalf("expected 0 actions, got %d", len(actions))
	}
}

func TestFieldExistsFix_MultipleFieldsMissing(t *testing.T) {
	content := []byte("---\ntitle: Hello\n---\n# Page\n")
	params := map[string]any{
		"frontmatter": []any{"tags", "created", "source"},
	}

	changed, newContent, actions, err := FieldExistsFix(content, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d: %v", len(actions), actions)
	}

	got := string(newContent)
	if !containsLine(got, "tags: []") {
		t.Errorf("missing 'tags: []':\n%s", got)
	}
	if !containsLine(got, "source: \"\"") {
		t.Errorf("missing 'source: \"\"':\n%s", got)
	}
	// title must remain
	if !containsLine(got, "title: Hello") {
		t.Errorf("title lost:\n%s", got)
	}
}

func TestFieldExistsFix_NoParams(t *testing.T) {
	content := []byte("---\ntags: []\n---\n# X\n")
	params := map[string]any{}

	changed, _, _, err := FieldExistsFix(content, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false with no frontmatter param")
	}
}

func TestFieldExistsFix_StringSliceParams(t *testing.T) {
	// Some callers may pass []string instead of []any
	content := []byte("---\ntitle: X\n---\n# Y\n")
	params := map[string]any{
		"frontmatter": []string{"tags"},
	}

	changed, newContent, actions, err := FieldExistsFix(content, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if !containsLine(string(newContent), "tags: []") {
		t.Errorf("missing tags:\n%s", string(newContent))
	}
}

func containsLine(s, substr string) bool {
	for _, line := range splitLines(s) {
		if trimSpace(line) == substr || line == substr {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}
