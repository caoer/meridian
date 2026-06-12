package run

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestExtractTasks(t *testing.T) {
	meta := map[string]any{
		"md-check":  "[[SYSTEM_PROMPT#^check]]",
		"md-deploy": "[[SYSTEM_PROMPT#^deploy]]",
		"md-all":    "check,deploy",
		"tags":      []any{"domain/locus"},
		"created":   "2026-06-12",
	}
	tasks, err := ExtractTasks(meta)
	if err != nil {
		t.Fatalf("ExtractTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("got %d tasks, want 3: %+v", len(tasks), tasks)
	}
	if tasks["check"].Ref != "[[SYSTEM_PROMPT#^check]]" {
		t.Errorf("check ref = %q", tasks["check"].Ref)
	}
	if !reflect.DeepEqual(tasks["all"].Composition, []string{"check", "deploy"}) {
		t.Errorf("all composition = %v", tasks["all"].Composition)
	}
}

func TestExtractTasksNonString(t *testing.T) {
	if _, err := ExtractTasks(map[string]any{"md-x": 42}); err == nil {
		t.Fatal("non-string md-* value should fail loud")
	}
}

func TestNormalizeNames(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`"check"`, []string{"check"}},
		{`"check,deploy"`, []string{"check", "deploy"}},
		{`" check , deploy "`, []string{"check", "deploy"}},
		{`["check","deploy"]`, []string{"check", "deploy"}},
	}
	for _, tc := range cases {
		got, err := NormalizeNames(json.RawMessage(tc.in))
		if err != nil {
			t.Fatalf("NormalizeNames(%s): %v", tc.in, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("NormalizeNames(%s) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := NormalizeNames(json.RawMessage(`42`)); err == nil {
		t.Error("numeric name should fail")
	}
}

func TestExpandNames(t *testing.T) {
	tasks := map[string]Task{
		"check":  {Name: "check", Ref: "[[x#^check]]"},
		"deploy": {Name: "deploy", Ref: "[[x#^deploy]]"},
		"all":    {Name: "all", Composition: []string{"check", "deploy"}},
	}
	got, err := ExpandNames(tasks, []string{"all"})
	if err != nil {
		t.Fatalf("ExpandNames: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"check", "deploy"}) {
		t.Errorf("ExpandNames(all) = %v", got)
	}
}

func TestExpandNamesUnknownListsAvailable(t *testing.T) {
	tasks := map[string]Task{"check": {Name: "check", Ref: "[[x#^c]]"}}
	_, err := ExpandNames(tasks, []string{"nope"})
	if err == nil {
		t.Fatal("unknown name should fail")
	}
	if !strings.Contains(err.Error(), "check") {
		t.Errorf("error should list available task names, got: %v", err)
	}
}

func TestExpandNamesCycle(t *testing.T) {
	tasks := map[string]Task{
		"a": {Name: "a", Composition: []string{"b"}},
		"b": {Name: "b", Composition: []string{"a"}},
	}
	if _, err := ExpandNames(tasks, []string{"a"}); err == nil {
		t.Fatal("composition cycle should fail loud")
	}
}

func TestExpandNamesFanOutCapped(t *testing.T) {
	// Doubling DAG: aN expands to 2^N leaves. Acyclic, so cycle detection
	// never fires — the leaf cap must stop it fast, before memory blows up.
	tasks := map[string]Task{"a0": {Name: "a0", Ref: "[[x#^a]]"}}
	prev := "a0"
	for i := 1; i <= 30; i++ {
		name := fmt.Sprintf("a%d", i)
		tasks[name] = Task{Name: name, Composition: []string{prev, prev}}
		prev = name
	}
	_, err := ExpandNames(tasks, []string{prev})
	if err == nil {
		t.Fatal("2^30-leaf expansion must fail at the cap")
	}
	if !strings.Contains(err.Error(), "256") {
		t.Errorf("error should name the cap, got: %v", err)
	}
}
