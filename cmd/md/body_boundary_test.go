package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caoer/meridian/pkg/body"
)

// body_boundary_test.go is U2's boundary-wiring test: it exercises the body engine
// through the SAME public surface (pkg/body) the md binary links, from the CLI's
// own package — not just the engine's internal tests. The read verbs (md toc / md
// read) land in U4; this test documents and locks the seam their handlers will
// wire (body.Load → Document.Toc / Document.Read), so a break in the public
// re-export surface is caught here at the entry package rather than in U4.
func TestBodyEngineBoundaryFromCLIPackage(t *testing.T) {
	src := []byte("---\ntype: agent\nrole: worker\n---\n" +
		"# Todo\n- [ ] flash image ^cct-2\nmore body\n" +
		"# Notes\n## Lab state\npads accessible\n")
	path := filepath.Join(t.TempDir(), "agent.md")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}

	doc, err := body.Load(path)
	if err != nil {
		t.Fatalf("body.Load via public surface: %v", err)
	}

	// I0: the engine never re-serializes — Load(f).Bytes() is byte-identical to f.
	if string(doc.Bytes()) != string(src) {
		t.Fatal("round-trip mutated bytes through the public surface")
	}

	// Toc is the shape query: three sections (Todo, Notes, Notes/Lab-state), each
	// carrying a non-empty sec_rev, and the document carries a file_rev.
	toc := doc.Toc()
	if toc.Rev == "" {
		t.Error("Toc.Rev (file_rev) is empty")
	}
	wantPaths := map[string]bool{"Todo": false, "Notes": false, "Notes/Lab-state": false}
	for _, s := range toc.Sections {
		if _, ok := wantPaths[s.HPath]; ok {
			wantPaths[s.HPath] = true
		}
		if s.Rev == "" {
			t.Errorf("section %q has an empty sec_rev", s.HPath)
		}
	}
	for hp, seen := range wantPaths {
		if !seen {
			t.Errorf("Toc missing expected section %q; got %v", hp, hpathList(toc))
		}
	}

	// Read populates Content over the span-law bytes: the "# Todo" heading line is
	// OUTSIDE the section span, and the " ^cct-2" marker is OUTSIDE the block span.
	sec, err := doc.Read("Todo")
	if err != nil {
		t.Fatalf("Read(Todo): %v", err)
	}
	if string(sec.Content) != "- [ ] flash image ^cct-2\nmore body\n" {
		t.Fatalf("Todo content = %q", sec.Content)
	}

	blk, err := doc.Read("^cct-2")
	if err != nil {
		t.Fatalf("Read(^cct-2): %v", err)
	}
	if string(blk.Content) != "- [ ] flash image" {
		t.Fatalf("block content = %q, want the line without its ' ^cct-2' marker", blk.Content)
	}
}

func hpathList(toc body.Toc) []string {
	out := make([]string, 0, len(toc.Sections))
	for _, s := range toc.Sections {
		out = append(out, s.HPath)
	}
	return out
}
