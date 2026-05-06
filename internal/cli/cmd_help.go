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

func newHelpHandler(commands func() []string) Handler {
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
				Data: map[string]any{
					"command":     p.Command,
					"description": info.Description,
					"params":      info.Params,
					"exit_codes":  info.ExitCodes,
				},
			}
		}

		// Search (stub — return empty for now)
		if p.Search != "" {
			return &Response{
				Version: ResponseVersion,
				Data: map[string]any{
					"results": []any{},
				},
			}
		}

		// List all commands
		cmds := commands()
		cmdList := make([]map[string]string, 0, len(cmds))
		for _, name := range cmds {
			desc := ""
			if info, ok := registry[name]; ok {
				desc = info.Description
			}
			cmdList = append(cmdList, map[string]string{
				"command":     name,
				"description": desc,
			})
		}

		return &Response{
			Version: ResponseVersion,
			Data: map[string]any{
				"commands": cmdList,
			},
		}
	}
}
