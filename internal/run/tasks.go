package run

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TaskPrefix is the frontmatter key prefix declaring runnable tasks.
const TaskPrefix = "md-"

// Task is one md-<name> frontmatter declaration: either a block reference
// (quoted wikilink) or a composition (comma-list of sibling task names).
type Task struct {
	Name        string
	Ref         string   // wikilink string, e.g. "[[note#^deploy]]"
	Composition []string // sibling task names, run sequentially, fail-fast
}

// ExtractTasks reads md-* keys from parsed frontmatter metadata.
// Values starting with "[[" are block references; anything else is a
// composition. Non-string values fail loud.
func ExtractTasks(meta map[string]any) (map[string]Task, error) {
	tasks := make(map[string]Task)
	for key, val := range meta {
		name, ok := strings.CutPrefix(key, TaskPrefix)
		if !ok || name == "" {
			continue
		}
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("frontmatter %s: value must be a string (wikilink or comma-list), got %T", key, val)
		}
		s = strings.TrimSpace(s)
		if strings.HasPrefix(s, "[[") {
			tasks[name] = Task{Name: name, Ref: s}
			continue
		}
		parts := strings.Split(s, ",")
		comp := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				comp = append(comp, p)
			}
		}
		if len(comp) == 0 {
			return nil, fmt.Errorf("frontmatter %s: empty task value", key)
		}
		tasks[name] = Task{Name: name, Composition: comp}
	}
	return tasks, nil
}

// TaskNames returns sorted task names — used in loud unknown-name failures.
func TaskNames(tasks map[string]Task) []string {
	names := make([]string, 0, len(tasks))
	for n := range tasks {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// NormalizeNames parses the `name` param: a string ("check" or "check,deploy")
// or an array of strings. Comma-strings are normalized to lists.
func NormalizeNames(raw json.RawMessage) ([]string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		var names []string
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				names = append(names, p)
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("empty name param")
		}
		return names, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		if len(list) == 0 {
			return nil, fmt.Errorf("empty name param")
		}
		return list, nil
	}
	return nil, fmt.Errorf("name must be a string or array of strings")
}

// ExpandNames resolves requested names into an ordered list of leaf
// (block-ref) task names, expanding compositions depth-first with cycle
// detection. Unknown names fail loud, listing available tasks.
func ExpandNames(tasks map[string]Task, names []string) ([]string, error) {
	var out []string
	seen := make(map[string]bool)
	var expand func(name string) error
	expand = func(name string) error {
		task, ok := tasks[name]
		if !ok {
			return fmt.Errorf("unknown task %q — available: %s", name, strings.Join(TaskNames(tasks), ", "))
		}
		if task.Ref != "" {
			out = append(out, name)
			return nil
		}
		if seen[name] {
			return fmt.Errorf("composition cycle through task %q", name)
		}
		seen[name] = true
		defer delete(seen, name)
		for _, sub := range task.Composition {
			if err := expand(sub); err != nil {
				return err
			}
		}
		return nil
	}
	for _, n := range names {
		if err := expand(n); err != nil {
			return nil, err
		}
	}
	return out, nil
}
