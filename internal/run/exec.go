package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// TimeoutExitCode is reported when a block is killed at its deadline —
// the shell convention for SIGKILL-after-timeout (cf. timeout(1)).
const TimeoutExitCode = 124

// Interpreter maps a fence language token to the interpreter argv prefix and
// the temp-file extension. Unknown languages fail loud.
func Interpreter(lang string) (argv []string, ext string, err error) {
	switch lang {
	case "bash", "sh":
		return []string{"bash", "-euo", "pipefail"}, ".sh", nil
	case "python":
		return []string{"python3"}, ".py", nil
	case "js":
		return []string{"bun"}, ".js", nil
	case "ts":
		return []string{"bun"}, ".ts", nil
	case "":
		return nil, "", fmt.Errorf("block has no language token — deployable fences need one (bash, sh, python, js, ts)")
	default:
		return nil, "", fmt.Errorf("unsupported language %q (supported: bash, sh, python, js, ts)", lang)
	}
}

// ExecBlock materializes a fence block to a temp file and executes it as a
// script: interpreter + file + args (argv). The process inherits the
// environment; cwd is set by the caller. stdin is /dev/null — a task that
// prompts for input fails or reads EOF instead of hanging an agent pipeline.
// A positive timeout is a wall-clock deadline: at expiry the whole process
// group is SIGKILLed (a wedged grandchild must not keep the pipes open) and
// the block reports TimeoutExitCode with timedOut=true. Zero means no limit.
// Returns the script's exit code.
func ExecBlock(b Block, args []string, cwd string, timeout time.Duration, stdout, stderr io.Writer) (code int, timedOut bool, err error) {
	if !b.Fence {
		return 0, false, fmt.Errorf("block ^%s is not a fenced code block", b.ID)
	}
	interp, ext, err := Interpreter(b.Lang)
	if err != nil {
		return 0, false, fmt.Errorf("block ^%s: %w", b.ID, err)
	}

	tmp, err := os.CreateTemp("", "md-run-"+b.ID+"-*"+ext)
	if err != nil {
		return 0, false, fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(b.Code); err != nil {
		tmp.Close()
		return 0, false, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, false, fmt.Errorf("close temp file: %w", err)
	}

	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmdArgs := append(append([]string{}, interp[1:]...), tmp.Name())
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(ctx, interp[0], cmdArgs...)
	cmd.Dir = cwd
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if timeout > 0 {
		// Own process group so the deadline kill reaches every descendant —
		// killing only the interpreter would leave a wedged child holding
		// stdout/stderr open, and Wait would block past the deadline anyway.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		// Backstop: unblock Wait even if something survives the group kill.
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
		return 0, false, fmt.Errorf("exec %s: %w", interp[0], err)
	}
	return 0, false, nil
}

// GitToplevel walks up from a file's directory to the nearest ancestor
// containing .git (directory or file — submodules use a file). Loud failure
// when none is found: cwd semantics must never silently degrade.
func GitToplevel(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(abs)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no git toplevel above %s — md run requires the markdown file to live in a git repo (cwd contract)", path)
		}
		dir = parent
	}
}
