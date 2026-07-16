package pipe

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// preflight.go is the static policy gate (plan decision 5 + feasibility F8 +
// review R4/R-discovery). It parses the program with LangBash and walks the AST
// BEFORE anything executes, rejecting every construct that would either escape
// the interpreter's handler mediation (source-verified mvdan v3.13.1 leaks) or
// violate the sandbox's write posture. Each rejection is a teaching error: what
// was refused, why, and the executable remedy.
//
// The rejection matrix, each row traced to its authority:
//
//	unknown command        → 127 + COMPLETE whitelist   (R-discovery)
//	non-literal cmd name   → 126 (cannot be statically vetted)
//	eval / source / .      → 126 (fresh-parser bypass — variant limits don't bind)
//	banned builtins        → 126 (exec, trap, wait, command, mapfile, …)
//	background & / coproc  → 126 (decision 5; Runner.Run does not reap bg — U8 Q5)
//	process substitution   → 126 (real mkfifo, OpenHandler skipped)
//	[ -r/-w/-x ] tests     → 126 (unmediated unix.Access, TODO(v4) gap)
//	[ -O / -G ] tests      → 126 (syscall.Stat_t + user.Current — feasibility F8)
//	pwd -P                 → 126 (filepath.EvalSymlinks leaks the real scratch path)
//	read -s                → 126 (term.ReadPassword reads real fd 0 — F8)
//	~user tilde            → 126 (real os/user.Lookup — F8; bare ~ and ~/ are fine)
//	write-redirect any path→ 126 except /dev/null       (R3; U8 delta 2 carve-out)
//	read of staged path    → 126 (R4 — reads serve T0; a read-back would be silently stale)
//	nested `md pipe`       → 126 + remedy               (R7 seam)
//	syntax error           → 2
//
// The staged-read check (R4) is static and literal: it tracks literal path
// words targeted by `md append|edit-section|create_section` and rejects any
// later statement that references the same literal path (command argument or
// input redirect). It is deliberately conservative — it cannot see through
// variables, but neither can a cold agent reading its own program; the dynamic
// backstop is that v1 reads always serve T0.

// allowedBuiltins are the interpreter builtins a program may call as plain
// commands. declare/local/export/readonly/typeset/let parse as their own AST
// nodes (DeclClause/LetClause) and never appear as CallExpr command names.
var allowedBuiltins = map[string]bool{
	":": true, "true": true, "false": true,
	"echo": true, "printf": true,
	"pwd": true, "cd": true,
	"test": true, "[": true,
	"exit": true, "return": true, "break": true, "continue": true,
	"shift": true, "set": true, "unset": true,
	"read": true, "type": true, "getopts": true,
}

// bannedBuiltins are recognized names refused by policy (126), with tailored
// teaching where the refusal is subtle.
var bannedBuiltins = map[string]string{
	"eval":      "eval reparses its argument with a fresh parser, bypassing this gate — write the logic directly",
	"source":    "source executes an external file outside the program text — inline the content",
	".":         "`.` (source) executes an external file outside the program text — inline the content",
	"exec":      "exec replaces or re-plumbs the shell — not available in the pipe sandbox",
	"trap":      "trap is not available in the pipe sandbox",
	"wait":      "background jobs are rejected, so wait has nothing to wait for",
	"command":   "command bypasses function lookup — call the tool directly",
	"builtin":   "builtin dispatch is not available in the pipe sandbox",
	"mapfile":   "mapfile is not available — use a while read loop",
	"readarray": "readarray is not available — use a while read loop",
	"jobs":      "background jobs are rejected in the pipe",
	"fg":        "background jobs are rejected in the pipe",
	"bg":        "background jobs are rejected in the pipe",
	"kill":      "kill is not available in the pipe sandbox",
	"umask":     "umask is meaningless in a read-only fabric",
	"ulimit":    "ulimit is not available in the pipe sandbox",
}

// unsafeUnaryTests are the test operators that hit the real OS unmediated
// (unix.Access for -r/-w/-x; Stat_t owner + user.Current for -O/-G).
var unsafeUnaryTests = map[syntax.UnTestOperator]string{
	syntax.TsRead:   "-r",
	syntax.TsWrite:  "-w",
	syntax.TsExec:   "-x",
	syntax.TsUsrOwn: "-O",
	syntax.TsGrpOwn: "-G",
}

// mdWriteVerbs are the md sub-verbs that stage a write (R4 tracking).
var mdWriteVerbs = map[string]bool{
	"append": true, "edit-section": true, "create_section": true,
}

// fileReaders are the commands whose path arguments are file READS (R4 scope);
// echo/printf mentioning a path is not a read.
var fileReaders = map[string]bool{
	"cut": true, "grep": true, "head": true, "sort": true,
	"tail": true, "uniq": true, "wc": true,
}

