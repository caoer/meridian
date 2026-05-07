package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/caoer/meridian/internal/domains"
)

func TestDomainsTreeHandler_OutputShape(t *testing.T) {
	reg := domains.NewRegistry()
	reg.Add([]string{"domain/locus", "type/reference"})
	reg.Add([]string{"domain/locus", "type/source"})
	reg.Add([]string{"domain/meridian"})

	handler := DomainsTreeHandler(reg)
	resp := handler(&Request{Command: "domains tree"})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Data == nil {
		t.Fatal("expected data in response")
	}

	raw, _ := json.Marshal(resp.Data)
	var data struct {
		Domains       map[string]json.RawMessage `json:"domains"`
		TotalPrefixes int                        `json:"total_prefixes"`
		TotalPages    int                        `json:"total_pages"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data.TotalPrefixes != 2 {
		t.Errorf("total_prefixes = %d, want 2", data.TotalPrefixes)
	}
	if data.TotalPages != 3 {
		t.Errorf("total_pages = %d, want 3", data.TotalPages)
	}
	if _, ok := data.Domains["domain"]; !ok {
		t.Error("missing 'domain' in domains map")
	}
	if _, ok := data.Domains["type"]; !ok {
		t.Error("missing 'type' in domains map")
	}
}

func TestDomainsTreeHandler_DomainDetail(t *testing.T) {
	reg := domains.NewRegistry()
	reg.Add([]string{"domain/locus"})
	reg.Add([]string{"domain/locus"})
	reg.Add([]string{"domain/meridian"})

	handler := DomainsTreeHandler(reg)
	resp := handler(&Request{Command: "domains tree"})

	raw, _ := json.Marshal(resp.Data)
	var data struct {
		Domains map[string]struct {
			Values []string       `json:"values"`
			Counts map[string]int `json:"counts"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	dom := data.Domains["domain"]
	if len(dom.Values) != 2 {
		t.Fatalf("domain values = %v, want 2 entries", dom.Values)
	}
	if dom.Counts["locus"] != 2 {
		t.Errorf("counts[locus] = %d, want 2", dom.Counts["locus"])
	}
	if dom.Counts["meridian"] != 1 {
		t.Errorf("counts[meridian] = %d, want 1", dom.Counts["meridian"])
	}
}

func TestDomainsShowHandler_ValidPrefix(t *testing.T) {
	reg := domains.NewRegistry()
	reg.Add([]string{"domain/locus"})
	reg.Add([]string{"domain/locus"})
	reg.Add([]string{"domain/meridian"})

	handler := DomainsShowHandler(reg)
	params := `{"prefix": "domain"}`
	resp := handler(&Request{Command: "domains show", Params: json.RawMessage(params)})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	if resp.Data == nil {
		t.Fatal("expected data")
	}

	raw, _ := json.Marshal(resp.Data)
	var data struct {
		Prefix string `json:"prefix"`
		Values []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"values"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.Prefix != "domain" {
		t.Errorf("prefix = %q, want domain", data.Prefix)
	}
	if len(data.Values) != 2 {
		t.Fatalf("values length = %d, want 2", len(data.Values))
	}
}

func TestDomainsShowHandler_MissingPrefix(t *testing.T) {
	reg := domains.NewRegistry()
	handler := DomainsShowHandler(reg)
	resp := handler(&Request{Command: "domains show"})

	if resp.Error == nil {
		t.Fatal("expected error for missing prefix")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %q, want %q", resp.Error.Code, ErrInvalidParams)
	}
}

func TestDomainsShowHandler_UnknownPrefix(t *testing.T) {
	reg := domains.NewRegistry()
	handler := DomainsShowHandler(reg)
	params := `{"prefix": "nonexistent"}`
	resp := handler(&Request{Command: "domains show", Params: json.RawMessage(params)})

	if resp.Error == nil {
		t.Fatal("expected error for unknown prefix")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %q, want %q", resp.Error.Code, ErrInvalidParams)
	}
}

func TestDomainsShowHandler_InvalidJSON(t *testing.T) {
	reg := domains.NewRegistry()
	handler := DomainsShowHandler(reg)
	params := `{invalid`
	resp := handler(&Request{Command: "domains show", Params: json.RawMessage(params)})

	if resp.Error == nil {
		t.Fatal("expected error for invalid JSON params")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %q, want %q", resp.Error.Code, ErrInvalidParams)
	}
}

func TestDomainsTree_EmptyRegistry(t *testing.T) {
	reg := domains.NewRegistry()
	handler := DomainsTreeHandler(reg)
	resp := handler(&Request{Command: "domains tree"})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	raw, _ := json.Marshal(resp.Data)
	var data struct {
		TotalPrefixes int `json:"total_prefixes"`
		TotalPages    int `json:"total_pages"`
	}
	json.Unmarshal(raw, &data)
	if data.TotalPrefixes != 0 {
		t.Errorf("total_prefixes = %d, want 0", data.TotalPrefixes)
	}
}

func TestDomainsTree_TextOutput(t *testing.T) {
	reg := domains.NewRegistry()
	reg.Add([]string{"domain/locus"})
	reg.Add([]string{"domain/locus"})
	reg.Add([]string{"domain/meridian"})

	handler := DomainsTreeHandler(reg)
	resp := handler(&Request{Command: "domains tree"})

	var buf bytes.Buffer
	RenderText(&buf, resp)
	out := buf.String()

	// Should contain prefix and values
	if !bytes.Contains([]byte(out), []byte("domain/")) {
		t.Errorf("text output missing 'domain/': %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("locus")) {
		t.Errorf("text output missing 'locus': %s", out)
	}
}

func TestDomainsTree_Integration(t *testing.T) {
	reg := domains.NewRegistry()
	reg.Add([]string{"domain/locus"})

	router := NewRouter()
	router.Handle("domains tree", DomainsTreeHandler(reg))
	router.SetFormat(FormatJSON)

	var buf bytes.Buffer
	router.SetOutput(&buf)
	code := router.Run([]string{"domains tree"}, nil)

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Version != ResponseVersion {
		t.Errorf("version = %q", resp.Version)
	}
}
