package cli

import "encoding/json"

type helpParams struct {
	Command string `json:"command"`
	Search  string `json:"search"`
}

// commandHelp stores help text for a command.
type commandHelp struct {
	Description string               `json:"description"`
	Usage       string               `json:"usage,omitempty"`
	Params      map[string]paramHelp `json:"params,omitempty"`
	ExitCodes   map[string]string    `json:"exit_codes,omitempty"`
	Examples    []string             `json:"examples,omitempty"`
}

type paramHelp struct {
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// HelpListData is the data payload for help (list all commands).
type HelpListData struct {
	Commands []HelpListEntry `json:"commands"`
}

// HelpListEntry is one command in the help list.
type HelpListEntry struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// HelpCommandData is the data payload for help (single command info).
type HelpCommandData struct {
	Command     string               `json:"command"`
	Description string               `json:"description"`
	Usage       string               `json:"usage,omitempty"`
	Params      map[string]paramHelp `json:"params,omitempty"`
	ExitCodes   map[string]string    `json:"exit_codes,omitempty"`
	Examples    []string             `json:"examples,omitempty"`
}

// SearchFunc is a function that searches rules/checks by query string.
type SearchFunc func(query string) HelpSearchData

func NewHelpHandler(commands func() []string, searchFn SearchFunc) Handler {
	registry := map[string]commandHelp{
		"check": {
			Description: "Scan files, match rules, evaluate, return findings. Positional sugar: `md check <path>` = `md check '{\"scope\":\"<path>\"}'` (path must exist). skill_tree runs the embedded wikilink-integrity pack over a shipped skill directory — config-less, no meridian.yaml needed. strict promotes warn findings to exit 1 (default from meridian.yaml `strict:`; per-run override here is the escape hatch — error findings fail in both modes)",
			Usage:       "md check <path>  |  md check '{\"scope\":\"<path>\", ...}'",
			Params: map[string]paramHelp{
				"scope":      {Type: "string", Required: false},
				"skill_tree": {Type: "string", Required: false},
				"strict":     {Type: "bool", Required: false},
				"no_cache":   {Type: "bool", Required: false},
				"format":     {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "clean", "1": "error findings (warn too when strict)", "2": "error (or MD_CACHE_VERIFY=1 divergence)"},
			Examples: []string{
				`md check wiki/`,
				`md check '{"scope":"wiki/","strict":true}'`,
				`md check '{"skill_tree":"skills/my-skill"}'`,
			},
		},
		"rules ls": {
			Description: "List loaded rules",
			Usage:       "md rules ls  |  md rules ls '{\"profile\":\"<name>\"}'",
			Params: map[string]paramHelp{
				"profile": {Type: "string", Required: false},
			},
		},
		"rules check": {
			Description: "Detect rule overlaps and conflicts",
			Usage:       "md rules check  |  md rules check '{\"profile\":\"<name>\"}'",
			Params: map[string]paramHelp{
				"profile": {Type: "string", Required: false},
			},
		},
		"debug": {
			Description: "Deep inspection of one rule",
			Usage:       "md debug '{\"rule\":\"<rule-id>\"}'",
			Params: map[string]paramHelp{
				"rule":  {Type: "string", Required: true},
				"scope": {Type: "string", Required: false},
			},
			Examples: []string{
				`md debug '{"rule":"broken-wikilink"}'`,
				`md debug '{"rule":"broken-wikilink","scope":"wiki/"}'`,
			},
		},
		"help": {
			Description: "Queryable help: list commands, detail one command, or search rules/checks",
			Usage:       "md help  |  md help '{\"command\":\"<cmd>\"}'  |  md help '{\"search\":\"<query>\"}'",
			Params: map[string]paramHelp{
				"command": {Type: "string", Required: false},
				"search":  {Type: "string", Required: false},
			},
			Examples: []string{
				`md help '{"command":"check"}'`,
				`md help '{"search":"wikilink"}'`,
			},
		},
		"version": {
			Description: "Show version information",
			Usage:       "md version",
		},
		"debt": {
			Description: "List incorporation debt (wiki/sources flagged do/incorporate, not yet incorporated)",
			Usage:       "md debt",
		},
		"cache stats": {
			Description: "Inventory this scan root's persistent fact cache: resolved path, accumulated version dirs, shard-file count, cached-document count, and total bytes. Read-only",
			Usage:       "md cache stats",
		},
		"cache clean": {
			Description: "Remove this scan root's entire persistent fact cache — every accumulated version dir (Decision 10: clean removes all versions; there is no auto-prune). Reports entries and bytes removed. Refuses unless the resolved path is a single roothash segment under UserCacheDir/meridian",
			Usage:       "md cache clean",
			ExitCodes:   map[string]string{"0": "removed (or nothing to remove)", "2": "refused unsafe path or removal error"},
		},
		"llm-wiki check": {
			Description: "Environment doctor for the llm-wiki system: verifies CCC_LLM_WIKI_PATH / CCC_LLM_WIKI_REPOS_ROOT and that every cataloged repo (sources/git/<slug>/) resolves at <root>/<slug> with the right git identity. Failures point at skill-shipped setup references; absent repos are a state, not a failure",
			Usage:       "md llm-wiki check  |  md llm-wiki check '{\"setup_dir\":\"<dir>\"}'",
			Params: map[string]paramHelp{
				"setup_dir": {Type: "string", Required: false},
				"format":    {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "environment healthy", "1": "check failures (setup refs named)", "2": "tool failure"},
		},
		"domains tree": {
			Description: "Show domain hierarchy from scanned wiki",
			Usage:       "md domains tree  |  md domains tree '{\"scope\":\"<path>\"}'",
			Params: map[string]paramHelp{
				"scope": {Type: "string", Required: false},
			},
		},
		"domains show": {
			Description: "Detail for one domain prefix",
			Usage:       "md domains show '{\"prefix\":\"<domain-prefix>\"}'",
			Params: map[string]paramHelp{
				"prefix": {Type: "string", Required: true},
				"scope":  {Type: "string", Required: false},
			},
			Examples: []string{
				`md domains show '{"prefix":"lang"}'`,
			},
		},
		"fix": {
			Description: "Auto-fix frontmatter violations",
			Usage:       "md fix '{\"scope\":\"<path>\",\"rules\":[\"<rule-id>\"],\"dry-run\":true}' (all optional)",
			Params: map[string]paramHelp{
				"scope":   {Type: "string", Required: false},
				"rules":   {Type: "array", Required: false},
				"dry-run": {Type: "bool", Required: false},
			},
			ExitCodes: map[string]string{"0": "clean", "2": "error"},
			Examples: []string{
				`md fix '{"dry-run":true}'`,
				`md fix '{"scope":"wiki/","rules":["created"]}'`,
			},
		},
		"mv": {
			Description: "Move/rename files, update frontmatter domains",
			Usage:       "md mv '{\"source\":\"<from.md>\",\"dest\":\"<to.md>\"}'",
			Params: map[string]paramHelp{
				"source":  {Type: "string", Required: true},
				"dest":    {Type: "string", Required: true},
				"dry-run": {Type: "bool", Required: false},
			},
			Examples: []string{
				`md mv '{"source":"wiki/old.md","dest":"wiki/new.md","dry-run":true}'`,
			},
		},
		"run": {
			Description: "Execute frontmatter-addressed task blocks (md-<name> keys → ^id fences); format json captures task stdout/stderr into the envelope, text streams it live. timeout bounds each task's wall clock (Go duration, e.g. \"30s\") — at the deadline the process group is killed and the task reports exit 124. record:true writes each task's outcome + output to a sidecar run record (<stem>.runs.md, block-addressable per task as ^<task>) without ever mutating the source doc; response carries record_path. inherit:true resolves any requested task the file does not itself declare by walking its ancestor blurb pages (the page's own directory blurb <DIR>/<UPPER>.md first, up to LLM_WIKI.md at the git toplevel) — nearest ancestor wins, a leaf-defined task replaces the inherited one wholesale — and projects MD_PARAM_PAGE = the file's repo-relative path into every task's env so a shared ^check/^apply block can locate the page it runs against. Any OTHER top-level key is a named task param: it must be declared by a requested task's md-<task>-params contract (undeclared keys fail loud regardless of value) and reaches the task as MD_PARAM_<UPPER> in its env — string/number verbatim, true → \"1\", false → omitted (opt-in: false and absent are the same env state). A param value of null or empty string fails loud (a param with no value is a mistake, not the default — omit the key to use the default)",
			Usage:       "md run '{\"file\":\"<doc.md>\",\"name\":\"<task>\"}'  |  md run '{\"file\":\"<doc.md>\",\"list\":true}'",
			Params: map[string]paramHelp{
				"file":       {Type: "string", Required: true},
				"name":       {Type: "string|array", Required: false},
				"args":       {Type: "array", Required: false},
				"list":       {Type: "bool", Required: false},
				"format":     {Type: "string", Required: false},
				"timeout":    {Type: "string", Required: false},
				"record":     {Type: "bool", Required: false},
				"inherit":    {Type: "bool", Required: false},
				"<declared>": {Type: "string|number|bool", Required: false},
			},
			ExitCodes: map[string]string{"0": "all tasks succeeded", "1": "a task exited non-zero (124 = timed out)", "2": "resolution or tool failure"},
			Examples: []string{
				`md run '{"file":"tasks.md","list":true}'`,
				`md run '{"file":"tasks.md","name":"build","timeout":"30s"}'`,
				`md run '{"file":"tasks.md","name":["lint","test"],"record":true}'`,
				`md run '{"file":"health/asset-normalization.md","name":"sweep","include_untracked":true}'`,
			},
		},
		"encode": {
			Description: "The ONE cross-wiki reference encoder (C24 canon): (slug, path[, fragment]) → canonical obsidian://open (no fragment) or advanced-uri (heading/^block) navigation URI; form nav = [display](uri) with caller display, form citation = [wiki://slug/path[@commit]](uri). parse extracts the triple from either grammar (strict decode: non-canonical encoding is flagged, never normalized). Config-less, pure grammar",
			Usage:       "md encode '{\"slug\":\"<wiki-slug>\",\"path\":\"<note-path>\"}'  |  md encode '{\"parse\":\"<encoded-ref>\"}'",
			Params: map[string]paramHelp{
				"slug":     {Type: "string", Required: false},
				"path":     {Type: "string", Required: false},
				"fragment": {Type: "string", Required: false},
				"commit":   {Type: "string", Required: false},
				"display":  {Type: "string", Required: false},
				"form":     {Type: "string", Required: false},
				"parse":    {Type: "string", Required: false},
				"format":   {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "encoded/parsed", "2": "invalid params or unrecognized grammar"},
			Examples: []string{
				`md encode '{"slug":"locus","path":"wiki/meridian/development.md"}'`,
				`md encode '{"slug":"locus","path":"wiki/meridian/development.md","fragment":"Testing","form":"citation"}'`,
			},
		},
		"pipe": {
			Description: "Unavailable: every invocation is refused with the named error \"pipe: unavailable\"",
			Usage:       "md pipe",
			ExitCodes: map[string]string{
				"2": "pipe: unavailable",
			},
		},
		"read": {
			Description: "Read vault-addressed content: path, [[note]], [[note#Heading]], or [[note#^block]]; text mode prints verification metadata (base, matches, warnings) to stderr, stdout stays pure content. With embeds:true, ![[...]] embeds are recursively inlined (frontmatter stripped from whole-note embeds). With strip-frontmatter:true, the matched file's own frontmatter is dropped — returns the deployable body",
			Usage:       "md read '{\"target\":\"<path | [[note]] | [[note#Heading]] | [[note#^block]]>\"}'",
			Params: map[string]paramHelp{
				"target":            {Type: "string", Required: true},
				"expect-unique":     {Type: "bool", Required: false},
				"expect-cwd":        {Type: "string", Required: false},
				"embeds":            {Type: "bool", Required: false},
				"strip-frontmatter": {Type: "bool", Required: false},
				"format":            {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "content resolved", "2": "not found, ambiguous (expect-unique/embed), or wrong cwd"},
			Examples: []string{
				`md read '{"target":"[[development#Testing]]"}'`,
				`md read '{"target":"wiki/meridian/development.md","strip-frontmatter":true}'`,
			},
		},
		"skill render": {
			Description: "Render a skill folder the way a harness does at load time: read <skill>/SKILL.md, strip frontmatter, execute both embedded-command styles in document order — fenced ```! blocks (whole fence replaced by merged stdout+stderr; a non-zero exit still inlines the output, the block's own failure line is the signal) and inline !`cmd` pre-resolution directives (stdout replaces on exit 0, stderr discarded; on failure the literal directive remains so fallback prose engages). Doc examples written as `` !`cmd` `` never execute. Env: caller's cwd, skill bin/ prepended to PATH, CCC_SKILL_DIR defaulted to the skill's parent dir (env wins). timeout bounds each directive (process group killed, exit 124). Text mode: rendered body on stdout, receipts on stderr; directive failures are warnings, never exit 1 — render is the load path",
			Usage:       "md skill render <skill-dir>  |  md skill render '{\"skill\":\"<skill-dir>\"}'",
			Params: map[string]paramHelp{
				"skill":   {Type: "string", Required: true},
				"timeout": {Type: "string", Required: false},
				"format":  {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "rendered (directive failures are warnings)", "2": "no SKILL.md, invalid params, or spawn failure"},
			Examples: []string{
				`md skill render skills/llm-wiki`,
				`md skill render '{"skill":"skills/llm-wiki","timeout":"30s"}'`,
			},
		},
		"watch": {
			Description: "Start filesystem watcher daemon",
			Usage:       "md watch",
		},
		"status": {
			Description: "Query running watch daemon status",
			Usage:       "md status",
		},
		"schema": {
			Description: "Print the effective frontmatter schema: contract defaults merged with the nearest SCHEMA.md overlay (searched from cwd up to git toplevel, or scan.root when config is loaded)",
			Usage:       "md schema",
			Params: map[string]paramHelp{
				"format": {Type: "string", Required: false},
			},
		},
	}

	return func(req *Request) *Response {
		var p helpParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return ErrorResponse(ErrInvalidParams, "invalid help params: "+err.Error())
			}
		}

		// Specific command help
		if p.Command != "" {
			info, ok := registry[p.Command]
			if !ok {
				return ErrorResponse(ErrInvalidParams, "unknown command: "+p.Command)
			}
			return &Response{
				Version: ResponseVersion,
				Data: HelpCommandData{
					Command:     p.Command,
					Description: info.Description,
					Usage:       info.Usage,
					Params:      info.Params,
					ExitCodes:   info.ExitCodes,
					Examples:    info.Examples,
				},
			}
		}

		// Search — delegate to search function
		if p.Search != "" {
			if searchFn != nil {
				return &Response{
					Version: ResponseVersion,
					Data:    searchFn(p.Search),
				}
			}
			return &Response{
				Version: ResponseVersion,
				Data:    HelpSearchData{Results: []SearchResult{}},
			}
		}

		// List all commands
		cmds := commands()
		cmdList := make([]HelpListEntry, 0, len(cmds))
		for _, name := range cmds {
			desc := ""
			if info, ok := registry[name]; ok {
				desc = info.Description
			}
			cmdList = append(cmdList, HelpListEntry{
				Command:     name,
				Description: desc,
			})
		}

		return &Response{
			Version: ResponseVersion,
			Data:    HelpListData{Commands: cmdList},
		}
	}
}