// Preflight parses program and applies the rejection matrix. On success it
// returns the parsed file for the engine to run (parse once, run once).
func Preflight(program string) (*syntax.File, *Error) {
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(program), "pipe")
	if err != nil {
		return nil, posErr(ExitSyntax, "E_SYNTAX", "program does not parse: "+err.Error(),
			"fix the bash syntax; `md pipe --grammar` shows a worked example", "")
	}

	// Pass 1: user-defined function names are callable.
	funcs := map[string]bool{}
	syntax.Walk(file, func(n syntax.Node) bool {
		if fd, ok := n.(*syntax.FuncDecl); ok {
			funcs[fd.Name.Value] = true
		}
		return true
	})

	// Pass 2: the matrix. Walk visits nodes in source order, which is what the
	// staged-read tracking relies on.
	staged := map[string]string{} // literal path → position it was staged at
	var perr *Error
	reject := func(e *Error) bool {
		if perr == nil {
			perr = e
		}
		return false // stop descending; first error wins
	}

	syntax.Walk(file, func(n syntax.Node) bool {
		if perr != nil {
			return false
		}
		switch x := n.(type) {
		case *syntax.Stmt:
			if x.Background {
				return reject(posErr(ExitRefused, "E_BANNED",
					"background `&` is rejected: the pipe runs one foreground program",
					"drop the `&`; the program already runs to completion before commit",
					x.Pos().String()))
			}
			if x.Coprocess {
				return reject(posErr(ExitRefused, "E_BANNED",
					"coproc is rejected: no background jobs in the pipe",
					"restructure as a plain pipeline", x.Pos().String()))
			}
		case *syntax.CoprocClause:
			return reject(posErr(ExitRefused, "E_BANNED",
				"coproc is rejected: no background jobs in the pipe",
				"restructure as a plain pipeline", x.Pos().String()))
		case *syntax.ProcSubst:
			return reject(posErr(ExitRefused, "E_BANNED",
				"process substitution <( ) creates a real FIFO outside the sandbox",
				"use a plain pipeline or an intermediate variable", x.Pos().String()))
		case *syntax.TestClause:
			if e := checkTestClause(x); e != nil {
				return reject(e)
			}
		case *syntax.Redirect:
			if e := checkRedirect(x, staged); e != nil {
				return reject(e)
			}
		case *syntax.Word:
			if e := checkTildeUser(x); e != nil {
				return reject(e)
			}
		case *syntax.CallExpr:
			if e := checkCall(x, funcs, staged); e != nil {
				return reject(e)
			}
		}
		return true
	})
	if perr != nil {
		return nil, perr
	}
	return file, nil
}

// checkCall vets one simple command: name known, banned builtins, per-command
// flag bans (pwd -P, read -s), md sub-verb seams, and R4 staged-read tracking.
func checkCall(call *syntax.CallExpr, funcs map[string]bool, staged map[string]string) *Error {
	if len(call.Args) == 0 {
		return nil // assignment-only statement
	}
	name := wordLit(call.Args[0])
	pos := call.Pos().String()
	if name == "" {
		return posErr(ExitRefused, "E_BANNED",
			"command name must be a literal word — a computed name cannot be statically vetted",
			"spell the command out; the whitelist is: "+WhitelistLine(), pos)
	}
	if msg, banned := bannedBuiltins[name]; banned {
		return posErr(ExitRefused, "E_BANNED", name+": "+msg, "", pos)
	}

	args := literalArgs(call.Args[1:])

	switch {
	case name == "pwd":
		for _, a := range args {
			if isFlagWith(a, 'P') {
				return posErr(ExitRefused, "E_BANNED",
					"pwd -P resolves real symlinks and leaks the scratch path",
					"use plain pwd — $PWD is already the fabric path", pos)
			}
		}
	case name == "read":
		for _, a := range args {
			if isFlagWith(a, 's') {
				return posErr(ExitRefused, "E_BANNED",
					"read -s reads the real terminal, outside the sandbox",
					"use plain read", pos)
			}
		}
	case name == "test" || name == "[":
		for _, a := range args {
			switch a {
			case "-r", "-w", "-x", "-O", "-G":
				return posErr(ExitRefused, "E_BANNED",
					"test "+a+" checks the REAL filesystem (unmediated access), not the fabric",
					"use -e / -f / -d / -s — those are served by the fabric", pos)
			}
		}
	case name == "md":
		if len(args) > 0 && args[0] == "pipe" {
			return posErr(ExitRefused, "E_BANNED",
				"nested `md pipe` is rejected: one program, one sandbox",
				"merge the inner program into this one", pos)
		}
		if len(args) > 0 && mdWriteVerbs[args[0]] {
			for _, a := range args[1:] {
				if p := pathishArg(a); p != "" {
					staged[stripFragment(p)] = pos
				}
			}
			return nil
		}
		// md read verbs: reading a path staged earlier is the R4 trap.
		for _, a := range args {
			if p := pathishArg(a); p != "" {
				if wpos, ok := staged[stripFragment(p)]; ok {
					return stagedReadErr(p, wpos, pos)
				}
			}
		}
	}

	// R4: a file-consuming tool reading a staged path (grep x staged.md, …).
	// echo/printf of the path string is not a read and stays legal.
	if fileReaders[name] {
		for _, a := range args {
			if wpos, ok := staged[a]; ok {
				return stagedReadErr(a, wpos, pos)
			}
		}
	}

	if allowedBuiltins[name] || funcs[name] {
		return nil
	}
	for _, w := range Whitelist {
		if name == w {
			return nil
		}
	}
	return posErr(ExitUnknown, "E_UNKNOWN_CMD",
		name+": not in the pipe whitelist. Available commands: "+WhitelistLine(),
		"`md pipe --grammar` shows the full grammar and a worked example", pos)
}

