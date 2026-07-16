package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// req is the common (actor, path, section, op) check with a CAS treated as
// provided — the shape for pinning actor/match semantics without the CAS rung.
func req(p *Pack, actor, path, section, op string) Decision {
	return p.Check(Request{Actor: actor, Path: path, Section: section, Op: op, CASProvided: true})
}

// TestBuiltinPackLoads: the builtin pack passes its own strict loader.
func TestBuiltinPackLoads(t *testing.T) {
	p := Builtin()
	if p.Version != 1 {
		t.Fatalf("builtin version = %d, want 1", p.Version)
	}
	if len(p.Rules) == 0 {
		t.Fatal("builtin pack has no rules")
	}
}

// TestBuiltinTasksSection: '# Tasks' is cc-task-sync's, in any file — even the
// file's own owner may not write it (the read-only sync direction). The deny
// teaches the owner (cc-task-sync) and the sanctioned tool.
func TestBuiltinTasksSection(t *testing.T) {
	p := Builtin()

	// non-cc-task-sync actor is refused, teaching names cc-task-sync.
	d := req(p, "worker", "agents/b.md", "Tasks", "append")
	if d.Allow || d.Code != "EPERM" {
		t.Fatalf("Tasks write by non-sync should EPERM: %+v", d)
	}
	if !strings.Contains(d.Teaching, "cc-task-sync") {
		t.Fatalf("Tasks deny must name the sanctioned writer: %q", d.Teaching)
	}

	// the file's OWNER is still refused on Tasks (this is the whole point).
	if d := req(p, "b", "agents/b.md", "Tasks", "append"); d.Allow {
		t.Fatalf("owner must not write its own synced Tasks: %+v", d)
	}

	// cc-task-sync is allowed.
	if d := req(p, "cc-task-sync", "agents/b.md", "Tasks", "append"); !d.Allow {
		t.Fatalf("cc-task-sync must be allowed on Tasks: %+v", d)
	}
}

// TestBuiltinAgentOwnsFile: an agent owns its own file; another agent is refused
// and the deny names the owner + path.
func TestBuiltinAgentOwnsFile(t *testing.T) {
	p := Builtin()

	// owner may write a plain section of its own file.
	if d := req(p, "b", "agents/b.md", "Notes", "append"); !d.Allow {
		t.Fatalf("owner denied on own Notes: %+v", d)
	}
	// a different agent may not.
	d := req(p, "a", "agents/b.md", "Notes", "append")
	if d.Allow || d.Code != "EPERM" {
		t.Fatalf("cross-agent write should EPERM: %+v", d)
	}
	if d.Owner != "b" {
		t.Fatalf("deny must resolve owner=b, got %q", d.Owner)
	}
	if !strings.Contains(d.Teaching, "b") || !strings.Contains(d.Teaching, "agents/b.md") {
		t.Fatalf("deny must name owner + path: %q", d.Teaching)
	}
}

// TestBuiltinHandoffSection: Handoff is writable by the owner or the handoff
// writer; a third party is refused.
func TestBuiltinHandoffSection(t *testing.T) {
	p := Builtin()
	if d := req(p, "b", "agents/b.md", "Handoff", "append"); !d.Allow {
		t.Fatalf("owner denied on own Handoff: %+v", d)
	}
	if d := req(p, "handoff", "agents/b.md", "Handoff", "append"); !d.Allow {
		t.Fatalf("handoff writer denied on Handoff: %+v", d)
	}
	if d := req(p, "a", "agents/b.md", "Handoff", "append"); d.Allow || d.Code != "EPERM" {
		t.Fatalf("third party should be denied on Handoff: %+v", d)
	}
}

