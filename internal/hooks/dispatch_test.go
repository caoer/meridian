package hooks

import (
	"testing"
	"time"
)

func makeEvent(path, op string) Event {
	return Event{Path: path, Op: op, Time: time.Now()}
}

func TestDispatch_OnCreate(t *testing.T) {
	hooks := []Hook{
		{Name: "on-create", Kind: KindCreate, Def: HookDef{Action: "check", Scope: "{{.Path}}"}},
	}
	d := NewDispatcher()
	results := d.Dispatch([]Event{makeEvent("wiki/new.md", "create")}, hooks)
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
	hooks := []Hook{
		{Name: "on-modify", Kind: KindModify, Def: HookDef{Action: "check", Scope: "{{.Path}}"}},
	}
	d := NewDispatcher()
	results := d.Dispatch([]Event{makeEvent("wiki/page.md", "modify")}, hooks)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Hook != "on-modify" {
		t.Errorf("hook = %q", results[0].Hook)
	}
}

func TestDispatch_NoMatchingHook(t *testing.T) {
	hooks := []Hook{
		{Name: "on-create", Kind: KindCreate, Def: HookDef{Action: "check"}},
	}
	d := NewDispatcher()
	// modify event won't match on-create hook
	results := d.Dispatch([]Event{makeEvent("wiki/page.md", "modify")}, hooks)
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestDispatch_FieldChange_Fires(t *testing.T) {
	hooks := []Hook{
		{Name: "on-field-change", Kind: KindFieldChange, Def: HookDef{Action: "check", Field: "tags", Scope: "{{.Path}}"}},
	}
	d2 := NewDispatcher()
	d2.CacheFrontmatter("wiki/page.md", map[string]any{"tags": []any{"old"}})
	results := d2.DispatchWithFrontmatter(
		[]Event{makeEvent("wiki/page.md", "modify")},
		hooks,
		map[string]map[string]any{
			"wiki/page.md": {"tags": []any{"new"}},
		},
	)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Hook != "on-field-change" {
		t.Errorf("hook = %q", results[0].Hook)
	}
}

func TestDispatch_FieldChange_NoFire_SameValue(t *testing.T) {
	hooks := []Hook{
		{Name: "on-field-change", Kind: KindFieldChange, Def: HookDef{Action: "check", Field: "tags", Scope: "{{.Path}}"}},
	}
	d := NewDispatcher()
	d.CacheFrontmatter("wiki/page.md", map[string]any{"tags": []any{"same"}})
	results := d.DispatchWithFrontmatter(
		[]Event{makeEvent("wiki/page.md", "modify")},
		hooks,
		map[string]map[string]any{
			"wiki/page.md": {"tags": []any{"same"}},
		},
	)
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (field didn't change)", len(results))
	}
}

func TestDispatch_FieldChange_NoFire_OtherField(t *testing.T) {
	hooks := []Hook{
		{Name: "on-field-change", Kind: KindFieldChange, Def: HookDef{Action: "check", Field: "tags", Scope: "{{.Path}}"}},
	}
	d := NewDispatcher()
	d.CacheFrontmatter("wiki/page.md", map[string]any{"tags": []any{"same"}, "title": "old"})
	results := d.DispatchWithFrontmatter(
		[]Event{makeEvent("wiki/page.md", "modify")},
		hooks,
		map[string]map[string]any{
			"wiki/page.md": {"tags": []any{"same"}, "title": "new"},
		},
	)
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (watched field didn't change)", len(results))
	}
}
