package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/contract"
	"github.com/caoer/meridian/internal/domains"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/fix"
	"github.com/caoer/meridian/internal/hooks"
	"github.com/caoer/meridian/internal/mv"
	"github.com/caoer/meridian/internal/rolepack"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/schema"
	"github.com/caoer/meridian/internal/skillpack"
	"github.com/caoer/meridian/internal/vfs"
	"github.com/caoer/meridian/internal/watch"
)

// exitError writes a proper JSON error response to stderr and exits.
func exitError(code, message string) {
	resp := cli.ErrorResponse(code, message)
	json.NewEncoder(os.Stderr).Encode(resp)
	os.Exit(2)
}

func stdinIfPiped() io.Reader {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	return os.Stdin
}

func main() {
	router := cli.NewRouter()

	// Register commands that don't need config/rules
	router.Handle("version", cli.NewVersionHandler())

	// Discover config
	cwd, err := os.Getwd()
	if err != nil {
		exitError(cli.ErrNoConfig, "cannot determine working directory: "+err.Error())
	}
	cfgPath, cfgErr := config.Discover(cwd)

	var loadedRules []rules.Rule
	var allRules []rules.Rule
	var cfg *config.Config

	// Config load failures are captured, not fatal: commands that need no
	// config (run, read, version, help) must keep working; config-needing
	// handlers surface cfgErr themselves.
	if cfgErr == nil {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			cfgErr = fmt.Errorf("cannot read config: %w", err)
		} else if cfg, err = config.Parse(data, filepath.Dir(cfgPath)); err != nil {
			cfgErr = err
		}
	}

	// Load rules from all packs. Failures are deferred like config failures:
	// run/read/version/help must keep working beside a broken rule pack.
	//
	// Override order: explicit config rule_packs > filesystem > embedded pack.
	// When config specifies rule_packs, those are used (filesystem).
	// When no config or no rule_packs, the compiled-in contract pack is the
	// fallback so the binary always has contract rules available.
	if cfg != nil {
		if len(cfg.RulePacks) > 0 {
			for _, pack := range cfg.RulePacks {
				rs, _, err := rules.LoadDir(pack.Path)
				if err != nil {
					cfgErr = fmt.Errorf("invalid rule pack %s: %w", pack.Path, err)
					break
				}
				loadedRules = append(loadedRules, rs...)
			}
		} else {
			// No rule_packs in config → fall back to embedded contract pack.
			rs, _, err := rules.LoadFS(contract.FS(), ".")
			if err != nil {
				cfgErr = fmt.Errorf("embedded contract pack: %w", err)
			} else {
				loadedRules = append(loadedRules, rs...)
			}
		}

		if cfgErr == nil {
			if err := rules.DetectDuplicates(loadedRules); err != nil {
				cfgErr = fmt.Errorf("duplicate rule: %w", err)
			}
		}

		// Apply config-level rule params (shallow merge, config wins).
		if cfgErr == nil && len(cfg.RuleParams) > 0 {
			loadedRules = rules.ApplyConfigParams(loadedRules, cfg.RuleParams)
		}

		if cfgErr != nil {
			loadedRules = nil
		}

		// Capture unfiltered rules for rules check (needs all rules).
		allRules = loadedRules

		// Apply default profile for other commands.
		if cfg.DefaultProfile != "" {
			if p, ok := cfg.Profiles[cfg.DefaultProfile]; ok {
				loadedRules = p.Resolve(loadedRules)
			}
		}

		// Role-selected lint pack (C45): the repo's role mechanically appends
		// the embedded pack — after profile resolution, so per-wiki profiles
		// can never filter policy off. Resolution failures (role drift,
		// invalid enum) are config errors: they must block, not degrade.
		if cfgErr == nil {
			role, _, err := rolepack.Resolve(cfg.Role, cfg.Scan.Root)
			if err == nil {
				var packRules []rules.Rule
				if packRules, err = rolepack.Rules(role); err == nil {
					loadedRules = append(loadedRules, packRules...)
					allRules = append(allRules, packRules...)
				}
			}
			if err != nil {
				cfgErr = err
				loadedRules = nil
				allRules = nil
			}
		}
	}

	// Register all built-in checks from checks package
	eng := engine.New()
	if cfg != nil {
		eng.SetSkip(cfg.Scan.Skip)
		eng.SetMaxFileSize(cfg.Scan.MaxFileSize)
		eng.SetForeignRoots(cfg.ForeignRoots)
		// Checks that leave the FS abstraction (probe execution) need the OS
		// root the scanned DirFS is rooted at — same root checkHandler scans.
		eng.SetScanRoot(cfg.Scan.Root)
	}
	registeredChecks := map[string]bool{}
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
		registeredChecks[name] = true
	}
	// Phase-2 checks (link family; U6 adds effect-pin) run over per-run snapshots
	// and are never cached — see checks.Phase2 / engine.MarkPhase2.
	eng.MarkPhase2(checks.Phase2...)

	// Build search function for help — calls search logic directly
	searchFn := func(query string) cli.HelpSearchData {
		return cli.SearchRulesAndChecks(loadedRules, registeredChecks, query)
	}

	// Profiles map for rules check handler.
	var profiles map[string]config.Profile
	if cfg != nil {
		profiles = cfg.Profiles
	}

	// Register commands
	router.Handle("help", cli.NewHelpHandler(router.Commands, searchFn))
	router.Handle("rules ls", requireConfig(cfgErr, cli.RulesLsHandler(loadedRules)))
	router.Handle("rules check", requireConfig(cfgErr, cli.RulesCheckHandler(allRules, profiles)))
	router.Handle("debug", requireConfig(cfgErr, cli.DebugHandler(loadedRules, registeredChecks)))
	router.Handle("check", checkHandler(eng, loadedRules, cfg, cfgErr))
	// `md check <path>` — positional sugar for {"scope": "<path>"}. The bare
	// path must exist (relative to cwd), otherwise fail loud: a typo'd path
	// must never read as a clean scoped check.
	router.HandlePositional("check", func(arg string) (json.RawMessage, error) {
		if _, err := os.Stat(arg); err != nil {
			return nil, fmt.Errorf("check: %q is neither JSON params nor an existing path", arg)
		}
		params, err := json.Marshal(map[string]string{"scope": arg})
		if err != nil {
			return nil, err
		}
		return params, nil
	})
	router.Handle("debt", debtHandler(cfg, cfgErr))
	router.Handle("cache stats", cacheStatsHandler(cfg, cfgErr))
	router.Handle("cache clean", cacheCleanHandler(cfg, cfgErr))
	router.Handle("domains tree", domainsTreeHandler(cfg, cfgErr))
	router.Handle("domains show", domainsShowHandler(cfg, cfgErr))
	router.Handle("fix", fixHandler(eng, loadedRules, cfg, cfgErr))
	router.Handle("mv", mvHandler(eng, loadedRules, cfg, cfgErr))
	router.Handle("watch", watchHandler(cfg, cfgErr, cfgPath))
	router.Handle("status", statusHandler(cfgPath, cfgErr))
	router.Handle("run", runHandler())
	registerBodyVerbs(router)
	registerDefVerbs(router)
	// resolve is config-gated: hashing resolves wikilinks against the corpus
	// index built from the scan root, exactly as check does.
	router.Handle("resolve", resolveHandler(cfg, cfgErr))
	// attest is config-gated like resolve: the receipt writer hashes inputs
	// against the corpus index built from the scan root. Strict sole writer —
	// working-tree bytes only, never git add/commit/push.
	router.Handle("attest", attestHandler(cfg, cfgErr))
	// chain promote is the migration scaffold verb (§6.2): draws-from → inputs.
	// Two-token verb (the `rules ls` precedent); config-gated like attest.
	router.Handle("chain promote", chainPromoteHandler(cfg, cfgErr))
	// encode is deliberately NOT config-gated: the encoder is pure grammar.
	router.Handle("encode", encodeHandler())
	router.Handle("schema", schemaHandler(cfg, cfgErr))
	// llm-wiki check is deliberately NOT config-gated: it is the environment
	// doctor that must run on a machine with nothing set up yet.
	router.Handle("llm-wiki check", llmWikiCheckHandler())
	// skill render is deliberately NOT config-gated: it renders a shipped
	// skill folder anywhere, standing in for a harness's load-time
	// pre-resolution.
	router.Handle("skill render", skillRenderHandler())
	// `md skill render <dir>` — positional sugar for {"skill": "<dir>"}. The
	// bare path must exist, otherwise fail loud: a typo'd folder must never
	// read as an empty render.
	router.HandlePositional("skill render", func(arg string) (json.RawMessage, error) {
		if _, err := os.Stat(arg); err != nil {
			return nil, fmt.Errorf("skill render: %q is neither JSON params nor an existing skill folder", arg)
		}
		params, err := json.Marshal(map[string]string{"skill": arg})
		if err != nil {
			return nil, err
		}
		return params, nil
	})

	os.Exit(router.Run(os.Args[1:], stdinIfPiped()))
}

