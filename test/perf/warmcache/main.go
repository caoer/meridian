// Command warmcache measures the U7 persistent fact cache on a corpus: one cold
// run populates and saves the store, then N warm runs reopen it (simulating fresh
// processes) and report hit-rate and duration. The CLI still uses an in-memory
// store (NewStore("")) until U8 wires the persistent path, so this harness is how
// U7's warm numbers are measured. Usage:
//
//	go run ./test/perf/warmcache -corpus /tmp/wiki10x [-runs 3] [-cache <dir>]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caoer/meridian/internal/cache"
	"github.com/caoer/meridian/internal/checks"
	"github.com/caoer/meridian/internal/config"
	"github.com/caoer/meridian/internal/contract"
	"github.com/caoer/meridian/internal/engine"
	"github.com/caoer/meridian/internal/rules"
)

func main() {
	corpus := flag.String("corpus", "", "corpus root (contains meridian.yaml)")
	runs := flag.Int("runs", 3, "number of warm runs (report the fastest)")
	cacheDir := flag.String("cache", "", "cache dir (default: a temp dir)")
	flag.Parse()
	if *corpus == "" {
		fmt.Fprintln(os.Stderr, "usage: warmcache -corpus <dir> [-runs N] [-cache <dir>]")
		os.Exit(2)
	}

	cfg, loadedRules := loadConfig(*corpus)
	dir := *cacheDir
	if dir == "" {
		d, err := os.MkdirTemp("", "warmcache-*")
		must(err)
		dir = d
		defer os.RemoveAll(dir)
	}

	fsRoot := os.DirFS(cfg.Scan.Root)

	// Cold: populate + persist.
	cold := cache.NewStore(dir)
	t0 := time.Now()
	coldFindings := newEngine(cfg, loadedRules).RunCached(fsRoot, loadedRules, cold)
	coldMs := time.Since(t0).Milliseconds()
	must(cold.Save())
	cs := cold.Stats()

	// Warm: reopen the store each run (fresh-process simulation), keep the best.
	var best time.Duration
	var warmHits, warmTotal int
	var warmFindings int
	for i := 0; i < *runs; i++ {
		warm := cache.NewStore(dir)
		t := time.Now()
		f := newEngine(cfg, loadedRules).RunCached(fsRoot, loadedRules, warm)
		d := time.Since(t)
		if i == 0 || d < best {
			best = d
		}
		st := warm.Stats()
		warmHits, warmTotal, warmFindings = st.Hits, st.Total, len(f)
	}

	hitRate := 0.0
	if warmTotal > 0 {
		hitRate = 100 * float64(warmHits) / float64(warmTotal)
	}
	fmt.Printf("corpus:       %s\n", cfg.Scan.Root)
	fmt.Printf("cache dir:    %s\n", dir)
	fmt.Printf("cold run:     %d ms  (%d findings, %d/%d hit)\n", coldMs, len(coldFindings), cs.Hits, cs.Total)
	fmt.Printf("warm run:     %d ms  (min of %d)\n", best.Milliseconds(), *runs)
	fmt.Printf("warm hits:    %d/%d = %.1f%%  (%d findings)\n", warmHits, warmTotal, hitRate, warmFindings)
	fmt.Printf("cache bytes:  %d\n", shardBytes(dir))
}

func loadConfig(corpus string) (*config.Config, []rules.Rule) {
	cfgPath, err := config.Discover(corpus)
	must(err)
	data, err := os.ReadFile(cfgPath)
	must(err)
	cfg, err := config.Parse(data, filepath.Dir(cfgPath))
	must(err)

	var loaded []rules.Rule
	if len(cfg.RulePacks) > 0 {
		for _, pack := range cfg.RulePacks {
			rs, _, err := rules.LoadDir(pack.Path)
			must(err)
			loaded = append(loaded, rs...)
		}
	} else {
		rs, _, err := rules.LoadFS(contract.FS(), ".")
		must(err)
		loaded = append(loaded, rs...)
	}
	if cfg.DefaultProfile != "" {
		if p, ok := cfg.Profiles[cfg.DefaultProfile]; ok {
			loaded = p.Resolve(loaded)
		}
	}
	return cfg, loaded
}

func newEngine(cfg *config.Config, _ []rules.Rule) *engine.Engine {
	eng := engine.New()
	eng.SetSkip(cfg.Scan.Skip)
	eng.SetMaxFileSize(cfg.Scan.MaxFileSize)
	eng.SetForeignRoots(cfg.ForeignRoots)
	eng.SetScanRoot(cfg.Scan.Root)
	for name, fn := range checks.All {
		eng.RegisterCheck(name, fn)
	}
	return eng
}

func shardBytes(dir string) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".gob" {
			if info, err := e.Info(); err == nil {
				total += info.Size()
			}
		}
	}
	return total
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
