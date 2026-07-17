package pipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// escape_test.go is the PERMANENT escape regression suite. The U9a sandbox
// passed adversarial review (R3/traversal/snapshot/resource all held); this
// file pins every hole that review probed so any regression fails CI.
//
// PIN: go.mod pins mvdan.cc/sh/v3 v3.13.1. On ANY mvdan bump, re-run this file
// FIRST — a new FS-touching builtin, redirect operator, or expansion form could
// open a hole neither the interpreter wiring nor the preflight gate closes yet.
//
// Two layers are asserted:
//   - preflight refusals (static): the construct never reaches execution.
//   - runtime holds (dynamic): even a construct that passes preflight cannot
//     read outside the fabric, mutate anything, or flood the caller.

// refuse asserts a program is rejected at preflight with the given code/exit.
func refuse(t *testing.T, program, wantCode string, wantExit int) {
	t.Helper()
	file, err := Preflight(program)
	if err == nil {
		t.Fatalf("Preflight(%q) passed; want %s", program, wantCode)
	}
	if file != nil {
		t.Fatalf("Preflight(%q) returned a non-nil file on rejection", program)
	}
	if err.Code != wantCode {
		t.Fatalf("Preflight(%q) code = %s, want %s (%v)", program, err.Code, wantCode, err)
	}
	if err.Exit != wantExit {
		t.Fatalf("Preflight(%q) exit = %d, want %d", program, err.Exit, wantExit)
	}
}

// accept asserts a program passes preflight (the legal surface must not regress
// into over-rejection).
func accept(t *testing.T, program string) {
	t.Helper()
	if _, err := Preflight(program); err != nil {
		t.Fatalf("Preflight(%q) rejected a legal program: %v", program, err)
	}
}

// ---- write posture: no writable path exists (R3) ----------------------------

func TestEscape_WriteRedirectsRefused(t *testing.T) {
	// Every write-class redirect operator at any path, plus a computed target
	// via command substitution (which wordLit cannot see through).
	for _, p := range []string{
		"echo x > out.md",
		"echo x >> log",
		"echo x >| clobber",
		": <> rw",
		"grep a agents/a1.md &> all",
		"grep a agents/a1.md &>> all",
		"grep a agents/a1.md 2> errs",
		"echo x >& dupfile",
		"echo x > $(echo out.md)",     // comsub target
		"echo x > \"$HOME/escape.md\"", // param-expanded target
	} {
		refuse(t, p, "EROFS", ExitRefused)
	}
}

func TestEscape_DevNullCarveOutIsTight(t *testing.T) {
	// the single legal write sink
	accept(t, "echo x >/dev/null")
	accept(t, "grep a agents/a1.md >/dev/null 2>&1")
	// look-alikes and /dev/null-prefixed traversal are NOT the carve-out
	for _, p := range []string{
		"echo x >/dev/nullx",
		"echo x >/dev/null/../escape.md",
		"echo x >/dev/nul",
	} {
		refuse(t, p, "EROFS", ExitRefused)
	}
}

// ---- preflight name bypass: only a vetted literal name may run --------------

func TestEscape_CommandNameBypassRefused(t *testing.T) {
	// computed, quoted-to-hide, backtick, $'...', and the banned builtins.
	for _, p := range []string{
		`x=grep; $x foo agents/a1.md`,   // computed
		"`echo grep` foo agents/a1.md",  // backtick comsub name
		`"$(echo grep)" foo agents/a1.md`, // comsub name in dquotes
		`$'\x67rep' foo agents/a1.md`,   // $'grep' ANSI-C name
		"command grep x agents/a1.md",   // command builtin
		"builtin echo hi",               // builtin dispatch
		"exec 3>&1",                     // exec re-plumb
	} {
		refuse(t, p, "E_BANNED", ExitRefused)
	}
}

// ---- item 1: $'...' ANSI-C fails closed everywhere -------------------------

func TestEscape_DollarAnsiCFailsClosed(t *testing.T) {
	// $'\x70ipe' decodes to "pipe", $'\x61ppend' to "append",
	// $'\x2e\x2e/escape.md' to "../escape.md" — the interpreter decodes, so a
	// verbatim-byte preflight would have diverged. All three are refused because
	// a $'...' md sub-arg is treated as non-literal.
	for _, p := range []string{
		`md $'\x70ipe' 'echo hi'`,               // nested-pipe dodge
		`md $'\x61ppend' notes.md hi`,           // write-verb dodge (R4 miss)
		`md append $'\x2e\x2e/escape.md' hi`,    // traversal write target (R5 miss)
		`md edit-section $'\x61gents/a1.md' hi`, // fabric-path write target
	} {
		refuse(t, p, "E_BANNED", ExitRefused)
	}
	// and a $'...' command word is refused as a computed name
	refuse(t, `$'\x77c' agents/a1.md`, "E_BANNED", ExitRefused)
}

// ---- item 2: fabric projection paths are read-only write targets (R5) ------

func TestEscape_FabricPathWriteTargetRefused(t *testing.T) {
	for _, p := range []string{
		"md append agents/a1.md hi",
		"md edit-section self/01-memo.md hi",
		"md append agents/a1/01-memo.md hi",
		"md edit-section sessions/MISSION.md hi",
		"md append types/agent.md hi",
		"md append .revs hi",
		"md append ./agents/a1.md hi", // normalization does not let ./ slip past
	} {
		refuse(t, p, "EROFS", ExitRefused)
	}
	// tasks/ is the writable work surface — a tasks write target is NOT refused
	// (the R4 staged-read machinery depends on it).
	accept(t, "md append tasks/t1.md hi")
	accept(t, "md edit-section tasks/t1.md#Notes old new")
}

