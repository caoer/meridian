// Command gen generates a deterministic, wiki-shaped markdown corpus for
// performance benchmarking of `md check`.
//
// The corpus models what makes real wikis expensive to check — not raw file
// count alone: dense wikilinks (resolving, broken, ambiguous, non-canonical
// long forms), duplicate basenames across directories, deep session trees,
// frontmatter variety (valid and violating), headings, code fences, and
// tables. Layout lives under wiki/; the shipped contract pack's `**` globs
// match everywhere.
//
// Same seed + same params → byte-identical corpus. Size is a parameter, not
// an artifact: small/medium/large are -docs values.
//
// Two profiles (-profile):
//
//   - corpus (default): a flat, wikilink-dense tree under wiki/. Size is -docs.
//   - wiki: a home-wiki-shaped tree at -mult scale (docs = mult*3827, the
//     measured real-wiki size). Vendors its own three rule packs (contract /
//     home-wiki / effects), builds ~17 deterministic fixture git repos under
//     <out>/.repos-fixtures, and emits effect pages whose pins exercise every
//     effect-pin rule (valid/stale/dangling/not-on-origin/checksum/unpinned).
//     Prints the CCC_LLM_WIKI_REPOS_ROOT export as the final stdout line;
//     progress goes to stderr.
//
// Usage:
//
//	go run ./test/perf/gen -out /tmp/corpus -docs 10000 -seed 1 -rules ./rules
//	go run ./test/perf/gen -profile wiki -out /tmp/wiki10x -mult 10 -seed 1
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

var (
	out     = flag.String("out", "", "output directory (required, must not exist or be empty)")
	docs    = flag.Int("docs", 1000, "number of markdown documents to generate (corpus profile)")
	seed    = flag.Int64("seed", 1, "PRNG seed")
	rules   = flag.String("rules", "", "rule-pack root to copy into the corpus (e.g. ./rules); packs land at <out>/rules/<pack> (corpus profile)")
	profile = flag.String("profile", "corpus", "generation profile: \"corpus\" (flat wikilink-dense tree) or \"wiki\" (home-wiki-shaped: 3-pack rules, effect pins, fixture git repos, sidecars)")
	mult    = flag.Int("mult", 10, "wiki profile: doc-count multiplier over the real home-wiki (~3827 docs); docs = mult*3827")
)

// Weighted directory classes, mirroring a real home-wiki's shape: sessions
// and inbox dominate, domains are the curated core.
var classes = []struct {
	name   string
	weight int
}{
	{"sessions", 45},
	{"inbox", 30},
	{"domains", 15},
	{"sources", 7},
	{"synthesis", 3},
}

var tagPrefixes = []string{"domain", "type", "status", "topic", "meta", "project"}

var words = strings.Fields(`
alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima
mike november oscar papa quebec romeo sierra tango uniform victor whiskey
xray yankee zulu anchor beacon cipher drift ember flux glyph harbor inlet
junction keel lattice meridian nexus orbit prism quartz relay summit
terrace umbra vertex wharf zenith archive bundle catalog domain effect
fixture gateway harness index journal kernel ledger module notion outline
pattern query record schema token upstream vector worker yield zone
`)

type gen struct {
	rnd   *rand.Rand
	paths []string // all generated doc paths, for resolving links
	stems []string // basename stems, for bare links
}

func (g *gen) word() string { return words[g.rnd.Intn(len(words))] }

func (g *gen) slug(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = g.word()
	}
	return strings.Join(parts, "-")
}

func (g *gen) pick(weightTotal int) int {
	r := g.rnd.Intn(weightTotal)
	for i, c := range classes {
		if r < c.weight {
			return i
		}
		r -= c.weight
	}
	return 0
}

// docPath produces a path under wiki/ for the given doc index. ~10% of docs
// deliberately reuse an earlier basename stem in a different directory to
// create ambiguity (exercises ambiguous-wikilink and the canonicalize
// shortest-unambiguous computation).
func (g *gen) docPath(i int) string {
	class := classes[g.pick(100)].name
	var dir string
	switch class {
	case "sessions":
		dir = fmt.Sprintf("wiki/sessions/year=2026/month=%02d/%02d-%02d-%s",
			1+g.rnd.Intn(12), 1+g.rnd.Intn(28), g.rnd.Intn(4), g.slug(1))
	case "inbox":
		dir = "wiki/inbox/" + g.slug(1)
		if g.rnd.Intn(2) == 0 {
			dir += "/" + g.slug(1)
		}
	case "domains":
		dir = "wiki/domains/" + g.word()
		if g.rnd.Intn(3) > 0 {
			dir += "/" + g.slug(1)
		}
	case "sources":
		dir = "wiki/sources/git/" + g.slug(2)
	case "synthesis":
		dir = "wiki/synthesis/" + g.word()
	}

	var stem string
	if len(g.stems) > 20 && g.rnd.Intn(10) == 0 {
		stem = g.stems[g.rnd.Intn(len(g.stems))] // deliberate duplicate
	} else {
		stem = g.slug(2 + g.rnd.Intn(2))
	}
	g.stems = append(g.stems, stem)
	return fmt.Sprintf("%s/%s-%04d.md", dir, stem, i%10000)
}

