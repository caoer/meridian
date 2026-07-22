package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/attest"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/frontmatter"
	"github.com/caoer/meridian/internal/run"
	"github.com/caoer/meridian/internal/watch"
)

// statusHandler is the `md status` verb. It carries two behaviours behind one
// name, split without a silent break (surface-spec/status):
//
//   - `md status <page>` (a page is present) → the three-color drift view: a
//     pure read that compares each declared input's recorded rev against live,
//     surfaces the origin tip-compare freshness line, and never executes a claim.
//   - `md status` (no page) → the legacy watch-daemon query, UNCHANGED. Its
//     canonical home is now `md watch status`; the bare form keeps answering as a
//     forwarding alias. Cutting the bare form over to a whole-corpus drift
//     roll-up collides with this legacy shape and is deferred to its own gated
//     card — never renamed out from under a caller.
//
// cfg/cfgErr drive the drift view (corpus scan); cfgPath locates the daemon
// socket for the legacy query.
func statusHandler(cfg *config.Config, cfgErr error, cfgPath string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		// `file` is the run/check spelling; the surface verbs take `page`. Catch
		// it before routing so a `file` typo points at `page` instead of silently
		// falling through to the daemon query.
		if resp := rejectFileKey(req.Params); resp != nil {
			return resp
		}
		page := extractPage(req.Params)
		if page == "" {
			// No page → the legacy daemon query. Other params are ignored exactly
			// as the pre-split handler ignored them (format is router-consumed).
			return queryDaemonStatus(cfgPath, cfgErr)
		}
		return statusDrift(cfg, cfgErr, req.Params, page)
	}
}

// watchStatusHandler is `md watch status` — the canonical home for the
// watch-daemon query. It is the same query the bare `md status` alias forwards
// to; both share queryDaemonStatus so the two spellings can never diverge.
func watchStatusHandler(cfgPath string, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		return queryDaemonStatus(cfgPath, cfgErr)
	}
}

// queryDaemonStatus is the watch-daemon status query (the pre-split
// statusHandler body): read the daemon's stats socket and wrap the bare stats in
// the standard envelope.
func queryDaemonStatus(cfgPath string, cfgErr error) *cli.Response {
	if cfgErr != nil {
		return cli.ErrorResponseWithHint(cli.ErrNoConfig,
			cfgErr.Error(),
			"create meridian.yaml or set MERIDIAN_CONFIG env var")
	}
	sockPath := watch.SocketPath(cfgPath)
	data, err := watch.QueryStatus(sockPath)
	if err != nil {
		return cli.ErrorResponseWithHint(cli.ErrNoDaemon,
			"no running daemon found",
			"start with: md watch")
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return cli.ErrorResponse(cli.ErrStatusFailed, fmt.Sprintf("invalid status response: %v", err))
	}
	return &cli.Response{Version: cli.ResponseVersion, Data: raw}
}

// statusDriftParams is the strict-decoded drift-view param set — an unknown key
// is INVALID_PARAMS, never silently dropped.
type statusDriftParams struct {
	Page   string `json:"page"`
	Fetch  bool   `json:"fetch"`
	Format string `json:"format"` // router-consumed; listed so strict parse admits it
}

// statusDrift runs the read-only drift view for one page. It is config-gated
// like attest/resolve: hashing resolves inputs against the corpus index built
// from the scan root.
func statusDrift(cfg *config.Config, cfgErr error, rawParams json.RawMessage, page string) *cli.Response {
	if cfgErr != nil {
		return cli.ErrorResponseWithHint(cli.ErrNoConfig,
			cfgErr.Error(),
			"create meridian.yaml or set MERIDIAN_CONFIG env var")
	}

	var params statusDriftParams
	if rawParams != nil {
		dec := json.NewDecoder(bytes.NewReader(rawParams))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&params); err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams,
				fmt.Sprintf("invalid params: %v — md status accepts: page, fetch, format", err))
		}
	}

	opts := engine.ScanOptions{Skip: cfg.Scan.Skip, MaxFileSize: cfg.Scan.MaxFileSize}
	eng, errResp := buildAttestEngine(os.DirFS(cfg.Scan.Root), cfg.Scan.Root, opts)
	if errResp != nil {
		return errResp
	}

	report, err := eng.Drift(attest.Options{Page: page})
	if err != nil {
		return cli.ErrorResponse(cli.ErrInvalidParams, err.Error())
	}
	if len(report.Pages) == 0 {
		return cli.ErrorResponse(cli.ErrInvalidParams, "no page resolved for status")
	}
	pd := report.Pages[0]

	// Map the attest domain verdict into the cli presentation payload — cli owns
	// the rendering, the attest engine owns the derivation.
	data := cli.StatusData{
		Page: page,
		Drift: cli.StatusDrift{
			Color:          pd.Color,
			State:          pd.State,
			Case:           pd.Case,
			Reason:         pd.Reason,
			RecordedCommit: pd.RecordedCommit,
			DriftedRefs:    pd.DriftedRefs,
		},
		Freshness: computeFreshness(cfg.Scan.Root, params.Fetch),
		Run:       readRunFacts(cfg.Scan.Root, page),
	}

	resp := &cli.Response{Version: cli.ResponseVersion, Data: data}

	// A red (drifted) page is a finding (exit 1): drift is the actionable signal
	// the caller wants a nonzero exit for. Grey and green are exit 0.
	if pd.Color == attest.ColorRed {
		resp.Findings = []cli.Finding{{
			RuleID:   "status",
			Severity: "warn",
			FilePath: pd.Page,
			Message:  "drifted (" + pd.Case + "): recorded rev != live rev",
		}}
	}
	// An infra abort (cat-file batch death) is a tool failure (exit 2): a
	// transient hiccup must never read as a confirmed verdict.
	if report.ToolFailure != "" {
		resp.Error = &cli.ErrorDetail{
			Code:    "STATUS_TOOL_FAILURE",
			Message: report.ToolFailure,
			Hint:    "the page is unverified, not drifted — fix the environment (git/cat-file) or retry",
		}
	}
	return resp
}

