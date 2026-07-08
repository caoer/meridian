package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// helper: create a router with JSON output for tests.
func newTestRouter() (*Router, *bytes.Buffer) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.SetFormat(FormatJSON)
	return r, &buf
}

// helper: decode the JSON written to buf into a Response.
func decodeResponse(t *testing.T, buf *bytes.Buffer) *Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response JSON: %v\nbody: %s", err, buf.String())
	}
	return &resp
}

func TestRouter_NoSubcommand(t *testing.T) {
	r, buf := newTestRouter()

	code := r.Run([]string{}, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrUnknownCommand {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrUnknownCommand)
	}
}

func TestRouter_UnknownCommand(t *testing.T) {
	r, buf := newTestRouter()

	code := r.Run([]string{"bogus"}, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrUnknownCommand {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrUnknownCommand)
	}
	if !strings.Contains(resp.Error.Message, "bogus") {
		t.Fatalf("error message = %q, want it to mention %q", resp.Error.Message, "bogus")
	}
}

func TestRouter_ValidDispatch(t *testing.T) {
	r, buf := newTestRouter()

	called := false
	r.Handle("test", func(req *Request) *Response {
		called = true
		if req.Command != "test" {
			t.Errorf("req.Command = %q, want %q", req.Command, "test")
		}
		return &Response{
			Version: ResponseVersion,
			Data:    json.RawMessage(`{"ok":true}`),
		}
	})

	code := r.Run([]string{"test"}, nil)

	if !called {
		t.Fatal("handler was not called")
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Version != ResponseVersion {
		t.Fatalf("version = %q, want %q", resp.Version, ResponseVersion)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestRouter_MalformedJSONArg(t *testing.T) {
	r, buf := newTestRouter()
	r.Handle("test", func(req *Request) *Response {
		t.Fatal("handler should not be called for malformed input")
		return nil
	})

	code := r.Run([]string{"test", "{not json"}, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrInvalidInput {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrInvalidInput)
	}
}

func TestRouter_JSONFromStdin(t *testing.T) {
	r, _ := newTestRouter()

	input := `{"scope":"wiki/"}`

	r.Handle("test", func(req *Request) *Response {
		if req.Params == nil {
			t.Fatal("expected params from stdin, got nil")
		}
		if string(req.Params) != input {
			t.Fatalf("params = %s, want %s", string(req.Params), input)
		}
		return &Response{Version: ResponseVersion}
	})

	code := r.Run([]string{"test", "-"}, strings.NewReader(input))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_InputTooLarge(t *testing.T) {
	r, buf := newTestRouter()
	r.Handle("test", func(req *Request) *Response {
		t.Fatal("handler should not be called for oversized input")
		return nil
	})

	// Create data that exceeds MaxInputSize.
	oversized := strings.NewReader(strings.Repeat("x", MaxInputSize+1))
	code := r.Run([]string{"test", "-"}, oversized)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrInputTooLarge {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrInputTooLarge)
	}
}

func TestRouter_EmptyStdin(t *testing.T) {
	r, _ := newTestRouter()

	r.Handle("test", func(req *Request) *Response {
		if req.Params != nil {
			t.Fatalf("expected nil params for empty stdin, got %s", string(req.Params))
		}
		return &Response{Version: ResponseVersion}
	})

	code := r.Run([]string{"test", "-"}, strings.NewReader(""))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_MalformedJSONStdin(t *testing.T) {
	r, buf := newTestRouter()
	r.Handle("test", func(req *Request) *Response {
		t.Fatal("handler should not be called for malformed stdin")
		return nil
	})

	code := r.Run([]string{"test", "-"}, strings.NewReader("{bad json"))

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrInvalidInput {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrInvalidInput)
	}
}

func TestRouter_TwoWordCommand(t *testing.T) {
	r, _ := newTestRouter()

	called := false
	r.Handle("rules ls", func(req *Request) *Response {
		called = true
		if req.Command != "rules ls" {
			t.Errorf("req.Command = %q, want %q", req.Command, "rules ls")
		}
		return &Response{Version: ResponseVersion}
	})

	// Two separate args, as real CLI would split them
	code := r.Run([]string{"rules", "ls"}, nil)

	if !called {
		t.Fatal("handler was not called for two-word command")
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_TwoWordCommand_WithParams(t *testing.T) {
	r, _ := newTestRouter()

	paramJSON := `{"profile":"strict"}`

	r.Handle("rules ls", func(req *Request) *Response {
		if req.Command != "rules ls" {
			t.Errorf("req.Command = %q, want %q", req.Command, "rules ls")
		}
		if string(req.Params) != paramJSON {
			t.Errorf("params = %s, want %s", string(req.Params), paramJSON)
		}
		return &Response{Version: ResponseVersion}
	})

	code := r.Run([]string{"rules", "ls", paramJSON}, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_Commands(t *testing.T) {
	r := NewRouter()
	r.Handle("check", func(req *Request) *Response { return nil })
	r.Handle("version", func(req *Request) *Response { return nil })
	r.Handle("help", func(req *Request) *Response { return nil })

	cmds := r.Commands()
	want := []string{"check", "help", "version"}
	if len(cmds) != len(want) {
		t.Fatalf("Commands() = %v, want %v", cmds, want)
	}
	for i, c := range cmds {
		if c != want[i] {
			t.Fatalf("Commands()[%d] = %q, want %q", i, c, want[i])
		}
	}
}

func TestRouter_ArgParamsTakesPrecedenceOverStdin(t *testing.T) {
	r, _ := newTestRouter()

	argJSON := `{"from":"arg"}`
	stdinJSON := `{"from":"stdin"}`

	r.Handle("test", func(req *Request) *Response {
		if string(req.Params) != argJSON {
			t.Fatalf("params = %s, want %s (arg should take precedence)", string(req.Params), argJSON)
		}
		return &Response{Version: ResponseVersion}
	})

	code := r.Run([]string{"test", argJSON}, strings.NewReader(stdinJSON))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_WhitespaceOnlyStdin(t *testing.T) {
	r, _ := newTestRouter()

	r.Handle("test", func(req *Request) *Response {
		if req.Params != nil {
			t.Fatalf("expected nil params for whitespace-only stdin, got %s", string(req.Params))
		}
		return &Response{Version: ResponseVersion}
	})

	code := r.Run([]string{"test", "-"}, strings.NewReader("   \n\t  "))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_NoArgs_DispatchesHelp(t *testing.T) {
	r, buf := newTestRouter()

	called := false
	r.Handle("help", func(req *Request) *Response {
		called = true
		if req.Command != "help" {
			t.Errorf("req.Command = %q, want %q", req.Command, "help")
		}
		return &Response{Version: ResponseVersion, Data: json.RawMessage(`{"commands":[]}`)}
	})

	code := r.Run([]string{}, nil)

	if !called {
		t.Fatal("help handler was not called for empty args")
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	_ = decodeResponse(t, buf)
}

func TestRouter_HelpFlag(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			r, buf := newTestRouter()

			called := false
			r.Handle("help", func(req *Request) *Response {
				called = true
				return &Response{Version: ResponseVersion, Data: json.RawMessage(`{"commands":[]}`)}
			})

			code := r.Run([]string{flag}, nil)

			if !called {
				t.Fatalf("help handler was not called for %s", flag)
			}
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			_ = decodeResponse(t, buf)
		})
	}
}

func TestRouter_NoArgs_NoHelpHandler(t *testing.T) {
	r, buf := newTestRouter()

	code := r.Run([]string{}, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrUnknownCommand {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrUnknownCommand)
	}
}

func TestRouter_HandlerVersionDefault(t *testing.T) {
	r, buf := newTestRouter()

	// Handler returns response with empty Version — router should fill it.
	r.Handle("test", func(req *Request) *Response {
		return &Response{}
	})

	code := r.Run([]string{"test"}, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Version != ResponseVersion {
		t.Fatalf("version = %q, want %q (should be filled by router)", resp.Version, ResponseVersion)
	}
}

func TestRouter_PositionalAdapter(t *testing.T) {
	var got json.RawMessage
	r := NewRouter()
	r.SetOutput(io.Discard)
	r.Handle("check", func(req *Request) *Response {
		got = req.Params
		return &Response{Version: ResponseVersion}
	})
	r.HandlePositional("check", func(arg string) (json.RawMessage, error) {
		if arg == "bad" {
			return nil, fmt.Errorf("no such path: %s", arg)
		}
		return json.RawMessage(`{"scope":"` + arg + `"}`), nil
	})

	// bare arg adapted to params
	if code := r.Run([]string{"check", "some/path.md"}, nil); code != 0 {
		t.Fatalf("adapted positional: exit %d, want 0", code)
	}
	if string(got) != `{"scope":"some/path.md"}` {
		t.Errorf("params = %s", got)
	}
	// adapter error stays loud: exit 2
	if code := r.Run([]string{"check", "bad"}, nil); code != 2 {
		t.Errorf("adapter error: exit %d, want 2", code)
	}
	// JSON arg still takes the normal path
	if code := r.Run([]string{"check", `{"scope":"x"}`}, nil); code != 0 {
		t.Errorf("json arg: exit %d, want 0", code)
	}
	if string(got) != `{"scope":"x"}` {
		t.Errorf("json params = %s", got)
	}
	// commands WITHOUT an adapter keep failing loud on non-JSON
	r2 := NewRouter()
	r2.SetOutput(io.Discard)
	r2.Handle("fix", func(req *Request) *Response { return &Response{Version: ResponseVersion} })
	if code := r2.Run([]string{"fix", "some/path.md"}, nil); code != 2 {
		t.Errorf("no adapter: exit %d, want 2", code)
	}
}

func TestRouter_PipedStdinWithoutDashFailsLoud(t *testing.T) {
	r, buf := newTestRouter()
	r.Handle("test", func(req *Request) *Response {
		t.Fatal("handler must not run with silently-empty params when stdin is piped without \"-\"")
		return nil
	})

	code := r.Run([]string{"test"}, strings.NewReader(`{"scope":"wiki/"}`))

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Error == nil || resp.Error.Code != ErrInvalidInput {
		t.Fatalf("want %s error, got %+v", ErrInvalidInput, resp.Error)
	}
	if !strings.Contains(resp.Error.Message, `"-"`) {
		t.Fatalf("error must carry the migration hint, got %q", resp.Error.Message)
	}
}

func TestRouter_DashWithoutStdinFails(t *testing.T) {
	r, buf := newTestRouter()
	r.Handle("test", func(req *Request) *Response {
		t.Fatal("handler must not run when \"-\" is given without piped stdin")
		return nil
	})

	if code := r.Run([]string{"test", "-"}, nil); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, buf)
	if resp.Error == nil || resp.Error.Code != ErrInvalidInput {
		t.Fatalf("want %s error, got %+v", ErrInvalidInput, resp.Error)
	}
}

// TestRouter_OpenPipeNeverBlocks encodes the hang class itself: an inherited
// open pipe whose write end never closes (hooks, daemons, tmux spawns) must
// not block dispatch — no-arg commands fail loud immediately, arg commands
// run normally; neither ever reads the pipe.
func TestRouter_OpenPipeNeverBlocks(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close() // write end stays open for the test's duration

	run := func(name string, args []string, wantCode int) {
		t.Helper()
		done := make(chan int, 1)
		go func() {
			r, _ := newTestRouter()
			r.Handle("test", func(req *Request) *Response {
				return &Response{Version: ResponseVersion}
			})
			done <- r.Run(args, pr)
		}()
		select {
		case code := <-done:
			if code != wantCode {
				t.Errorf("%s: exit code = %d, want %d", name, code, wantCode)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: blocked on open pipe — the hang class is back", name)
		}
	}

	run("no-arg command", []string{"test"}, 2)                   // fails loud, promptly
	run("arg command", []string{"test", `{"scope":"wiki/"}`}, 0) // stdin never touched
}
