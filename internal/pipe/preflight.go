package pipe

import (
	"path"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// PIN: go.mod pins mvdan.cc/sh/v3 v3.13.1. escape_test.go MUST be re-run on any
// mvdan bump — a new FS-touching builtin, redirect operator, or expansion form
// could open a hole this static gate does not yet know to close.
//
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
//	brace expansion > cap  → 126 (eager, un-ctx'd, pre-output alloc — U12 DoS; see checkBraceExpansion)
//	nested `md pipe`       → 126 + remedy               (R7)
//	md verb off-allowlist  → 126 (R7 — run/rules/skill/fix are exec-capable; only MdVerbs exist in-pipe)
//	md write to projection → 126 EROFS (R5 write-target model — see vetWriteTarget)
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

// mdVerbAllowed is the R7 allowlist as a set ("pipe" is present only so the
// nested-pipe rejection above owns that teaching; it never reaches the handler).
var mdVerbAllowed = func() map[string]bool {
	m := map[string]bool{"pipe": true}
	for _, v := range MdVerbs {
		m[v] = true
	}
	return m
}()

// THE WRITE-TARGET MODEL (R5, resolved in U9b — this is the authority both the
// static gate below and the runtime handler in mdcmd.go implement):
//
// A pipe write addresses a REAL session file's section, spelled
// "<base>#<hpath>" or "<base>#^id", where <base> is a whole-file fabric
// projection of a real file: agents/<id>.md, tasks/<slug>.md, sessions/<name>.md,
// or types/<kind>.md. The base names WHICH real file; the #fragment is the
// hpath/^id write address plan R5 requires. Cross-agent writes are governed by
// I3 (body.Splice's policy check, actor session/daemon-derived) — addressing is
// not authorization.
//
// NEVER write targets (read-only projections, refused with teaching):
//   - exploded section files (agents/<id>/NN-slug.md) and .properties.yml —
//     projections of section CONTENT; their section space is not the file's;
//   - self/** — a mirror; write your own file as agents/<id>.md#…;
//   - .revs — the T0 manifest;
//   - a FRAGMENTLESS base spelling — that is a whole-FILE projection write,
//     exactly what R5 bans (writes address sections, not projections);
//   - traversal (../, absolute) spellings — outside the fabric namespace.
//
// The static gate is defense-in-depth over LITERAL spellings; mdcmd.go's
// runtime check is authoritative (it also requires the base to exist in the T0
// fabric). One deliberate static relaxation: a fragmentless tasks/ spelling
// passes preflight (U9a's R4 staged-read machinery and suite stage against
// bare tasks/ paths) and is refused at the handler with the same teaching.
var fabricProjectionRoots = map[string]bool{
	"agents": true, "sessions": true, "types": true,
}

// vetWriteTarget applies the write-target model to one literal md write target.
// Nil means the spelling is statically plausible (the handler still decides).
func vetWriteTarget(verb, target, pos string) *Error {
	frag := ""
	base := target
	if i := strings.IndexByte(target, '#'); i >= 0 {
		base, frag = target[:i], target[i+1:]
	}
	base = path.Clean(base)
	refuse := func(why, remedy string) *Error {
		return posErr(ExitRefused, "EROFS",
			"`md "+verb+"` cannot write to "+target+": "+why,
			remedy, pos)
	}
	sectionRemedy := "name the real file's section: <file>.md#<Heading> or <file>.md#^<block-id>"
	if strings.HasPrefix(base, "/") || base == ".." || strings.HasPrefix(base, "../") {
		return refuse("the target escapes the fabric namespace",
			"write targets are session files: agents/<id>.md, tasks/, sessions/, types/ — "+sectionRemedy)
	}
	if base == ".revs" {
		return refuse(".revs is the read-only T0 rev manifest", sectionRemedy)
	}
	if path.Base(base) == ".properties.yml" {
		return refuse(".properties.yml is a read-only frontmatter projection",
			"set properties on the real file (outside the pipe: md set-prop); body writes use "+sectionRemedy)
	}
	seg, rest := base, ""
	if i := strings.IndexByte(base, '/'); i >= 0 {
		seg, rest = base[:i], base[i+1:]
	}
	if seg == "self" {
		return refuse("self/ is a read-only mirror of your own agent file",
			"write your own file by name: agents/<your-id>.md#<Heading>")
	}
	if seg == "agents" && strings.ContainsRune(rest, '/') {
		return refuse("exploded section files (agents/<id>/NN-slug.md) are read-only projections",
			"address the section on the base file: agents/<id>.md#<Heading>")
	}
	if fabricProjectionRoots[seg] && frag == "" {
		return refuse("a whole-file spelling is a read-only projection — writes address a section",
			sectionRemedy)
	}
	return nil
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
			if e := checkBraceExpansion(x); e != nil {
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
		// Every md sub-arg must be a literal. A computed sub-verb or target
		// (parameter, command substitution, or $'...') cannot be statically
		// vetted for the nested-pipe guard, R4 staging, or the R5 write-target
		// check — and $'...' in particular decodes at runtime to bytes preflight
		// never saw. Fail closed rather than analyze a spelling that lies.
		for _, w := range call.Args[1:] {
			if _, lit := wordLitOK(w); !lit {
				return posErr(ExitRefused, "E_BANNED",
					"md argument is not a literal — a computed md sub-verb or target (parameter, command substitution, or $'...') cannot be statically vetted",
					"spell the md sub-verb and its target out as literal words", pos)
			}
		}
		if len(args) > 0 && args[0] == "pipe" {
			return posErr(ExitRefused, "E_BANNED",
				"nested `md pipe` is rejected: one program, one sandbox",
				"merge the inner program into this one", pos)
		}
		// R7 verb allowlist, statically: only the MdVerbs surface exists inside a
		// pipe. Exec-capable verbs (run, rules, skill, fix) are refused BEFORE
		// execution; the in-process handler (mdcmd.go) enforces the same list at
		// runtime as defense in depth.
		if len(args) > 0 && !mdVerbAllowed[args[0]] {
			return posErr(ExitRefused, "E_BANNED",
				"`md "+args[0]+"` is not available inside a pipe. In-pipe md verbs: md "+strings.Join(MdVerbs, " | md "),
				"exec-capable verbs (run, rules, skill, fix) never run in the sandbox; use the allowlisted verbs or run `md "+args[0]+"` outside the pipe", pos)
		}
		if len(args) > 0 && mdWriteVerbs[args[0]] {
			// R5 write-target model (see vetWriteTarget): projections refused.
			if len(args) > 1 {
				if e := vetWriteTarget(args[0], args[1], pos); e != nil {
					return e
				}
			}
			for _, a := range args[1:] {
				if p := pathishArg(a); p != "" {
					staged[normPath(p)] = pos
				}
			}
			return nil
		}
		// md read verbs: reading a path staged earlier is the R4 trap.
		for _, a := range args {
			if p := pathishArg(a); p != "" {
				if wpos, ok := staged[normPath(p)]; ok {
					return stagedReadErr(p, wpos, pos)
				}
			}
		}
	}

	// R4: a file-consuming tool reading a staged path (grep x staged.md, …).
	// echo/printf of the path string is not a read and stays legal.
	if fileReaders[name] {
		for _, a := range args {
			if wpos, ok := staged[normPath(a)]; ok {
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
		if nt := normPath(target); nt != "" {
			if wpos, ok := staged[nt]; ok {
				return stagedReadErr(target, wpos, pos)
			}
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

// braceCap bounds how many words a single word's brace expansion may produce.
//
// U12 DoS: mvdan's expand.Braces (expand/braces.go) materializes the ENTIRE
// product of a word's brace groups into []*syntax.Word with no ctx parameter and
// no size cap, on the normal arg path (expand/expand.go FieldsSeq → SplitBraces).
// A single fully-whitelisted word — `echo {1..1000000000}` or the multiplicative
// `echo {1..100000}{1..100000}` (1e10) — allocates ~1e9-1e10 Words BEFORE any
// byte is written, so it is invisible to both the wall-clock timeout (Braces
// takes no ctx) and the stdout cap (nothing is emitted yet), OOM-ing the shared
// daemon. This gate rejects the word statically.
//
// 10000 is generous for every legitimate hand-written brace: lists ({a,b,c}),
// small ranges ({1..20}), a byte range ({0..255}), a letter range ({a..z}) — all
// far below it. 10000 Words materialize in microseconds and well under a
// megabyte; a genuinely larger range belongs in a `for` loop (ctx-checked per
// iteration), not an eager pre-output allocation.
const braceCap = 10000

// checkBraceExpansion rejects a word whose brace/sequence expansion would exceed
// braceCap elements. It must run SplitBraces itself: the parser does NOT emit
// [syntax.BraceExp] nodes (they "only appear as a result of SplitBraces", which
// the interpreter runs at expansion time), so a raw AST walk never sees a brace
// group. SplitBraces mutates in place, so it runs on a copy — the real AST the
// engine later runs is untouched. Counting is multiplicative across the word's
// brace groups (adjacent groups form a cartesian product, exactly as Braces
// recurses) and saturates at braceCap+1, so a nested product like 1e27 cannot
// overflow int.
func checkBraceExpansion(w *syntax.Word) *Error {
	wc := *w // SplitBraces replaces Parts in place; count on a copy
	if !syntax.SplitBraces(&wc) {
		return nil // no brace groups in this word
	}
	// Count saturates at braceCap+1, so past the cap the exact size is unknown.
	if braceExpansionCount(wc.Parts) <= braceCap {
		return nil
	}
	return posErr(ExitRefused, "E_BRACE_TOO_BIG",
		"brace expansion exceeds the "+strconv.Itoa(braceCap)+"-element cap: the whole range is materialized in memory before any output, uninterruptible by the timeout and invisible to the output cap",
		"use a loop for a large range (for i in ...; do ... done); small brace lists like {a,b,c} or {1..20} are fine",
		w.Pos().String())
}

// braceExpansionCount returns how many words the fully brace-expanded parts
// yield, mirroring expand.Braces' multiplicative model: adjacent brace groups
// multiply, a comma-list's factor is the sum of its elements' own counts (nested
// braces recurse), and a sequence's factor is its step count. Saturates at
// braceCap+1 so an over-cap nested product cannot overflow int.
func braceExpansionCount(parts []syntax.WordPart) int {
	product := 1
	for _, part := range parts {
		br, ok := part.(*syntax.BraceExp)
		if !ok {
			continue // literal / other parts contribute a factor of 1
		}
		product = satMulCap(product, braceGroupFactor(br))
		if product > braceCap {
			return braceCap + 1
		}
	}
	return product
}

// braceGroupFactor is one brace group's multiplier.
func braceGroupFactor(br *syntax.BraceExp) int {
	if br.Sequence {
		return braceSeqSteps(br)
	}
	sum := 0
	for _, elem := range br.Elems {
		sum += braceExpansionCount(elem.Parts)
		if sum > braceCap {
			return braceCap + 1
		}
	}
	return sum
}

// braceSeqSteps returns the number of elements a {from..to[..incr]} sequence
// generates, replicating expand.Braces' arithmetic. After a successful
// SplitBraces the endpoints of a Sequence group are always static (both integer
// literals or both single ASCII letters — otherwise SplitBraces leaves the word
// non-brace), so Lit() is well-defined here.
//
// The span is computed in uint64 because a full-int64-range sequence like
// {-9223372036854775808..9223372036854775807} (both endpoints parse, so mvdan
// treats it as a valid ~1.8e19-element sequence and loops effectively forever) —
// a signed `to - from` there overflows and would UNDERCOUNT to ~1, silently
// passing the cap. uint64 magnitude arithmetic is exact across the whole range,
// and the quotient is compared to braceCap before the `+1` so nothing wraps.
func braceSeqSteps(br *syntax.BraceExp) int {
	if len(br.Elems) < 2 {
		return 1
	}
	fromLit := br.Elems[0].Lit()
	toLit := br.Elems[1].Lit()
	from, err1 := strconv.Atoi(fromLit)
	to, err2 := strconv.Atoi(toLit)
	if err1 != nil || err2 != nil {
		if fromLit == "" || toLit == "" {
			return 1
		}
		from, to = int(fromLit[0]), int(toLit[0]) // char sequence
	}
	upward := from <= to
	incr := 1
	if !upward {
		incr = -1
	}
	if len(br.Elems) > 2 {
		if n, err := strconv.Atoi(br.Elems[2].Lit()); err == nil && n != 0 && (n > 0) == upward {
			incr = n
		}
	}
	// |to - from| as uint64 (exact even when the signed difference overflows).
	var span uint64
	if to >= from {
		span = uint64(to) - uint64(from)
	} else {
		span = uint64(from) - uint64(to)
	}
	step := uint64(incr)
	if incr < 0 {
		step = -uint64(incr) // unsigned negation = exact magnitude, even for minint64
	}
	q := span / step // number of steps beyond the first element (step is never 0)
	if q >= uint64(braceCap) {
		return braceCap + 1 // over cap; avoid the +1 wrapping at uint64 max
	}
	return int(q) + 1
}

// satMulCap multiplies two non-negative factors, saturating at braceCap+1 so the
// running product never overflows int (both inputs are first clamped to the
// ceiling, so the multiply stays far below math.MaxInt).
func satMulCap(a, b int) int {
	const ceil = braceCap + 1
	if a > ceil {
		a = ceil
	}
	if b > ceil {
		b = ceil
	}
	if a == 0 || b == 0 {
		return 0
	}
	if a > ceil/b {
		return ceil
	}
	return a * b
}

func stagedReadErr(path, writePos, readPos string) *Error {
	return posErr(ExitRefused, "E_STAGED_READ",
		"you staged a write to "+path+" (at "+writePos+") and then read it — v1 reads serve the PRE-program snapshot, so the read would silently miss your write",
		"move the read before the write, or split into two programs", readPos)
}

// wordLit returns the word's literal value when statically known, or "" for a
// computed (non-literal) word. Callers that must distinguish a literal empty
// string ("" / ”) from a computed word use wordLitOK.
func wordLit(w *syntax.Word) string {
	s, _ := wordLitOK(w)
	return s
}

// wordLitOK returns the word's literal value and whether it is statically
// known: pure Lit parts, a plain single-quoted string, or a double-quoted
// string of literals are literal. Anything with an expansion — parameter,
// command substitution, arithmetic — is non-literal (ok == false).
//
// A `$'...'` ANSI-C string (SglQuoted with Dollar) is treated as NON-literal
// even though its bytes are statically present: the interpreter DECODES it via
// expand.Format at runtime, so the raw p.Value preflight would see diverges
// from what actually executes (`$'\x70ipe'` → `pipe`). Reporting it as computed
// fails closed — a `$'...'` command word is refused as a computed name and a
// `$'...'` md sub-arg is conservatively rejected, so no decoded byte sequence
// can dodge the nested-pipe guard, R4 staging, or the R5 write-target check.
func wordLitOK(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", true
	}
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", false // $'...' — runtime ANSI-C decode diverges from these bytes
			}
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, ip := range p.Parts {
				il, ok := ip.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(il.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
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

// normPath canonicalizes a path word for staged-map keying and lookup so the
// write side and read side agree on spelling: drop the "#fragment", then
// path.Clean (which strips a leading "./", collapses "//" and interior ".").
// Glob-spelled paths (agents/*.md) are intentionally left un-expanded — the T0
// snapshot is the backstop for a staged read reached only through a glob.
func normPath(p string) string {
	p = stripFragment(p)
	if p == "" {
		return ""
	}
	return path.Clean(p)
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