// extractPage leniently reads a top-level "page" string from JSON params,
// returning "" when params are absent, not an object, or carry no page. Lenient
// by design: the no-page branch forwards to the legacy daemon query, which must
// keep answering for every historical invocation shape — a strict parse here
// would reject a legacy param blob the old handler ignored.
func extractPage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	rawPage, ok := obj["page"]
	if !ok {
		return ""
	}
	var page string
	if err := json.Unmarshal(rawPage, &page); err != nil {
		return ""
	}
	return strings.TrimSpace(page)
}

// --- origin tip-compare (freshness) ---

// gitOut runs one read-only git command in dir with a 5s timeout and returns
// trimmed stdout.
func gitOut(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// computeFreshness builds the origin tip-compare line for the vault repo rooted
// at root. It returns nil when root is not a git working tree (no anchor to
// compare — the freshness line is simply absent, never a false "fresh").
func computeFreshness(root string, doFetch bool) *cli.StatusFreshness {
	top, err := gitOut(root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil
	}
	branch, err := gitOut(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" || branch == "HEAD" {
		return nil
	}
	fr := &cli.StatusFreshness{Branch: branch}
	if doFetch {
		// Explicit net.git opt-in: refresh origin refs before comparing.
		if _, ferr := gitOut(root, "fetch", "--quiet"); ferr != nil {
			fr.Note = "origin fetch failed: " + ferr.Error()
		}
	}
	upstream := "origin/" + branch
	fr.Upstream = upstream
	if _, err := gitOut(root, "rev-parse", "--verify", "--quiet", upstream); err != nil {
		fr.Summary = "no " + upstream + " ref"
		fr.Note = "origin never fetched for this branch — the anchor's freshness is unknown (grey)"
		fr.RefsFetched = "never"
		return fr
	}
	if cnt, err := gitOut(root, "rev-list", "--count", "HEAD.."+upstream); err == nil {
		if n, convErr := strconv.Atoi(cnt); convErr == nil {
			fr.BehindBy = n
			if n > 0 {
				fr.Summary = fmt.Sprintf("behind %s by %d", upstream, n)
			} else {
				fr.Summary = "up to date with " + upstream
			}
		}
	}
	if age, ok := originRefAge(top, branch); ok {
		fr.RefsFetched = humanizeAge(age)
	} else {
		fr.RefsFetched = "unknown"
	}
	return fr
}

// originRefAge is the wall-clock age of the local origin ref — when origin was
// last fetched, read from the ref file's mtime (loose ref first, then
// FETCH_HEAD). gitDir is the absolute .git directory.
func originRefAge(gitDir, branch string) (time.Duration, bool) {
	for _, c := range []string{
		filepath.Join(gitDir, "refs", "remotes", "origin", branch),
		filepath.Join(gitDir, "FETCH_HEAD"),
	} {
		if fi, err := os.Stat(c); err == nil {
			return time.Since(fi.ModTime()), true
		}
	}
	return 0, false
}

// humanizeAge renders a coarse "N<unit> ago" for the ref-age line.
func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// --- exec-facts from the run record ---

// readRunFacts loads the page's run record exec-facts from its sidecar
// (<stem>.runs.md). It reads the record's frontmatter `runs:` map directly — the
// same fields md run persists — so the status envelope reports run states from
// the record, never a fresh re-derivation. Returns nil when no record exists (or
// it is unreadable: status never fabricates exec-facts).
func readRunFacts(root, page string) *cli.StatusRun {
	pageFile := page
	if i := strings.IndexByte(pageFile, '#'); i >= 0 {
		pageFile = pageFile[:i]
	}
	abs := filepath.Join(root, filepath.FromSlash(pageFile))
	recPath := run.RecordPath(abs)
	data, err := os.ReadFile(recPath)
	if err != nil {
		return nil
	}
	doc, err := frontmatter.ParseBytes(data)
	if err != nil || doc == nil {
		return nil
	}
	runs, ok := doc.Meta["runs"].(map[string]any)
	if !ok || len(runs) == 0 {
		return nil
	}
	relRec, err := filepath.Rel(root, recPath)
	if err != nil {
		relRec = recPath
	}
	facts := &cli.StatusRun{RecordPath: filepath.ToSlash(relRec), Tasks: map[string]cli.StatusRunTask{}}
	for name, raw := range runs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// Read the record's fields verbatim (self-contained: status-colors owns
		// no shared frontmatter-meta helper — sourced, never re-derived).
		at, _ := m["at"].(string)
		commit, _ := m["commit"].(string)
		timedOut, _ := m["timed_out"].(bool)
		exit := 0
		switch v := m["exit"].(type) {
		case int:
			exit = v
		case int64:
			exit = int(v)
		case float64:
			exit = int(v)
		}
		facts.Tasks[name] = cli.StatusRunTask{Exit: exit, TimedOut: timedOut, At: at, Commit: commit}
	}
	if len(facts.Tasks) == 0 {
		return nil
	}
	return facts
}
