package attest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caoer/meridian/internal/resolve"
)

const fence = "```"

// --- fakes ---

type fakeFacts struct {
	slices  map[resolve.Node]resolve.Hash
	embeds  map[resolve.Node][]resolve.Ref
	pointed map[string]resolve.Hash
}

func (f fakeFacts) SliceHash(n resolve.Node) (resolve.Hash, bool) {
	h, ok := f.slices[n]
	return h, ok
}
func (f fakeFacts) Embeds(n resolve.Node) []resolve.Ref { return f.embeds[n] }
func (f fakeFacts) PointedReceiptChecksum(p string) (resolve.Hash, bool) {
	h, ok := f.pointed[p]
	return h, ok
}

type fakeRes struct {
	m     map[string]string
	ambig map[string]bool
	cands map[string][]string
}

func (r fakeRes) Resolve(t string) (string, bool) { p, ok := r.m[t]; return p, ok }
func (r fakeRes) IsAmbiguous(t string) bool       { return r.ambig[t] }
func (r fakeRes) Candidates(t string) []string    { return r.cands[t] }

// fakeGit scripts cat-file batches (objs: query → "sha type size" line),
// merge-base ancestry ("commit->ref" keys), rev-list attribution (revlist:
// source path → stdout; non-empty = an unexplained commit touched the file), and
// working-tree status (dirty: source path → porcelain output; non-empty =
// uncommitted/untracked). failBatch simulates cat-file infra death; failRevList
// a rev-list death.
type fakeGit struct {
	objs        map[string]string
	ancestors   map[string]bool
	revlist     map[string]string
	dirty       map[string]string
	failBatch   bool
	failRevList bool
}

func (g *fakeGit) Run(dir string, stdin []byte, args ...string) ([]byte, error) {
	switch args[0] {
	case "cat-file":
		if g.failBatch {
			return nil, errors.New("simulated cat-file death")
		}
		var out strings.Builder
		for _, q := range strings.Split(strings.TrimRight(string(stdin), "\n"), "\n") {
			if line, ok := g.objs[q]; ok {
				out.WriteString(line + "\n")
			} else {
				out.WriteString(q + " missing\n")
			}
		}
		return []byte(out.String()), nil
	case "merge-base":
		if g.ancestors[args[2]+"->"+args[3]] {
			return nil, nil
		}
		return nil, errors.New("not an ancestor")
	case "rev-list":
		if g.failRevList {
			return nil, errors.New("simulated rev-list death")
		}
		// args tail is "-- <path>"; the path is the blame target.
		path := args[len(args)-1]
		return []byte(g.revlist[path]), nil
	case "status":
		// `status --porcelain -- <path>`; empty = clean & tracked.
		path := args[len(args)-1]
		return []byte(g.dirty[path]), nil
	}
	return nil, errors.New("unexpected git call: " + strings.Join(args, " "))
}

// --- fixture plumbing ---

var (
	tipSha   = strings.Repeat("a", 40)
	oldSha   = strings.Repeat("b", 40)
	aheadSha = strings.Repeat("c", 40)
	sumSha   = strings.Repeat("d", 40)
	oldSum   = strings.Repeat("e", 40)
	fixedNow = time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	procV1   = "procedure-v1:" + strings.Repeat("1", 64)
	procV2   = "procedure-v1:" + strings.Repeat("2", 64)
)

func hexHash(seed string) resolve.Hash {
	sum := sha256.Sum256([]byte(seed))
	return resolve.Hash("sha256:" + hex.EncodeToString(sum[:]))
}

// pointedPage assembles a receipt-shape pointed page. receiptFM is the
// frontmatter receipt value; receiptBlock, when non-empty, is the ^receipt
// fenced unit's YAML content.
func pointedPage(receiptFM, inputHash, receiptBlock string) string {
	s := "---\n" +
		"name: skillx\n" +
		"description: test effect\n" +
		"repo: cc-continuity\n" +
		"location: skills/skillx/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"receipt: " + receiptFM + "\n" +
		"tags: [type/effect, effect/skill]\n" +
		"---\n" +
		"\n# Skillx\n\nClaim prose.\n\n## Chain\n\n" +
		fence + "yaml\n" +
		"- ref: '[[dep#Sec]]'\n" +
		"  claim: 'primary'\n" +
		"  hash: " + inputHash + "\n" +
		fence + "\n^inputs\n"
	if receiptBlock != "" {
		s += "\n## Receipt\n\n" + fence + "yaml\n" + receiptBlock + fence + "\n^receipt\n"
	}
	return s
}

