package pipe

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

// TestPreflightRejectionMatrix is the full policy matrix: every banned
// construct, its exit code, and a fragment of its teaching text.
func TestPreflightRejectionMatrix(t *testing.T) {
	cases := []struct {
		name     string
		program  string
		wantCode string
		wantExit int
		contains string
	}{
		{"unknown command", "curl http://example.com", "E_UNKNOWN_CMD", 127, "not in the pipe whitelist"},
		{"computed command name", `x=grep; $x foo agents/a1.md`, "E_BANNED", 126, "literal"},
		{"eval", `eval "echo hi"`, "E_BANNED", 126, "eval"},
		{"source", "source setup.sh", "E_BANNED", 126, "source"},
		{"dot source", ". setup.sh", "E_BANNED", 126, "source"},
		{"exec", "exec 3>&1", "E_BANNED", 126, "exec"},
		{"trap", "trap 'echo x' EXIT", "E_BANNED", 126, "trap"},
		{"command builtin", "command grep x f.md", "E_BANNED", 126, "command"},
		{"mapfile", "mapfile lines", "E_BANNED", 126, "mapfile"},
		{"background &", "head agents/a1.md &", "E_BANNED", 126, "background"},
		{"coproc", "coproc head agents/a1.md", "E_BANNED", 126, "coproc"},
		{"process substitution", "head <(grep a agents/a1.md)", "E_BANNED", 126, "process substitution"},
		{"test -r bracket", "[ -r agents/a1.md ] && echo y", "E_BANNED", 126, "REAL filesystem"},
		{"test -w double bracket", "[[ -w agents/a1.md ]] && echo y", "E_BANNED", 126, "REAL filesystem"},
		{"test -x word", "test -x agents/a1.md", "E_BANNED", 126, "REAL filesystem"},
		{"test -O owner", "[ -O agents/a1.md ]", "E_BANNED", 126, "REAL filesystem"},
		{"test -G group double bracket", "[[ -G agents/a1.md ]]", "E_BANNED", 126, "REAL filesystem"},
		{"pwd -P", "pwd -P", "E_BANNED", 126, "symlink"},
		{"pwd -LP cluster", "pwd -LP", "E_BANNED", 126, "symlink"},
		{"read -s", "read -s secret", "E_BANNED", 126, "terminal"},
		{"read -rs cluster", "read -rs secret", "E_BANNED", 126, "terminal"},
		{"tilde user", "head ~root/notes.md", "E_BANNED", 126, "user lookup"},
		{"write redirect md", "echo x > out.md", "EROFS", 126, "read-only projection"},
		{"append redirect", "echo x >> log", "EROFS", 126, "read-only projection"},
		{"clobber redirect", "echo x >| f", "EROFS", 126, "read-only projection"},
		{"stderr file redirect", "grep a agents/a1.md 2> errs", "EROFS", 126, "read-only projection"},
		{"all redirect", "grep a agents/a1.md &> f", "EROFS", 126, "read-only projection"},
		{"append all redirect", "grep a agents/a1.md &>> f", "EROFS", 126, "read-only projection"},
		{"read-write redirect", ": <> f", "EROFS", 126, "read-only projection"},
		{"dup-to-file redirect", "echo x >& f", "EROFS", 126, "read-only projection"},
		{"staged read via tool arg", `md append tasks/t1.md "note"; grep q tasks/t1.md`, "E_STAGED_READ", 126, "PRE-program snapshot"},
		{"staged read via input redirect", `md append tasks/t1.md "note"; head < tasks/t1.md`, "E_STAGED_READ", 126, "PRE-program snapshot"},
		{"staged read with fragment target", `md edit-section tasks/t1.md#Notes old new; wc -l tasks/t1.md`, "E_STAGED_READ", 126, "PRE-program snapshot"},
		{"staged read via md read verb", `md append tasks/t1.md x; md read tasks/t1.md#Notes`, "E_STAGED_READ", 126, "PRE-program snapshot"},
		{"nested md pipe", `md pipe 'echo hi'`, "E_BANNED", 126, "nested"},
		{"brace sequence bomb", "echo {1..1000000000}", "E_BRACE_TOO_BIG", 126, "materialized in memory"},
		{"brace nested multiplicative", "echo {1..100000}{1..100000}", "E_BRACE_TOO_BIG", 126, "cap"},
		{"brace sequence just over cap", "echo {1..10001}", "E_BRACE_TOO_BIG", 126, "cap"},
		{"brace product over cap", "echo {1..100}{1..100}{1..100}", "E_BRACE_TOO_BIG", 126, "cap"},
		{"brace bomb in md target", "md read {1..1000000000}", "E_BRACE_TOO_BIG", 126, "cap"},
		{"syntax error", "if then fi", "E_SYNTAX", 2, "does not parse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, err := Preflight(tc.program)
			if err == nil {
				t.Fatalf("Preflight(%q) passed; want %s", tc.program, tc.wantCode)
			}
			if file != nil {
				t.Fatalf("rejected program returned a non-nil file")
			}
			if err.Code != tc.wantCode {
				t.Fatalf("code = %s, want %s (%v)", err.Code, tc.wantCode, err)
			}
			if err.Exit != tc.wantExit {
				t.Fatalf("exit = %d, want %d (%v)", err.Exit, tc.wantExit, err)
			}
			if !strings.Contains(err.Message+" "+err.Remedy, tc.contains) {
				t.Fatalf("teaching text %q does not contain %q", err.Message+" — "+err.Remedy, tc.contains)
			}
		})
	}
}

