package pipe

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/caoer/meridian/internal/body"
	"github.com/caoer/meridian/internal/defs"
	"github.com/caoer/meridian/internal/frontmatter"
)

// mdcmd.go is the in-pipe `md` handler — the R7 seam, closed:
//
//   - VERB ALLOWLIST (R7, security-critical): toc · read · append ·
//     edit-section · create_section · def-check. NOTHING ELSE. The exec-capable
//     verbs (run, rules, skill, fix --write) are excluded by construction: this
//     handler routes to IN-PROCESS functions over the body engine and NEVER
//     spawns the `md` binary, so there is no argv surface an excluded verb
//     could reach. Preflight already rejects off-allowlist verbs statically;
//     this file enforces the same list at runtime as defense in depth.
//   - CURATED ENV: nothing here reads os.Environ. The write actor arrives on
//     the Txn (session/daemon-derived, bound by the host face); the def cascade
//     resolves from the record's real path. A program can reflect no
//     CCC_*/token environment through this handler (adversarial F1 oracle).
//   - READS SERVE T0: toc / read / def-check operate on the fabric's snapshot
//     bytes, never the live files — consistent with every other read in the
//     sandbox (preflight's R4 staged-read rejection is the static guard; the
//     snapshot is the dynamic backstop).
//   - WRITES STAGE ONLY: append / edit-section / create_section validate
//     against the T0 state (teaching errors surface mid-program, at the call
//     site) and stage a body.Edit into the Txn. No disk write happens before
//     the program ends; the commit (txn.go) carries I3 authorization, the rev
//     ladder, the reparse gate and the journal via body.Splice.
//
// The write-target model these verbs enforce is pinned in preflight.go (R5,
// vetWriteTarget): base file#hpath / #^id only; projections never.

// MdCmd routes in-pipe `md` calls. Fab serves the T0 snapshot; Txn accumulates
// staged writes.
type MdCmd struct {
	Fab *Fabric
	Txn *Txn
}

// Handler adapts MdCmd to the interpreter's MdHandler seam.
func (m *MdCmd) Handler() MdHandler {
	return func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
		return m.run(ctx, args, stdin, stdout, stderr)
	}
}

// teach prints a teaching error to stderr and returns its exit code.
func teach(stderr io.Writer, e *Error) int {
	fmt.Fprintln(stderr, "md: "+e.Error())
	return e.Exit
}

func usageErr(msg, remedy string) *Error {
	return &Error{Exit: ExitRefused, Code: "E_USAGE", Message: msg, Remedy: remedy}
}

func (m *MdCmd) run(_ context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return teach(stderr, usageErr("md needs a verb",
			"in-pipe verbs: md "+strings.Join(MdVerbs, " | md ")))
	}
	verb := args[0]
	switch verb {
	case "toc":
		return m.toc(args[1:], stdout, stderr)
	case "read":
		return m.read(args[1:], stdout, stderr)
	case "def-check":
		return m.defCheck(args[1:], stdout, stderr)
	case "append", "edit-section", "create_section":
		return m.stageWrite(verb, args[1:], stdin, stderr)
	case "pipe":
		// Preflight owns this teaching; kept as runtime defense in depth.
		return teach(stderr, &Error{Exit: ExitRefused, Code: "E_BANNED",
			Message: "nested `md pipe` is rejected: one program, one sandbox",
			Remedy:  "merge the inner program into this one"})
	default:
		// R7: everything off the allowlist — run, rules, skill, fix, … — is
		// refused. This handler has no path to the md binary or to any exec.
		return teach(stderr, &Error{Exit: ExitRefused, Code: "E_BANNED",
			Message: "`md " + verb + "` is not available inside a pipe. In-pipe md verbs: md " + strings.Join(MdVerbs, " | md "),
			Remedy:  "exec-capable verbs never run in the sandbox; run `md " + verb + "` outside the pipe"})
	}
}

// snapshotDoc parses the T0 snapshot of a fabric-relative file.
func (m *MdCmd) snapshotDoc(rel string) (*body.Document, *Error) {
	raw := m.Fab.Snapshot(rel)
	if raw == nil {
		return nil, &Error{Exit: ExitRefused, Code: "E_NO_MATCH",
			Message: rel + ": no such fabric file in the snapshot",
			Remedy:  "paths are fabric-relative (agents/<id>.md, tasks/, sessions/, types/); ls or glob to discover them"}
	}
	doc, err := body.Parse(raw)
	if err != nil {
		return nil, &Error{Exit: ExitRefused, Code: "E_FAIL_LOUD",
			Message: rel + ": snapshot does not parse: " + err.Error()}
	}
	return doc, nil
}

// splitTarget splits "file#frag" (frag may be "Heading/Path" or "^id").
func splitTarget(target string) (rel, frag string) {
	if i := strings.IndexByte(target, '#'); i >= 0 {
		return target[:i], target[i+1:]
	}
	return target, ""
}