func ownedPage(inputHash string) string {
	return "---\n" +
		"name: caveman\n" +
		"repo: home-wiki\n" +
		"location: effects/skills/caveman/\n" +
		"inputs: '[[#^inputs]]'\n" +
		"tags: [type/effect, effect/skill]\n" +
		"---\n" +
		"\n# Caveman\n\n## Chain\n\n" +
		fence + "yaml\n" +
		"- ref: '[[dep#Sec]]'\n" +
		"  hash: " + inputHash + "\n" +
		fence + "\n^inputs\n"
}

// receiptYAML renders a standard recorded receipt block.
func receiptYAML(commit, checksum, proc string) string {
	return "commit: " + commit + "\n" +
		"checksum: " + checksum + "\n" +
		"applied_at: 2026-07-01T00:00:00Z\n" +
		"verdict: 'year=2026/month=07/01-old/verdicts/k.reviewer.md@" + oldSha + "'\n" +
		"procedure-hash: '" + proc + "'\n" +
		"note-field: keep-me\n" // unknown field — must survive every write
}

type fixture struct {
	eng   *Engine
	root  string
	git   *fakeGit
	wrote *bool // flipped by WriteDisk
}

// newFixture wires a corpus of pages plus the dep page every chain points at,
// a scripted git, and a repos root carrying cc-continuity.
func newFixture(t *testing.T, pages map[string]string) *fixture {
	t.Helper()
	root := t.TempDir()
	raw := map[string][]byte{}
	var list []string
	for rel, content := range pages {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		raw[rel] = []byte(content)
		list = append(list, rel)
	}
	// owned locations referenced by fixtures
	for _, dir := range []string{"effects/skills/caveman", "skills/skillx"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reposRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(reposRoot, "cc-continuity"), 0o755); err != nil {
		t.Fatal(err)
	}
	git := &fakeGit{
		objs: map[string]string{
			"origin/main":             tipSha + " commit 250",
			tipSha + ":skills/skillx": sumSha + " tree 0",
			// declared-commit existence: a bare-sha cat-file query resolves to a
			// commit object (bulk_reattest's up-front check).
			tipSha:   tipSha + " commit 100",
			aheadSha: aheadSha + " commit 101",
		},
		ancestors: map[string]bool{oldSha + "->" + tipSha: true},
	}
	wrote := false
	eng := &Engine{
		Root:  root,
		Pages: list,
		Raw:   raw,
		Source: fakeFacts{slices: map[resolve.Node]resolve.Hash{
			{Path: "wiki/dep.md", Anchor: "Sec"}: hexHash("dep-sec-v1"),
		}},
		Res:       fakeRes{m: map[string]string{"dep": "wiki/dep.md"}},
		Git:       git,
		ReposRoot: reposRoot,
		Branch:    func(slug string) (string, bool) { return "main", slug == "cc-continuity" },
		RunCheck:  func(string) CheckResult { return CheckResult{} },
		ProcHash:  func(string) (string, error) { return procV1, nil },
		Now:       func() time.Time { return fixedNow },
	}
	eng.WriteDisk = func(abs string, data []byte, mode os.FileMode) error {
		wrote = true
		return writeFileAtomic(abs, data, mode)
	}
	return &fixture{eng: eng, root: root, git: git, wrote: &wrote}
}