// TestPreflightUnknownListsCompleteWhitelist is R-discovery: the first 127
// must list the COMPLETE whitelist, not a sample.
func TestPreflightUnknownListsCompleteWhitelist(t *testing.T) {
	_, err := Preflight("frobnicate")
	if err == nil || err.Exit != 127 {
		t.Fatalf("want 127, got %v", err)
	}
	for _, w := range Whitelist {
		if !strings.Contains(err.Message, w) {
			t.Errorf("127 message misses whitelist entry %q: %s", w, err.Message)
		}
	}
}

// TestPreflightAccepts pins the legal surface.
func TestPreflightAccepts(t *testing.T) {
	programs := []string{
		"grep -c foo agents/*/01-memo.md | sort | head -3",
		"echo hi >/dev/null 2>&1",
		"grep a agents/a1.md 2>&1 | head -1",
		"x=5; echo $x",
		"[ -f agents/a1.md ] && echo yes",
		"[[ -d agents ]] && echo yes",
		"test -s tasks/t1.md",
		"for f in agents/*.md; do wc -l $f; done",
		"f() { echo hi; }; f",
		"md toc agents/a1.md",
		"md read 'agents/a1.md#Notes' | head -2",
		"read line < tasks/t1.md; echo $line",
		"declare -i n=1; let n+=1; echo $n",
		"echo ~/notes",
		"echo ~",
		"pwd",
		"read -r line < tasks/t1.md",
		"cd agents && echo *.md",
		"wc -l <<< 'one line'",
		"head -1 <<EOF\nhi\nEOF",
		// write-then-read of DIFFERENT paths is legal
		"md append tasks/t1.md done; grep open tasks/t2.md",
		// echo of a staged path is not a read
		"md append tasks/t1.md done; echo tasks/t1.md",
		// small brace use stays legal — the cap targets pathological sizes only
		"echo {a,b,c}",
		"echo {1..20}",
		"echo {0..255}",
		"echo {z..a}",
		"echo agents/{a1,a2,a3}.md",
		"for f in agents/{a1,a2}.md; do wc -l $f; done",
		"echo {1..10000}",              // exactly at the cap
		"echo {1..100}{1..100}",        // 1e4 product, exactly at the cap
		"echo pre{a,{1..3},{x,y}}post", // nested, small
	}
	for _, p := range programs {
		if _, err := Preflight(p); err != nil {
			t.Errorf("Preflight(%q) rejected: %v", p, err)
		}
	}
}

// TestGrammarDiscoverySurface is R-discovery: --help/--grammar must carry the
// whole whitelist, the md sub-verbs, and a worked example.
func TestGrammarDiscoverySurface(t *testing.T) {
	g := Grammar()
	for _, w := range Whitelist {
		if !strings.Contains(g, w) {
			t.Errorf("grammar misses whitelist entry %q", w)
		}
	}
	for _, v := range MdVerbs {
		if !strings.Contains(g, v) {
			t.Errorf("grammar misses md verb %q", v)
		}
	}
	if !strings.Contains(g, "for f in agents/") {
		t.Errorf("grammar misses the worked example")
	}
}

