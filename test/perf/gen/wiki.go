package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

// baseDocs is the real home-wiki document count (measured 2026-07-10:
// 3,827 docs / 24MB / avg 6.5KB / 257 pins / 17 repos). The wiki profile
// scales this by -mult: mult=10 → ~38k docs, the perf regression floor.
const baseDocs = 3827

// wikiClasses are the directory classes for the wiki profile, root-relative
// (no wiki/ prefix — mirrors the real home-wiki, whose rule globs are
// effects/**, synthesis/**, sources/**, …). effects carries ~6.7% of docs,
// matching the real 257/3827 pin ratio.
var wikiClasses = []struct {
	name   string
	weight int
}{
	{"sessions", 42},
	{"inbox", 28},
	{"domains", 13},
	{"effects", 7},
	{"sources", 7},
	{"synthesis", 3},
}

var inboxLanes = []string{"_unstaged", "clipper", "scraped", "filed", "curated-sessions", "refresh"}

// docSpec is a precomputed doc: its path and the class that shaped it. Pass 2
// re-reads class to decide effect-page vs regular-page generation.
type docSpec struct {
	path  string
	class string
}

// wikiGen extends the base gen with fixture pin surfaces and per-doc class
// tracking. It reuses gen's word/slug/wikilink/body helpers (same package).
type wikiGen struct {
	*gen
	fixtures []repoFixture
	specs    []docSpec
	effIdx   int      // running effect-page index, for scenario round-robin
	taskDocs []string // doc paths that grew an md-<name> task block (need a sidecar)
	mult     int
}

func (w *wikiGen) pickClass() string {
	total := 0
	for _, c := range wikiClasses {
		total += c.weight
	}
	r := w.rnd.Intn(total)
	for _, c := range wikiClasses {
		if r < c.weight {
			return c.name
		}
		r -= c.weight
	}
	return wikiClasses[0].name
}

// wikiDocPath builds a root-relative path for a doc of the given class. ~10%
// of docs reuse an earlier basename stem in a different directory to create
// ambiguity (exercises ambiguous-wikilink + canonicalize).
func (w *wikiGen) wikiDocPath(class string, i int) string {
	var dir string
	switch class {
	case "sessions":
		dir = fmt.Sprintf("sessions/year=2026/month=%02d/%02d-%02d-%s",
			1+w.rnd.Intn(12), 1+w.rnd.Intn(28), w.rnd.Intn(24), w.slug(1))
	case "inbox":
		dir = "inbox/" + inboxLanes[w.rnd.Intn(len(inboxLanes))]
		if w.rnd.Intn(2) == 0 {
			dir += "/" + w.slug(1)
		}
	case "domains":
		dir = "domains/" + w.word()
		if w.rnd.Intn(3) > 0 {
			dir += "/" + w.slug(1)
		}
	case "effects":
		dir = "effects/" + w.word()
	case "sources":
		dir = "sources/git/" + w.slug(2)
	case "synthesis":
		dir = "synthesis/" + w.word()
	}

	var stem string
	if len(w.stems) > 20 && w.rnd.Intn(10) == 0 {
		stem = w.stems[w.rnd.Intn(len(w.stems))]
	} else {
		stem = w.slug(2 + w.rnd.Intn(2))
	}
	w.stems = append(w.stems, stem)
	return fmt.Sprintf("%s/%s-%04d.md", dir, stem, i%10000)
}

