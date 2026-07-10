// Command warmcache measures the U7 persistent fact cache on a corpus across the
// three store modes, isolating the persistence cost:
//
//	-store nil   no cache        (pure parallel cold eval — the no-regression floor)
//	-store mem   NewStore("")    (in-memory; the current CLI default)
//	-store disk  persistent      (populate + Save, then reopen for warm runs)
//
// The CLI uses -store mem today; -store disk is the U8 shape. Usage:
//
//	go run ./test/perf/warmcache -corpus /tmp/wiki10x -store disk -runs 3
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	mode := flag.String("store", "disk", "store mode: nil | mem | disk")
	runs := flag.Int("runs", 3, "min-of-N")
	cacheDir := flag.String("cache", "", "cache dir for disk mode (default: temp)")
	flag.Parse()
	if *corpus == "" {
		fmt.Fprintln(os.Stderr, "usage: warmcache -corpus <dir> [-store nil|mem|disk] [-runs N]")
		os.Exit(2)
	}

	cfg, loadedRules := loadConfig(*corpus)
	fsRoot := os.DirFS(cfg.Scan.Root)
	newStore, diskDir := storeFactory(*mode, *cacheDir)
	fmt.Fprintf(os.Stderr, "[debug] GOMAXPROCS=%d rules=%d skip=%v maxfilesize=%d root=%s\n",
		runtime.GOMAXPROCS(0), len(loadedRules), cfg.Scan.Skip, cfg.Scan.MaxFileSize, cfg.Scan.Root)

	// Cold: min-of-N. Each run gets a fresh store (disk mode wipes its dir) so
	// every cold run is a genuine cold population. Save is timed separately.
	var coldBest, saveBest time.Duration
	var coldFindings int
	for i := 0; i < *runs; i++ {
		st := newStore(true) // fresh
		t := time.Now()
		f := newEngine(cfg).RunCached(fsRoot, loadedRules, st)
		d := time.Since(t)
		var sd time.Duration
		if st != nil {
			ts := time.Now()
			must(st.Save())
			sd = time.Since(ts)
		}
		fmt.Fprintf(os.Stderr, "[debug] cold run %d: eval=%dms save=%dms\n", i, d.Milliseconds(), sd.Milliseconds())
		if i == 0 || d < coldBest {
			coldBest = d
		}
		if i == 0 || sd < saveBest {
			saveBest = sd
		}
		coldFindings = len(f)
	}

	fmt.Printf("corpus:      %s\n", cfg.Scan.Root)
	fmt.Printf("store mode:  %s\n", *mode)
	fmt.Printf("cold eval:   %d ms  (min of %d, %d findings)\n", coldBest.Milliseconds(), *runs, coldFindings)
	if *mode == "disk" {
		fmt.Printf("save:        %d ms  (min of %d)\n", saveBest.Milliseconds(), *runs)
		fmt.Printf("cold total:  %d ms  (eval + save)\n", (coldBest + saveBest).Milliseconds())

		// Warm: reopen the persisted store each run (fresh-process simulation).
		var warmBest time.Duration
		var hits, total int
		for i := 0; i < *runs; i++ {
			st := newStore(false) // reopen, keep on-disk shards
			t := time.Now()
			newEngine(cfg).RunCached(fsRoot, loadedRules, st)
			d := time.Since(t)
			if i == 0 || d < warmBest {
				warmBest = d
			}
			s := st.Stats()
			hits, total = s.Hits, s.Total
		}
		rate := 0.0
		if total > 0 {
			rate = 100 * float64(hits) / float64(total)
		}
		fmt.Printf("warm:        %d ms  (min of %d)\n", warmBest.Milliseconds(), *runs)
		fmt.Printf("warm hits:   %d/%d = %.1f%%\n", hits, total, rate)
		fmt.Printf("cache bytes: %d\n", shardBytes(diskDir))
	}
}

// storeFactory returns a constructor and (for disk mode) the shard directory.
// fresh=true wipes disk state for a cold run; fresh=false reopens the existing
// shards (warm). nil/mem ignore fresh.
func storeFactory(mode, cacheDir string) (func(fresh bool) *cache.Store, string) {
	switch mode {
	case "nil":
		return func(bool) *cache.Store { return nil }, ""
	case "mem":
		return func(bool) *cache.Store { return cache.NewStore("") }, ""
	case "disk":
		dir := cacheDir
		if dir == "" {
			d, err := os.MkdirTemp("", "warmcache-*")
			must(err)
			dir = d
		}
		return func(fresh bool) *cache.Store {
			if fresh {
				os.RemoveAll(dir)
				must(os.MkdirAll(dir, 0o700))
			}
			return cache.NewStore(dir)
		}, dir
	default:
		fmt.Fprintln(os.Stderr, "unknown -store mode:", mode)
		os.Exit(2)
		return nil, ""
	}
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

func newEngine(cfg *config.Config) *engine.Engine {
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
