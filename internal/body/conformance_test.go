package body

// conformance_test.go is the I0 CONFORMANCE HARNESS, authored (U1) BEFORE the
// engine (U2/U3). It encodes the invariants the body package must satisfy against
// the corpus under testdata/. Assertions that require the real engine reach it
// through Parse / Load / Read, which currently return ErrNotImpl; those sub-tests
// skip with "impl pending U2" and turn green automatically once U2/U3 land. The
// completeness test (every corpus file present) and the corpus's own byte shape
// are green now.
//
// Two cases encode a ccc-mdfs KNOWN-BUG FIX TARGET (fm-eof-no-newline,
// bare-backticks-midcontent): they assert the CORRECT behavior, so they will fail
// against a naive port until U2 fixes the bug — that is the point. The corpus is
// the specification of the fix.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpusCase describes one entry of the conformance corpus.
type corpusCase struct {
	rel         string // path relative to testdata/
	failLoud    bool   // Load/Parse must return a loud (non-ErrNotImpl) error
	knownBugFix bool   // encodes a ccc-mdfs known-bug fix target
	note        string
}

// corpus is the complete required case set (task u1-corpus step 4). Every file
// listed here must exist; the round-trip, span-law, fail-loud, and known-bug
// assertions below range over it.
var corpus = []corpusCase{
	// Five demo goldens (schema-v2 §2).
	{rel: "demo/agent.md", note: "agent file: Tasks/Memo/Notes/Handoff (decision 7)"},
	{rel: "demo/task.md", note: "task file mid-life: Objective/Context/Gate Evidence/Activity"},
	{rel: "demo/card.md", note: "ask/attention card, answered state"},
	{rel: "demo/memo.md", note: "standalone compound memo with provenance frontmatter"},
	{rel: "demo/session-standard.md", note: "standard-preset session record: Board/Agents/Log"},

	// Ported ccc-mdfs fixtures (byte-exact).
	{rel: "ported/agent-sample.md", note: "claimed_by anchor, blocks, nested sections"},
	{rel: "ported/multibyte.md", note: "CJK + emoji: multibyte span offsets, no trailing newline norm"},
	{rel: "ported/traps.md", note: "duplicate subheadings, heading punctuation, comment-in-fence"},
	{rel: "ported/fences.md", note: "phantom-heading fence traps (tilde + trailing-text closer)"},

	// Adversarial cases (all required).
	{rel: "adversarial/adj-fences-blockid.md", note: "adjacent fences, no blank line, + ^id (meridian P1 bug class)"},
	{rel: "adversarial/fm-eof-no-newline.md", failLoud: false, knownBugFix: true, note: "frontmatter closed by --- at EOF, no trailing newline (ccc-mdfs bug #1)"},
	{rel: "adversarial/bare-backticks-midcontent.md", knownBugFix: true, note: "```-prefixed line with trailing text mid-fence must not toggle (ccc-mdfs bug #2)"},
	{rel: "adversarial/fence-trailing-text.md", note: "closer with trailing text is not a closer"},
	{rel: "adversarial/nested-fences.md", note: "``` nested inside a ```` block (backtick-count distinguished)"},
	{rel: "adversarial/heading-in-fence.md", note: "ATX headings inside a fence are content, not sections"},
	{rel: "adversarial/crlf.md", note: "CRLF line endings preserved byte-identically"},
	{rel: "adversarial/tabs.md", note: "tabs in indentation and mid-line, preserved"},
	{rel: "adversarial/leading-html-comment.md", failLoud: true, note: "content before frontmatter must fail loud"},
	{rel: "adversarial/duplicate-headings.md", note: "duplicate headings: hpath ambiguity"},
	{rel: "adversarial/long-line.md", note: "single 100k-character line"},
}

func testdataPath(rel string) string { return filepath.Join("testdata", rel) }

func readCorpusFile(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(testdataPath(rel))
	if err != nil {
		t.Fatalf("read corpus file %s: %v", rel, err)
	}
	return b
}

// skipIfUnimplemented reports true (and skips) when err is the ErrNotImpl sentinel,
// so engine-dependent assertions are expected-unimplemented rather than failures.
func skipIfUnimplemented(t *testing.T, err error) bool {
	t.Helper()
	if errors.Is(err, ErrNotImpl) {
		t.Skip("impl pending U2/U3")
		return true
	}
	return false
}

// TestConformanceCorpusComplete is GREEN NOW: every required case file exists and
// is non-empty. It is the corpus-completeness gate — a missing case fails here
// before any engine work.
func TestConformanceCorpusComplete(t *testing.T) {
	for _, c := range corpus {
		t.Run(c.rel, func(t *testing.T) {
			fi, err := os.Stat(testdataPath(c.rel))
			if err != nil {
				t.Fatalf("missing corpus file: %v", err)
			}
			if fi.Size() == 0 {
				t.Fatalf("corpus file is empty: %s", c.rel)
			}
		})
	}
}