// TestBraceExpansionCap is the U12 resource-DoS guard: brace/sequence expansion
// is materialized in full before any output (mvdan expand.Braces — no ctx, no
// size cap), so a single fully-whitelisted word can OOM the shared daemon. The
// gate bounds it at braceCap. The reject cases FLIP at this commit: before the
// guard they parsed clean (Preflight returned a nil error and a runnable file),
// after it they are refused at preflight with exit 126.
func TestBraceExpansionCap(t *testing.T) {
	reject := []struct {
		name    string
		program string
	}{
		{"sequence bomb", "echo {1..1000000000}"},                // ~1e9 words in 15 bytes
		{"nested multiplicative", "echo {1..100000}{1..100000}"}, // ~1e10 via the cartesian product
		{"triple product", "echo {1..100}{1..100}{1..100}"},      // 1e6 > cap
		{"char sequence bomb", "echo {A..Z}{A..Z}{A..Z}{A..Z}"},  // 26^4 > cap (char sequences too)
		{"just over the cap", "echo {1..10001}"},
		{"descending bomb", "echo {1000000000..1}"},
		{"reached through md arg", "md read {1..1000000000}"},
		{"array literal bomb", "arr=({1..1000000000})"},                  // array assignments DO brace-expand
		{"declared array bomb", "declare -a a=({1..100000}{1..100000})"}, // via DeclClause too
		{"prefixed sequence bomb", "echo pre{1..1000000000}post"},        // affix does not dodge the count
		// int64-overflow spans: both endpoints parse, so mvdan treats these as
		// valid (effectively unbounded) sequences; the count must not wrap to a
		// small value and slip past the cap.
		{"full int64 span", "echo {-9223372036854775808..9223372036854775807}"},
		{"maxint64 upper bound", "echo {0..9223372036854775807}"},
		{"minint64 lower bound", "echo {-9223372036854775807..0}"},
	}
	for _, tc := range reject {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			file, err := Preflight(tc.program)
			if err == nil {
				t.Fatalf("Preflight(%q) passed; want E_BRACE_TOO_BIG (this is the un-guarded OOM path)", tc.program)
			}
			if err.Code != "E_BRACE_TOO_BIG" || err.Exit != ExitRefused {
				t.Fatalf("Preflight(%q) = code %s exit %d; want E_BRACE_TOO_BIG / %d", tc.program, err.Code, err.Exit, ExitRefused)
			}
			if file != nil {
				t.Fatalf("rejected program returned a non-nil file")
			}
		})
	}

	// The cap must not over-block legitimate small brace use.
	accept := []string{
		"echo {a,b,c}",
		"echo {1..20}",
		"echo {0..255}",
		"echo {a..z}",
		"echo {1..10000}",       // exactly at the cap
		"echo {1..100}{1..100}", // 1e4 product, exactly at the cap
		"echo {5..1..2}",        // stepped descending: 5,3,1
		"echo pre{a,{1..3}}post",
	}
	for _, p := range accept {
		t.Run("accept/"+p, func(t *testing.T) {
			if _, err := Preflight(p); err != nil {
				t.Fatalf("Preflight(%q) rejected legitimate small brace use: %v", p, err)
			}
		})
	}
}

// TestBraceExpansionCount pins the multiplicative counting model directly, so a
// future change to the walk can't silently drift the accounting the cap relies
// on.
func TestBraceExpansionCount(t *testing.T) {
	cases := []struct {
		program string
		want    int
	}{
		{"echo {a,b,c}", 3},
		{"echo {1..20}", 20},
		{"echo {0..255}", 256},
		{"echo {a..z}", 26},
		{"echo {10..1}", 10},  // descending
		{"echo {1..5..2}", 3}, // stepped: 1,3,5
		{"echo {1..100}{1..100}", 10000},
		{"echo x{1..5}y{a,b}z", 10},         // 5 * 2
		{"echo pre{a,{1..3},{x,y}}post", 6}, // 1 + 3 + 2
	}
	for _, tc := range cases {
		t.Run(tc.program, func(t *testing.T) {
			w := firstBraceWord(t, tc.program)
			got := braceExpansionCount(w.Parts)
			if got != tc.want {
				t.Fatalf("braceExpansionCount(%q) = %d, want %d", tc.program, got, tc.want)
			}
		})
	}
}

// firstBraceWord parses program and returns the first word that carries a brace
// group (after SplitBraces), for direct counting assertions.
func firstBraceWord(t *testing.T, program string) *syntax.Word {
	t.Helper()
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(program), "test")
	if err != nil {
		t.Fatalf("parse %q: %v", program, err)
	}
	var found *syntax.Word
	syntax.Walk(file, func(n syntax.Node) bool {
		if found != nil {
			return false
		}
		if w, ok := n.(*syntax.Word); ok {
			wc := *w
			if syntax.SplitBraces(&wc) {
				found = &wc
				return false
			}
		}
		return true
	})
	if found == nil {
		t.Fatalf("no brace word in %q", program)
	}
	return found
}