// requireConfig gates handlers whose data is silently empty under a broken
// config (rules ls/check, debug) — without it they report false success
// ("no conflicts detected", exit 0) when meridian.yaml or a rule pack failed
// to load.
func requireConfig(cfgErr error, h cli.Handler) cli.Handler {
	if cfgErr == nil {
		return h
	}
	return func(req *cli.Request) *cli.Response {
		return cli.ErrorResponseWithHint(cli.ErrInvalidConfig, cfgErr.Error(),
			"fix meridian.yaml (or MERIDIAN_CONFIG) — this command needs a loadable config")
	}
}

func buildRegistry(cfg *config.Config, cfgErr error) (*domains.Registry, *cli.Response) {
	if cfgErr != nil {
		return nil, cli.ErrorResponseWithHint(cli.ErrNoConfig,
			cfgErr.Error(),
			"create meridian.yaml or set MERIDIAN_CONFIG env var")
	}

	fsys := os.DirFS(cfg.Scan.Root)
	docs, err := engine.Scan(fsys)
	if err != nil {
		return nil, cli.ErrorResponse("SCAN_ERROR", "scan failed: "+err.Error())
	}

	reg := domains.NewRegistry()
	for _, doc := range docs {
		reg.Add(doc.Tags)
	}
	return reg, nil
}

