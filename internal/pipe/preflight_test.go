package pipe

import (
	"strings"
	"testing"
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
