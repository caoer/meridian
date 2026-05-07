package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/fix"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/internal/vfs"
)

// exitError writes a proper JSON error response to stderr and exits.
func exitError(code, message string) {
	resp := cli.ErrorResponse(code, message)
	json.NewEncoder(os.Stderr).Encode(resp)
	os.Exit(2)
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
	var cfg *config.Config

	if cfgErr == nil {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			exitError(cli.ErrInvalidConfig, "cannot read config: "+err.Error())
		}
		cfgDir := filepath.Dir(cfgPath)
		cfg, err = config.Parse(data, cfgDir)
		if err != nil {
			exitError(cli.ErrInvalidConfig, err.Error())
		}
	}

	// Load rules from all packs
	if cfg != nil {
		for _, pack := range cfg.RulePacks {
			rs, _, err := rules.LoadDir(pack.Path)
			if err != nil {
				exitError(cli.ErrInvalidRule, err.Error())
			}
			loadedRules = append(loadedRules, rs...)
		}

		if err := rules.DetectDuplicates(loadedRules); err != nil {
			exitError(cli.ErrDuplicateRule, err.Error())
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
	router.Handle("fix", fixHandler(eng, loadedRules, cfg, cfgErr))

	os.Exit(router.Run(os.Args[1:], os.Stdin))
}

func fixHandler(eng *engine.Engine, loadedRules []rules.Rule, cfg *config.Config, cfgErr error) cli.Handler {
	return func(req *cli.Request) *cli.Response {
		if cfgErr != nil {
			return cli.ErrorResponseWithHint(cli.ErrNoConfig,
				cfgErr.Error(),
				"create meridian.yaml or set MERIDIAN_CONFIG env var")
		}

		var params struct {
			Scope  string `json:"scope"`
			DryRun bool   `json:"dry-run"`
		}
		if req.Params != nil {
			json.Unmarshal(req.Params, &params)
		}

		// Filter rules by scope if set
		targetRules := loadedRules
		if params.Scope != "" {
			// Scope filtering happens at engine level via file paths,
			// so we pass all rules and let the engine match.
		}

		fsys := vfs.NewOSFS(cfg.Scan.Root)
		fixer := fix.New(eng, fix.All)
		report, err := fixer.Fix(fsys, targetRules, fix.Options{DryRun: params.DryRun})
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

func checkHandler(eng *engine.Engine, loadedRules []rules.Rule, cfg *config.Config, cfgErr error) cli.Handler {
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
		findings := eng.Run(fsys, loadedRules)

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

		return &cli.Response{
			Version:  cli.ResponseVersion,
			Findings: findings,
			Stats: &cli.Stats{
				RulesApplied:  len(loadedRules),
				FindingsCount: len(findings),
				DurationMs:    int(dur.Milliseconds()),
			},
			Warnings: cliWarnings,
		}
	}
}
