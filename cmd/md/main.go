package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/domains"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/fix"
	"github.com/caoer/meridian/internal/hooks"
	"github.com/caoer/meridian/internal/mv"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/schema"
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
	if cfg != nil {
		for _, pack := range cfg.RulePacks {
			rs, _, err := rules.LoadDir(pack.Path)
			if err != nil {
				cfgErr = fmt.Errorf("invalid rule pack %s: %w", pack.Path, err)
				break
			}
			loadedRules = append(loadedRules, rs...)
		}

		if cfgErr == nil {
			if err := rules.DetectDuplicates(loadedRules); err != nil {
				cfgErr = fmt.Errorf("duplicate rule: %w", err)
			}
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
	}

	// Register all built-in checks from checks package
	eng := engine.New()
	if cfg != nil {
		eng.SetSkip(cfg.Scan.Skip)
		eng.SetMaxFileSize(cfg.Scan.MaxFileSize)
	}
	registeredChecks := map[string]bool{}
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
		registeredChecks[name] = true
	}

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
	router.Handle("debt", debtHandler(cfg, cfgErr))
	router.Handle("domains tree", domainsTreeHandler(cfg, cfgErr))
	router.Handle("domains show", domainsShowHandler(cfg, cfgErr))
	router.Handle("fix", fixHandler(eng, loadedRules, cfg, cfgErr))
	router.Handle("mv", mvHandler(eng, loadedRules, cfg, cfgErr))
	router.Handle("watch", watchHandler(cfg, cfgErr, cfgPath))
	router.Handle("status", statusHandler(cfgPath, cfgErr))
	router.Handle("run", runHandler())
	router.Handle("read", readHandler())
	router.Handle("schema", schemaHandler(cfg, cfgErr))

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
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
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
			if err := json.Unmarshal(req.Params, &params); err != nil {
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

func checkHandler(eng *engine.Engine, loadedRules []rules.Rule, cfg *config.Config, cfgErr error) cli.Handler {
	store := cache.NewStore("") // in-memory; persists across handler calls within same process
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}

		// Parse scope from params
		var params struct {
			Scope string `json:"scope"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
		}

		start := time.Now()
		fsys := os.DirFS(cfg.Scan.Root)

		store.ResetStats() // per-invocation stats, not cumulative
		findings := eng.RunCached(fsys, loadedRules, store)

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

		cliWarnings := make([]cli.Warning, len(warnings))
		for i, w := range warnings {
			cliWarnings[i] = cli.Warning(w)
		}

		// Populate cache stats
		cs := store.Stats()
		var hitRate float64
		if cs.Total > 0 {
			hitRate = float64(cs.Hits) / float64(cs.Total)
		}

		return &cli.Response{
			Version:  cli.ResponseVersion,
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
