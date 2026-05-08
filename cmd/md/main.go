package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
)

func main() {
	router := cli.NewRouter()

	// Register commands that don't need config/rules
	router.Handle("version", cli.NewVersionHandler())

	// Discover config
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"version":"0.1","error":{"code":"NO_CONFIG","message":"cannot determine working directory: %s"}}`+"\n", err.Error())
		os.Exit(2)
	}
	cfgPath, cfgErr := config.Discover(cwd)

	var loadedRules []rules.Rule
	var cfg *config.Config

	if cfgErr == nil {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, `{"version":"0.1","error":{"code":"INVALID_CONFIG","message":"cannot read config: %s"}}`+"\n", err.Error())
			os.Exit(2)
		}
		cfgDir := filepath.Dir(cfgPath)
		cfg, err = config.Parse(data, cfgDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, `{"version":"0.1","error":{"code":"INVALID_CONFIG","message":%q}}`+"\n", err.Error())
			os.Exit(2)
		}
	}

	// Load rules from all packs
	if cfg != nil {
		for _, pack := range cfg.RulePacks {
			rs, _, err := rules.LoadDir(pack.Path)
			if err != nil {
				fmt.Fprintf(os.Stderr, `{"version":"0.1","error":{"code":"INVALID_RULE","message":%q}}`+"\n", err.Error())
				os.Exit(2)
			}
			loadedRules = append(loadedRules, rs...)
		}

		if err := rules.DetectDuplicates(loadedRules); err != nil {
			fmt.Fprintf(os.Stderr, `{"version":"0.1","error":{"code":"DUPLICATE_RULE","message":%q}}`+"\n", err.Error())
			os.Exit(2)
		}

		// Apply profile
		if cfg.DefaultProfile != "" {
			if p, ok := cfg.Profiles[cfg.DefaultProfile]; ok {
				loadedRules = p.Resolve(loadedRules)
			}
		}
	}

	// Register all built-in checks from checks package
	eng := engine.New()
	registeredChecks := map[string]bool{}
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
		registeredChecks[name] = true
	}

	// Build search function for help — calls search logic directly
	searchFn := func(query string) cli.HelpSearchData {
		return cli.SearchRulesAndChecks(loadedRules, registeredChecks, query)
	}

	// Register commands
	router.Handle("help", cli.NewHelpHandler(router.Commands, searchFn))
	router.Handle("rules ls", cli.RulesLsHandler(loadedRules))
	router.Handle("debug", cli.DebugHandler(loadedRules, registeredChecks))
	router.Handle("check", checkHandler(eng, loadedRules, cfg, cfgErr))

	os.Exit(router.Run(os.Args[1:], os.Stdin))
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
			json.Unmarshal(req.Params, &params)
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
