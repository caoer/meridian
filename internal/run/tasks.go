package run

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// TaskPrefix is the frontmatter key prefix declaring runnable tasks.
const TaskPrefix = "md-"

// Task is one md-<name> frontmatter declaration: either a block reference
// (quoted wikilink) or a composition (comma-list of sibling task names).
// Args/Env/Params carry the task's parameter contract (md-<name>-args /
// md-<name>-env / md-<name>-params keys), validated at resolution time
// before anything executes.
type Task struct {
	Name        string
	Ref         string   // wikilink string, e.g. "[[note#^deploy]]"
	Composition []string // sibling task names, run sequentially, fail-fast
	Args        []string // required positional arg names, in order
	Env         []string // required (non-empty) env var names
	Params      []string // optional named param names (md run top-level keys → MD_PARAM_* env)
}

// ReservedParams are `md run`'s own top-level JSON keys. A declared named
// param may not shadow one — the CLI consumes these before task params are
// partitioned, so a shadowing param could never reach its task.
var ReservedParams = []string{"file", "name", "args", "list", "format", "timeout", "record"}

// ParamEnvPrefix namespaces the env projection of named params: param
// include_untracked arrives in the task as MD_PARAM_INCLUDE_UNTRACKED.
// The prefix makes provenance explicit and keeps ambient shell env from
// colliding with a task's parameter surface.
const ParamEnvPrefix = "MD_PARAM_"

// ParamEnvVar maps a declared param name to its projected env var name.
func ParamEnvVar(name string) string {
	return ParamEnvPrefix + strings.ToUpper(name)
}

// validParamName gates declared param names: lowercase snake_case, so the
// JSON surface reads naturally and the env projection is a valid identifier.
func validParamName(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// splitCommaList splits a comma list, trimming blanks.
func splitCommaList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ExtractTasks reads md-* keys from parsed frontmatter metadata.
// Values starting with "[[" are block references; anything else is a
// composition. The suffixes -args, -env, and -params are RESERVED:
// md-<name>-args / md-<name>-env / md-<name>-params declare <name>'s
// parameter contract, never a task — a contract key whose base task is
// missing, or on a composition (where it would be dead config), fails loud.
// Non-string values fail loud.
func ExtractTasks(meta map[string]any) (map[string]Task, error) {
	tasks := make(map[string]Task)
	type contract struct {
		key, base, kind, val string
	}
	var contracts []contract
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
		if base, isArgs := strings.CutSuffix(name, "-args"); isArgs && base != "" {
			contracts = append(contracts, contract{key: key, base: base, kind: "args", val: s})
			continue
		}
		if base, isEnv := strings.CutSuffix(name, "-env"); isEnv && base != "" {
			contracts = append(contracts, contract{key: key, base: base, kind: "env", val: s})
			continue
		}
		if base, isParams := strings.CutSuffix(name, "-params"); isParams && base != "" {
			contracts = append(contracts, contract{key: key, base: base, kind: "params", val: s})
			continue
		}
		if strings.HasPrefix(s, "[[") {
			tasks[name] = Task{Name: name, Ref: s}
			continue
		}
		comp := splitCommaList(s)
		if len(comp) == 0 {
			return nil, fmt.Errorf("frontmatter %s: empty task value", key)
		}
		tasks[name] = Task{Name: name, Composition: comp}
	}
	for _, c := range contracts {
		task, ok := tasks[c.base]
		if !ok {
			return nil, fmt.Errorf("frontmatter %s: contract key has no base task md-%s — the -args/-env/-params suffixes are reserved for task contracts", c.key, c.base)
		}
		if task.Ref == "" {
			return nil, fmt.Errorf("frontmatter %s: contracts attach to block-ref tasks, not compositions — declare it on the leaves of md-%s", c.key, c.base)
		}
		list := splitCommaList(c.val)
		if len(list) == 0 {
			return nil, fmt.Errorf("frontmatter %s: empty contract value", c.key)
		}
		switch c.kind {
		case "args":
			task.Args = list
		case "env":
			task.Env = list
		case "params":
			for _, p := range list {
				if !validParamName(p) {
					return nil, fmt.Errorf("frontmatter %s: invalid param name %q — lowercase snake_case ([a-z][a-z0-9_]*), it becomes a JSON key and the %s%s env var", c.key, p, ParamEnvPrefix, strings.ToUpper(p))
				}
				for _, r := range ReservedParams {
					if p == r {
						return nil, fmt.Errorf("frontmatter %s: param %q shadows a reserved md run key (%s) — the CLI would consume it before the task ever sees it", c.key, p, strings.Join(ReservedParams, ", "))
					}
				}
			}
			task.Params = list
		}
		tasks[c.base] = task
	}
	return tasks, nil
}

// checkContract validates a leaf task's parameter contract against the
// provided argv and the process environment. Resolution-time gate: a
// violation must abort before anything executes. Required env vars must be
// set AND non-empty — for a declared-required secret or setting, empty is
// indistinguishable from a bug. The error lists what is missing and what was
// provided, so a blind JSON caller can self-correct.
func checkContract(task Task, args []string) error {
	var problems []string
	if len(args) < len(task.Args) {
		provided := "none"
		if len(args) > 0 {
			provided = fmt.Sprintf("%d (%s)", len(args), strings.Join(args, ", "))
		}
		problems = append(problems, fmt.Sprintf("requires %d args (%s), provided: %s — missing: %s",
			len(task.Args), strings.Join(task.Args, ", "), provided,
			strings.Join(task.Args[len(args):], ", ")))
	}
	var missingEnv []string
	for _, v := range task.Env {
		if os.Getenv(v) == "" {
			missingEnv = append(missingEnv, v)
		}
	}
	if len(missingEnv) > 0 {
		problems = append(problems, fmt.Sprintf("requires env (%s) — missing or empty: %s",
			strings.Join(task.Env, ", "), strings.Join(missingEnv, ", ")))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("task %s: parameter contract violated — %s", task.Name, strings.Join(problems, "; "))
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

// maxExpandedLeaves caps composition fan-out: cycles are caught separately,
// but a DAG of doubling compositions re-expands shared members into 2^N
// leaves and can exhaust memory before anything executes.
const maxExpandedLeaves = 256

// ExpandNames resolves requested names into an ordered list of leaf
// (block-ref) task names, expanding compositions depth-first with cycle
// detection. Unknown names fail loud, listing available tasks; expansions
// past maxExpandedLeaves fail loud naming the cap.
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
			if len(out) >= maxExpandedLeaves {
				return fmt.Errorf("task expansion exceeds the %d-leaf cap at %q — compositions fan out too widely", maxExpandedLeaves, name)
			}
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
