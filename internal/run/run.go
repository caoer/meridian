package run

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
)

// TaskResult is the outcome of one executed task.
type TaskResult struct {
	Name     string `json:"name"`
	BlockID  string `json:"block_id"`
	Lang     string `json:"lang"`
	ExitCode int    `json:"exit_code"`
}

// TaskInfo is one row of `md run` list-mode introspection.
type TaskInfo struct {
	Name        string   `json:"name"`
	Ref         string   `json:"ref,omitempty"`
	Composition []string `json:"composition,omitempty"`
	Language    string   `json:"language,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// loadTasks parses the markdown file and extracts its md-* task table.
func loadTasks(mdPath string) (map[string]Task, string, error) {
	f, err := os.Open(mdPath)
	if err != nil {
		return nil, "", fmt.Errorf("open %s: %w", mdPath, err)
	}
	defer f.Close()
	doc, err := frontmatter.ParseReader(f)
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", mdPath, err)
	}
	if doc == nil {
		return nil, "", fmt.Errorf("%s has no frontmatter — md run needs md-* task keys", mdPath)
	}
	tasks, err := ExtractTasks(doc.Meta)
	if err != nil {
		return nil, "", err
	}
	if len(tasks) == 0 {
		return nil, "", fmt.Errorf("%s declares no md-* tasks in frontmatter", mdPath)
	}
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", mdPath, err)
	}
	return tasks, string(data), nil
}

// resolveTaskBlock resolves one leaf task's wikilink ref to its fence block,
// enforcing the same-file-refs-only contract.
func resolveTaskBlock(mdPath, content string, task Task) (Block, error) {
	link, err := ParseWikilink(task.Ref)
	if err != nil {
		return Block{}, fmt.Errorf("task %s: %w", task.Name, err)
	}
	if link.BlockID == "" {
		return Block{}, fmt.Errorf("task %s: ref %s must point at a block (^id), not a heading", task.Name, task.Ref)
	}
	stem := strings.TrimSuffix(filepath.Base(mdPath), ".md")
	if link.Target != "" && link.Target != stem && !strings.HasSuffix(link.Target, "/"+stem) {
		return Block{}, fmt.Errorf("task %s: ref %s targets another note — only same-file refs are supported", task.Name, task.Ref)
	}
	b, err := FindBlock(content, link.BlockID)
	if err != nil {
		return Block{}, fmt.Errorf("task %s: dangling ref %s: %w", task.Name, task.Ref, err)
	}
	return b, nil
}

// RunTasks executes the named tasks declared in mdPath's frontmatter.
// All names are expanded and all blocks resolved before anything executes;
// execution is sequential and fail-fast (a non-zero exit aborts the chain and
// is reported in the last TaskResult). cwd is the file's git toplevel.
func RunTasks(mdPath string, names, args []string, stdout, stderr io.Writer) ([]TaskResult, error) {
	tasks, content, err := loadTasks(mdPath)
	if err != nil {
		return nil, err
	}
	leaves, err := ExpandNames(tasks, names)
	if err != nil {
		return nil, err
	}

	// Resolve everything before executing anything — no side effects on a
	// chain that cannot complete resolution.
	blocks := make([]Block, len(leaves))
	for i, name := range leaves {
		b, err := resolveTaskBlock(mdPath, content, tasks[name])
		if err != nil {
			return nil, err
		}
		if _, _, err := Interpreter(b.Lang); err != nil {
			return nil, fmt.Errorf("task %s: %w", name, err)
		}
		blocks[i] = b
	}

	cwd, err := GitToplevel(mdPath)
	if err != nil {
		return nil, err
	}

	var results []TaskResult
	for i, name := range leaves {
		code, err := ExecBlock(blocks[i], args, cwd, stdout, stderr)
		if err != nil {
			return results, fmt.Errorf("task %s: %w", name, err)
		}
		results = append(results, TaskResult{
			Name: name, BlockID: blocks[i].ID, Lang: blocks[i].Lang, ExitCode: code,
		})
		if code != 0 {
			break // fail-fast
		}
	}
	return results, nil
}

// ListTasks introspects mdPath's md-* tasks without executing: names, refs,
// compositions, and resolved fence languages. Resolution failures are
// reported per-task, not fatal — list is for discovery.
func ListTasks(mdPath string) ([]TaskInfo, error) {
	tasks, content, err := loadTasks(mdPath)
	if err != nil {
		return nil, err
	}
	infos := make([]TaskInfo, 0, len(tasks))
	for _, name := range TaskNames(tasks) {
		task := tasks[name]
		info := TaskInfo{Name: name, Ref: task.Ref, Composition: task.Composition}
		if task.Ref != "" {
			if b, err := resolveTaskBlock(mdPath, content, task); err != nil {
				info.Error = err.Error()
			} else {
				info.Language = b.Lang
			}
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}
