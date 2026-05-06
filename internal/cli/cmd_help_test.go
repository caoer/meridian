package cli

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"
)

func TestVersion_Output(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("version", newVersionHandler())

	exit := r.Run([]string{"version"}, nil)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}

	resp := decodeResponse(t, &buf)
	if resp.Version != ResponseVersion {
		t.Fatalf("version = %q, want %q", resp.Version, ResponseVersion)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data type = %T, want map[string]any", resp.Data)
	}

	for _, key := range []string{"meridian", "go", "platform"} {
		v, exists := data[key]
		if !exists {
			t.Errorf("missing key %q in Data", key)
			continue
		}
		s, ok := v.(string)
		if !ok || s == "" {
			t.Errorf("Data[%q] = %v, want non-empty string", key, v)
		}
	}

	if data["meridian"] != "0.1.0" {
		t.Errorf("meridian version = %q, want %q", data["meridian"], "0.1.0")
	}
	if data["go"] != runtime.Version() {
		t.Errorf("go version = %q, want %q", data["go"], runtime.Version())
	}
	wantPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if data["platform"] != wantPlatform {
		t.Errorf("platform = %q, want %q", data["platform"], wantPlatform)
	}
}

func TestHelp_CommandInfo(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("help", newHelpHandler(r.Commands))
	r.Handle("check", func(req *Request) *Response { return &Response{Version: ResponseVersion} })

	exit := r.Run([]string{"help", `{"command":"check"}`}, nil)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}

	resp := decodeResponse(t, &buf)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data type = %T, want map[string]any", resp.Data)
	}

	if data["command"] != "check" {
		t.Errorf("command = %v, want %q", data["command"], "check")
	}
	if data["description"] == nil || data["description"] == "" {
		t.Error("description should not be empty")
	}

	params, ok := data["params"].(map[string]any)
	if !ok {
		t.Fatalf("params type = %T, want map[string]any", data["params"])
	}
	for _, key := range []string{"scope", "profile", "rules", "config"} {
		if _, exists := params[key]; !exists {
			t.Errorf("missing param %q", key)
		}
	}

	exitCodes, ok := data["exit_codes"].(map[string]any)
	if !ok {
		t.Fatalf("exit_codes type = %T, want map[string]any", data["exit_codes"])
	}
	if exitCodes["0"] != "clean" {
		t.Errorf("exit_codes[0] = %v, want %q", exitCodes["0"], "clean")
	}
}

func TestHelp_UnknownCommand(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("help", newHelpHandler(r.Commands))

	exit := r.Run([]string{"help", `{"command":"nonexistent"}`}, nil)
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}

	resp := decodeResponse(t, &buf)
	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %q, want %q", resp.Error.Code, ErrInvalidParams)
	}
}

func TestHelp_ListCommands(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("help", newHelpHandler(r.Commands))
	r.Handle("check", func(req *Request) *Response { return &Response{Version: ResponseVersion} })
	r.Handle("version", newVersionHandler())

	exit := r.Run([]string{"help"}, nil)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}

	resp := decodeResponse(t, &buf)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data type = %T, want map[string]any", resp.Data)
	}

	cmdsRaw, ok := data["commands"]
	if !ok {
		t.Fatal("missing 'commands' key in Data")
	}

	// Re-marshal and unmarshal to get typed slice.
	raw, _ := json.Marshal(cmdsRaw)
	var cmds []map[string]string
	if err := json.Unmarshal(raw, &cmds); err != nil {
		t.Fatalf("failed to decode commands list: %v", err)
	}

	if len(cmds) != 3 {
		t.Fatalf("len(commands) = %d, want 3", len(cmds))
	}

	// Commands should be sorted (router.Commands sorts).
	wantOrder := []string{"check", "help", "version"}
	for i, c := range cmds {
		if c["command"] != wantOrder[i] {
			t.Errorf("commands[%d] = %q, want %q", i, c["command"], wantOrder[i])
		}
		if c["description"] == "" {
			t.Errorf("commands[%d] description is empty", i)
		}
	}
}

func TestHelp_SearchStub(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("help", newHelpHandler(r.Commands))

	exit := r.Run([]string{"help", `{"search":"wiki"}`}, nil)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}

	resp := decodeResponse(t, &buf)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data type = %T, want map[string]any", resp.Data)
	}

	results, ok := data["results"]
	if !ok {
		t.Fatal("missing 'results' key in Data")
	}

	raw, _ := json.Marshal(results)
	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("results is not an array: %v", err)
	}
	if len(arr) != 0 {
		t.Errorf("results len = %d, want 0 (stub)", len(arr))
	}
}

func TestHelp_InvalidParams(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("help", newHelpHandler(r.Commands))

	exit := r.Run([]string{"help", `{"command": 123}`}, nil)
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}

	resp := decodeResponse(t, &buf)
	if resp.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %q, want %q", resp.Error.Code, ErrInvalidParams)
	}
}
