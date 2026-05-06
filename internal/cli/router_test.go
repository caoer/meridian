package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

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
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)

	code := r.Run([]string{}, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, &buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrUnknownCommand {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrUnknownCommand)
	}
}

func TestRouter_UnknownCommand(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)

	code := r.Run([]string{"bogus"}, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, &buf)
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
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)

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
	resp := decodeResponse(t, &buf)
	if resp.Version != ResponseVersion {
		t.Fatalf("version = %q, want %q", resp.Version, ResponseVersion)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestRouter_MalformedJSONArg(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("test", func(req *Request) *Response {
		t.Fatal("handler should not be called for malformed input")
		return nil
	})

	code := r.Run([]string{"test", "{not json"}, nil)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, &buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrInvalidInput {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrInvalidInput)
	}
}

func TestRouter_JSONFromStdin(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)

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

	code := r.Run([]string{"test"}, strings.NewReader(input))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_InputTooLarge(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("test", func(req *Request) *Response {
		t.Fatal("handler should not be called for oversized input")
		return nil
	})

	// Create data that exceeds MaxInputSize.
	oversized := strings.NewReader(strings.Repeat("x", MaxInputSize+1))
	code := r.Run([]string{"test"}, oversized)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, &buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrInputTooLarge {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrInputTooLarge)
	}
}

func TestRouter_EmptyStdin(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)

	r.Handle("test", func(req *Request) *Response {
		if req.Params != nil {
			t.Fatalf("expected nil params for empty stdin, got %s", string(req.Params))
		}
		return &Response{Version: ResponseVersion}
	})

	code := r.Run([]string{"test"}, strings.NewReader(""))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_MalformedJSONStdin(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)
	r.Handle("test", func(req *Request) *Response {
		t.Fatal("handler should not be called for malformed stdin")
		return nil
	})

	code := r.Run([]string{"test"}, strings.NewReader("{bad json"))

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	resp := decodeResponse(t, &buf)
	if resp.Error == nil {
		t.Fatal("expected error in response, got nil")
	}
	if resp.Error.Code != ErrInvalidInput {
		t.Fatalf("error code = %q, want %q", resp.Error.Code, ErrInvalidInput)
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
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)

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
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)

	r.Handle("test", func(req *Request) *Response {
		if req.Params != nil {
			t.Fatalf("expected nil params for whitespace-only stdin, got %s", string(req.Params))
		}
		return &Response{Version: ResponseVersion}
	})

	code := r.Run([]string{"test"}, strings.NewReader("   \n\t  "))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
}

func TestRouter_HandlerVersionDefault(t *testing.T) {
	var buf bytes.Buffer
	r := NewRouter()
	r.SetOutput(&buf)

	// Handler returns response with empty Version — router should fill it.
	r.Handle("test", func(req *Request) *Response {
		return &Response{}
	})

	code := r.Run([]string{"test"}, nil)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	resp := decodeResponse(t, &buf)
	if resp.Version != ResponseVersion {
		t.Fatalf("version = %q, want %q (should be filled by router)", resp.Version, ResponseVersion)
	}
}