// genWiki generates a home-wiki-shaped corpus at -mult scale under out, builds
// the fixture repos, vendors the three packs, and prints the REPOS_ROOT export
// line as the final stdout line (progress → stderr).
func genWiki(out string, mult int, seed int64) error {
	if mult < 1 {
		return fmt.Errorf("mult must be >= 1, got %d", mult)
	}
	docs := mult * baseDocs

	reposRoot := filepath.Join(out, ".repos-fixtures")
	fmt.Fprintf(os.Stderr, "gen: building %d fixture repos under %s …\n", len(fixtureSlugs), reposRoot)
	fixtures, err := buildFixtures(reposRoot, fixtureSlugs)
	if err != nil {
		return err
	}

	w := &wikiGen{
		gen:      &gen{rnd: rand.New(rand.NewSource(seed))},
		fixtures: fixtures,
		mult:     mult,
	}

	// Pass 1: precompute every path so links resolve forward and backward.
	fmt.Fprintf(os.Stderr, "gen: planning %d doc paths …\n", docs)
	seen := make(map[string]bool, docs)
	for i := 0; i < docs; i++ {
		class := w.pickClass()
		p := w.wikiDocPath(class, i)
		for seen[p] {
			p = strings.TrimSuffix(p, ".md") + "-" + w.word() + ".md"
		}
		seen[p] = true
		w.paths = append(w.paths, p)
		w.specs = append(w.specs, docSpec{path: p, class: class})
	}

	// Pass 2: write bodies.
	fmt.Fprintf(os.Stderr, "gen: writing %d docs …\n", docs)
	for _, spec := range w.specs {
		full := filepath.Join(out, spec.path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		var content string
		if spec.class == "effects" {
			content = w.effectPage()
		} else {
			content = w.regularPage(spec)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return err
		}
	}

	if err := w.writeSidecarsAndBases(out); err != nil {
		return err
	}
	if err := writePacks(out); err != nil {
		return err
	}
	if err := writeWikiConfig(out); err != nil {
		return err
	}

	absRepos, err := filepath.Abs(reposRoot)
	if err != nil {
		absRepos = reposRoot
	}
	fmt.Fprintf(os.Stderr, "gen: generated %d docs + %d fixture repos under %s (seed=%d, mult=%d)\n",
		docs, len(fixtures), out, seed, mult)
	// Final stdout line: the export a caller eval's before `md check`.
	fmt.Printf("export CCC_LLM_WIKI_REPOS_ROOT=%s\n", absRepos)
	return nil
}

// regularPage builds a non-effect doc: rich frontmatter + body, with a
// draws-from field on synthesis pages and an occasional run-record task block.
func (w *wikiGen) regularPage(spec docSpec) string {
	var b strings.Builder
	b.WriteString(w.wikiFrontmatter(spec))
	b.WriteString("\n")
	if task := w.maybeTaskBlock(spec); task != "" {
		b.WriteString(task)
	}
	b.WriteString(w.body())
	return b.String()
}

// wikiFrontmatter is richer than the base gen frontmatter: tag taxonomy,
// created dates, and a draws-from wikilink on synthesis pages (some resolving,
// some broken — draws-from findings).
func (w *wikiGen) wikiFrontmatter(spec docSpec) string {
	var b strings.Builder
	b.WriteString("---\n")
	switch w.rnd.Intn(12) {
	case 0: // missing tags (tag-taxonomy + frontmatter-minima)
	case 1: // invalid prefix (tag-taxonomy violation)
		fmt.Fprintf(&b, "tags:\n  - bogus/%s\n", w.word())
	default:
		fmt.Fprintf(&b, "tags:\n  - %s/%s\n  - type/%s\n",
			tagPrefixes[w.rnd.Intn(len(tagPrefixes))], w.word(), w.word())
	}
	if w.rnd.Intn(10) > 0 { // created present most of the time
		fmt.Fprintf(&b, "created: 2026-%02d-%02d\n", 1+w.rnd.Intn(12), 1+w.rnd.Intn(28))
	}
	if spec.class == "synthesis" {
		// draws-from: 70% resolve to a real stem, 30% dangle.
		if len(w.paths) > 0 && w.rnd.Intn(10) < 7 {
			fmt.Fprintf(&b, "draws-from: \"[[%s]]\"\n", stemOf(w.paths[w.rnd.Intn(len(w.paths))]))
		} else {
			fmt.Fprintf(&b, "draws-from: \"[[%s-missing]]\"\n", w.slug(2))
		}
	}
	if spec.class == "sources" {
		fmt.Fprintf(&b, "source: https://example.com/%s\n", w.slug(2))
	}
	b.WriteString("---\n")
	return b.String()
}

// maybeTaskBlock adds an md-<name> task frontmatter key + its ^block on ~3% of
// session/domain docs — the stale-run-record read path. The paired sidecar is
// written in writeSidecarsAndBases.
func (w *wikiGen) maybeTaskBlock(spec docSpec) string {
	if spec.class != "sessions" && spec.class != "domains" {
		return ""
	}
	if w.rnd.Intn(32) != 0 {
		return ""
	}
	// Record which docs get a task so the sidecar pass can find them.
	w.taskDocs = append(w.taskDocs, spec.path)
	name := "step"
	return fmt.Sprintf("```sh\necho running %s\n```\n^%s\n\n", spec.path, name)
}

// effectPage builds an effects/ page pinning a fixture repo. Scenarios are
// round-robined across the first len(scenarios) pages (guaranteeing at least
// one finding per effect-pin rule even in a tiny corpus), then weighted toward
// valid pins for the rest (realism).
func (w *wikiGen) effectPage() string {
	sc := w.pickScenario()
	f := w.fixtures[w.rnd.Intn(len(w.fixtures))]
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("tags:\n  - type/effect\n  - domain/wiki\n")
	fmt.Fprintf(&b, "created: 2026-%02d-%02d\n", 1+w.rnd.Intn(12), 1+w.rnd.Intn(28))
	b.WriteString(pinFrontmatter(sc, f))
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# Effect: %s\n\n", f.Slug)
	b.WriteString(w.body())
	return b.String()
}

type scenario int

const (
	scValidBlob scenario = iota
	scValidTree
	scStale
	scDangling
	scNotOnOrigin
	scChecksumMismatch
	scUnpinned
	scAbsentRepo
	scIncomplete
)

var allScenarios = []scenario{
	scValidBlob, scValidTree, scStale, scDangling, scNotOnOrigin,
	scChecksumMismatch, scUnpinned, scAbsentRepo, scIncomplete,
}

// pickScenario round-robins the first len(allScenarios) effect pages through
// every scenario (coverage guarantee), then draws valid-heavy for the rest.
func (w *wikiGen) pickScenario() scenario {
	idx := w.effIdx
	w.effIdx++
	if idx < len(allScenarios) {
		return allScenarios[idx]
	}
	// Weighted: valid pins dominate (a real wiki is mostly healthy).
	switch w.rnd.Intn(20) {
	case 0:
		return scStale
	case 1:
		return scDangling
	case 2:
		return scNotOnOrigin
	case 3:
		return scChecksumMismatch
	case 4:
		return scUnpinned
	case 5:
		return scAbsentRepo
	case 6:
		return scIncomplete
	case 7, 8, 9:
		return scValidTree
	default:
		return scValidBlob
	}
}

// pinFrontmatter renders the pin fields for a scenario against a fixture repo.
func pinFrontmatter(sc scenario, f repoFixture) string {
	blob := func(commit, loc, sum string) string {
		return fmt.Sprintf("repo: %s\nbranch: %s\ncommit: %s\nlocation:\n  - %s\nchecksum:\n  - %s\n",
			f.Slug, f.Branch, commit, loc, sum)
	}
	switch sc {
	case scValidBlob:
		return blob(f.C1, f.Location, f.BlobV2)
	case scValidTree:
		return blob(f.C1, f.TreeLoc, f.TreeC1)
	case scStale:
		// pin C0 with C0's checksum; origin advanced to C1 and content drifted.
		return blob(f.C0, f.Location, f.BlobV1)
	case scDangling:
		return blob(f.Dangling, f.Location, f.BlobV2)
	case scNotOnOrigin:
		return blob(f.CX, f.Location, f.BlobSide)
	case scChecksumMismatch:
		return blob(f.C1, f.Location, f.BlobV1) // wrong checksum (C0's)
	case scUnpinned:
		return "" // type/effect page, no pin fields
	case scAbsentRepo:
		// repo slug not among the fixtures → resolveRepoDir fails → skipped.
		return fmt.Sprintf("repo: absent-%s\nbranch: %s\ncommit: %s\nlocation:\n  - %s\nchecksum:\n  - %s\n",
			f.Slug, f.Branch, f.C1, f.Location, f.BlobV2)
	case scIncomplete:
		// commit present but branch missing → "incomplete pin" finding.
		return fmt.Sprintf("repo: %s\ncommit: %s\nlocation:\n  - %s\nchecksum:\n  - %s\n",
			f.Slug, f.C1, f.Location, f.BlobV2)
	}
	return ""
}

// writeSidecarsAndBases writes the .runs.md sidecars for task docs (stale +
// never-recorded, alternating → stale-run-record findings) and a bounded
// handful of .base files (Obsidian Bases shape; not scanned — .base is not
// markdown — pure directory realism).
func (w *wikiGen) writeSidecarsAndBases(out string) error {
	for i, docPath := range w.taskDocs {
		stem := stemOf(docPath)
		recPath := filepath.Join(out, filepath.Dir(docPath), stem+".runs.md")
		var runs string
		if i%2 == 0 {
			// stale: recorded block_sha won't match the live block.
			runs = fmt.Sprintf("  step:\n    block_sha: %s\n", danglingSHA(docPath))
		} else {
			// never recorded: sidecar exists but has no entry for "step".
			runs = fmt.Sprintf("  other:\n    block_sha: %s\n", danglingSHA(docPath))
		}
		content := fmt.Sprintf("---\ntags:\n  - type/run-record\ncreated: 2026-03-15\nruns:\n%s---\n\nRun record for %s.\n", runs, stem)
		if err := os.WriteFile(recPath, []byte(content), 0o644); err != nil {
			return err
		}
	}

	// A small, bounded set of .base files (scaled loosely by mult, capped).
	nBase := 2 * w.mult
	if nBase > 40 {
		nBase = 40
	}
	for i := 0; i < nBase; i++ {
		dir := filepath.Join(out, "domains", w.word())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		name := w.slug(2) + ".base"
		base := fmt.Sprintf("filters:\n  and:\n    - file.hasTag(\"type/%s\")\nviews:\n  - type: table\n    name: %s\n    order:\n      - file.name\n      - created\n",
			w.word(), w.word())
		if err := os.WriteFile(filepath.Join(dir, name), []byte(base), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// writeWikiConfig writes the corpus meridian.yaml with the three vendored packs
// and a skip list that excludes the fixture repos and vendored rules.
func writeWikiConfig(out string) error {
	var packs strings.Builder
	for _, p := range wikiPacks {
		fmt.Fprintf(&packs, "  - path: rules/%s\n", p.name)
	}
	cfg := "version: \"0.1\"\nrule_packs:\n" + packs.String() + `scan:
  root: .
  max_file_size: 10485760
  skip:
    - .git
    - rules
    - .repos-fixtures
`
	return os.WriteFile(filepath.Join(out, "meridian.yaml"), []byte(cfg), 0o644)
}