// debtHandler outputs the incorporation-debt list: wiki/sources/** documents
// whose frontmatter tags include `do/incorporate`. On-disk/agent-readable
// complement to DIGEST.md's Obsidian-live Dataview query — always fresh.
func debtHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}

		fsys := os.DirFS(cfg.Scan.Root)
		docs, err := engine.ScanWithOpts(fsys, engine.ScanOptions{
			Skip:        cfg.Scan.Skip,
			MaxFileSize: cfg.Scan.MaxFileSize,
		})
		if err != nil {
			return cli.ErrorResponse("SCAN_ERROR", "scan failed: "+err.Error())
		}

		sources := make([]cli.DebtSource, 0, len(docs))
		for _, d := range docs {
			var mt time.Time
			if info, statErr := fs.Stat(fsys, d.Path); statErr == nil {
				mt = info.ModTime()
			}
			sources = append(sources, cli.NewDebtSource(d.Path, d.Tags, d.Frontmatter, mt))
		}

		data := cli.FilterDebt(sources, "wiki/sources/", "do/incorporate")
		return &cli.Response{Version: cli.ResponseVersion, Data: data}
	}
}

func domainsTreeHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		reg, errResp := buildRegistry(cfg, cfgErr)
		if errResp != nil {
			return errResp
		}
		return cli.DomainsTreeHandler(reg)(req)
	}
}

func domainsShowHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		reg, errResp := buildRegistry(cfg, cfgErr)
		if errResp != nil {
			return errResp
		}
		return cli.DomainsShowHandler(reg)(req)
	}
}

