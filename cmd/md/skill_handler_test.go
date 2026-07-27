package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caoer/meridian/internal/cli"
)

const skillHandlerDoc = `---
name: demo-skill
description: test skill
---

# Demo

Branch: !` + "`printf main`" + `

` + "```!" + `
echo "loaded ok"
` + "```" + `

Broken: !` + "`exit 7`" + ` fallback prose.
`

func writeSkillDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillHandlerDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSkillRenderHandler_MissingSkillParam(t *testing.T) {
	resp := skillRenderHandlerWith(nil)(&cli.Request{Command: "skill render"})
	if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Fatalf("want invalid_params error, got %+v", resp.Error)
	}
}

func TestSkillRenderHandler_InvalidTimeout(t *testing.T) {
	params, _ := json.Marshal(map[string]string{"skill": writeSkillDir(t), "timeout": "banana"})
	resp := skillRenderHandlerWith(nil)(&cli.Request{Command: "skill render", Params: params})
	if resp.Error == nil || resp.Error.Code != cli.ErrInvalidParams {
		t.Fatalf("want invalid_params error, got %+v", resp.Error)
	}
}

func TestSkillRenderHandler_NotASkillFolder(t *testing.T) {
	params, _ := json.Marshal(map[string]string{"skill": t.TempDir()})
	resp := skillRenderHandlerWith(nil)(&cli.Request{Command: "skill render", Params: params})
	if resp.Error == nil {
		t.Fatal("want loud error for a folder without SKILL.md")
	}
	if !strings.Contains(resp.Error.Message, "SKILL.md") {
		t.Errorf("error must name SKILL.md: %s", resp.Error.Message)
	}
}

func TestSkillRenderHandler_RendersAndReports(t *testing.T) {
	dir := writeSkillDir(t)
	var meta bytes.Buffer
	params, _ := json.Marshal(map[string]string{"skill": dir})
	resp := skillRenderHandlerWith(&meta)(&cli.Request{Command: "skill render", Params: params})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	data, ok := resp.Data.(cli.SkillRenderData)
	if !ok {
		t.Fatalf("Data type = %T", resp.Data)
	}
	if strings.Contains(data.Content, "description:") {
		t.Errorf("frontmatter leaked:\n%s", data.Content)
	}
	if !strings.Contains(data.Content, "Branch: main") {
		t.Errorf("inline directive unresolved:\n%s", data.Content)
	}
	if !strings.Contains(data.Content, "loaded ok") || strings.Contains(data.Content, "```!") {
		t.Errorf("fenced ! block unresolved:\n%s", data.Content)
	}
	if !strings.Contains(data.Content, "Broken: !`exit 7` fallback prose.") {
		t.Errorf("failed inline must stay literal:\n%s", data.Content)
	}
	if len(data.Directives) != 3 {
		t.Fatalf("directives = %+v, want 3", data.Directives)
	}
	// Failed directive → warning, but the render is the load path: exit 0.
	if len(resp.Warnings) != 1 || resp.Warnings[0].Code != "SKILL_DIRECTIVE_FAILED" {
		t.Errorf("warnings = %+v, want one SKILL_DIRECTIVE_FAILED", resp.Warnings)
	}
	if resp.ExitCode() != 0 {
		t.Errorf("exit = %d, want 0", resp.ExitCode())
	}
	// Text-mode receipts on the side channel.
	if !strings.Contains(meta.String(), "skill: ") || !strings.Contains(meta.String(), "exec: inline") {
		t.Errorf("meta receipts missing:\n%s", meta.String())
	}
}

func TestSkillRenderHandler_JSONFormat_NoMetaLines(t *testing.T) {
	dir := writeSkillDir(t)
	var meta bytes.Buffer
	params, _ := json.Marshal(map[string]string{"skill": dir, "format": "json"})
	resp := skillRenderHandlerWith(&meta)(&cli.Request{Command: "skill render", Params: params})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	if meta.Len() != 0 {
		t.Errorf("json mode must not write meta lines, got:\n%s", meta.String())
	}
}

func TestSkillRenderRouter_TextMode_PureContentOnStdout(t *testing.T) {
	dir := writeSkillDir(t)
	var out bytes.Buffer
	r := cli.NewRouter()
	r.SetOutput(&out)
	r.Handle("skill render", skillRenderHandlerWith(io.Discard))
	params, _ := json.Marshal(map[string]string{"skill": dir})
	code := r.Run([]string{"skill", "render", string(params)}, nil)
	if code != 0 {
		t.Fatalf("exit = %d, output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "Branch: main") {
		t.Errorf("stdout must carry the rendered body:\n%s", out.String())
	}
	if strings.Contains(out.String(), "skill: ") || strings.Contains(out.String(), "WARN") {
		t.Errorf("stdout must stay pure content:\n%s", out.String())
	}
}