// toc renders a document's shape as header-less TSV — pipe-composable rows:
// N, depth, words, sec_rev, hpath (hpath last: it may contain spaces).
func (m *MdCmd) toc(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return teach(stderr, usageErr("md toc takes exactly one file", "md toc agents/<id>.md"))
	}
	rel, frag := splitTarget(args[0])
	if frag != "" {
		return teach(stderr, usageErr("md toc operates on a whole file, not a fragment",
			"drop the #"+frag+"; use `md read "+rel+"#"+frag+"` for one section"))
	}
	doc, perr := m.snapshotDoc(rel)
	if perr != nil {
		return teach(stderr, perr)
	}
	for _, s := range doc.Toc().Sections {
		fmt.Fprintf(stdout, "%s\t%d\t%d\t%s\t%s\n", s.N, s.Depth, s.Words, s.Rev, s.HPath)
	}
	return 0
}

// read serves T0 content: the whole file (raw snapshot bytes — works for .revs
// and .properties.yml too), or one section/block via #fragment.
func (m *MdCmd) read(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return teach(stderr, usageErr("md read takes exactly one target", "md read tasks/t1.md#Task"))
	}
	rel, frag := splitTarget(args[0])
	if frag == "" {
		raw := m.Fab.Snapshot(rel)
		if raw == nil {
			return teach(stderr, &Error{Exit: ExitRefused, Code: "E_NO_MATCH",
				Message: rel + ": no such fabric file in the snapshot"})
		}
		stdout.Write(raw)
		return 0
	}
	doc, perr := m.snapshotDoc(rel)
	if perr != nil {
		return teach(stderr, perr)
	}
	sec, err := doc.Read(frag)
	if err != nil {
		return teach(stderr, &Error{Exit: ExitRefused, Code: "E_NO_MATCH",
			Message: rel + "#" + frag + ": " + err.Error()})
	}
	stdout.Write(sec.Content)
	return 0
}

// defCheck runs the def-driven validator over the T0 snapshot of a base file.
// The record's CONTENT is the snapshot; the def cascade resolves from the real
// session path (defs are host config, not program-addressable state).
func (m *MdCmd) defCheck(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return teach(stderr, usageErr("md def-check takes exactly one file", "md def-check agents/<id>.md"))
	}
	rel, frag := splitTarget(args[0])
	if frag != "" {
		return teach(stderr, usageErr("md def-check operates on a whole record", "drop the #fragment"))
	}
	real, isBase := m.Fab.RealPaths[rel]
	if !isBase {
		return teach(stderr, &Error{Exit: ExitRefused, Code: "E_NO_MATCH",
			Message: rel + ": def-check targets a base record (agents/<id>.md, tasks/, sessions/, types/)"})
	}
	doc, perr := m.snapshotDoc(rel)
	if perr != nil {
		return teach(stderr, perr)
	}
	fm, err := frontmatter.ParseBytes(doc.Bytes())
	if err != nil || fm == nil {
		return teach(stderr, &Error{Exit: ExitRefused, Code: "E_FAIL_LOUD",
			Message: rel + ": unreadable frontmatter", Remedy: "def-check needs a `type:` key"})
	}
	kind := fm.StringField("type")
	if kind == "" {
		return teach(stderr, &Error{Exit: ExitRefused, Code: "E_FAIL_LOUD",
			Message: rel + ": no `type:` in frontmatter",
			Remedy:  "def-check resolves the def by the record's kind"})
	}
	def, derr := defs.Resolve(kind, fm.StringField("preset"), defs.DiscoverLayers(real))
	if derr != nil {
		// Fail closed like the CLI: stratum-1 findings only, nonzero exit.
		fmt.Fprintf(stdout, "verdict\tfail-closed\n")
		for _, f := range defs.ScanNested(doc) {
			fmt.Fprintf(stdout, "finding\t%s\t%s\t%s\n", f.Severity, f.RuleID, f.Message)
		}
		fmt.Fprintf(stdout, "finding\terror\tdef/malformed\t%s\n", derr.Error())
		return 1
	}
	rep := defs.Check(doc, def)
	fmt.Fprintf(stdout, "verdict\t%s\n", rep.Verdict)
	for _, s := range rep.Sections {
		fmt.Fprintf(stdout, "section\t%s\t%s\n", s.Verdict, s.Title)
	}
	exit := 0
	for _, f := range rep.Findings {
		fmt.Fprintf(stdout, "finding\t%s\t%s\t%s\n", f.Severity, f.RuleID, f.Message)
		if f.Severity == "error" {
			exit = 1
		}
	}
	if rep.Verdict == "invalid" {
		exit = 1
	}
	return exit
}

// maxStdinContent caps a `-` stdin read for a staged write (bounded before the
// Txn's own transaction cap sees it).
const maxStdinContent = 1 << 20

