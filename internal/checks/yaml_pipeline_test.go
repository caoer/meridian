package checks_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
	"github.com/caoer/meridian/pkg/testkit"
)

func loadLocusRules(t *testing.T) []rules.Rule {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	locusDir := filepath.Join(repoRoot, "rules", "locus")
	loaded, _, err := rules.LoadDir(locusDir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	return loaded
}

func findRule(t *testing.T, ruleList []rules.Rule, id string) rules.Rule {
	t.Helper()
	for _, r := range ruleList {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("rule %q not found", id)
	return rules.Rule{}
}

func newTestEngine() *engine.Engine {
	e := engine.New()
	for name, fn := range checks.All {
		e.RegisterCheck(name, fn)
	}
	return e
}

// --- Param type verification: YAML → loader → params ---

func TestYAMLPipeline_BrokenWikilink_Params(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "broken-wikilink")

	if r.Check != "broken-wikilink" {
		t.Fatalf("Check = %q, want broken-wikilink", r.Check)
	}

	roots, ok := r.Params["roots"]
	if !ok {
		t.Fatal("params missing 'roots'")
	}
	rootSlice, ok := roots.([]any)
	if !ok {
		t.Fatalf("roots type = %T, want []any", roots)
	}
	if len(rootSlice) != 1 {
		t.Fatalf("roots len = %d, want 1", len(rootSlice))
	}
	if rootSlice[0] != "wiki/**" {
		t.Errorf("roots[0] = %v, want wiki/**", rootSlice[0])
	}

	skip, ok := r.Params["skip-prefixes"]
	if !ok {
		t.Fatal("params missing 'skip-prefixes'")
	}
	skipSlice, ok := skip.([]any)
	if !ok {
		t.Fatalf("skip-prefixes type = %T, want []any", skip)
	}
	wantSkip := []string{"inbox/", ".repos/", "http"}
	if len(skipSlice) != len(wantSkip) {
		t.Fatalf("skip-prefixes len = %d, want %d", len(skipSlice), len(wantSkip))
	}
	for i, want := range wantSkip {
		if skipSlice[i] != want {
			t.Errorf("skip-prefixes[%d] = %v, want %s", i, skipSlice[i], want)
		}
	}
}

func TestYAMLPipeline_Pattern_Params(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "filename-lowercase-dash")

	if r.Check != "pattern" {
		t.Fatalf("Check = %q, want pattern", r.Check)
	}

	target, ok := r.Params["target"].(string)
	if !ok {
		t.Fatalf("target type = %T, want string", r.Params["target"])
	}
	if target != "filename" {
		t.Errorf("target = %q, want filename", target)
	}

	match, ok := r.Params["match"].(string)
	if !ok {
		t.Fatalf("match type = %T, want string", r.Params["match"])
	}
	if match == "" {
		t.Error("match is empty")
	}
}

func TestYAMLPipeline_BacktickedWikilink_Params(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "backticked-wikilink")

	if r.Check != "backticked-wikilink" {
		t.Fatalf("Check = %q, want backticked-wikilink", r.Check)
	}
	if len(r.Params) != 0 {
		t.Errorf("backticked-wikilink should have 0 params, got %d: %v", len(r.Params), r.Params)
	}
}

func TestYAMLPipeline_HeadingStructure_Params(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "heading-structure")

	if r.Check != "heading-structure" {
		t.Fatalf("Check = %q, want heading-structure", r.Check)
	}
	if len(r.Params) != 0 {
		t.Errorf("heading-structure should have 0 params, got %d: %v", len(r.Params), r.Params)
	}
}

func TestYAMLPipeline_Tags_PropertyParams(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "tags")

	if r.Check != "property" {
		t.Fatalf("Check = %q, want property", r.Check)
	}

	prop, ok := r.Params["property"].(string)
	if !ok {
		t.Fatalf("property type = %T, want string", r.Params["property"])
	}
	if prop != "tags" {
		t.Errorf("property = %q, want tags", prop)
	}

	req, ok := r.Params["required"].(bool)
	if !ok {
		t.Fatalf("required type = %T, want bool", r.Params["required"])
	}
	if !req {
		t.Error("required = false, want true")
	}

	tagBlock, ok := r.Params["tag"]
	if !ok {
		t.Fatal("params missing 'tag' block")
	}
	tagMap, ok := tagBlock.(map[string]any)
	if !ok {
		t.Fatalf("tag block type = %T, want map[string]any", tagBlock)
	}
	prefixBlock, ok := tagMap["prefix"].(map[string]any)
	if !ok {
		t.Fatalf("tag.prefix type = %T, want map[string]any", tagMap["prefix"])
	}
	inList, ok := prefixBlock["in"].([]any)
	if !ok {
		t.Fatalf("tag.prefix.in type = %T, want []any", prefixBlock["in"])
	}
	if len(inList) == 0 {
		t.Error("tag.prefix.in is empty")
	}
	if inList[0] != "domain" {
		t.Errorf("tag.prefix.in[0] = %v, want domain", inList[0])
	}
}