// TestCorpusByteShape is GREEN NOW: it pins the load-bearing byte properties the
// goldens were authored for, independent of the engine — so an editor normalizing
// newlines can never silently rot the corpus.
func TestCorpusByteShape(t *testing.T) {
	// fm-eof-no-newline.md must END at the closing '---' with NO trailing newline.
	fm := readCorpusFile(t, "adversarial/fm-eof-no-newline.md")
	if !bytes.HasSuffix(fm, []byte("---")) || bytes.HasSuffix(fm, []byte("\n")) {
		t.Errorf("fm-eof-no-newline.md must end at '---' with no trailing newline; last bytes = %q", tail(fm, 6))
	}
	// crlf.md must actually contain CRLF pairs and no bare LF-only lines.
	crlf := readCorpusFile(t, "adversarial/crlf.md")
	if !bytes.Contains(crlf, []byte("\r\n")) {
		t.Error("crlf.md must contain CRLF line endings")
	}
	if bytes.Count(crlf, []byte("\n")) != bytes.Count(crlf, []byte("\r\n")) {
		t.Error("crlf.md must have a \\r before every \\n (no bare LF lines)")
	}
	// long-line.md must carry a single line of at least 100k characters.
	long := readCorpusFile(t, "adversarial/long-line.md")
	if !hasLineOfAtLeast(long, 100000) {
		t.Error("long-line.md must contain a single line of >= 100000 characters")
	}
	// leading-html-comment.md must place the comment BEFORE the frontmatter opener.
	lead := readCorpusFile(t, "adversarial/leading-html-comment.md")
	if !bytes.HasPrefix(lead, []byte("<!--")) {
		t.Error("leading-html-comment.md must begin with an HTML comment before the frontmatter")
	}
	// tabs.md must contain literal tab bytes.
	if !bytes.Contains(readCorpusFile(t, "adversarial/tabs.md"), []byte("\t")) {
		t.Error("tabs.md must contain literal tab characters")
	}
}