func fixHandler(eng *engine.Engine, loadedRules []rules.Rule, cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}

		var params struct {
			Scope  string   `json:"scope"`
			Rules  []string `json:"rules"`
			DryRun bool     `json:"dry-run"`
			Format string   `json:"format"` // router-consumed (output rendering); listed so strict parse admits it
		}
		if req.Params != nil {
			dec := json.NewDecoder(bytes.NewReader(req.Params))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}

		// Filter rules if specific IDs requested
		targetRules := loadedRules
		if len(params.Rules) > 0 {
			ruleSet := make(map[string]bool, len(params.Rules))
			for _, id := range params.Rules {
				ruleSet[id] = true
			}
			var filtered []rules.Rule
			for _, r := range loadedRules {
				if ruleSet[r.ID] {
					filtered = append(filtered, r)
				}
			}
			targetRules = filtered
		}

		fsys := vfs.NewOSFS(cfg.Scan.Root)
		fixer := fix.New(eng, fix.All)
		report, err := fixer.Fix(fsys, targetRules, fix.Options{
			DryRun: params.DryRun,
			Scope:  params.Scope,
		})
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
		}

		// Convert fix.FixReport → cli.FixData
		data := cli.FixData{
			FixedCount:     report.FixedCount,
			UnfixableCount: report.UnfixableCount,
		}
		for _, f := range report.Fixed {
			data.Fixed = append(data.Fixed, cli.FixResultItem{
				FilePath: f.FilePath,
				RuleID:   f.RuleID,
				Action:   f.Action,
			})
		}
		for _, s := range report.Unfixable {
			data.Unfixable = append(data.Unfixable, cli.SkipItem{
				FilePath: s.FilePath,
				RuleID:   s.RuleID,
				Reason:   s.Reason,
			})
		}

		return &cli.Response{
			Version: cli.ResponseVersion,
			Data:    data,
		}
	}
}

func mvHandler(eng *engine.Engine, loadedRules []rules.Rule, cfg *config.Config, cfgErr error) cli.Handler {
	return mvHandlerFS(eng, loadedRules, cfgErr, func() vfs.WriteFS {
		return vfs.NewOSFS(cfg.Scan.Root)
	})
}

func mvHandlerFS(eng *engine.Engine, loadedRules []rules.Rule, cfgErr error, makeFS func() vfs.WriteFS) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}

		var params struct {
			Source string `json:"source"`
			Dest   string `json:"dest"`
			DryRun bool   `json:"dry-run"`
		}
		if req.Params != nil {
			dec := json.NewDecoder(bytes.NewReader(req.Params))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}

		if params.Source == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: source")
		}
		if params.Dest == "" {
			return cli.ErrorResponse(cli.ErrInvalidParams, "missing required param: dest")
		}

		fsys := makeFS()
		result, err := mv.Move(fsys, params.Source, params.Dest, eng, loadedRules, params.DryRun)
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidParams, err.Error())
		}

		var warnings []cli.Warning
		for _, w := range result.Warnings {
			warnings = append(warnings, cli.Warning{Message: w})
		}

		return &cli.Response{
			Version:  cli.ResponseVersion,
			Data:     result,
			Warnings: warnings,
		}
	}
}

