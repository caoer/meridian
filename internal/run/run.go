package run

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	Cwd      string // directory the block executed in: toplevel of its defining file
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
	params map[string]string
}

// Record enables per-task output capture for a sidecar run record: each task's
// stdout+stderr is teed into a bounded per-task buffer (the caller's writers
// still stream live) and the record fields of TaskResult are populated.
// Pair with WriteRecord to persist. The source document is never mutated.
func Record() RunOpt {
	return func(c *runConfig) { c.record = true }
}

// Params supplies named task parameters (md run's non-reserved top-level JSON
// keys, already string-converted). Every key must be declared by a requested
// leaf's md-<name>-params contract — an undeclared key fails loud before
// anything executes, so a typo can never silently run the default behavior.
// Each leaf receives only the params IT declares, projected as MD_PARAM_*
// env vars.
func Params(p map[string]string) RunOpt {
	return func(c *runConfig) { c.params = p }
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
	Args        []string // required positional arg names (md-<name>-args contract)
	Env         []string // required env var names (md-<name>-env contract)
	Params      []string // optional named param names (md-<name>-params contract)
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

// noteResolver lazily resolves Obsidian-style note targets vault-wide,
// rooted at the referencing file's git toplevel. Successful resolutions are
// memoized — one invocation walks the vault at most once per distinct target.
// A construction error (no toplevel) is carried and surfaced per-ref, so
// same-file-only documents never pay for it.
type noteResolver struct {
	root  string
	err   error
	cache map[string][]string
}

func newNoteResolver(mdPath string) *noteResolver {
	root, err := GitToplevel(mdPath)
	return &noteResolver{root: root, err: err, cache: map[string][]string{}}
}

func (r *noteResolver) resolve(target string) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	if m, ok := r.cache[target]; ok {
		return m, nil
	}
	matches, err := ResolveNote(os.DirFS(r.root), target)
	if err != nil {
		return nil, err
	}
	r.cache[target] = matches
	return matches, nil
}

// isSameFile reports whether a wikilink target addresses mdPath itself:
// empty target, the file's stem, or a path suffix of its actual path.
// Same-file interpretation takes precedence over vault-wide resolution —
// [[note#^x]] inside note.md never becomes ambiguous with another note.md.
func isSameFile(mdPath, target string) (bool, error) {
	if target == "" {
		return true, nil
	}
	target = strings.TrimSuffix(target, ".md")
	if target == strings.TrimSuffix(filepath.Base(mdPath), ".md") {
		return true, nil
	}
	abs, err := filepath.Abs(mdPath)
	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", mdPath, err)
	}
	fullStem := strings.TrimSuffix(filepath.ToSlash(abs), ".md")
	return strings.HasSuffix(fullStem, "/"+target), nil
}

// resolveTaskBlock resolves one leaf task's wikilink ref to its fence block
// and the path of the file that defines it. Same-file refs read the already
// loaded content; cross-file refs resolve the target Obsidian-style under
// the caller's vault (git toplevel) and must match exactly one note —
// dangling and ambiguous targets fail loud with the wikilink.
func resolveTaskBlock(mdPath, content string, task Task, res *noteResolver) (Block, string, error) {
	link, err := ParseWikilink(task.Ref)
	if err != nil {
		return Block{}, "", fmt.Errorf("task %s: %w", task.Name, err)
	}
	if link.BlockID == "" {
		return Block{}, "", fmt.Errorf("task %s: ref %s must point at a block (^id), not a heading", task.Name, task.Ref)
	}
	same, err := isSameFile(mdPath, link.Target)
	if err != nil {
		return Block{}, "", fmt.Errorf("task %s: %w", task.Name, err)
	}
	if same {
		b, err := FindBlock(content, link.BlockID)
		if err != nil {
			return Block{}, "", fmt.Errorf("task %s: dangling ref %s: %w", task.Name, task.Ref, err)
		}
		return b, mdPath, nil
	}

	matches, err := res.resolve(link.Target)
	if err != nil {
		return Block{}, "", fmt.Errorf("task %s: ref %s: %w", task.Name, task.Ref, err)
	}
	if len(matches) > 1 {
		return Block{}, "", fmt.Errorf("task %s: ref %s is ambiguous — resolves to %d notes: %s",
			task.Name, task.Ref, len(matches), strings.Join(matches, ", "))
	}
	targetPath := filepath.Join(res.root, filepath.FromSlash(matches[0]))
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return Block{}, "", fmt.Errorf("task %s: ref %s: read %s: %w", task.Name, task.Ref, matches[0], err)
	}
	b, err := FindBlock(string(data), link.BlockID)
	if err != nil {
		return Block{}, "", fmt.Errorf("task %s: dangling ref %s in %s: %w", task.Name, task.Ref, matches[0], err)
	}
	return b, targetPath, nil
}