// depHash is the merkle hash the fixture chain computes for [[dep#Sec]].
func depHash(t *testing.T, f *fixture) string {
	t.Helper()
	h, err := resolve.Compose(resolve.Node{Path: "wiki/dep.md", Anchor: "Sec"}, f.eng.Source, f.eng.Res, 0)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

func onePage(t *testing.T, rep *Report) PageResult {
	t.Helper()
	if len(rep.Pages) != 1 {
		t.Fatalf("want 1 page result, got %+v", rep.Pages)
	}
	return rep.Pages[0]
}

func diskContent(t *testing.T, f *fixture, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// rescan reloads the on-disk page into the engine snapshot (a second run).
func rescan(t *testing.T, f *fixture, rel string) {
	t.Helper()
	f.eng.Raw[rel] = []byte(diskContent(t, f, rel))
}

// --- the tests ---

// First attestation: receipt: null → block created, pointer flipped, chain
// hashed — then a second run over the written bytes reports `unchanged` (the
// parse and write halves agree byte-for-byte).
func TestFirstAttestationThenIdempotent(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("null", "null", "")})

	rep, err := f.eng.Attest(Options{Page: rel, Verdict: "year=2026/month=07/13-x/verdicts/k.reviewer.md@" + tipSha})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusAttested || pr.Case != CaseFirst {
		t.Fatalf("want attested/first, got %+v", pr)
	}
	got := diskContent(t, f, rel)
	for _, want := range []string{
		"receipt: '[[#^receipt]]'",
		"commit: " + tipSha,
		"checksum: " + sumSha,
		"applied_at: 2026-07-13T12:00:00Z",
		"procedure-hash: '" + procV1 + "'",
		"verdict: 'year=2026/month=07/13-x/verdicts/k.reviewer.md@" + tipSha + "'",
		"hash: '" + depHash(t, f) + "'",
		"^receipt",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("written page missing %q\n---\n%s", want, got)
		}
	}
	if strings.Contains(got, "receipt: null") {
		t.Error("frontmatter receipt: null survived the flip")
	}

	rescan(t, f, rel)
	*f.wrote = false
	rep2, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr2 := onePage(t, rep2); pr2.Status != StatusUnchanged {
		t.Fatalf("second run: want unchanged, got %+v", pr2)
	}
	if *f.wrote {
		t.Error("unchanged run wrote the file")
	}
}

// Owned pages write input hashes only — no ^receipt block, no applied_at (P20).
func TestOwnedWritesInputHashesOnly(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: ownedPage("null")})

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseInputsOnly {
		t.Fatalf("want attested/inputs-only, got %+v", pr)
	}
	got := diskContent(t, f, rel)
	if !strings.Contains(got, "hash: '"+depHash(t, f)+"'") {
		t.Errorf("chain hash not written:\n%s", got)
	}
	for _, banned := range []string{"^receipt", "applied_at", "commit:", "checksum:"} {
		if strings.Contains(got, banned) {
			t.Errorf("owned page gained %q — owned pages are receipt-free (P20):\n%s", banned, got)
		}
	}

	rescan(t, f, rel)
	rep2, _ := f.eng.Attest(Options{Page: rel})
	if pr2 := onePage(t, rep2); pr2.Status != StatusUnchanged {
		t.Fatalf("second owned run: want unchanged, got %+v", pr2)
	}
}

// Case 2a: inputs-only change → chain hashes + verdict pointer refresh;
// commit/checksum/applied_at/procedure-hash lines byte-stable.
func TestCase2aInputsOnly(t *testing.T) {
	rel := "effects/skills/skillx.md"
	stale := "'merkle-v1:" + strings.Repeat("0", 64) + "'"
	f := newFixture(t, map[string]string{
		rel: pointedPage("'[[#^receipt]]'", stale, receiptYAML(tipSha, sumSha, procV1)),
	})
	newVerdict := "year=2026/month=07/13-x/verdicts/k.reviewer.md@" + tipSha

	rep, err := f.eng.Attest(Options{Page: rel, Verdict: newVerdict})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseInputsOnly {
		t.Fatalf("want attested/inputs-only, got %+v", pr)
	}
	got := diskContent(t, f, rel)
	for _, stable := range []string{
		"commit: " + tipSha,
		"checksum: " + sumSha,
		"applied_at: 2026-07-01T00:00:00Z", // the OLD timestamp — byte-stable
		"procedure-hash: '" + procV1 + "'",
		"note-field: keep-me", // unknown field survives
	} {
		if !strings.Contains(got, stable) {
			t.Errorf("case 2a moved a byte-stable field — missing %q:\n%s", stable, got)
		}
	}
	if !strings.Contains(got, "verdict: '"+newVerdict+"'") {
		t.Errorf("case 2a did not refresh the verdict pointer:\n%s", got)
	}
	if !strings.Contains(got, "hash: '"+depHash(t, f)+"'") {
		t.Errorf("case 2a did not rewrite the chain hash:\n%s", got)
	}
}

