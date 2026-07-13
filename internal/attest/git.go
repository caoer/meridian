package attest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitRunner runs a single git command in dir with optional stdin, returning
// stdout. Injectable so tests script git behavior (including batch failure)
// without a repository. Mirrors the checks-package seam (effect_pin.go).
type GitRunner interface {
	Run(dir string, stdin []byte, args ...string) ([]byte, error)
}

// execGit is the production runner: git -C <dir> <args…> with a 5s timeout.
type execGit struct{}

func (execGit) Run(dir string, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.Output()
}

// objInfo is one resolved `git cat-file --batch-check` line: object id + type,
// or exists=false for a missing input.
type objInfo struct {
	sha    string
	typ    string
	exists bool
}

// resolveRepoDir resolves a repo slug at <reposRoot>/<slug> with the
// filepath.Rel containment belt (defense in depth beside checkSlug's ".."
// rejection): a slug resolving outside the repos root is refused even if it
// somehow arrived unvalidated.
func resolveRepoDir(reposRoot, slug string) (string, bool) {
	if reposRoot == "" || slug == "" {
		return "", false
	}
	dir := filepath.Join(reposRoot, slug)
	if rel, err := filepath.Rel(reposRoot, dir); err != nil || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir, true
	}
	return "", false
}

// isObjectName reports whether tok is a bare git object id — the first field
// of a resolved cat-file line, distinguishing it from a "<input> missing" line.
func isObjectName(tok string) bool {
	if len(tok) != 40 && len(tok) != 64 {
		return false
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// batchCheck runs one `git cat-file --batch-check` for the queries and records
// each result positionally (output is 1:1 with input order). An infra error
// (timeout, git missing) returns err — the caller MUST treat that as a tool
// failure (exit 2), never as a false `unchanged`, a false finding, or a
// receipt computed from an empty object table (§3.3 step 3 / PR #12 #1).
func batchCheck(git GitRunner, dir string, queries []string) (map[string]objInfo, error) {
	objs := make(map[string]objInfo, len(queries))
	if len(queries) == 0 {
		return objs, nil
	}
	stdin := []byte(strings.Join(queries, "\n") + "\n")
	out, err := git.Run(dir, stdin, "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for i, q := range queries {
		if i >= len(lines) {
			break
		}
		fields := strings.Fields(lines[i])
		if len(fields) == 3 && isObjectName(fields[0]) {
			objs[q] = objInfo{sha: fields[0], typ: fields[1], exists: true}
		} else {
			objs[q] = objInfo{}
		}
	}
	return objs, nil
}

// isAncestor reports whether commit is an ancestor of (or equal to) ref.
// merge-base --is-ancestor exits 0 for yes; both "no" and "cannot answer"
// (unknown commit) return an error and read as false — fail closed: an
// ancestry the tool cannot prove never authorizes a receipt bump (R3).
func isAncestor(git GitRunner, dir, commit, ref string) bool {
	_, err := git.Run(dir, nil, "merge-base", "--is-ancestor", commit, ref)
	return err == nil
}
