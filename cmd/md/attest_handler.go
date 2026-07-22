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

// attestParams is the strict-decoded param set — an unknown key is
// INVALID_PARAMS, never silently dropped: a stale binary must not mis-scope a
// write verb.
type attestParams struct {
	Page         string        `json:"page"`
	Scope        string        `json:"scope"`
	DryRun       bool          `json:"dry_run"`
	Verdict      string        `json:"verdict"`
	Commit       string        `json:"commit"`
	BulkReattest *bulkReattest `json:"bulk_reattest"`
	Format       string        `json:"format"` // router-consumed; listed so strict parse admits it
}

// bulkReattest is the declared-cosmetic sweep param (§6.1): the commit set every
// drifted input must be attributable to.
type bulkReattest struct {
	Commits []string `json:"commits"`
}

// attestWithFS is the injectable core (the resolveWithFS pattern): scan fsys,
// build facts + index, wire the attest engine. mutate, when non-nil, lets a
// test reshape the engine (fake git, clock, write seams) before it runs.
func attestWithFS(fsys fs.FS, root string, opts engine.ScanOptions, mutate func(*attest.Engine), req *cli.Request) *cli.Response {
	var params attestParams
	if req.Params != nil {
		if resp := rejectFileKey(req.Params); resp != nil {
			return resp
		}
		dec := json.NewDecoder(bytes.NewReader(req.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("invalid params: %v — md attest accepts: page, scope, dry_run, verdict, commit, bulk_reattest, format", err))
		}
	}
	if (params.Page == "") == (params.Scope == "") {
		return cli.ErrorResponse(cli.ErrInvalidParams,
			"exactly one of page or scope is required (they are mutually exclusive)")
	}

	eng, errResp := buildAttestEngine(fsys, root, opts)
	if errResp != nil {
		return errResp
	}
	if mutate != nil {
		mutate(eng)
	}

	var bulk *attest.BulkOptions
	if params.BulkReattest != nil {
		bulk = &attest.BulkOptions{Commits: params.BulkReattest.Commits}
	}
	report, err := eng.Attest(attest.Options{
		Page:         params.Page,
		Scope:        params.Scope,
		DryRun:       params.DryRun,
		Verdict:      params.Verdict,
		Commit:       params.Commit,
		BulkReattest: bulk,
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

// buildAttestEngine scans fsys and wires an attest engine over the fact table +
// corpus index — the shared core of `md attest` and `md chain promote` (both
// resolve inputs against the same fact-served chain). A scan failure returns an
// error response; the caller may reshape the returned engine's seams.
func buildAttestEngine(fsys fs.FS, root string, opts engine.ScanOptions) (*attest.Engine, *cli.Response) {
	docs, err := engine.ScanWithOpts(fsys, opts)
	if err != nil {
		return nil, cli.ErrorResponse("SCAN_ERROR", "scan failed: "+err.Error())
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
	return eng, nil
}

// chainPromoteHandler is the `md chain promote` verb (§6.2): it scaffolds an
// effect page's `inputs:` chain from its `draws-from` provenance — resolving and
// hashing each entry, emitting a paste-ready block. Report-only by default;
// {"write": true} persists the scaffold to pages with no chain yet, never
// merging into an existing one. Config-gated like attest.
func chainPromoteHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}
		opts := engine.ScanOptions{Skip: cfg.Scan.Skip, MaxFileSize: cfg.Scan.MaxFileSize}
		return chainPromoteWithFS(os.DirFS(cfg.Scan.Root), cfg.Scan.Root, opts, nil, req)
	}
}

// chainPromoteHandlerWith is the injectable core used by tests (the
// attestHandlerWith pattern): scan fsys, reshape the engine's seams via mutate.
func chainPromoteHandlerWith(fsys fs.FS, root string, opts engine.ScanOptions, mutate func(*attest.Engine)) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		return chainPromoteWithFS(fsys, root, opts, mutate, req)
	}
}

// chainPromoteParams is the strict-decoded param set — an unknown key is
// INVALID_PARAMS, never silently dropped.
type chainPromoteParams struct {
	Page   string `json:"page"`
	Scope  string `json:"scope"`
	Write  bool   `json:"write"`
	DryRun bool   `json:"dry_run"`
	Format string `json:"format"` // router-consumed; listed so strict parse admits it
}