func TestYAMLPipeline_Created_PropertyParams(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "created")

	if r.Check != "property" {
		t.Fatalf("Check = %q, want property", r.Check)
	}
	prop, ok := r.Params["property"].(string)
	if !ok {
		t.Fatalf("property type = %T, want string", r.Params["property"])
	}
	if prop != "created" {
		t.Errorf("property = %q, want created", prop)
	}

	dateBlock, ok := r.Params["date"]
	if !ok {
		t.Fatal("params missing 'date' block")
	}
	dateBool, ok := dateBlock.(bool)
	if !ok {
		t.Fatalf("date type = %T, want bool", dateBlock)
	}
	if !dateBool {
		t.Error("date = false, want true")
	}
}

func TestYAMLPipeline_SkillFields_MultiProperty(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "skill-fields")

	if r.Check != "property" {
		t.Fatalf("Check = %q, want property", r.Check)
	}

	prop, ok := r.Params["property"].([]any)
	if !ok {
		t.Fatalf("property type = %T, want []any (multi-field)", r.Params["property"])
	}
	want := []string{"name", "description", "deploy-target"}
	if len(prop) != len(want) {
		t.Fatalf("property len = %d, want %d", len(prop), len(want))
	}
	for i, w := range want {
		if prop[i] != w {
			t.Errorf("property[%d] = %v, want %s", i, prop[i], w)
		}
	}
}

func TestYAMLPipeline_DeployMethod_TextBlock(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "deploy-method")

	if r.Check != "property" {
		t.Fatalf("Check = %q, want property", r.Check)
	}

	textBlock, ok := r.Params["text"]
	if !ok {
		t.Fatal("params missing 'text' block")
	}
	textMap, ok := textBlock.(map[string]any)
	if !ok {
		t.Fatalf("text block type = %T, want map[string]any", textBlock)
	}
	inList, ok := textMap["in"].([]any)
	if !ok {
		t.Fatalf("text.in type = %T, want []any", textMap["in"])
	}
	if len(inList) != 3 {
		t.Fatalf("text.in len = %d, want 3", len(inList))
	}
}

func TestYAMLPipeline_DeployTarget_WikilinkBlock(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "deploy-target")

	if r.Check != "property" {
		t.Fatalf("Check = %q, want property", r.Check)
	}

	wlBlock, ok := r.Params["wikilink"]
	if !ok {
		t.Fatal("params missing 'wikilink' block")
	}
	wlMap, ok := wlBlock.(map[string]any)
	if !ok {
		t.Fatalf("wikilink block type = %T, want map[string]any", wlBlock)
	}
	if _, ok := wlMap["link_display"]; !ok {
		t.Error("wikilink block missing 'link_display'")
	}
}

func TestYAMLPipeline_DerivedFrom_WikilinkResolve(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "derived-from")

	if r.Check != "property" {
		t.Fatalf("Check = %q, want property", r.Check)
	}

	wlBlock, ok := r.Params["wikilink"]
	if !ok {
		t.Fatal("params missing 'wikilink' block")
	}
	wlMap, ok := wlBlock.(map[string]any)
	if !ok {
		t.Fatalf("wikilink block type = %T, want map[string]any", wlBlock)
	}
	resolve, ok := wlMap["resolve"].(string)
	if !ok {
		t.Fatalf("wikilink.resolve type = %T, want string", wlMap["resolve"])
	}
	if resolve != "file_exists" {
		t.Errorf("wikilink.resolve = %q, want file_exists", resolve)
	}

	req, ok := r.Params["required"].(bool)
	if !ok {
		t.Fatalf("required type = %T, want bool", r.Params["required"])
	}
	if !req {
		t.Error("required = false, want true")
	}
}

// --- End-to-end: YAML → loader → engine → check → findings ---

func TestYAMLPipeline_BrokenWikilink_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "broken-wikilink")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page-a.md", map[string]any{"tags": []string{"domain/test"}}, "see [[nonexistent]] here"),
		testkit.FM("wiki/page-b.md", map[string]any{"tags": []string{"domain/test"}}, "see [[page-a]] here"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	var brokenFindings int
	for _, f := range findings {
		if f.RuleID == "broken-wikilink" {
			brokenFindings++
		}
	}
	if brokenFindings != 1 {
		t.Errorf("want 1 broken-wikilink finding, got %d", brokenFindings)
		for _, f := range findings {
			t.Logf("  %s %s:%d %s", f.RuleID, f.FilePath, f.Line, f.Message)
		}
	}
}

func TestYAMLPipeline_BrokenWikilink_SkipPrefix_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "broken-wikilink")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page-a.md", map[string]any{"tags": []string{"domain/test"}},
			"see [[inbox/something]] and [[http://example.com]] and [[.repos/foo]]"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	for _, f := range findings {
		if f.RuleID == "broken-wikilink" {
			t.Errorf("skip-prefixes should filter %q: %s", f.Message, f.FilePath)
		}
	}
}