func watchHandler(cfg *config.Config, cfgErr error, cfgPath string) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}
		if cfg.Watch == nil {
			return cli.ErrorResponseWithHint(cli.ErrInvalidConfig,
				"no watch section in config",
				"add a 'watch:' section to meridian.yaml")
		}

		parsedHooks, err := hooks.ParseHooks(cfg.Watch.Hooks)
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidConfig, err.Error())
		}

		w, err := watch.New(cfg.Scan.Root, cfg.Watch.Ignore, cfg.Watch.DebounceMs)
		if err != nil {
			return cli.ErrorResponse(cli.ErrWatchFailed, fmt.Sprintf("cannot start watcher: %v", err))
		}

		d := watch.NewDaemon(w, parsedHooks, os.Stdout)
		// md-on-change: docs declare their own reactivity; runs are receipted
		// via sidecar run records (record:true semantics).
		onChangeTimeout := time.Duration(cfg.Watch.OnChangeTimeoutMs) * time.Millisecond
		d.SetOnChangeRunner(func(p string) *watch.OnChangeResult {
			return watch.RunOnChange(p, onChangeTimeout)
		})

		// Start status socket
		sockPath := watch.SocketPath(cfgPath)
		srv, err := watch.NewStatusServer(sockPath, d.Stats())
		if err != nil {
			w.Close()
			return cli.ErrorResponse(cli.ErrWatchFailed, fmt.Sprintf("cannot start status socket: %v", err))
		}

		// Handle signals for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigCh
			srv.Close()
			d.Stop()
		}()

		// Run blocks until stopped by signal or watcher close
		d.Run()

		// Ensure cleanup regardless of exit path (signal or watcher close).
		// Both are idempotent via sync.Once.
		signal.Stop(sigCh)
		srv.Close()
		d.Stop()
		return &cli.Response{Version: cli.ResponseVersion}
	}
}

func statusHandler(cfgPath string, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
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

		// Socket returns bare stats — wrap in standard envelope
		var raw json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return cli.ErrorResponse(cli.ErrStatusFailed, fmt.Sprintf("invalid status response: %v", err))
		}

		return &cli.Response{
			Version: cli.ResponseVersion,
			Data:    raw,
		}
	}
}

// findSchemaRoot walks upward from start looking for a directory containing
// SCHEMA.md.  It stops at the git toplevel (directory containing .git) or
// the filesystem root.  Returns ("", false) if no SCHEMA.md is found.
func findSchemaRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "SCHEMA.md")); err == nil {
			return dir, true
		}
		// Stop at git toplevel.
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // filesystem root
		}
		dir = parent
	}
	return "", false
}

func schemaHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		// schema does not strictly require config — it needs a scan root
		// to find SCHEMA.md. When config provides scan.root, use it.
		// Otherwise walk upward from cwd to find SCHEMA.md (prevents
		// silent wrong answers when invoked from a wiki subdir).
		var warnings []cli.Warning

		root := ""
		if cfg != nil {
			root = cfg.Scan.Root
		} else {
			if found, ok := findSchemaRoot("."); ok {
				root = found
			} else {
				// No SCHEMA.md anywhere up to git toplevel / fs root.
				// Contract-only is correct, but surface a NOTE so the
				// caller knows this is not a local overlay.
				root = "."
				warnings = append(warnings, cli.Warning{
					Message: "no SCHEMA.md found (searched from cwd to git toplevel); returning contract-only schema",
				})
			}
		}

		merged, err := schema.Effective(root)
		if err != nil {
			return cli.ErrorResponse(cli.ErrInvalidInput, err.Error())
		}

		return &cli.Response{
			Version:  cli.ResponseVersion,
			Data:     cli.SchemaData{Schema: merged},
			Warnings: warnings,
		}
	}
}

// skillTreeCheck runs the embedded skill-tree pack over one directory —
// its own engine (default skips, no config, no cache: skill trees are
// small and the run is one-shot).
func skillTreeCheck(dir string) *cli.Response {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return cli.ErrorResponse(cli.ErrInvalidParams, fmt.Sprintf("skill_tree: %q is not an existing directory", dir))
	}
	packRules, err := skillpack.Rules()
	if err != nil {
		return cli.ErrorResponse(cli.ErrInvalidConfig, err.Error())
	}

	eng := engine.New()
	eng.SetSkip([]string{".git", ".obsidian", "node_modules"})
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
	}
	eng.MarkPhase2(checks.Phase2...)

	start := time.Now()
	findings := eng.Run(os.DirFS(dir), packRules)
	warnings := eng.Warnings()

	cliWarnings := make([]cli.Warning, len(warnings))
	for i, w := range warnings {
		cliWarnings[i] = cli.Warning(w)
	}
	return &cli.Response{
		Version:  cli.ResponseVersion,
		Findings: findings,
		Stats: &cli.Stats{
			RulesApplied:  len(packRules),
			FindingsCount: len(findings),
			DurationMs:    int(time.Since(start).Milliseconds()),
		},
		Warnings: cliWarnings,
	}
}