// wikilink emits one link. Mix: resolving bare stem, resolving full path
// (non-canonical when the stem is unique — canonicalize work), broken,
// backticked (backticked-wikilink), or with display text.
func (g *gen) wikilink() string {
	if len(g.paths) == 0 {
		return "[[bootstrap-page]]"
	}
	target := g.paths[g.rnd.Intn(len(g.paths))]
	switch g.rnd.Intn(10) {
	case 0, 1: // broken
		return fmt.Sprintf("[[%s-missing]]", g.slug(2))
	case 2: // backticked
		return fmt.Sprintf("`[[%s]]`", stemOf(target))
	case 3, 4, 5: // full path (often non-canonical)
		return fmt.Sprintf("[[%s]]", strings.TrimSuffix(target, ".md"))
	case 6: // display text
		return fmt.Sprintf("[[%s|%s]]", stemOf(target), g.slug(2))
	default: // bare stem
		return fmt.Sprintf("[[%s]]", stemOf(target))
	}
}

func stemOf(p string) string {
	return strings.TrimSuffix(filepath.Base(p), ".md")
}

func (g *gen) frontmatter() string {
	var b strings.Builder
	b.WriteString("---\n")
	switch g.rnd.Intn(10) {
	case 0: // missing tags entirely (tags violation)
	case 1: // invalid prefix (tags violation)
		fmt.Fprintf(&b, "tags:\n  - bogus/%s\n", g.word())
	default:
		fmt.Fprintf(&b, "tags:\n  - %s/%s\n  - type/%s\n",
			tagPrefixes[g.rnd.Intn(len(tagPrefixes))], g.word(), g.word())
	}
	if g.rnd.Intn(10) > 1 { // created present most of the time
		fmt.Fprintf(&b, "created: 2026-%02d-%02d\n", 1+g.rnd.Intn(12), 1+g.rnd.Intn(28))
	}
	if g.rnd.Intn(5) == 0 {
		fmt.Fprintf(&b, "source: https://example.com/%s\n", g.slug(2))
	}
	b.WriteString("---\n")
	return b.String()
}

func (g *gen) body() string {
	var b strings.Builder
	sections := 2 + g.rnd.Intn(4)
	b.WriteString("# " + strings.Title(g.slug(2)) + "\n\n")
	for s := 0; s < sections; s++ {
		// Occasionally skip a heading level (heading-structure violation).
		level := "##"
		if g.rnd.Intn(8) == 0 {
			level = "####"
		}
		fmt.Fprintf(&b, "%s %s\n\n", level, strings.Title(g.slug(2)))

		paras := 1 + g.rnd.Intn(3)
		for p := 0; p < paras; p++ {
			for w := 0; w < 20+g.rnd.Intn(40); w++ {
				if g.rnd.Intn(8) == 0 {
					b.WriteString(g.wikilink())
				} else {
					b.WriteString(g.word())
				}
				b.WriteString(" ")
			}
			b.WriteString("\n\n")
		}

		switch g.rnd.Intn(6) {
		case 0: // code fence with wikilink-looking content (must NOT be flagged)
			fmt.Fprintf(&b, "```\n[[%s]] inside fence\ncode %s\n```\n\n", g.slug(2), g.slug(3))
		case 1: // table with wikilinks (table-wikilink-pipe territory)
			fmt.Fprintf(&b, "| Page | Note |\n|---|---|\n| %s | %s |\n\n", g.wikilink(), g.slug(3))
		}
	}
	return b.String()
}

func main() {
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "gen: -out is required")
		os.Exit(2)
	}

	if *profile == "wiki" {
		if err := genWiki(*out, *mult, *seed); err != nil {
			fatal(err)
		}
		return
	}
	if *profile != "corpus" {
		fmt.Fprintf(os.Stderr, "gen: unknown -profile %q (want \"corpus\" or \"wiki\")\n", *profile)
		os.Exit(2)
	}

	g := &gen{rnd: rand.New(rand.NewSource(*seed))}

	// Pre-compute all paths first so links can resolve forward and backward.
	seen := make(map[string]bool, *docs)
	for i := 0; i < *docs; i++ {
		p := g.docPath(i)
		for seen[p] {
			p = strings.TrimSuffix(p, ".md") + "-" + g.word() + ".md"
		}
		seen[p] = true
		g.paths = append(g.paths, p)
	}

	for _, p := range g.paths {
		full := filepath.Join(*out, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(full, []byte(g.frontmatter()+"\n"+g.body()), 0o644); err != nil {
			fatal(err)
		}
	}

	// Copy rule packs so the corpus is self-contained and checkable.
	// Packs may ship overlapping rule files; rule IDs must be unique across
	// packs, so the first pack (alphabetical) wins and later duplicates are
	// skipped.
	var packLines []string
	copied := make(map[string]bool)
	if *rules != "" {
		packs, err := os.ReadDir(*rules)
		if err != nil {
			fatal(err)
		}
		for _, pack := range packs {
			if !pack.IsDir() {
				continue
			}
			src := filepath.Join(*rules, pack.Name())
			dst := filepath.Join(*out, "rules", pack.Name())
			if err := os.MkdirAll(dst, 0o755); err != nil {
				fatal(err)
			}
			files, err := os.ReadDir(src)
			if err != nil {
				fatal(err)
			}
			for _, f := range files {
				if f.IsDir() || copied[f.Name()] {
					continue
				}
				copied[f.Name()] = true
				data, err := os.ReadFile(filepath.Join(src, f.Name()))
				if err != nil {
					fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dst, f.Name()), data, 0o644); err != nil {
					fatal(err)
				}
			}
			packLines = append(packLines, "  - path: rules/"+pack.Name())
		}
	}

	cfg := "version: \"0.1\"\nrule_packs:\n" + strings.Join(packLines, "\n") + `
scan:
  root: .
  max_file_size: 10485760
  skip:
    - .git
    - rules
`
	if err := os.WriteFile(filepath.Join(*out, "meridian.yaml"), []byte(cfg), 0o644); err != nil {
		fatal(err)
	}

	fmt.Printf("generated %d docs under %s (seed=%d)\n", len(g.paths), *out, *seed)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}
