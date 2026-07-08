package cli

import "encoding/json"

type helpParams struct {
	Command string `json:"command"`
	Search  string `json:"search"`
}

// commandHelp stores help text for a command.
type commandHelp struct {
	Description string               `json:"description"`
	Params      map[string]paramHelp `json:"params,omitempty"`
	ExitCodes   map[string]string    `json:"exit_codes,omitempty"`
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
	Params      map[string]paramHelp `json:"params,omitempty"`
	ExitCodes   map[string]string    `json:"exit_codes,omitempty"`
}

// SearchFunc is a function that searches rules/checks by query string.
type SearchFunc func(query string) HelpSearchData

func NewHelpHandler(commands func() []string, searchFn SearchFunc) Handler {
	registry := map[string]commandHelp{
		"check": {
			Description: "Scan files, match rules, evaluate, return findings. Positional sugar: `md check <path>` = `md check '{\"scope\":\"<path>\"}'` (path must exist). skill_tree runs the embedded wikilink-integrity pack over a shipped skill directory — config-less, no meridian.yaml needed",
			Params: map[string]paramHelp{
				"scope":      {Type: "string", Required: false},
				"skill_tree": {Type: "string", Required: false},
				"format":     {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "clean", "1": "findings", "2": "error"},
		},
		"rules ls": {
			Description: "List loaded rules",
			Params: map[string]paramHelp{
				"profile": {Type: "string", Required: false},
			},
		},
		"rules check": {
			Description: "Detect rule overlaps and conflicts",
			Params: map[string]paramHelp{
				"profile": {Type: "string", Required: false},
			},
		},
		"debug": {
			Description: "Deep inspection of one rule",
			Params: map[string]paramHelp{
				"rule":  {Type: "string", Required: true},
				"scope": {Type: "string", Required: false},
			},
		},
		"help": {
			Description: "Queryable help",
			Params: map[string]paramHelp{
				"command": {Type: "string", Required: false},
				"search":  {Type: "string", Required: false},
			},
		},
		"version": {
			Description: "Show version information",
		},
		"debt": {
			Description: "List incorporation debt (wiki/sources flagged do/incorporate, not yet incorporated)",
		},
		"llm-wiki check": {
			Description: "Environment doctor for the llm-wiki system: verifies CCC_LLM_WIKI_PATH / CCC_LLM_WIKI_REPOS_ROOT and that every cataloged repo (sources/git/<slug>/) resolves at <root>/<slug> with the right git identity. Failures point at skill-shipped setup references; absent repos are a state, not a failure",
			Params: map[string]paramHelp{
				"setup_dir": {Type: "string", Required: false},
				"format":    {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "environment healthy", "1": "check failures (setup refs named)", "2": "tool failure"},
		},
		"domains tree": {
			Description: "Show domain hierarchy from scanned wiki",
			Params: map[string]paramHelp{
				"scope": {Type: "string", Required: false},
			},
		},
		"domains show": {
			Description: "Detail for one domain prefix",
			Params: map[string]paramHelp{
				"prefix": {Type: "string", Required: true},
				"scope":  {Type: "string", Required: false},
			},
		},
		"fix": {
			Description: "Auto-fix frontmatter violations",
			Params: map[string]paramHelp{
				"scope":   {Type: "string", Required: false},
				"rules":   {Type: "array", Required: false},
				"dry-run": {Type: "bool", Required: false},
			},
			ExitCodes: map[string]string{"0": "clean", "2": "error"},
		},
		"mv": {
			Description: "Move/rename files, update frontmatter domains",
			Params: map[string]paramHelp{
				"source":  {Type: "string", Required: true},
				"dest":    {Type: "string", Required: true},
				"dry-run": {Type: "bool", Required: false},
			},
		},
		"run": {
			Description: "Execute frontmatter-addressed task blocks (md-<name> keys → ^id fences); format json captures task stdout/stderr into the envelope, text streams it live. timeout bounds each task's wall clock (Go duration, e.g. \"30s\") — at the deadline the process group is killed and the task reports exit 124",
			Params: map[string]paramHelp{
				"file":    {Type: "string", Required: true},
				"name":    {Type: "string|array", Required: false},
				"args":    {Type: "array", Required: false},
				"list":    {Type: "bool", Required: false},
				"format":  {Type: "string", Required: false},
				"timeout": {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "all tasks succeeded", "1": "a task exited non-zero (124 = timed out)", "2": "resolution or tool failure"},
		},
		"read": {
			Description: "Read vault-addressed content: path, [[note]], [[note#Heading]], or [[note#^block]]; text mode prints verification metadata (base, matches, warnings) to stderr, stdout stays pure content. With embeds:true, ![[...]] embeds are recursively inlined (frontmatter stripped from whole-note embeds). With strip-frontmatter:true, the matched file's own frontmatter is dropped — returns the deployable body",
			Params: map[string]paramHelp{
				"target":            {Type: "string", Required: true},
				"expect-unique":     {Type: "bool", Required: false},
				"expect-cwd":        {Type: "string", Required: false},
				"embeds":            {Type: "bool", Required: false},
				"strip-frontmatter": {Type: "bool", Required: false},
				"format":            {Type: "string", Required: false},
			},
			ExitCodes: map[string]string{"0": "content resolved", "2": "not found, ambiguous (expect-unique/embed), or wrong cwd"},
		},
		"watch": {
			Description: "Start filesystem watcher daemon",
		},
		"status": {
			Description: "Query running watch daemon status",
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
					Params:      info.Params,
					ExitCodes:   info.ExitCodes,
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