// cacheStatsHandler wraps the read-only cache inventory with the standard config
// gate: the store location is derived from the scan root, so a broken config has
// no root to resolve.
func cacheStatsHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}
		return cli.CacheStatsHandler(cfg.Scan.Root)(req)
	}
}

// cacheCleanHandler wraps the destructive cache removal with the config gate. The
// refuse-unless-under-UserCacheDir/meridian safety check lives in cache.CleanForRoot.
func cacheCleanHandler(cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}
		return cli.CacheCleanHandler(cfg.Scan.Root)(req)
	}
}

// cacheVerifyEnabled reports whether MD_CACHE_VERIFY requests the recompute-on-hit
// honesty gate (accepts "1" or "true", case-insensitive).
func cacheVerifyEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MD_CACHE_VERIFY")))
	return v == "1" || v == "true"
}

// runCacheVerify implements MD_CACHE_VERIFY: it serves a run from the warm
// persistent cache, recomputes the same corpus with no cache (the authority), and
// reports every finding that differs. EntriesChecked is the number of cache hits
// the served run actually consumed — 0 means the cache was cold (the check is
// trivially clean; CI must warm it first). It never writes the cache (no
// Prune/Save), so verifying is side-effect-free.
func runCacheVerify(eng *engine.Engine, loadedRules []rules.Rule, cfg *config.Config, fsys fs.FS) *cli.Response {
	store, _ := cache.OpenForRoot(cfg.Scan.Root)
	store.ResetStats()
	cached := eng.RunCached(fsys, loadedRules, store)
	if ferr := eng.FatalErr(); ferr != nil {
		// The stale-binary floor holds even under the cache-verify honesty gate:
		// a required unregistered check fails loud rather than reporting a
		// divergence verdict computed with the rule silently skipped.
		return cli.ErrorResponseWithHint(cli.ErrCheckNotRegistered, ferr.Error(),
			"the rule pack requires a check this binary lacks — reinstall md (just install) or rebuild from the pinned commit")
	}
	hits := store.Stats().Hits
	warnings := eng.Warnings()

	fresh := eng.RunCached(fsys, loadedRules, nil)

	divergences := diffFindings(cached, fresh)

	// Facts-level coverage: the finding diff above sees only phase-2 consumers of
	// facts. The persistent cache also carries phase-1 fact fields with no consumer
	// yet (SliceHashes, anchored Embeds, repo scalars); recompute them fresh and
	// compare so a stale slice hash can't ride the cache silently until U-B2b wires
	// chain-fresh/repo-cataloged. Reported as facts_diverge divergences so the same
	// verified==true CI gate catches them.
	for _, p := range eng.VerifyFactsAgainstStore(fsys, store) {
		divergences = append(divergences, cli.CacheDivergence{
			FilePath: p,
			Kind:     "facts_diverge",
			Message:  "cached facts differ from a fresh extraction (SliceHashes/Embeds/repo scalars)",
		})
	}

	cliWarnings := make([]cli.Warning, 0, len(warnings))
	for _, w := range warnings {
		cliWarnings = append(cliWarnings, cli.Warning(w))
	}

	return &cli.Response{
		Version: cli.ResponseVersion,
		Data: cli.CacheVerifyData{
			Verified:       len(divergences) == 0,
			EntriesChecked: hits,
			Divergences:    divergences,
		},
		Warnings: cliWarnings,
	}
}

