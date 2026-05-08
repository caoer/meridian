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

func TestParseHooks_FieldChangeRejected(t *testing.T) {
	defs := map[string]HookDef{
		"on-field-change": {Action: "check"},
	}
	_, err := ParseHooks(defs)
	if err == nil {
		t.Fatal("expected error: on-field-change not yet supported")
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