// TestOwnerOf pins the agent-file owner derivation, including the case- and
// traversal-variant spellings that must NOT dodge ownership (CRITICAL-2): on a
// case-insensitive filesystem "agents/b.MD" and "AGENTS/b.md" are the same file as
// "agents/b.md", so all three derive owner "b"; "./", "//", and ".." spellings are
// path.Clean'd to the same address.
func TestOwnerOf(t *testing.T) {
	cases := []struct{ path, want string }{
		{"agents/b.md", "b"},
		{"/Users/x/repo/agents/0fb10e08.md", "0fb10e08"},
		{"agents/b.md/", "b"},
		{"tasks/x.md", ""},
		{"agents/b.txt", ""},
		{"agents/", ""},
		{"b.md", ""},
		{"notagents/b.md", ""},
		// CRITICAL-2 case variants: ext, dir, and mixed case fold to the same owner.
		{"agents/b.MD", "b"},
		{"AGENTS/b.md", "b"},
		{"Agents/b.Md", "b"},
		{"AGENTS/b.MD", "b"},
		// CRITICAL-2 non-canonical spellings: ./ // .. all reduce to agents/b.md.
		{"./agents/b.md", "b"},
		{"agents//b.md", "b"},
		{"agents/../agents/b.md", "b"},
		{"/repo/./agents/b.md", "b"},
		// a case-variant on a NON-agents dir still yields no owner.
		{"nOtAgEnTs/b.md", ""},
	}
	for _, c := range cases {
		if got := OwnerOf(c.path); got != c.want {
			t.Errorf("OwnerOf(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestCaseVariantPathOwnership is CRITICAL-2 at the policy layer: a non-owner
// addressing another agent's file through a case-variant or non-canonical path is
// still denied EPERM (the path rule matches case-insensitively and OwnerOf folds the
// structural parts), and the true owner is still admitted through the same spellings.
func TestCaseVariantPathOwnership(t *testing.T) {
	p := Builtin()
	for _, path := range []string{
		"agents/b.MD", "AGENTS/b.md", "Agents/b.Md",
		"./agents/b.md", "agents//b.md", "agents/../agents/b.md",
	} {
		if d := req(p, "a", path, "Notes", "append"); d.Allow || d.Code != "EPERM" {
			t.Fatalf("non-owner via %q must EPERM (case/path variant dodged ownership): %+v", path, d)
		}
		if d := req(p, "b", path, "Notes", "append"); !d.Allow {
			t.Fatalf("true owner via %q must be allowed: %+v", path, d)
		}
	}
}

// TestSectionBeatsPath: a section-scoped rule outranks a path-only rule on the same
// address (the Tasks rule must beat the agent-owns-file rule).
func TestSectionBeatsPath(t *testing.T) {
	p := Builtin()
	// agents/b.md#Tasks matches BOTH the section:Tasks rule and path:agents/*.md.
	// The section rule wins → owner b is denied, cc-task-sync allowed.
	if d := req(p, "b", "agents/b.md", "Tasks", "append"); d.Allow {
		t.Fatal("path (owner) rule outranked the section:Tasks rule")
	}
	if d := req(p, "cc-task-sync", "agents/b.md", "Tasks", "append"); !d.Allow {
		t.Fatalf("section:Tasks rule should govern: %+v", d)
	}
}

// TestStrictLoad: the pack is data with a contract — typo'd fields, bad versions,
// and rules matching nothing are load errors, never silent no-ops.
func TestStrictLoad(t *testing.T) {
	bad := []string{
		// typo'd rule field silently dropping enforcement
		"version: 1\nrules:\n  - section: Tasks\n    write_actors: [\"x\"]\n    append_onIy: true\n",
		// unknown top-level field
		"version: 1\ndefaults: {}\nrules: []\n",
		"version: 2\nrules: []\n",
		"rules: []\n",
		// rule that constrains nothing
		"version: 1\nrules:\n  - write_actors: [\"x\"]\n",
	}
	for i, y := range bad {
		if _, err := LoadBytes([]byte(y)); err == nil {
			t.Errorf("case %d: LoadBytes should error", i)
		}
	}
	// a well-formed catch-all is fine.
	if _, err := LoadBytes([]byte("version: 1\nrules:\n  - path: \"**\"\n    write_actors: [\"*\"]\n")); err != nil {
		t.Fatalf("catch-all pack should load: %v", err)
	}
}

// TestLoadFromDisk covers the file path (missing file errors, present file loads).
func TestLoadFromDisk(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("Load of a missing pack should error, not default-allow")
	}
	f := filepath.Join(t.TempDir(), "pack.yaml")
	if err := os.WriteFile(f, []byte("version: 1\nrules:\n  - path: \"**\"\n    write_actors: [\"*\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(f); err != nil {
		t.Fatalf("Load of a valid pack: %v", err)
	}
}

// TestCASRequired: a cas_required rule denies a write with no CAS precondition
// (ECAS), allows it once one is supplied; the actor deny still outranks it.
func TestCASRequired(t *testing.T) {
	p, err := LoadBytes([]byte(`
version: 1
rules:
  - section: Ledger
    write_actors: ["*"]
    cas_required: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if d := p.Check(Request{Actor: "a", Section: "Ledger", Path: "x.md", Op: "replace", CASProvided: false}); d.Allow || d.Code != "ECAS" {
		t.Fatalf("no-CAS write should ECAS: %+v", d)
	}
	if d := p.Check(Request{Actor: "a", Section: "Ledger", Path: "x.md", Op: "replace", CASProvided: true}); !d.Allow {
		t.Fatalf("CAS-provided write should allow: %+v", d)
	}

	p2, _ := LoadBytes([]byte(`
version: 1
rules:
  - section: Ledger
    write_actors: ["scribe"]
    cas_required: true
`))
	// actor deny (EPERM) outranks the CAS check.
	if d := p2.Check(Request{Actor: "rogue", Section: "Ledger", Path: "x.md", Op: "replace", CASProvided: false}); d.Code != "EPERM" {
		t.Fatalf("actor deny should outrank CAS: %+v", d)
	}
}

// TestAppendOnly: an append_only rule admits only the append family; any rewriting
// op is EAPPEND, once actor + CAS pass.
func TestAppendOnly(t *testing.T) {
	p, err := LoadBytes([]byte(`
version: 1
rules:
  - section: Log
    write_actors: ["*"]
    append_only: true
    teaching: "{section} is append-only; {actor} must append."
`))
	if err != nil {
		t.Fatal(err)
	}
	base := Request{Actor: "a", Section: "Log", Path: "x.md", CASProvided: true}
	appendReq := base
	appendReq.Op = "append"
	if d := p.Check(appendReq); !d.Allow {
		t.Fatalf("append to append-only denied: %+v", d)
	}
	createReq := base
	createReq.Op = "create_section"
	if d := p.Check(createReq); !d.Allow {
		t.Fatalf("create_section to append-only denied: %+v", d)
	}
	for _, op := range []string{"replace", "delete", "blank", "replace_section", "prepend", "insert_after"} {
		r := base
		r.Op = op
		d := p.Check(r)
		if d.Allow || d.Code != "EAPPEND" {
			t.Fatalf("op %q on append-only should EAPPEND: %+v", op, d)
		}
		if !strings.Contains(d.Teaching, "append-only") || !strings.Contains(d.Teaching, "a") {
			t.Fatalf("op %q teaching not interpolated: %q", op, d.Teaching)
		}
	}
}

// TestDefaultAllow: an address no rule matches is allowed for any actor.
func TestDefaultAllow(t *testing.T) {
	p := Builtin()
	if d := req(p, "anyone", "tasks/x.md", "Objective", "replace"); !d.Allow {
		t.Fatalf("unmatched address should default-allow: %+v", d)
	}
}

// TestWildcardAndOwnerSentinel: "*" admits anyone; "$owner" admits only the
// path-derived owner and nobody on a non-agent path.
func TestWildcardAndOwnerSentinel(t *testing.T) {
	p, _ := LoadBytes([]byte(`
version: 1
rules:
  - section: Open
    write_actors: ["*"]
  - section: Owned
    write_actors: ["$owner"]
`))
	if d := req(p, "nobody", "x.md", "Open", "replace"); !d.Allow {
		t.Fatalf("wildcard should admit anyone: %+v", d)
	}
	if d := req(p, "b", "agents/b.md", "Owned", "replace"); !d.Allow {
		t.Fatalf("$owner should admit the owner: %+v", d)
	}
	if d := req(p, "a", "agents/b.md", "Owned", "replace"); d.Allow {
		t.Fatalf("$owner must deny a non-owner: %+v", d)
	}
	// $owner on a non-agent path resolves to no owner → denies everyone.
	if d := req(p, "b", "docs/b.md", "Owned", "replace"); d.Allow {
		t.Fatalf("$owner on a non-agent path should deny: %+v", d)
	}
}
