package run

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/frontmatter"
)

// TaskResult is the outcome of one executed task. Stderr carries the tail of
// the task's standard error and is populated only on a non-zero exit — the
// failure detail travels with the result so callers can surface *why* a script
// failed, not just that it did. The record fields (Output, BlockSHA, StartedAt,
// Duration) are populated only under the Record() option — capture is opt-in
// so a chatty task costs nothing on a plain run.
type TaskResult struct {
	Name     string
	BlockID  string
	Lang     string
	ExitCode int
	TimedOut bool // killed at the wall-clock deadline (ExitCode = TimeoutExitCode)
	Stderr   string

	Output          string // interleaved stdout+stderr tail (≤ recordOutputMax)
	OutputTruncated bool
	BlockSHA        string // git blob object id of the executed block's code
	StartedAt       time.Time
	Duration        time.Duration
}

// RunOpt configures a RunTasks invocation.
type RunOpt func(*runConfig)

type runConfig struct {
	record bool
}

// Record enables per-task output capture for a sidecar run record: each task's
// stdout+stderr is teed into a bounded per-task buffer (the caller's writers
// still stream live) and the record fields of TaskResult are populated.
// Pair with WriteRecord to persist. The source document is never mutated.
func Record() RunOpt {
	return func(c *runConfig) { c.record = true }
}

// stderrTailMax bounds how much of a failing task's stderr is retained — enough
// for the error detail, capped so a chatty script can't blow up the envelope.
const stderrTailMax = 4 << 10 // 4 KiB

// tailWriter retains only the last stderrTailMax bytes written. It tees a
// task's stderr (which still streams live or into the JSON capture buffer)
// into a bounded tail so a failure's error detail can be attached to its
// result without buffering unbounded output.
type tailWriter struct {
	buf []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTailMax {
		t.buf = t.buf[len(t.buf)-stderrTailMax:]
	}
	return len(p), nil
}

// tail returns the retained stderr, trimmed of surrounding whitespace and — if
// the buffer was truncated mid-line — of the leading partial line.
func (t *tailWriter) tail() string {
	s := t.buf
	if len(s) == stderrTailMax {
		if nl := bytes.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
	}
	return strings.TrimSpace(string(s))
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
// A positive timeout bounds EACH task's wall clock (see ExecBlock) — a wedged
// block must not hang the caller (e.g. a skill-load preflight) indefinitely.
func RunTasks(mdPath string, names, args []string, timeout time.Duration, stdout, stderr io.Writer, opts ...RunOpt) ([]TaskResult, string, error) {
	var cfg runConfig
	for _, o := range opts {
		o(&cfg)
	}
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
		// Tee this task's stderr into a bounded tail buffer: it still streams
		// live (text mode) or into the capture buffer (JSON mode), and the tail
		// lets us attach the failure detail to the result.
		tail := &tailWriter{}
		outW, errW := stdout, io.MultiWriter(stderr, tail)
		var capture *cappedWriter
		var startedAt time.Time
		if cfg.record {
			// One interleaved per-task buffer: the record shows the task's
			// output as a terminal would, not two disjoint streams.
			capture = &cappedWriter{max: recordOutputMax}
			outW = io.MultiWriter(outW, capture)
			errW = io.MultiWriter(errW, capture)
			startedAt = time.Now()
		}
		code, timedOut, err := ExecBlock(b, args, cwd, timeout, outW, errW)
		if err != nil {
			return results, cwd, fmt.Errorf("task %s: %w", name, err)
		}
		res := TaskResult{Name: name, BlockID: b.ID, Lang: b.Lang, ExitCode: code, TimedOut: timedOut}
		if cfg.record {
			res.Output = capture.String()
			res.OutputTruncated = capture.truncated
			res.BlockSHA = BlobSHA(b.Code)
			res.StartedAt = startedAt
			res.Duration = time.Since(startedAt)
		}
		if code != 0 {
			res.Stderr = tail.tail()
			results = append(results, res)
			break // fail-fast
		}
		results = append(results, res)
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