// checkRedirect enforces the write posture on redirections: every write-class
// operator is refused at ANY path (R3) except the /dev/null carve-out
// (U8 delta 2). Pure fd-dups (2>&1, >&2) stay legal. Input redirects are
// legal but participate in R4 staged-read tracking.
func checkRedirect(r *syntax.Redirect, staged map[string]string) *Error {
	pos := r.Pos().String()
	target := ""
	if r.Word != nil {
		target = wordLit(r.Word)
	}
	switch r.Op {
	case syntax.RdrIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		if wpos, ok := staged[target]; ok && target != "" {
			return stagedReadErr(target, wpos, pos)
		}
		return nil
	case syntax.DplIn:
		return nil
	case syntax.DplOut:
		// >&N / >&- are fd dups; >&file is a write redirect in disguise.
		if target == "-" || isAllDigits(target) {
			return nil
		}
	}
	// Every remaining operator (>, >>, >|, <>, &>, &>>, and >&file) writes.
	if target == "/dev/null" {
		return nil
	}
	return posErr(ExitRefused, "EROFS",
		"write redirect refused: no writable path exists in the pipe — the fabric is a read-only projection",
		"writes go through `md` (staged, committed at program end); `>/dev/null` is allowed", pos)
}

// checkTestClause rejects [[ ]] operators that bypass handler mediation.
func checkTestClause(tc *syntax.TestClause) *Error {
	var perr *Error
	syntax.Walk(tc, func(n syntax.Node) bool {
		if perr != nil {
			return false
		}
		if ut, ok := n.(*syntax.UnaryTest); ok {
			if flag, bad := unsafeUnaryTests[ut.Op]; bad {
				perr = posErr(ExitRefused, "E_BANNED",
					"[[ "+flag+" ]] checks the REAL filesystem (unmediated access), not the fabric",
					"use -e / -f / -d / -s — those are served by the fabric", ut.Pos().String())
				return false
			}
		}
		return true
	})
	return perr
}

// checkTildeUser rejects ~user (real os/user.Lookup); bare ~ and ~/ expand to
// the curated HOME and are fine.
func checkTildeUser(w *syntax.Word) *Error {
	if len(w.Parts) == 0 {
		return nil
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return nil
	}
	v := lit.Value
	if !strings.HasPrefix(v, "~") || v == "~" || strings.HasPrefix(v, "~/") {
		return nil
	}
	return posErr(ExitRefused, "E_BANNED",
		v+": ~user expansion performs a real user lookup outside the sandbox",
		"use ~ or ~/ (the curated HOME), or an explicit fabric path", w.Pos().String())
}

func stagedReadErr(path, writePos, readPos string) *Error {
	return posErr(ExitRefused, "E_STAGED_READ",
		"you staged a write to "+path+" (at "+writePos+") and then read it — v1 reads serve the PRE-program snapshot, so the read would silently miss your write",
		"move the read before the write, or split into two programs", readPos)
}

// wordLit returns the word's literal value when it is statically known:
// pure Lit parts, a single-quoted string, or a double-quoted string of
// literals. Anything with expansions returns "".
func wordLit(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, ip := range p.Parts {
				il, ok := ip.(*syntax.Lit)
				if !ok {
					return ""
				}
				b.WriteString(il.Value)
			}
		default:
			return ""
		}
	}
	return b.String()
}

func literalArgs(words []*syntax.Word) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, wordLit(w))
	}
	return out
}

// pathishArg reports a literal arg that plausibly names a fabric file: it
// contains a path separator or a .md/.yml suffix, and is not a flag.
func pathishArg(a string) string {
	if a == "" || strings.HasPrefix(a, "-") {
		return ""
	}
	if strings.Contains(a, "/") || strings.HasSuffix(a, ".md") || strings.HasSuffix(a, ".yml") ||
		strings.Contains(a, ".md#") {
		return a
	}
	return ""
}

// stripFragment drops an "#hpath" / "#^id" fragment from an md target.
func stripFragment(p string) string {
	if i := strings.IndexByte(p, '#'); i >= 0 {
		return p[:i]
	}
	return p
}

// isFlagWith reports whether a is a short-flag cluster containing c (e.g.
// -P, -LP, -rs) — long flags (--…) don't count.
func isFlagWith(a string, c byte) bool {
	if len(a) < 2 || a[0] != '-' || a[1] == '-' {
		return false
	}
	return strings.IndexByte(a[1:], c) >= 0
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
