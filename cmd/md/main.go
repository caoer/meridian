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
	"github.com/caoer/meridian/internal/domains"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/fix"
	"github.com/caoer/meridian/internal/mv"
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
	var allRules []rules.Rule
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
				exitError("INVALID_RULE", err.Error())
			}
			loadedRules = append(loadedRules, rs...)
		}

		if err := rules.DetectDuplicates(loadedRules); err != nil {
			exitError("DUPLICATE_RULE", err.Error())
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
	router.Handle("rules ls", cli.RulesLsHandler(loadedRules))
	router.Handle("rules check", cli.RulesCheckHandler(allRules, profiles))
	router.Handle("debug", cli.DebugHandler(loadedRules, registeredChecks))
	router.Handle("check", checkHandler(eng, loadedRules, cfg, cfgErr))
	router.Handle("domains tree", domainsTreeHandler(cfg, cfgErr))
	router.Handle("domains show", domainsShowHandler(cfg, cfgErr))
	router.Handle("fix", fixHandler(eng, loadedRules, cfg, cfgErr))
	router.Handle("mv", mvHandler(eng, loadedRules, cfg, cfgErr))

	os.Exit(router.Run(os.Args[1:], os.Stdin))
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
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return cli.ErrorResponse(cli.ErrInvalidParams, "invalid params: "+err.Error())
			}
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