func chainPromoteWithFS(fsys fs.FS, root string, opts engine.ScanOptions, mutate func(*attest.Engine), req *cli.Request) *cli.Response {
	var params chainPromoteParams
	if req.Params != nil {
		if resp := rejectFileKey(req.Params); resp != nil {
			return resp
		}
		dec := json.NewDecoder(bytes.NewReader(req.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("invalid params: %v — md chain promote accepts: page, scope, write, dry_run, format", err))
		}
	}
	if (params.Page == "") == (params.Scope == "") {
		return cli.ErrorResponse(cli.ErrInvalidParams,
			"exactly one of page or scope is required (they are mutually exclusive)")
	}

	eng, errResp := buildAttestEngine(fsys, root, opts)
	if errResp != nil {
		return errResp
	}
	if mutate != nil {
		mutate(eng)
	}

	report, err := eng.ChainPromote(attest.PromoteOptions{
		Page:   params.Page,
		Scope:  params.Scope,
		Write:  params.Write,
		DryRun: params.DryRun,
	})
	if err != nil {
		return cli.ErrorResponse(cli.ErrInvalidParams, err.Error())
	}

	// An ambiguous or dead draws-from is a warn-severity finding for the
	// migrating agent (exit 0) — the scaffold for the resolvable entries still
	// ships; judgment about the real dependency set stays with tier-2.
	var findings []cli.Finding
	for _, pp := range report.Pages {
		for _, en := range pp.Entries {
			if en.Problem != "" {
				findings = append(findings, cli.Finding{
					RuleID:   "chain-promote",
					Severity: "warn",
					FilePath: pp.Page,
					Message:  "draws-from " + en.Ref + ": " + en.Problem,
				})
			}
		}
	}
	return &cli.Response{
		Version:  cli.ResponseVersion,
		Findings: findings,
		Data:     report,
	}
}

// chainDeclareHandler is the `md chain declare` verb: declare draw edges
// page→each draws-from selector and merge them into the page's ^inputs chain
// (results/chain-merge-proof.md — pure-writer composition). Unlike chain
// promote (frontmatter-only scaffold, never merges), declare takes explicit
// selectors and splices new edges into an existing chain, computing each new
// edge's hash. Config-gated like attest.
func chainDeclareHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}
		opts := engine.ScanOptions{Skip: cfg.Scan.Skip, MaxFileSize: cfg.Scan.MaxFileSize}
		return chainDeclareWithFS(os.DirFS(cfg.Scan.Root), cfg.Scan.Root, opts, nil, req)
	}
}

// chainDeclareHandlerWith is the injectable core used by tests (the
// chainPromoteHandlerWith pattern): scan fsys, reshape the engine's seams.
func chainDeclareHandlerWith(fsys fs.FS, root string, opts engine.ScanOptions, mutate func(*attest.Engine)) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		return chainDeclareWithFS(fsys, root, opts, mutate, req)
	}
}

// chainDeclareParams is the strict-decoded param set — an unknown key is
// INVALID_PARAMS, never silently dropped.
type chainDeclareParams struct {
	Page      string   `json:"page"`
	DrawsFrom []string `json:"draws-from"`
	DryRun    bool     `json:"dry_run"`
	Format    string   `json:"format"` // router-consumed; listed so strict parse admits it
}

func chainDeclareWithFS(fsys fs.FS, root string, opts engine.ScanOptions, mutate func(*attest.Engine), req *cli.Request) *cli.Response {
	var params chainDeclareParams
	if req.Params != nil {
		if resp := rejectFileKey(req.Params); resp != nil {
			return resp
		}
		dec := json.NewDecoder(bytes.NewReader(req.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("invalid params: %v — md chain declare accepts: page, draws-from, dry_run, format", err))
		}
	}
	if params.Page == "" {
		return cli.ErrorResponse(cli.ErrInvalidParams, "page is required")
	}
	if len(params.DrawsFrom) == 0 {
		return cli.ErrorResponse(cli.ErrInvalidParams, "draws-from is required — at least one selector to declare")
	}

	eng, errResp := buildAttestEngine(fsys, root, opts)
	if errResp != nil {
		return errResp
	}
	if mutate != nil {
		mutate(eng)
	}

	res, err := eng.ChainDeclare(attest.DeclareOptions{
		Page:      params.Page,
		DrawsFrom: params.DrawsFrom,
		DryRun:    params.DryRun,
	})
	if err != nil {
		return cli.ErrorResponse(cli.ErrInvalidParams, err.Error())
	}

	// A dead/ambiguous selector is a warn finding for the caller (exit 0); the
	// resolvable edges still merge — judgment about the real dependency set stays
	// with the human via the empty claim.
	var findings []cli.Finding
	for _, en := range res.Entries {
		if en.Problem != "" {
			findings = append(findings, cli.Finding{
				RuleID:   "chain-declare",
				Severity: "warn",
				FilePath: res.Page,
				Message:  "draws-from " + en.Ref + ": " + en.Problem,
			})
		}
	}
	return &cli.Response{
		Version:  cli.ResponseVersion,
		Findings: findings,
		Data:     res,
	}
}
