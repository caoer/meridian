package main

import (
	"encoding/json"
	"testing"

	"github.com/caoer/meridian/internal/cli"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
)

func TestFixHandler_InvalidParamsReturnsError(t *testing.T) {
	eng := engine.New()
	cfg := &config.Config{Scan: config.ScanConfig{Root: "."}}
	handler := fixHandler(eng, nil, cfg, nil)

	// Send dry-run as string "yes" instead of bool — should error
	raw := json.RawMessage(`{"dry-run":"yes"}`)
	req := &cli.Request{Params: raw}
	resp := handler(req)

	if resp.Error == nil {
		t.Fatal("expected error response for invalid params, got nil")
	}
	if resp.Error.Code != cli.ErrInvalidParams {
		t.Errorf("error code = %q, want %q", resp.Error.Code, cli.ErrInvalidParams)
	}
}

func TestFixHandler_ValidParamsNoError(t *testing.T) {
	eng := engine.New()
	cfg := &config.Config{Scan: config.ScanConfig{Root: "."}}
	handler := fixHandler(eng, []rules.Rule{}, cfg, nil)

	raw := json.RawMessage(`{"dry-run":true}`)
	req := &cli.Request{Params: raw}
	resp := handler(req)

	if resp.Error != nil {
		t.Errorf("unexpected error for valid params: %s", resp.Error.Message)
	}
}
