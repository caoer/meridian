package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// RenderedDirective is one embedded command executed during a skill render.
type RenderedDirective struct {
	Kind     string // "block" (fenced ```! block) or "inline" (!`cmd` directive)
	Command  string // the script / command text as declared
	Line     int    // 1-indexed line in the source file (block: opening fence)
	ExitCode int
	TimedOut bool
}

// SkillRender is the result of rendering a skill folder.
type SkillRender struct {
	Path       string // the file that was rendered (<skill>/SKILL.md)
	Content    string // frontmatter-stripped body with directives resolved
	Directives []RenderedDirective
}

// RenderSkill renders <skillDir>/SKILL.md the way a skill harness does at
// load time: frontmatter is stripped (discovery metadata, not injected
// content) and both embedded-command styles are executed in document order.
//
//   - Fenced blocks whose info string is `!` run as a bash script; the whole
//     fence is replaced by the merged stdout+stderr capture (harness fenced-!
//     blocks merge stderr into the capture — a block's own failure line, e.g.
//     "BLOCK FAILED: …", IS the content the model must see), so a non-zero
//     exit still inlines the output.
//   - Inline !`cmd` pre-resolution directives run as bash; on exit 0 the
//     directive is replaced by stdout (stderr discarded), on failure the
//     literal directive text remains so the skill's fallback prose engages.
//     Only true single-backtick code spans qualify — doc examples written as
//     `` !`cmd` `` are longer-delimiter spans and stay literal.
//
// Execution environment: the caller's cwd (not the skill directory), the
// process env with the skill's bin/ prepended to PATH when present, and
// CCC_SKILL_DIR defaulted to the skill's parent directory (an env-set value
// wins — the install layout knows better). A positive timeout bounds each
// directive's wall clock; at the deadline the process group is killed and
// the directive reports TimeoutExitCode. Directive failures are results,
// not errors — only an unreadable skill or a spawn failure errors.
func RenderSkill(skillDir string, timeout time.Duration) (*SkillRender, error) {
	abs, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(abs, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s — the skill param is the skill folder (must contain SKILL.md): %w", path, err)
	}
	env := renderEnv(abs)

	lines := scanLines(string(data)) // frontmatter skipped, fences tracked
	// scanLines splits on \n, so a trailing newline yields one final empty
	// line — a split artifact, not content; writing it would add a blank line.
	if n := len(lines); n > 0 && lines[n-1].text == "" && strings.HasSuffix(string(data), "\n") {
		lines = lines[:n-1]
	}
	res := &SkillRender{Path: path}
	var out strings.Builder
	for i := 0; i < len(lines); {
		li := lines[i]
		if li.inFence && li.fenceOpen == i {
			// Whole fence: the run of lines recording this opener.
			j := i
			for j+1 < len(lines) && lines[j+1].inFence && lines[j+1].fenceOpen == i {
				j++
			}
			open := fenceDelim.FindStringSubmatch(li.text)
			lang := ""
			if fields := strings.Fields(strings.TrimSpace(open[2])); len(fields) > 0 {
				lang = fields[0]
			}
			// An unclosed fence at EOF is malformed markdown — copy verbatim,
			// never execute what the author did not finish declaring.
			if lang == "!" && j > i && isFenceClose(lines[j].text, open[1]) {
				var code strings.Builder
				for k := i + 1; k < j; k++ {
					code.WriteString(lines[k].text + "\n")
				}
				var buf bytes.Buffer
				exit, timedOut, xerr := execShell(code.String(), env, timeout, &buf, &buf)
				if xerr != nil {
					return nil, xerr
				}
				res.Directives = append(res.Directives, RenderedDirective{
					Kind: "block", Command: code.String(), Line: li.num, ExitCode: exit, TimedOut: timedOut,
				})
				if s := strings.TrimRight(buf.String(), "\n"); s != "" {
					out.WriteString(s + "\n")
				}
			} else {
				for k := i; k <= j; k++ {
					out.WriteString(lines[k].text + "\n")
				}
			}
			i = j + 1
			continue
		}
		rendered, err := renderInlineLine(li.text, li.num, env, timeout, res)
		if err != nil {
			return nil, err
		}
		out.WriteString(rendered + "\n")
		i++
	}
	res.Content = out.String()
	return res, nil
}

