package run

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/caoer/meridian/internal/frontmatter"
)

// TaskResult is the outcome of one executed task.
type TaskResult struct {
	Name     string
	BlockID  string
	Lang     string
	ExitCode int
}

// TaskInfo is one row of `md run` list-mode introspection.
type TaskInfo struct {
	Name        string
	Ref         string
	Composition []string
	Language    string
	Error       string
}

// loadTasks parses the markdown file and extracts its md-* task table.
// One read serves both the frontmatter and the block content — task table
// and block resolution must never see different bytes.
func loadTasks(mdPath string) (map[string]Task, string, error) {
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", mdPath, err)
	}
	doc, err := frontmatter.ParseBytes(data)
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
	return tasks, string(data), nil
}

// resolveTaskBlock resolves one leaf task's wikilink ref to its fence block,
// enforcing the same-file-refs-only contract: the ref target must be the
// file's stem, or a path suffix of the file's actual path.
func resolveTaskBlock(mdPath, content string, task Task) (Block, error) {
	link, err := ParseWikilink(task.Ref)
	if err != nil {
		return Block{}, fmt.Errorf("task %s: %w", task.Name, err)
	}
	if link.BlockID == "" {
		return Block{}, fmt.Errorf("task %s: ref %s must point at a block (^id), not a heading", task.Name, task.Ref)
	}
	stem := strings.TrimSuffix(filepath.Base(mdPath), ".md")
	if link.Target != "" && link.Target != stem {
		abs, absErr := filepath.Abs(mdPath)
		if absErr != nil {
			return Block{}, fmt.Errorf("task %s: resolve %s: %w", task.Name, mdPath, absErr)
		}
		fullStem := strings.TrimSuffix(filepath.ToSlash(abs), ".md")
		if !strings.HasSuffix(fullStem, "/"+link.Target) {
			return Block{}, fmt.Errorf("task %s: ref %s targets another note — only same-file refs are supported", task.Name, task.Ref)
		}
	}
	b, err := FindBlock(content, link.BlockID)
	if err != nil {
		return Block{}, fmt.Errorf("task %s: dangling ref %s: %w", task.Name, task.Ref, err)
	}
	return b, nil
}

// RunTasks executes the named tasks declared in mdPath's frontmatter.
// All names are expanded, every distinct leaf's block is resolved once, and
// every required interpreter is verified on PATH before anything executes —
// no side effects on a chain that cannot complete. Execution is sequential
// and fail-fast (a non-zero exit aborts the chain and is reported in the
// last TaskResult). cwd is the file's git toplevel and is returned so
// callers report the directory tasks actually ran in. On a mid-chain
// execution error the results so far are returned alongside the error.
func RunTasks(mdPath string, names, args []string, stdout, stderr io.Writer) ([]TaskResult, string, error) {
	tasks, content, err := loadTasks(mdPath)
	if err != nil {
		return nil, "", err
	}
	leaves, err := ExpandNames(tasks, names)
	if err != nil {
		return nil, "", err
	}

	blocks := make(map[string]Block, len(leaves))
	onPath := make(map[string]bool)
	for _, name := range leaves {
		if _, done := blocks[name]; done {
			continue
		}
		b, err := resolveTaskBlock(mdPath, content, tasks[name])
		if err != nil {
			return nil, "", err
		}
		interp, _, err := Interpreter(b.Lang)
		if err != nil {
			return nil, "", fmt.Errorf("task %s: %w", name, err)
		}
		if !onPath[interp[0]] {
			if _, err := exec.LookPath(interp[0]); err != nil {
				return nil, "", fmt.Errorf("task %s needs %s — not found on PATH", name, interp[0])
			}
			onPath[interp[0]] = true
		}
		blocks[name] = b
	}

	cwd, err := GitToplevel(mdPath)
	if err != nil {
		return nil, "", err
	}

	var results []TaskResult
	for _, name := range leaves {
		b := blocks[name]
		code, err := ExecBlock(b, args, cwd, stdout, stderr)
		if err != nil {
			return results, cwd, fmt.Errorf("task %s: %w", name, err)
		}
		results = append(results, TaskResult{
			Name: name, BlockID: b.ID, Lang: b.Lang, ExitCode: code,
		})
		if code != 0 {
			break // fail-fast
		}
	}
	return results, cwd, nil
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
	return infos, nil
}