// diffFindings is the multiset symmetric difference between the cache-served and
// freshly-recomputed finding sets. A finding the cache served but a fresh run does
// not produce is "only_in_cache" (stale); one a fresh run produces but the cache
// dropped is "only_in_fresh". Output is sorted for deterministic (CI-parseable)
// reports.
func diffFindings(cached, fresh []cli.Finding) []cli.CacheDivergence {
	type fkey struct {
		FilePath string
		RuleID   string
		Message  string
		Line     int
		Column   int
	}
	key := func(f cli.Finding) fkey {
		return fkey{f.FilePath, f.RuleID, f.Message, f.Line, f.Column}
	}
	count := func(list []cli.Finding) map[fkey]int {
		m := make(map[fkey]int, len(list))
		for _, f := range list {
			m[key(f)]++
		}
		return m
	}
	cachedC := count(cached)
	freshC := count(fresh)

	var divs []cli.CacheDivergence
	add := func(k fkey, kind string, n int) {
		for i := 0; i < n; i++ {
			divs = append(divs, cli.CacheDivergence{
				FilePath: k.FilePath,
				RuleID:   k.RuleID,
				Line:     k.Line,
				Column:   k.Column,
				Kind:     kind,
				Message:  k.Message,
			})
		}
	}
	for k, c := range cachedC {
		if extra := c - freshC[k]; extra > 0 {
			add(k, "only_in_cache", extra)
		}
	}
	for k, c := range freshC {
		if missing := c - cachedC[k]; missing > 0 {
			add(k, "only_in_fresh", missing)
		}
	}
	sort.Slice(divs, func(i, j int) bool {
		a, b := divs[i], divs[j]
		if a.FilePath != b.FilePath {
			return a.FilePath < b.FilePath
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Message < b.Message
	})
	return divs
}

func checkHandler(eng *engine.Engine, loadedRules []rules.Rule, cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		// Parse params — strict: an unknown key is rejected, never silently
		// ignored. A stale binary that drops a param it doesn't know
		// mis-scopes the run and reports a clean check that never happened
		// (the skill_tree hazard); the class dies here.
		var params struct {
			Scope     string `json:"scope"`
			SkillTree string `json:"skill_tree"`
			Strict    *bool  `json:"strict"`   // per-run override of config strict; nil = config default
			NoCache   bool   `json:"no_cache"` // per-run opt-out of the persistent fact cache (Decision 13: no bare-flag mechanism)
			Format    string `json:"format"`   // router-consumed (output rendering); listed so strict parse admits it
		}
		if req.Params != nil {
			dec := json.NewDecoder(bytes.NewReader(req.Params))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams,
					fmt.Sprintf("invalid params: %v — md check accepts: scope, skill_tree, strict, no_cache, format (assert the binary with `md version`)", err))
			}
		}

		// skill_tree: config-less scoped run over a shipped skill directory
		// with the embedded wikilink-integrity pack. Deliberately NOT
		// config-gated (llm-wiki check precedent): a skill tree lives outside
		// any wiki — no meridian.yaml is its normal state, not an error.
		if params.SkillTree != "" {
			if params.Scope != "" {
				return cli.ErrorResponse(cli.ErrInvalidParams, "skill_tree and scope are mutually exclusive — skill_tree already scopes the run")
			}
			resp := skillTreeCheck(params.SkillTree)
			// Config-less path: strict comes only from the param.
			if params.Strict != nil {
				resp.Strict = *params.Strict
			}
			return resp
		}

		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}

		start := time.Now()
		fsys := os.DirFS(cfg.Scan.Root)

		// MD_CACHE_VERIFY: recompute-on-hit honesty gate. Serve from the warm
		// cache, recompute everything fresh, and report any finding that differs
		// (nonzero exit on divergence). A pure check — it never mutates the
		// on-disk cache. Structurally cannot observe cross-run git staleness:
		// phase-2 findings are never cached (see internal/cli/cmd_cache.go).
		if cacheVerifyEnabled() {
			return runCacheVerify(eng, loadedRules, cfg, fsys)
		}

		// Open the store this run uses. The persistent store lives under
		// UserCacheDir (OpenForRoot); {"no_cache":true} or config cache.enabled:
		// false selects the in-memory NewStore("") — always cold, never persisted.
		// A user-cache-dir failure degrades to in-memory with a surfaced warning,
		// never a failed run.
		useCache := !params.NoCache && cfg.CacheEnabled()
		var cacheWarnings []cli.Warning
		var store *cache.Store
		if useCache {
			s, w := cache.OpenForRoot(cfg.Scan.Root)
			store = s
			if w != nil {
				cacheWarnings = append(cacheWarnings, *w)
			}
		} else {
			store = cache.NewStore("")
		}

		store.ResetStats() // per-invocation stats, not cumulative
		findings := eng.RunCached(fsys, loadedRules, store)

		// A `required:true` rule whose check the binary does not register is a tool
		// failure (exit 2), not a warn-and-continue: fail loud before persisting a
		// cache or reporting a green gate. This is the stale-binary floor — an old
		// build against an evolved pack must never pass emptily.
		if ferr := eng.FatalErr(); ferr != nil {
			return cli.ErrorResponseWithHint(cli.ErrCheckNotRegistered, ferr.Error(),
				"the rule pack requires a check this binary lacks — reinstall md (just install) or rebuild from the pinned commit")
		}

		// Persist after a full run: evict entries for vanished documents, then
		// write dirty shards. checkHandler always scans the whole corpus (scope is
		// a post-filter below), so pruning is safe here — but only when the run
		// actually completed a full scan. A nil/empty universe (no active rules, a
		// scan error) must skip Prune, or Prune(nil) would wipe a still-valid
		// cache. Save failure is a single non-fatal warning (results are correct;
		// the next run is merely colder).
		if useCache {
			if universe := eng.ScannedPaths(); len(universe) > 0 {
				store.Prune(universe)
				if err := store.Save(); err != nil {
					cacheWarnings = append(cacheWarnings, cli.Warning{
						Code:    "CACHE_SAVE_FAILED",
						Message: "cache save failed (results are correct, next run is colder): " + err.Error(),
					})
				}
			}
		}

		// Normalize scope: findings carry scan-root-relative paths with no
		// "./" prefix, so "." (and "./x" forms) must be cleaned or the filter
		// silently drops every finding — a clean scoped check that never
		// happened. "." means the whole root: unscoped.
		if params.Scope != "" {
			params.Scope = path.Clean(filepath.ToSlash(params.Scope))
			if params.Scope == "." {
				params.Scope = ""
			}
		}
		// Filter by scope prefix if set
		if params.Scope != "" {
			var scoped []cli.Finding
			dirPrefix := params.Scope
			if !strings.HasSuffix(dirPrefix, "/") {
				dirPrefix += "/"
			}
			for _, f := range findings {
				switch {
				case f.FilePath == params.Scope: // exact match
				case f.FilePath == params.Scope+".md": // file shorthand (wiki/bad → wiki/bad.md)
				case strings.HasPrefix(f.FilePath, dirPrefix): // directory children
				default:
					continue
				}
				scoped = append(scoped, f)
			}
			findings = scoped
		}
		warnings := eng.Warnings()
		dur := time.Since(start)

		// Engine warnings first, then the run's cache warnings (CACHE_UNAVAILABLE
		// on open, CACHE_SAVE_FAILED on persist) — both surface as NOTE lines and
		// are counted in the check summary.
		cliWarnings := make([]cli.Warning, 0, len(warnings)+len(cacheWarnings))
		for _, w := range warnings {
			cliWarnings = append(cliWarnings, cli.Warning(w))
		}
		cliWarnings = append(cliWarnings, cacheWarnings...)

		// Populate cache stats
		cs := store.Stats()
		var hitRate float64
		if cs.Total > 0 {
			hitRate = float64(cs.Hits) / float64(cs.Total)
		}

		strict := cfg.Strict
		if params.Strict != nil {
			strict = *params.Strict
		}

		return &cli.Response{
			Version:  cli.ResponseVersion,
			Strict:   strict,
			Findings: findings,
			Stats: &cli.Stats{
				FilesScanned:  cs.Total,
				FilesSkipped:  cs.Hits,
				CacheHitRate:  hitRate,
				RulesApplied:  len(loadedRules),
				FindingsCount: len(findings),
				DurationMs:    int(dur.Milliseconds()),
			},
			Warnings: cliWarnings,
		}
	}
}
