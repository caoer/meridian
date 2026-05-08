package hooks

import (
	"testing"
)

func TestParseHooks_Valid(t *testing.T) {
	defs := map[string]HookDef{
		"on-create": {Action: "check", Scope: "{{.Path}}"},
		"on-modify": {Action: "check", Scope: "{{.Path}}"},
	}
	hooks, err := ParseHooks(defs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("got %d hooks, want 2", len(hooks))
	}
}

func TestParseHooks_FieldChange(t *testing.T) {
	defs := map[string]HookDef{
		"on-field-change": {Action: "check", Scope: "{{.Path}}", Field: "tags"},
	}
	hooks, err := ParseHooks(defs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("got %d hooks, want 1", len(hooks))
	}
	if hooks[0].Kind != KindFieldChange {
		t.Errorf("kind = %v, want KindFieldChange", hooks[0].Kind)
	}
	if hooks[0].Def.Field != "tags" {
		t.Errorf("field = %q, want tags", hooks[0].Def.Field)
	}
}

func TestParseHooks_MissingAction(t *testing.T) {
	defs := map[string]HookDef{
		"on-create": {Scope: "{{.Path}}"},
	}
	_, err := ParseHooks(defs)
	if err == nil {
		t.Fatal("expected error for missing action")
	}
}

func TestParseHooks_UnknownAction(t *testing.T) {
	defs := map[string]HookDef{
		"on-create": {Action: "deploy"},
	}
	_, err := ParseHooks(defs)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestParseHooks_FieldChangeNoField(t *testing.T) {
	defs := map[string]HookDef{
		"on-field-change": {Action: "check"},
	}
	_, err := ParseHooks(defs)
	if err == nil {
		t.Fatal("expected error for on-field-change without field")
	}
}

func TestParseHooks_UnknownHookName(t *testing.T) {
	defs := map[string]HookDef{
		"on-explode": {Action: "check"},
	}
	_, err := ParseHooks(defs)
	if err == nil {
		t.Fatal("expected error for unknown hook name")
	}
}

func TestParseHooks_Empty(t *testing.T) {
	hooks, err := ParseHooks(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 0 {
		t.Fatalf("got %d hooks, want 0", len(hooks))
	}
}