// stageWrite validates one write verb against the T0 state and stages it.
// Teaching errors surface HERE, mid-program, at the call site; commit-time
// guards (I3, CAS, reparse, I4) surface in the structured commit receipt.
func (m *MdCmd) stageWrite(verb string, args []string, stdin io.Reader, stderr io.Writer) int {
	if len(args) == 0 {
		return teach(stderr, usageErr("md "+verb+" needs a target",
			"md "+verb+" <file>.md#<Heading> …"))
	}
	target := args[0]
	// The write-target model — same authority as the static gate.
	if e := vetWriteTarget(verb, target, ""); e != nil {
		return teach(stderr, e)
	}
	rel, frag := splitTarget(target)
	rel = normPath(rel)
	if frag == "" {
		return teach(stderr, &Error{Exit: ExitRefused, Code: "EROFS",
			Message: "`md " + verb + "` addresses a section, not a whole file: " + target,
			Remedy:  "name the section: " + rel + "#<Heading> or " + rel + "#^<block-id>"})
	}
	real, isBase := m.Fab.RealPaths[rel]
	if !isBase || real == "" {
		return teach(stderr, &Error{Exit: ExitRefused, Code: "E_NO_MATCH",
			Message: rel + ": not a session file in the T0 snapshot — the pipe writes only base files that existed at snapshot time",
			Remedy:  "base files: agents/<id>.md, tasks/<slug>.md, sessions/<name>.md, types/<kind>.md"})
	}
	doc, perr := m.snapshotDoc(rel)
	if perr != nil {
		return teach(stderr, perr)
	}

	content := func(i int) (string, *Error) {
		if len(args) <= i {
			return "", usageErr("md "+verb+" is missing its content argument",
				"pass the content as one literal word, or `-` to read it from stdin")
		}
		if len(args) > i+1 {
			return "", usageErr("md "+verb+" takes one content argument; got extra words",
				"quote the content as ONE word: md "+verb+" '"+target+"' 'line of content'")
		}
		if args[i] == "-" {
			b, err := io.ReadAll(io.LimitReader(stdin, maxStdinContent+1))
			if err != nil {
				return "", &Error{Exit: ExitRefused, Code: "E_FAIL_LOUD", Message: "reading stdin content: " + err.Error()}
			}
			if len(b) > maxStdinContent {
				return "", &Error{Exit: ExitRefused, Code: "E_OVERFLOW",
					Message: "stdin content exceeds the 1MB per-write cap",
					Remedy:  "narrow the piped content (head/grep) — a pipe write is not a bulk loader"}
			}
			return string(b), nil
		}
		return args[i], nil
	}

	var edit body.Edit
	switch verb {
	case "append":
		c, cerr := content(1)
		if cerr != nil {
			return teach(stderr, cerr)
		}
		if c == "" {
			return teach(stderr, usageErr("md append has no content", "empty appends are refused"))
		}
		if _, err := doc.Read(frag); err != nil {
			return teach(stderr, &Error{Exit: ExitRefused, Code: "E_NO_MATCH",
				Message: rel + "#" + frag + ": " + err.Error(),
				Remedy:  "md toc " + rel + " lists the sections"})
		}
		edit = body.Edit{Op: body.OpAppend, Target: frag, New: c}
	case "edit-section":
		if len(args) != 3 {
			return teach(stderr, usageErr("md edit-section takes: <file>#<Section> <old> <new>",
				"old is the exact bytes to replace (byte-exact, unique in the section)"))
		}
		old, repl := args[1], args[2]
		if old == "" {
			return teach(stderr, usageErr("md edit-section needs a non-empty <old> anchor", ""))
		}
		sec, err := doc.Read(frag)
		if err != nil {
			return teach(stderr, &Error{Exit: ExitRefused, Code: "E_NO_MATCH",
				Message: rel + "#" + frag + ": " + err.Error(),
				Remedy:  "md toc " + rel + " lists the sections"})
		}
		switch n := strings.Count(string(sec.Content), old); {
		case n == 0:
			return teach(stderr, &Error{Exit: ExitRefused, Code: "E_NO_MATCH",
				Message: "anchor not found in " + rel + "#" + frag + " (T0 snapshot)",
				Remedy:  "md read " + rel + "#" + frag + " shows the current content"})
		case n > 1:
			return teach(stderr, &Error{Exit: ExitRefused, Code: "E_AMBIGUOUS",
				Message: fmt.Sprintf("anchor matches %d times in %s#%s", n, rel, frag),
				Remedy:  "widen the anchor until it is unique in the section"})
		}
		// Pin the T0 sec_rev as the CAS token: commit conflicts loudly if the
		// section drifted (belt to the commit's file-level CAS suspenders).
		edit = body.Edit{Op: body.OpReplace, Target: frag, Find: old, New: repl, Rev: sec.Rev}
	case "create_section":
		c := ""
		if len(args) > 1 {
			var cerr *Error
			if c, cerr = content(1); cerr != nil {
				return teach(stderr, cerr)
			}
		}
		if strings.HasPrefix(frag, "^") {
			return teach(stderr, usageErr("create_section takes a heading, not a ^block", ""))
		}
		if _, err := doc.Read(frag); err == nil {
			return teach(stderr, &Error{Exit: ExitRefused, Code: "E_EXISTS",
				Message: rel + "#" + frag + " already exists",
				Remedy:  "append to it, or create a distinct heading"})
		}
		edit = body.Edit{Op: body.OpCreateSection, Target: frag, New: c}
	}

	if e := m.Txn.Stage(rel, edit); e != nil {
		return teach(stderr, e)
	}
	return 0 // silent success: the receipt is the record; reads still serve T0
}
