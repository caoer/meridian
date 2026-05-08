package hooks

import (
	"testing"
	"time"
)

func makeEvent(path, op string) Event {
	return Event{Path: path, Op: op, Time: time.Now()}
}

func TestDispatch_OnCreate(t *testing.T) {
	hks := []Hook{
		{Name: "on-create", Kind: KindCreate, Def: HookDef{Action: "check", Scope: "{{.Path}}"}},
	}
	results := Dispatch([]Event{makeEvent("wiki/new.md", "create")}, hks)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Hook != "on-create" {
		t.Errorf("hook = %q", results[0].Hook)
	}
	if results[0].Action != "check" {
		t.Errorf("action = %q", results[0].Action)
	}
	if results[0].Scope != "wiki/new.md" {
		t.Errorf("scope = %q", results[0].Scope)
	}
}

func TestDispatch_OnModify(t *testing.T) {
	hks := []Hook{
		{Name: "on-modify", Kind: KindModify, Def: HookDef{Action: "check", Scope: "{{.Path}}"}},
	}
	results := Dispatch([]Event{makeEvent("wiki/page.md", "modify")}, hks)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Hook != "on-modify" {
		t.Errorf("hook = %q", results[0].Hook)
	}
}

func TestDispatch_NoMatchingHook(t *testing.T) {
	hks := []Hook{
		{Name: "on-create", Kind: KindCreate, Def: HookDef{Action: "check"}},
	}
	// modify event won't match on-create hook
	results := Dispatch([]Event{makeEvent("wiki/page.md", "modify")}, hks)
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestDispatch_EmptyScopeUsesPath(t *testing.T) {
	hks := []Hook{
		{Name: "on-create", Kind: KindCreate, Def: HookDef{Action: "check"}},
	}
	results := Dispatch([]Event{makeEvent("wiki/page.md", "create")}, hks)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Scope != "wiki/page.md" {
		t.Errorf("scope = %q, want wiki/page.md", results[0].Scope)
	}
}

func TestDispatch_MultipleEventsAndHooks(t *testing.T) {
	hks := []Hook{
		{Name: "on-create", Kind: KindCreate, Def: HookDef{Action: "check"}},
		{Name: "on-modify", Kind: KindModify, Def: HookDef{Action: "check"}},
	}
	events := []Event{
		makeEvent("a.md", "create"),
		makeEvent("b.md", "modify"),
	}
	results := Dispatch(events, hks)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}