// checkParams gates named params at resolution time: every passed key must be
// declared by at least one requested leaf's md-<name>-params contract. The
// error lists what the requested tasks accept, so a blind JSON caller can
// self-correct.
func checkParams(tasks map[string]Task, leaves []string, params map[string]string) error {
	if len(params) == 0 {
		return nil
	}
	declared := make(map[string]bool)
	var accepted []string
	for _, name := range leaves {
		for _, p := range tasks[name].Params {
			if !declared[p] {
				declared[p] = true
				accepted = append(accepted, p)
			}
		}
	}
	var unknown []string
	for k := range params {
		if !declared[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	sort.Strings(accepted)
	accepts := "none"
	if len(accepted) > 0 {
		accepts = strings.Join(accepted, ", ")
	}
	return fmt.Errorf("unknown param(s): %s — the requested task(s) accept: %s (declared via md-<task>-params)",
		strings.Join(unknown, ", "), accepts)
}

// taskEnv builds a leaf's execution environment: the process environment with
// every ambient MD_PARAM_* scrubbed, plus this run's projections of the params
// the leaf DECLARES. Scrubbing keeps the parameter surface per-invocation —
// a default-off guard must not flip on because a parent process (e.g. a task
// that itself calls md run) had a param set.
func taskEnv(task Task, params map[string]string) []string {
	environ := os.Environ()
	env := make([]string, 0, len(environ)+len(task.Params))
	for _, kv := range environ {
		if !strings.HasPrefix(kv, ParamEnvPrefix) {
			env = append(env, kv)
		}
	}
	for _, p := range task.Params {
		if v, ok := params[p]; ok {
			env = append(env, ParamEnvVar(p)+"="+v)
		}
	}
	return env
}

// RunTasks executes the named tasks declared in mdPath's frontmatter.
// All names are expanded, every distinct leaf's block is resolved once, and
// every required interpreter is verified on PATH before anything executes —
// no side effects on a chain that cannot complete. Execution is sequential
// and fail-fast (a non-zero exit aborts the chain and is reported in the
// last TaskResult). Each block executes with cwd = the git toplevel of the
// file that DEFINES it (same-file refs: this file; cross-file refs: the
// target file), carried per-result in TaskResult.Cwd; the returned cwd is
// the referencing file's toplevel — the vault root and default execution
// directory callers report. On a mid-chain
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
	if err := checkParams(tasks, leaves, cfg.params); err != nil {
		return nil, "", err
	}

	// Base cwd: the referencing file's toplevel — also the vault root for
	// cross-file resolution. Loud failure before any resolution work.
	cwd, err := GitToplevel(mdPath)
	if err != nil {
		return nil, "", err
	}

	res := &noteResolver{root: cwd, cache: map[string][]string{}}
	blocks := make(map[string]Block, len(leaves))
	cwds := make(map[string]string, len(leaves)) // leaf name → execution cwd
	tops := map[string]string{mdPath: cwd}       // defining path → toplevel (memo)
	onPath := make(map[string]bool)
	for _, name := range leaves {
		if _, done := blocks[name]; done {
			continue
		}
		// Parameter contract gates first — the cheapest resolution-time check,
		// and the whole point is failing before any side effect.
		if err := checkContract(tasks[name], args); err != nil {
			return nil, "", err
		}
		b, definingPath, err := resolveTaskBlock(mdPath, content, tasks[name], res)
		if err != nil {
			return nil, "", err
		}
		// cwd contract: a block executes in the toplevel of the file that
		// DEFINES it — a shared block's relative paths stay stable regardless
		// of caller. Resolved here so a target outside any repo fails before
		// anything executes.
		top, ok := tops[definingPath]
		if !ok {
			top, err = GitToplevel(definingPath)
			if err != nil {
				return nil, "", fmt.Errorf("task %s: %w", name, err)
			}
			tops[definingPath] = top
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
		cwds[name] = top
	}

	var results []TaskResult
	for _, name := range leaves {
		b := blocks[name]
		taskCwd := cwds[name]
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
		code, timedOut, err := ExecBlock(b, args, taskEnv(tasks[name], cfg.params), taskCwd, timeout, outW, errW)
		if err != nil {
			return results, cwd, fmt.Errorf("task %s: %w", name, err)
		}
		res := TaskResult{Name: name, BlockID: b.ID, Lang: b.Lang, Cwd: taskCwd, ExitCode: code, TimedOut: timedOut}
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
	res := newNoteResolver(mdPath)
	infos := make([]TaskInfo, 0, len(tasks))
	for _, name := range TaskNames(tasks) {
		task := tasks[name]
		info := TaskInfo{Name: name, Ref: task.Ref, Composition: task.Composition, Args: task.Args, Env: task.Env, Params: task.Params}
		if task.Ref != "" {
			if b, _, err := resolveTaskBlock(mdPath, content, task, res); err != nil {
				info.Error = err.Error()
			} else {
				info.Language = b.Lang
			}
		}
		infos = append(infos, info)
	}
	return infos, nil
}