// TestConformanceRoundTripByteIdentity is the CORE invariant (I0): Load(f) then
// Bytes() is byte-identical to the file, for every non-fail-loud case. The engine
// never re-serializes, so the round-trip holds by construction. Skips pending U2.
func TestConformanceRoundTripByteIdentity(t *testing.T) {
	for _, c := range corpus {
		if c.failLoud {
			continue
		}
		t.Run(c.rel, func(t *testing.T) {
			want := readCorpusFile(t, c.rel)
			doc, err := Load(testdataPath(c.rel))
			if skipIfUnimplemented(t, err) {
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := doc.Bytes(); !bytes.Equal(got, want) {
				t.Fatalf("round-trip mutated %s (len %d -> %d)", c.rel, len(want), len(got))
			}
		})
	}
}

// TestConformanceFailLoud pins the fail-loud contract: content before the opening
// frontmatter delimiter is a LOUD error, never a silent body-only fallback. Skips
// pending U2 (ErrNotImpl is distinct from the real loud error).
func TestConformanceFailLoud(t *testing.T) {
	for _, c := range corpus {
		if !c.failLoud {
			continue
		}
		t.Run(c.rel, func(t *testing.T) {
			_, err := Load(testdataPath(c.rel))
			if skipIfUnimplemented(t, err) {
				return
			}
			if err == nil {
				t.Fatalf("%s: expected a loud parse error, got nil", c.rel)
			}
			// Once implemented, the loud error must NOT be the not-impl sentinel.
			if errors.Is(err, ErrNotImpl) {
				t.Fatalf("%s: fail-loud must be a real error, not ErrNotImpl", c.rel)
			}
		})
	}
}

// TestConformanceSpanLaw pins the span law through the public Read API: a section
// body span excludes its heading line, and a block span excludes its " ^id"
// marker. Byte offsets are asserted against the exact source. Skips pending U2.
func TestConformanceSpanLaw(t *testing.T) {
	t.Run("section body excludes heading line", func(t *testing.T) {
		src := []byte("---\ntype: agent\n---\n# Todo\n- [ ] item\nmore\n# Next\ntail\n")
		doc, err := Parse(src)
		if skipIfUnimplemented(t, err) {
			return
		}
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		sec, err := doc.Read("Todo")
		if err != nil {
			t.Fatalf("Read(Todo): %v", err)
		}
		// The span starts AFTER "# Todo\n" and ends before "# Next".
		wantContent := "- [ ] item\nmore\n"
		if string(sec.Content) != wantContent {
			t.Fatalf("Todo content = %q, want %q", sec.Content, wantContent)
		}
		if got := string(src[sec.Start:sec.End]); got != wantContent {
			t.Fatalf("Todo span [%d,%d) = %q, want %q", sec.Start, sec.End, got, wantContent)
		}
		// The heading line's bytes are OUTSIDE the span.
		if bytes.Contains(src[sec.Start:sec.End], []byte("# Todo")) {
			t.Fatal("span law violated: heading line is inside the section span")
		}
	})

	t.Run("block span excludes the ^id marker", func(t *testing.T) {
		src := []byte("# Todo\n- [/] Flash system partition ^cct-2\n- [ ] Boot-verify ^cct-3\n")
		doc, err := Parse(src)
		if skipIfUnimplemented(t, err) {
			return
		}
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		sec, err := doc.Read("^cct-2")
		if err != nil {
			t.Fatalf("Read(^cct-2): %v", err)
		}
		want := "- [/] Flash system partition"
		if string(sec.Content) != want {
			t.Fatalf("block content = %q, want %q (the ' ^cct-2' marker must stay outside)", sec.Content, want)
		}
		if strings.Contains(string(sec.Content), "^cct-2") {
			t.Fatal("span law violated: block span includes its ^id marker")
		}
	})
}

// TestConformanceLineNumberConvention pins the deliberate whole-file, 1-based,
// physical line-number convention (line 1 = the opening "---"), the choice that
// avoids the historical bodyOffset+i+1 off-by-one. Skips pending U2.
func TestConformanceLineNumberConvention(t *testing.T) {
	// Line 1: "---", 2: "type: agent", 3: "---", 4: "# Sec", 5: "alpha", 6: "beta".
	src := []byte("---\ntype: agent\n---\n# Sec\nalpha\nbeta\n")
	doc, err := Parse(src)
	if skipIfUnimplemented(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	sec, err := doc.Read("Sec")
	if err != nil {
		t.Fatalf("Read(Sec): %v", err)
	}
	if sec.StartLine != 5 || sec.EndLine != 6 {
		t.Fatalf("Sec content lines = [%d,%d], want [5,6] (whole-file physical, heading on line 4)", sec.StartLine, sec.EndLine)
	}
}

// TestConformanceKnownBugFixes drives the two ccc-mdfs known-bug fix targets to
// their CORRECT behavior. Until U2 fixes the port these fail; the corpus IS the
// fix spec. Skips pending U2 (the sentinel), then asserts the fixed behavior.
func TestConformanceKnownBugFixes(t *testing.T) {
	t.Run("bug#1: frontmatter closed at EOF without trailing newline", func(t *testing.T) {
		src := readCorpusFile(t, "adversarial/fm-eof-no-newline.md")
		doc, err := Parse(src)
		if skipIfUnimplemented(t, err) {
			return
		}
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		// The closing '---' at EOF (no trailing '\n') must be recognized as the
		// frontmatter terminator: the 'status' key resolves as a frontmatter value,
		// and no phantom section is produced from the delimiter lines.
		toc := doc.Toc()
		if len(toc.Sections) != 0 {
			t.Fatalf("frontmatter-only file must have no sections, got %d", len(toc.Sections))
		}
		if !bytes.Equal(doc.Bytes(), src) {
			t.Fatal("round-trip must preserve the missing trailing newline byte-for-byte")
		}
	})

	t.Run("bug#2: ```-prefixed line with trailing text mid-fence does not toggle", func(t *testing.T) {
		src := readCorpusFile(t, "adversarial/bare-backticks-midcontent.md")
		doc, err := Parse(src)
		if skipIfUnimplemented(t, err) {
			return
		}
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		// "# This heading is fence content, not a section" sits inside the fence and
		// must NOT be a section; "Snippet" and "After" are the only sections.
		got := hpaths(doc.Toc())
		want := []string{"Snippet", "After"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("sections = %v, want %v (the mid-fence heading must stay fence content)", got, want)
		}
	})
}

// TestConformanceDuplicateHeadingAmbiguous pins that a duplicate hpath resolves to
// a candidate-list error at read time (never a silent pick), while both sections
// remain visible in the TOC. Skips pending U2.
func TestConformanceDuplicateHeadingAmbiguous(t *testing.T) {
	src := readCorpusFile(t, "adversarial/duplicate-headings.md")
	doc, err := Parse(src)
	if skipIfUnimplemented(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := doc.Read("Notes"); err == nil {
		t.Fatal("Read(Notes) on duplicate headings must return an ambiguity error, got nil")
	}
	// Both Notes sections plus Handoff appear in the TOC.
	if n := countHPath(doc.Toc(), "Notes"); n != 2 {
		t.Fatalf("TOC must list both duplicate Notes sections, got %d", n)
	}
}

// --- helpers -------------------------------------------------------------

func tail(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}

func hasLineOfAtLeast(b []byte, n int) bool {
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(line) >= n {
			return true
		}
	}
	return false
}

func hpaths(toc Toc) []string {
	out := make([]string, 0, len(toc.Sections))
	for _, s := range toc.Sections {
		out = append(out, s.HPath)
	}
	return out
}

func countHPath(toc Toc, hpath string) int {
	n := 0
	for _, s := range toc.Sections {
		if s.HPath == hpath || s.Title == hpath {
			n++
		}
	}
	return n
}
