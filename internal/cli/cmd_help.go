package cli

import "encoding/json"

type helpParams struct {
	Command string `json:"command"`
	Search  string `json:"search"`
}

// commandHelp stores help text for a command.
type commandHelp struct {
	Description string              `json:"description"`
	Params      map[string]paramHelp `json:"params,omitempty"`
	ExitCodes   map[string]string   `json:"exit_codes,omitempty"`
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
	Command     string              `json:"command"`
	Description string              `json:"description"`
	Params      map[string]paramHelp `json:"params,omitempty"`
	ExitCodes   map[string]string   `json:"exit_codes,omitempty"`
}

// SearchFunc is a function that searches rules/checks by query string.
type SearchFunc func(query string) HelpSearchData

func NewHelpHandler(commands func() []string, searchFn SearchFunc) Handler {
	registry := map[string]commandHelp{
		"check": {
			Description: "Scan files, match rules, evaluate, return findings",
			Params: map[string]paramHelp{
				"scope":   {Type: "string", Required: false},
				"profile": {Type: "string", Required: false},
				"rules":   {Type: "array", Required: false},
				"config":  {Type: "string", Required: false},
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
				"scope":  {Type: "string", Required: false},
				"rules":  {Type: "array", Required: false},
				"dry-run": {Type: "bool", Required: false},
			},
			ExitCodes: map[string]string{"0": "clean", "2": "error"},
		},
		"mv": {
			Description: "Move/rename files, update frontmatter domains",
			Params: map[string]paramHelp{
				"source": {Type: "string", Required: true},
				"dest":   {Type: "string", Required: true},
				"dry-run": {Type: "bool", Required: false},
			},
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
			Data: HelpListData{Commands: cmdList},
		}
	}
}
