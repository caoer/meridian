package checks

import (
	"testing"
)

// --- toStringSlice ---

func TestToStringSlice_AnyStrings(t *testing.T) {
	got := toStringSlice([]any{"a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v, want [a b]", got)
	}
}

func TestToStringSlice_NativeStrings(t *testing.T) {
	got := toStringSlice([]string{"a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v, want [a b]", got)
	}
}

func TestToStringSlice_Nil(t *testing.T) {
	got := toStringSlice(nil)
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestToStringSlice_EmptyAnySlice(t *testing.T) {
	got := toStringSlice([]any{})
	if got == nil || len(got) != 0 {
		t.Fatalf("got %v (nil=%v), want non-nil empty slice", got, got == nil)
	}
}

func TestToStringSlice_NonStringElements(t *testing.T) {
	got := toStringSlice([]any{123, true})
	if len(got) != 0 {
		t.Fatalf("got %v, want empty (non-string elements dropped)", got)
	}
}

func TestToStringSlice_MixedElements(t *testing.T) {
	got := toStringSlice([]any{"a", 123, "b", true})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v, want [a b]", got)
	}
}

func TestToStringSlice_PlainString(t *testing.T) {
	got := toStringSlice("just a string")
	if len(got) != 1 || got[0] != "just a string" {
		t.Fatalf("got %v, want [just a string] for scalar string input", got)
	}
}

func TestToStringSlice_MapInput(t *testing.T) {
	got := toStringSlice(map[string]any{"k": "v"})
	if got != nil {
		t.Fatalf("got %v, want nil for map input", got)
	}
}

func TestToStringSlice_IntInput(t *testing.T) {
	got := toStringSlice(42)
	if got != nil {
		t.Fatalf("got %v, want nil for int input", got)
	}
}

// --- shouldSkip ---

func TestShouldSkip_MatchesPrefix(t *testing.T) {
	if !shouldSkip("http://example.com", []string{"http"}) {
		t.Fatal("want true for matching prefix")
	}
}

func TestShouldSkip_NoMatch(t *testing.T) {
	if shouldSkip("wiki/page", []string{"http", "inbox/"}) {
		t.Fatal("want false when no prefix matches")
	}
}

func TestShouldSkip_EmptyPrefixes(t *testing.T) {
	if shouldSkip("anything", nil) {
		t.Fatal("want false for nil prefixes")
	}
	if shouldSkip("anything", []string{}) {
		t.Fatal("want false for empty prefixes")
	}
}

func TestShouldSkip_MultiplePrefixes_OneMatches(t *testing.T) {
	if !shouldSkip("inbox/note", []string{"http", "inbox/", "drafts/"}) {
		t.Fatal("want true when one of multiple prefixes matches")
	}
}

func TestShouldSkip_CaseInsensitive(t *testing.T) {
	if !shouldSkip("HTTP://example.com", []string{"http"}) {
		t.Fatal("want true: target uppercase, prefix lowercase")
	}
	if !shouldSkip("http://example.com", []string{"HTTP"}) {
		t.Fatal("want true: target lowercase, prefix uppercase")
	}
}

func TestShouldSkip_ExactMatch(t *testing.T) {
	if !shouldSkip("http", []string{"http"}) {
		t.Fatal("want true for exact match (prefix == target)")
	}
}

func TestShouldSkip_EmptyTarget(t *testing.T) {
	if shouldSkip("", []string{"http"}) {
		t.Fatal("want false: empty target doesn't start with 'http'")
	}
}

func TestShouldSkip_EmptyPrefix(t *testing.T) {
	if !shouldSkip("anything", []string{""}) {
		t.Fatal("want true: empty prefix matches everything")
	}
}

// --- buildResolvedIndex ---

func TestBuildResolvedIndex_RootsAndPaths(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page-a.md", "wiki/sub/page-b.md"},
	}
	idx := buildResolvedIndex(params)
	if !idx["page-a"] {
		t.Error("want page-a resolved")
	}
	if !idx["page-b"] {
		t.Error("want page-b resolved")
	}
}

func TestBuildResolvedIndex_EmptyRoots(t *testing.T) {
	params := map[string]any{
		"roots":           []any{},
		"__scanned_paths": []string{"wiki/page.md"},
	}
	idx := buildResolvedIndex(params)
	if len(idx) != 0 {
		t.Fatalf("want empty index when roots empty, got %v", idx)
	}
}

