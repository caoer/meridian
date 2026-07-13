package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/caoer/meridian/internal/attest"
	"github.com/caoer/meridian/internal/canon"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
)

// envReposRootAttest mirrors the checks-package constant (import would expose
// nothing — it is unexported there).
const envReposRootAttest = "CCC_LLM_WIKI_REPOS_ROOT"

// errAttestToolFailure is the exit-2 error code for an infra-aborted attest
// (cat-file batch death, CAS mismatch): the run is unverified, never cleanly
// failed — a transient hiccup must not read as a page verdict.
const errAttestToolFailure = "ATTEST_TOOL_FAILURE"

// attestHandler is the `md attest` verb — the strict sole receipt writer
// (contract 4f3cbef §4.2): check gate, input + procedure hashing, on-origin,
// four-case idempotency, CAS + ancestry guards, atomic working-tree write.
// Config-gated: hashing resolves against the corpus index from the scan root,
// exactly as check and resolve do.
func attestHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}
		opts := engine.ScanOptions{Skip: cfg.Scan.Skip, MaxFileSize: cfg.Scan.MaxFileSize}
		return attestWithFS(os.DirFS(cfg.Scan.Root), cfg.Scan.Root, opts, nil, req)
	}
}

// attestHandlerWith is the injectable core used by tests (the
// resolveHandlerWith pattern): scan fsys as the corpus, reshape the engine's
// seams (fake git, clock, write) via mutate before it runs.
func attestHandlerWith(fsys fs.FS, root string, opts engine.ScanOptions, mutate func(*attest.Engine)) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		return attestWithFS(fsys, root, opts, mutate, req)
	}
}

// attestParams is the strict-decoded param set — an unknown key (including
// B3d's future bulk_reattest on an old binary) is INVALID_PARAMS, never
// silently dropped: a stale binary must not mis-scope a write verb.
type attestParams struct {
	Page    string `json:"page"`
	Scope   string `json:"scope"`
	DryRun  bool   `json:"dry_run"`
	Verdict string `json:"verdict"`
	Commit  string `json:"commit"`
	Format  string `json:"format"` // router-consumed; listed so strict parse admits it
}

// attestWithFS is the injectable core (the resolveWithFS pattern): scan fsys,
// build facts + index, wire the attest engine. mutate, when non-nil, lets a
// test reshape the engine (fake git, clock, write seams) before it runs.
func attestWithFS(fsys fs.FS, root string, opts engine.ScanOptions, mutate func(*attest.Engine), req *cli.Request) *cli.Response {
	var params attestParams
	if req.Params != nil {
		dec := json.NewDecoder(bytes.NewReader(req.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("invalid params: %v — md attest accepts: page, scope, dry_run, verdict, commit, format", err))
		}
	}
	if (params.Page == "") == (params.Scope == "") {
		return cli.ErrorResponse(cli.ErrInvalidParams,
			"exactly one of page or scope is required (they are mutually exclusive)")
	}

	docs, err := engine.ScanWithOpts(fsys, opts)
	if err != nil {
		return cli.ErrorResponse("SCAN_ERROR", "scan failed: "+err.Error())
	}
	facts := make(map[string]engine.Facts, len(docs))
	raw := make(map[string][]byte, len(docs))
	paths := make([]string, 0, len(docs))
	// repoBranch: the §7 DRY relocation — branch is a repo-level fact on the
	// type:repo source page; a duplicate catalog page makes the fact ambiguous
	// and the lookup refuses (never guess).
	repoBranch := map[string]string{}
	repoDup := map[string]bool{}
	for _, d := range docs {
		f := engine.ExtractFacts(d)
		facts[d.Path] = f
		raw[d.Path] = d.RawContent
		paths = append(paths, d.Path)
		if f.IsRepoPage && f.RepoName != "" {
			if _, seen := repoBranch[f.RepoName]; seen {
				repoDup[f.RepoName] = true
				continue
			}
			if b, ok := d.Frontmatter["branch"].(string); ok {
				repoBranch[f.RepoName] = strings.TrimSpace(b)
			}
		}
	}

	eng := &attest.Engine{
		Root:  root,
		Pages: paths,
		Raw:   raw,
		// ownedRepo "" = the resolve-verb posture: never substitute a receipt
		// checksum we cannot confirm is external (see resolveHandler).
		Source:    factAdapter{facts: facts, ownedRepo: ""},
		Res:       canon.BuildIndex(paths),
		ReposRoot: os.Getenv(envReposRootAttest),
		Branch: func(slug string) (string, bool) {
			if repoDup[slug] {
				return "", false
			}
			b, ok := repoBranch[slug]
			return b, ok && b != ""
		},
	}
	if mutate != nil {
		mutate(eng)
	}

	report, err := eng.Attest(attest.Options{
		Page:    params.Page,
		Scope:   params.Scope,
		DryRun:  params.DryRun,
		Verdict: params.Verdict,
		Commit:  params.Commit,
	})
	if err != nil {
		return cli.ErrorResponse(cli.ErrInvalidParams, err.Error())
	}

	// Step 6: failed and refused pages are error findings (exit 1); an infra
	// abort escalates to a tool failure (exit 2) with the page list preserved
	// in data — `failed: batch` / `failed: cas-retry` stay visible.
	var findings []cli.Finding
	for _, pr := range report.Pages {
		if pr.Status == attest.StatusFailed || pr.Status == attest.StatusRefused {
			findings = append(findings, cli.Finding{
				RuleID:   "attest",
				Severity: "error",
				FilePath: pr.Page,
				Message:  pr.Status + ": " + pr.Reason,
			})
		}
	}
	resp := &cli.Response{
		Version:  cli.ResponseVersion,
		Findings: findings,
		Data:     report,
	}
	if report.ToolFailure != "" {
		resp.Error = &cli.ErrorDetail{
			Code:    errAttestToolFailure,
			Message: report.ToolFailure,
			Hint:    "the run is unverified, not failed — fix the environment (git/cat-file) or retry after the concurrent edit settles",
		}
	}
	return resp
}