// Case 2b: procedure-only change → the receipt moves (procedure-hash,
// applied_at, verdict); checksum and commit stay byte-stable.
func TestCase2bProcedureMoved(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{
		rel: pointedPage("'[[#^receipt]]'", "null", receiptYAML(tipSha, sumSha, procV1)),
	})
	f.eng.ProcHash = func(string) (string, error) { return procV2, nil }

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseProcedure {
		t.Fatalf("want attested/procedure, got %+v", pr)
	}
	got := diskContent(t, f, rel)
	for _, want := range []string{
		"procedure-hash: '" + procV2 + "'",
		"applied_at: 2026-07-13T12:00:00Z",
		"commit: " + tipSha,
		"checksum: " + sumSha,
		"note-field: keep-me",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("case 2b: missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, procV1) {
		t.Error("old procedure-hash survived a procedure move")
	}
}

// Case 3: tree moved → full receipt write at the new target commit.
func TestCaseTreeMoved(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{
		rel: pointedPage("'[[#^receipt]]'", "null", receiptYAML(oldSha, oldSum, procV1)),
	})

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	if pr := onePage(t, rep); pr.Status != StatusAttested || pr.Case != CaseTree {
		t.Fatalf("want attested/tree, got %+v", pr)
	}
	got := diskContent(t, f, rel)
	for _, want := range []string{
		"commit: " + tipSha,
		"checksum: " + sumSha,
		"applied_at: 2026-07-13T12:00:00Z",
		"note-field: keep-me",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tree case: missing %q:\n%s", want, got)
		}
	}
}

// Ancestry guard (R3): an incumbent that is not an ancestor of the target
// refuses the write — nothing lands on disk.
func TestAncestryRefusalWritesNothing(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("'[[#^receipt]]'", "null", receiptYAML(aheadSha, oldSum, procV1))
	f := newFixture(t, map[string]string{rel: before})
	// aheadSha is NOT in fakeGit.ancestors → not an ancestor of tip.

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusRefused {
		t.Fatalf("want refused, got %+v", pr)
	}
	if !strings.Contains(pr.Reason, aheadSha) {
		t.Errorf("refusal must name the incumbent commit: %q", pr.Reason)
	}
	if diskContent(t, f, rel) != before {
		t.Error("refusal wrote bytes")
	}
	if *f.wrote {
		t.Error("WriteDisk called on refusal")
	}
}

// CAS guard (P6): a concurrent editor between read and write aborts as a tool
// failure — the foreign edit is never clobbered.
func TestCASAbort(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("null", "null", "")})
	foreign := strings.Replace(pointedPage("null", "null", ""), "Claim prose.", "Concurrent edit.", 1)
	if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(rel)), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusFailed || !strings.Contains(pr.Reason, "cas-retry") {
		t.Fatalf("want failed cas-retry, got %+v", pr)
	}
	if rep.ToolFailure == "" {
		t.Error("CAS abort must escalate to a tool failure (exit 2)")
	}
	if diskContent(t, f, rel) != foreign {
		t.Error("CAS abort clobbered the concurrent edit")
	}
}

// batchFailed (§3.3 step 3): cat-file death aborts the page as a tool failure —
// never a false unchanged, never a receipt from an empty object table.
func TestBatchFailureExit2(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("null", "null", "")})
	f.git.failBatch = true

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusFailed || !strings.Contains(pr.Reason, "batch") {
		t.Fatalf("want failed batch, got %+v", pr)
	}
	if rep.ToolFailure == "" {
		t.Error("batch death must escalate to a tool failure (exit 2)")
	}
	if *f.wrote {
		t.Error("batch failure wrote bytes")
	}
}