// ---- item 3: read-side spelling normalization catches staged reads ---------

func TestEscape_StagedReadNormalizationHeld(t *testing.T) {
	// write tasks/t1.md, then read it back spelled differently — each must trip
	// the staged-read trap after normalization (./ strip, #fragment strip).
	for _, p := range []string{
		`md append tasks/t1.md x; grep q ./tasks/t1.md`,      // ./ prefix
		`md append tasks/t1.md x; wc -l tasks/t1.md`,         // exact
		`md append tasks/t1.md x; head < ./tasks/t1.md`,      // ./ in input redirect
		`md edit-section tasks/t1.md#Notes a b; md read tasks/t1.md#Other`, // fragment both sides
	} {
		refuse(t, p, "E_STAGED_READ", ExitRefused)
	}
	// a DIFFERENT normalized path is still legal
	accept(t, `md append tasks/t1.md x; grep q ./tasks/t2.md`)
}

// ---- runtime holds: even preflight-passing reads cannot leave the fabric ----

func TestEscape_TraversalReadsHeld(t *testing.T) {
	fab := buildTestFabric(t, "")
	for _, p := range []string{
		"head ../../../../etc/passwd",
		"head /etc/passwd",
		"grep root /etc/passwd",
		"head /proc/self/mem",
		"head /dev/fd/0",
		"wc -l /etc/hosts",
	} {
		res, err := run(t, fab, p, Options{})
		if err != nil {
			// a preflight/engine refusal is an acceptable hold too
			continue
		}
		if res.Exit == 0 {
			t.Errorf("%q exited 0 — a real file may have been read", p)
		}
		if strings.Contains(string(res.Stdout), "root:") || strings.Contains(string(res.Stdout), "/bin/") {
			t.Errorf("%q leaked real-file content: %q", p, res.Stdout)
		}
	}
}

func TestEscape_PostT0RealFileReadServesSnapshot(t *testing.T) {
	session := testSession(t)
	fab, err := BuildFabric(session, "")
	if err != nil {
		t.Fatal(err)
	}
	defer fab.Close()

	// overwrite the real source AFTER T0
	p := filepath.Join(session, "agents", "a1.md")
	if err := os.WriteFile(p, []byte("---\ntype: agent\n---\n\n# Memo\n\nSECRET-POST-T0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := mustRun(t, fab, "grep -h . agents/a1/01-memo.md")
	if strings.Contains(string(res.Stdout), "SECRET-POST-T0") {
		t.Fatalf("post-T0 mutation leaked into a running read: %q", res.Stdout)
	}
	if !strings.Contains(string(res.Stdout), "alpha line") {
		t.Fatalf("read did not serve the T0 snapshot: %q", res.Stdout)
	}
}

// ---- runtime holds: resource / output floods are bounded -------------------

func TestEscape_OutputFloodCancelled(t *testing.T) {
	fab := buildTestFabric(t, "")
	start := time.Now()
	res, err := run(t, fab, "while :; do echo AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; done",
		Options{MaxStdout: 1 << 10, Timeout: 10 * time.Second})
	if perr, ok := asPipeErr(err); !ok || perr.Code != "E_OVERFLOW" {
		t.Fatalf("want E_OVERFLOW, got %v", err)
	}
	if len(res.Stdout) > 1<<10 {
		t.Errorf("retained %d bytes past the cap", len(res.Stdout))
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("overflow cancel did not fire promptly")
	}
}

func TestEscape_CpuSpinTimesOut(t *testing.T) {
	fab := buildTestFabric(t, "")
	start := time.Now()
	_, err := run(t, fab, "while :; do :; done", Options{Timeout: 300 * time.Millisecond})
	if perr, ok := asPipeErr(err); !ok || perr.Code != "E_TIMEOUT" {
		t.Fatalf("want E_TIMEOUT, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("timeout halt was not prompt")
	}
}

// ---- runtime holds: GNU flag I/O is absent from the toolset ----------------

func TestEscape_ToolsetFlagIOInert(t *testing.T) {
	fab := buildTestFabric(t, "")

	// sort has no GNU -o (write output to file): "-o" is an inert flag and its
	// value becomes an input operand opened via the VFS — no real file is
	// written.
	out := filepath.Join(t.TempDir(), "escape_sort_out")
	if _, err := run(t, fab, "sort -o "+out+" agents/a1.md", Options{}); err != nil {
		t.Fatalf("engine error: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("sort -o wrote a real file at %s (err=%v)", out, statErr)
	}

	// grep has no GNU -f (patterns-from-file): "-f" is inert and the following
	// token is the regex pattern, never opened. Prove it by pointing -f at a
	// real secret file and confirming its content never surfaces.
	secret := filepath.Join(t.TempDir(), "patterns.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET-PATTERN\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := run(t, fab, "grep -f "+secret+" agents/a1.md", Options{})
	if err != nil {
		t.Fatalf("engine error: %v", err)
	}
	if strings.Contains(string(res.Stdout), "TOPSECRET-PATTERN") {
		t.Fatalf("grep -f read a real pattern file: %q", res.Stdout)
	}
}

// asPipeErr unwraps the pipe's *Error from a Run error.
func asPipeErr(err error) (*Error, bool) {
	var perr *Error
	if err == nil {
		return nil, false
	}
	if e, ok := err.(*Error); ok {
		perr = e
	}
	return perr, perr != nil
}