// renderInlineLine resolves !`cmd` directives on one non-fence line,
// appending executed directives to res.
func renderInlineLine(line string, num int, env []string, timeout time.Duration, res *SkillRender) (string, error) {
	spans := codeSpans(line)
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		if sp.runLen != 1 || sp.start == 0 || line[sp.start-1] != '!' {
			continue
		}
		cmd := line[sp.start+1 : sp.end-1]
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		var stdout bytes.Buffer
		exit, timedOut, xerr := execShell(cmd, env, timeout, &stdout, io.Discard)
		if xerr != nil {
			return "", xerr
		}
		res.Directives = append(res.Directives, RenderedDirective{
			Kind: "inline", Command: cmd, Line: num, ExitCode: exit, TimedOut: timedOut,
		})
		b.WriteString(line[last : sp.start-1])
		if exit == 0 {
			b.WriteString(strings.TrimRight(stdout.String(), "\n"))
		} else {
			b.WriteString(line[sp.start-1 : sp.end]) // literal remains — fallback prose engages
		}
		last = sp.end
	}
	b.WriteString(line[last:])
	return b.String(), nil
}

// span is one backtick code span on a line: [start,end) covers the span
// including delimiters; runLen is the delimiter length.
type span struct {
	start, end, runLen int
}

// codeSpans tokenizes backtick code spans CommonMark-style: an opener run of
// N backticks closes at the next run of exactly N. Runs of other lengths in
// between are span content — which is exactly why `` !`cmd` `` doc examples
// never register as directives.
func codeSpans(line string) []span {
	type btRun struct{ start, len int }
	var runs []btRun
	for i := 0; i < len(line); {
		if line[i] == '`' {
			j := i
			for j < len(line) && line[j] == '`' {
				j++
			}
			runs = append(runs, btRun{i, j - i})
			i = j
		} else {
			i++
		}
	}
	var spans []span
	for i := 0; i < len(runs); i++ {
		closer := -1
		for j := i + 1; j < len(runs); j++ {
			if runs[j].len == runs[i].len {
				closer = j
				break
			}
		}
		if closer < 0 {
			continue
		}
		spans = append(spans, span{
			start:  runs[i].start,
			end:    runs[closer].start + runs[closer].len,
			runLen: runs[i].len,
		})
		i = closer
	}
	return spans
}

// isFenceClose reports whether line validly closes a fence opened with the
// given delimiter run (same character, at least the opener's length, no
// trailing info) — the same rule scanLines applies.
func isFenceClose(line, openDelim string) bool {
	m := fenceDelim.FindStringSubmatch(line)
	return m != nil && m[1][0] == openDelim[0] && len(m[1]) >= len(openDelim) && strings.TrimSpace(m[2]) == ""
}

// renderEnv builds the directive environment: process env, skill bin/ on
// PATH, CCC_SKILL_DIR defaulted to the skill's parent directory.
func renderEnv(skillDir string) []string {
	env := os.Environ()
	if fi, err := os.Stat(filepath.Join(skillDir, "bin")); err == nil && fi.IsDir() {
		// Replace PATH in place — a duplicate entry's winner is libc-dependent.
		path := "PATH=" + filepath.Join(skillDir, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
		replaced := false
		for i, kv := range env {
			if strings.HasPrefix(kv, "PATH=") {
				env[i] = path
				replaced = true
				break
			}
		}
		if !replaced {
			env = append(env, path)
		}
	}
	if os.Getenv("CCC_SKILL_DIR") == "" {
		env = append(env, "CCC_SKILL_DIR="+filepath.Dir(skillDir))
	}
	return env
}

// execShell runs a script via `bash -c` — plain bash, no -euo pipefail: the
// render must reproduce harness semantics, and skills written for the
// harness rely on non-strict shell (explicit `|| { …; exit 1; }` guards).
// Stdin is /dev/null; timeout semantics match ExecBlock (process-group kill,
// TimeoutExitCode).
func execShell(script string, env []string, timeout time.Duration, stdout, stderr io.Writer) (code int, timedOut bool, err error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if timeout > 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		cmd.WaitDelay = 2 * time.Second
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return TimeoutExitCode, true, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), false, nil
		}
		return 0, false, fmt.Errorf("exec bash: %w", err)
	}
	return 0, false, nil
}