func TestYAMLPipeline_Pattern_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "filename-lowercase-dash")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/good-name.md", map[string]any{}, "body"),
		testkit.FM("wiki/UPPERCASE.md", map[string]any{}, "body"),
		testkit.FM("wiki/Bad Name.md", map[string]any{}, "body"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	var patternFindings []string
	for _, f := range findings {
		if f.RuleID == "filename-lowercase-dash" {
			patternFindings = append(patternFindings, f.FilePath)
		}
	}
	if len(patternFindings) != 1 {
		t.Errorf("want 1 pattern finding (Bad Name.md), got %d: %v", len(patternFindings), patternFindings)
	}
}

func TestYAMLPipeline_BacktickedWikilink_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "backticked-wikilink")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{"tags": []string{"domain/test"}},
			"normal [[link]] and `code with [[bad-link]]`"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	var found bool
	for _, f := range findings {
		if f.RuleID == "backticked-wikilink" {
			found = true
		}
	}
	if !found {
		t.Error("want backticked-wikilink finding, got none")
	}
}

func TestYAMLPipeline_HeadingStructure_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "heading-structure")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{"tags": []string{"domain/test"}},
			"# Title\n\n### Skipped H3"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	var found bool
	for _, f := range findings {
		if f.RuleID == "heading-structure" {
			found = true
		}
	}
	if !found {
		t.Error("want heading-structure finding for skipped level, got none")
	}
}

func TestYAMLPipeline_Tags_MissingRequired_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "tags")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{}, "body"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	var found bool
	for _, f := range findings {
		if f.RuleID == "tags" {
			found = true
		}
	}
	if !found {
		t.Error("want tags finding for missing required field, got none")
	}
}

func TestYAMLPipeline_Tags_InvalidPrefix_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "tags")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{
			"tags": []string{"unknown/bad-prefix"},
		}, "body"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	var found bool
	for _, f := range findings {
		if f.RuleID == "tags" {
			found = true
		}
	}
	if !found {
		t.Error("want tags finding for invalid prefix, got none")
	}
}

func TestYAMLPipeline_Tags_ValidPrefix_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "tags")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{
			"tags": []string{"domain/test", "type/note"},
		}, "body"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	for _, f := range findings {
		if f.RuleID == "tags" {
			t.Errorf("want 0 tags findings for valid prefixes, got: %s", f.Message)
		}
	}
}

func TestYAMLPipeline_SkillFields_MissingRequired_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "skill-fields")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/outbox/skills/test-skill/SKILL.md", map[string]any{
			"name": "test",
		}, "body"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	var count int
	for _, f := range findings {
		if f.RuleID == "skill-fields" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("want 2 skill-fields findings (description + deploy-target), got %d", count)
		for _, f := range findings {
			t.Logf("  %s %s:%d %s", f.RuleID, f.FilePath, f.Line, f.Message)
		}
	}
}

func TestYAMLPipeline_Created_MissingDate_E2E(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "created")
	e := newTestEngine()

	fs := testkit.Wiki(
		testkit.FM("wiki/page.md", map[string]any{}, "body"),
	)

	findings := e.Run(fs, []rules.Rule{r})

	var found bool
	for _, f := range findings {
		if f.RuleID == "created" {
			found = true
		}
	}
	if !found {
		t.Error("want created finding for missing required field, got none")
	}
}

// --- Regression: params must NOT be nested under a "params" key ---

func TestYAMLPipeline_NoNestedParamsKey(t *testing.T) {
	ruleList := loadLocusRules(t)

	for _, r := range ruleList {
		if _, hasNested := r.Params["params"]; hasNested {
			t.Errorf("rule %q has nested 'params' key — params should be top-level YAML keys", r.ID)
		}
	}
}

// --- Verify every check rule's check function is registered ---

func TestYAMLPipeline_AllChecksRegistered(t *testing.T) {
	ruleList := loadLocusRules(t)

	for _, r := range ruleList {
		if _, ok := checks.All[r.Check]; !ok {
			t.Errorf("rule %q references unregistered check %q", r.ID, r.Check)
		}
	}
}

func TestYAMLPipeline_BrokenWikilink_ExcludesNotNotOn(t *testing.T) {
	ruleList := loadLocusRules(t)
	r := findRule(t, ruleList, "broken-wikilink")

	wantExcludes := []string{
		"wiki/outbox/starters/**",
		"wiki/outbox/agents/**",
		"wiki/outbox/skills/locus-cloudflare/skills/**",
		"wiki/sources/**",
	}
	if len(r.On.Excludes) != len(wantExcludes) {
		t.Fatalf("Excludes len = %d, want %d: %+v", len(r.On.Excludes), len(wantExcludes), r.On.Excludes)
	}
	for i, want := range wantExcludes {
		if r.On.Excludes[i].Path != want {
			t.Errorf("Excludes[%d].Path = %q, want %q", i, r.On.Excludes[i].Path, want)
		}
	}

	if _, hasNotOn := r.Params["not-on"]; hasNotOn {
		t.Error("params contains 'not-on' — dead config should be removed from YAML")
	}
}
