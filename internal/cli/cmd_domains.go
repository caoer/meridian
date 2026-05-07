package cli

import (
	"encoding/json"
	"sort"

	"github.com/caoer/meridian/internal/domains"
)

// DomainsTreeData is the data payload for `domains tree`.
type DomainsTreeData struct {
	Domains       map[string]DomainSummary `json:"domains"`
	TotalPrefixes int                      `json:"total_prefixes"`
	TotalPages    int                      `json:"total_pages"`
}

// DomainSummary is the per-prefix summary in tree output.
type DomainSummary struct {
	Values []string       `json:"values"`
	Counts map[string]int `json:"counts"`
}

// DomainsShowData is the data payload for `domains show`.
type DomainsShowData struct {
	Prefix string             `json:"prefix"`
	Values []DomainValueEntry `json:"values"`
}

// DomainValueEntry is one value within a domain prefix.
type DomainValueEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DomainsTreeHandler creates a handler for `md domains tree`.
func DomainsTreeHandler(reg *domains.Registry) Handler {
	return func(req *Request) *Response {
		tree := reg.Tree()
		out := make(map[string]DomainSummary, len(tree))

		for prefix, dom := range tree {
			out[prefix] = DomainSummary{
				Values: dom.Values,
				Counts: dom.Count,
			}
		}

		return &Response{
			Version: ResponseVersion,
			Data: DomainsTreeData{
				Domains:       out,
				TotalPrefixes: len(tree),
				TotalPages:    reg.PageCount(),
			},
		}
	}
}

// DomainsShowHandler creates a handler for `md domains show`.
func DomainsShowHandler(reg *domains.Registry) Handler {
	return func(req *Request) *Response {
		var params struct {
			Prefix string `json:"prefix"`
		}
		if req.Params != nil {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return ErrorResponse(ErrInvalidParams, "invalid params: "+err.Error())
			}
		}
		if params.Prefix == "" {
			return ErrorResponse(ErrInvalidParams, "domains show requires 'prefix' parameter")
		}

		dom := reg.Get(params.Prefix)
		if dom == nil {
			return ErrorResponse(ErrInvalidParams, "unknown prefix: "+params.Prefix)
		}

		values := make([]DomainValueEntry, 0, len(dom.Values))
		for _, v := range dom.Values {
			values = append(values, DomainValueEntry{
				Name:  v,
				Count: dom.Count[v],
			})
		}

		// Sort by count descending, then name ascending
		sort.Slice(values, func(i, j int) bool {
			if values[i].Count != values[j].Count {
				return values[i].Count > values[j].Count
			}
			return values[i].Name < values[j].Name
		})

		return &Response{
			Version: ResponseVersion,
			Data: DomainsShowData{
				Prefix: params.Prefix,
				Values: values,
			},
		}
	}
}