func TestBuildResolvedIndex_EmptyPaths(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{},
	}
	idx := buildResolvedIndex(params)
	if len(idx) != 0 {
		t.Fatalf("want empty index when paths empty, got %v", idx)
	}
}

func TestBuildResolvedIndex_NilPaths(t *testing.T) {
	params := map[string]any{
		"roots": []any{"wiki/**"},
	}
	idx := buildResolvedIndex(params)
	if len(idx) != 0 {
		t.Fatalf("want empty index when __scanned_paths missing, got %v", idx)
	}
}

func TestBuildResolvedIndex_RootsFiltersPaths(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/good.md", "inbox/excluded.md"},
	}
	idx := buildResolvedIndex(params)
	if !idx["good"] {
		t.Error("want good resolved (matches wiki/**)")
	}
	if idx["excluded"] {
		t.Error("want excluded NOT resolved (matches inbox/, not wiki/**)")
	}
}

func TestBuildResolvedIndex_MultipleRoots(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"wiki/**", "docs/**"},
		"__scanned_paths": []string{"wiki/a.md", "docs/b.md", "tmp/c.md"},
	}
	idx := buildResolvedIndex(params)
	if !idx["a"] {
		t.Error("want a resolved")
	}
	if !idx["b"] {
		t.Error("want b resolved")
	}
	if idx["c"] {
		t.Error("want c NOT resolved")
	}
}

func TestBuildResolvedIndex_CaseInsensitiveKeys(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/MyPage.md"},
	}
	idx := buildResolvedIndex(params)
	if !idx["mypage"] {
		t.Error("want lowercase key to resolve")
	}
	if idx["MyPage"] {
		t.Error("want original-case key absent (keys are lowered)")
	}
}

func TestBuildResolvedIndex_StripsExtension(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"**"},
		"__scanned_paths": []string{"notes/readme.md"},
	}
	idx := buildResolvedIndex(params)
	if !idx["readme"] {
		t.Error("want .md extension stripped from stem")
	}
	if idx["readme.md"] {
		t.Error("want stem without extension, not full filename")
	}
}

func TestBuildResolvedIndex_BasenameDedupe(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"**"},
		"__scanned_paths": []string{"a/page.md", "b/page.md"},
	}
	idx := buildResolvedIndex(params)
	if !idx["page"] {
		t.Error("want page resolved (both paths contribute same stem)")
	}
}

func TestBuildResolvedIndex_FallbackResolvedIndex(t *testing.T) {
	params := map[string]any{
		"resolved_index": map[string]bool{"alpha": true, "Beta": true},
	}
	idx := buildResolvedIndex(params)
	if !idx["alpha"] {
		t.Error("want alpha resolved via fallback")
	}
	if !idx["beta"] {
		t.Error("want beta resolved (case-lowered from Beta)")
	}
}

func TestBuildResolvedIndex_FallbackIgnoredWhenRootsPresent(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"wiki/**"},
		"__scanned_paths": []string{"wiki/page.md"},
		"resolved_index":  map[string]bool{"fallback-only": true},
	}
	idx := buildResolvedIndex(params)
	if !idx["page"] {
		t.Error("want page resolved via roots path")
	}
	if idx["fallback-only"] {
		t.Error("want fallback-only absent when roots path used")
	}
}

func TestBuildResolvedIndex_NilParams(t *testing.T) {
	idx := buildResolvedIndex(nil)
	if idx == nil {
		t.Fatal("want non-nil empty map, got nil")
	}
	if len(idx) != 0 {
		t.Fatalf("want empty map, got %v", idx)
	}
}

func TestBuildResolvedIndex_EmptyParams(t *testing.T) {
	idx := buildResolvedIndex(map[string]any{})
	if len(idx) != 0 {
		t.Fatalf("want empty map, got %v", idx)
	}
}

func TestBuildResolvedIndex_NonMdExtension(t *testing.T) {
	params := map[string]any{
		"roots":           []any{"**"},
		"__scanned_paths": []string{"data/file.txt", "img/photo.png"},
	}
	idx := buildResolvedIndex(params)
	if !idx["file.txt"] {
		t.Error("want file.txt as stem (only .md is stripped)")
	}
	if !idx["photo.png"] {
		t.Error("want photo.png as stem (only .md is stripped)")
	}
}