// Dry-run writes nothing — the WriteDisk seam must never be reached.
func TestDryRunWritesNothing(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("null", "null", "")
	f := newFixture(t, map[string]string{rel: before})
	f.eng.WriteDisk = func(string, []byte, os.FileMode) error {
		t.Fatal("dry-run reached WriteDisk")
		return nil
	}

	rep, err := f.eng.Attest(Options{Page: rel, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusAttested || !pr.WouldWrite {
		t.Fatalf("want attested would_write, got %+v", pr)
	}
	if diskContent(t, f, rel) != before {
		t.Error("dry-run changed the file")
	}
}

// A failed ^check MUST NOT produce a receipt (step 1 gates everything).
func TestFailedCheckWritesNothing(t *testing.T) {
	rel := "effects/skills/skillx.md"
	before := pointedPage("null", "null", "")
	f := newFixture(t, map[string]string{rel: before})
	f.eng.RunCheck = func(string) CheckResult {
		return CheckResult{ExitCode: 3, Detail: "gate says no"}
	}

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusFailed || !strings.Contains(pr.Reason, "gate says no") {
		t.Fatalf("want failed with detail, got %+v", pr)
	}
	if diskContent(t, f, rel) != before {
		t.Error("failed check wrote bytes")
	}
}

// Legacy-shape pages (frontmatter commit pin) are counted, named, skipped.
func TestLegacyShapeSkipped(t *testing.T) {
	rel := "effects/skills/legacy.md"
	legacy := "---\n" +
		"repo: cc-continuity\n" +
		"branch: main\n" +
		"commit: " + oldSha + "\n" +
		"location: skills/legacy/\n" +
		"checksum: " + oldSum + "\n" +
		"tags: [type/effect]\n" +
		"---\n\n# Legacy\n"
	f := newFixture(t, map[string]string{rel: legacy})

	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusSkipped || !strings.Contains(pr.Reason, "legacy") {
		t.Fatalf("want skipped legacy, got %+v", pr)
	}
}

// Scope mode sweeps type/effect pages under the prefix and stays silent on
// everything else.
func TestScopeSelection(t *testing.T) {
	f := newFixture(t, map[string]string{
		"effects/skills/skillx.md": pointedPage("null", "null", ""),
		"effects/skills/notes.md":  "---\ntags: [type/note]\n---\n\n# Not an effect\n",
		"domains/other.md":         "---\ntags: [type/effect]\n---\n\n# Outside scope\n",
	})

	rep, err := f.eng.Attest(Options{Scope: "effects/", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Pages) != 1 || rep.Pages[0].Page != "effects/skills/skillx.md" {
		t.Fatalf("scope selected wrong population: %+v", rep.Pages)
	}
}

// Param gates: verdict form, sweep-verdict, unknown page.
func TestInvocationValidation(t *testing.T) {
	rel := "effects/skills/skillx.md"
	f := newFixture(t, map[string]string{rel: pointedPage("null", "null", "")})
	sha := tipSha
	cases := []struct {
		name string
		opts Options
	}{
		{"verdict traversal", Options{Page: rel, Verdict: "../../escape.md@" + sha}},
		{"verdict no sha", Options{Page: rel, Verdict: "verdicts/k.reviewer.md"}},
		{"verdict short sha", Options{Page: rel, Verdict: "verdicts/k.md@abc123"}},
		{"verdict control chars", Options{Page: rel, Verdict: "verd\nicts/k.md@" + sha}},
		{"verdict absolute", Options{Page: rel, Verdict: "/etc/passwd@" + sha}},
		{"verdict on sweep", Options{Scope: "effects/", Verdict: "verdicts/k.md@" + sha}},
		{"commit not full sha", Options{Page: rel, Commit: "abc123"}},
		{"commit injection", Options{Page: rel, Commit: "--upload-pack=evil"}},
		{"unknown page", Options{Page: "effects/typo.md"}},
		{"page and scope", Options{Page: rel, Scope: "effects/"}},
		{"neither", Options{}},
	}
	for _, c := range cases {
		if _, err := f.eng.Attest(c.opts); err == nil {
			t.Errorf("%s: want invocation error, got none", c.name)
		}
	}
	if *f.wrote {
		t.Error("a rejected invocation wrote bytes")
	}
}

// A verdict against an owned page is not recordable (P20).
func TestOwnedVerdictRejected(t *testing.T) {
	rel := "effects/skills/caveman.md"
	f := newFixture(t, map[string]string{rel: ownedPage("null")})
	rep, err := f.eng.Attest(Options{Page: rel, Verdict: "verdicts/k.reviewer.md@" + tipSha})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusFailed || !strings.Contains(pr.Reason, "owned") {
		t.Fatalf("want failed owned-verdict, got %+v", pr)
	}
}

// An unhashable input (unresolved ref) aborts the page — never a partial write.
func TestUnresolvedInputFails(t *testing.T) {
	rel := "effects/skills/skillx.md"
	page := strings.Replace(pointedPage("null", "null", ""), "[[dep#Sec]]", "[[ghost#Sec]]", 1)
	f := newFixture(t, map[string]string{rel: page})
	rep, err := f.eng.Attest(Options{Page: rel})
	if err != nil {
		t.Fatal(err)
	}
	pr := onePage(t, rep)
	if pr.Status != StatusFailed || !strings.Contains(pr.Reason, "unhashable") {
		t.Fatalf("want failed unhashable, got %+v", pr)
	}
	if *f.wrote {
		t.Error("unhashable input wrote bytes")
	}
}
