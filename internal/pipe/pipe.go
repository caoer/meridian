// Package pipe is the md-as-filesystem pipe sandbox, tier-1 (U9a): an embedded
// mvdan.cc/sh interpreter over a read-only virtual projection of a session
// ("/fabric"), a static preflight gate that rejects every construct known to
// escape the interpreter's mediation, and a small pure-Go toolset (grep, head,
// tail, wc, sort, uniq, cut) running in-process over stdio.
//
// The sandbox posture (plan decisions 4/5/6/9 + review findings R3/R4/R5):
//
//   - No writable path exists. The OpenHandler denies every write-mode open
//     regardless of path (R3); the only carve-out is /dev/null, which is served
//     as an in-memory discard so `>/dev/null 2>&1` keeps working (U8 delta 2).
//     Writes happen exclusively through the staged `md` handler (U9b), never an
//     OS-level open.
//   - Reads serve the T0 snapshot. The fabric is materialized once at build
//     time; mutating the real files mid-program does not change what a running
//     program reads.
//   - Fabric paths are read-only projections. The `md` write handler (U9b)
//     never accepts a fabric path as a write target — only hpath/`^id` (R5).
//   - Preflight is the policy gate: everything the interpreter cannot mediate
//     (unix.Access tests, process substitution, eval's fresh-parser bypass,
//     background jobs, pwd -P, read -s, ~user lookup) is rejected before
//     execution with a teaching error (see preflight.go).
//   - cd works because the directory SKELETON is real (mkdir-only scratch tree,
//     zero symlinks); file content is virtual via handlers (decision 4).
package pipe

import (
	"strings"
)

// Exit-code convention (plan decision 8): 127 unknown command, 126 recognized
// but refused by policy, 124 wall-clock timeout (GNU timeout convention), 141
// output-cap overflow (SIGPIPE-like: the reader stopped listening), 2 syntax.
const (
	ExitUnknown  = 127
	ExitRefused  = 126
	ExitTimeout  = 124
	ExitOverflow = 141
	ExitSyntax   = 2
)

// Error is the pipe's structured error, one envelope across every emission site
// (preflight, VFS denial, engine) mirroring the body engine's
// {code, message, remedy, context} teaching-error shape, plus the exit code the
// CLI maps it to.
type Error struct {
	// Exit is the exit code per the decision-8 convention above.
	Exit int
	// Code is the stable machine code, e.g. "E_UNKNOWN_CMD", "E_BANNED",
	// "E_SYNTAX", "E_TIMEOUT", "E_OVERFLOW", "E_INTERP", "EROFS".
	Code string
	// Message states what was refused and why, in one line.
	Message string
	// Remedy is the executable next step.
	Remedy string
	// Context carries structured detail (position, offending word).
	Context map[string]string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Remedy != "" {
		return e.Code + ": " + e.Message + " — " + e.Remedy
	}
	return e.Code + ": " + e.Message
}

// Whitelist is the complete set of external commands a pipe program may call
// (decision 6). Everything else is a 127 teaching error listing this set.
// Bash builtins (echo, printf, cd, pwd, test, read, …) are handled by the
// interpreter itself and are allowed except where preflight bans them.
var Whitelist = []string{"cut", "grep", "head", "md", "sort", "tail", "uniq", "wc"}

// MdVerbs is the in-pipe `md` sub-verb allowlist (R7). U9a documents it in the
// grammar; U9b enforces it in the staged handler. Exec-capable verbs
// (run, rules, skill, fix --write) are excluded by construction.
var MdVerbs = []string{"append", "create_section", "def-check", "edit-section", "read", "toc"}

// WhitelistLine renders the complete whitelist as one teaching line
// (R-discovery: the first 127 must list ALL of it, not a sample).
func WhitelistLine() string {
	return strings.Join(Whitelist, " ")
}

// Grammar is the discovery surface printed by `md pipe --help` / `--grammar`
// (R-discovery): the complete command whitelist, the `md` sub-verbs, and one
// worked example — enough for a cold agent to write its first program.
func Grammar() string {
	var b strings.Builder
	b.WriteString("md pipe — run one bash program over the read-only /fabric projection.\n\n")
	b.WriteString("Commands (complete whitelist):\n  " + WhitelistLine() + "\n")
	b.WriteString("  plus bash builtins: echo printf cd pwd test [ [[ read set shift local declare export true false :\n\n")
	b.WriteString("md sub-verbs inside a pipe:\n  md " + strings.Join(MdVerbs, " | md ") + "\n")
	b.WriteString("  (reads serve the pre-program snapshot; writes are STAGED and commit at program end)\n\n")
	b.WriteString("Layout:\n")
	b.WriteString("  agents/<id>.md          whole agent file\n")
	b.WriteString("  agents/<id>/            exploded: .properties.yml + NN-slug.md per section\n")
	b.WriteString("  self/                   your own agent file, exploded\n")
	b.WriteString("  tasks/ sessions/ types/ task files, session files, defs\n")
	b.WriteString("  .revs                   rev manifest at snapshot time\n\n")
	b.WriteString("Example:\n")
	b.WriteString("  for f in agents/*/01-memo.md; do wc -l \"$f\"; done | sort | head -5\n\n")
	b.WriteString("Not available: writes via redirect (use md), eval/source, background &,\n")
	b.WriteString("process substitution, [ -r/-w/-x/-O/-G ], pwd -P, read -s, ~user, nested md pipe.\n")
	return b.String()
}

// posErr builds a positioned Error.
func posErr(exit int, code, msg, remedy, pos string) *Error {
	e := &Error{Exit: exit, Code: code, Message: msg, Remedy: remedy}
	if pos != "" {
		e.Context = map[string]string{"pos": pos}
	}
	return e
}
