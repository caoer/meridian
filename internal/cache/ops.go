package cache

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StatsReport is the machine-readable inventory of a scan root's on-disk cache,
// aggregated across every version directory under the root-hash dir. It backs
// `md cache stats`.
type StatsReport struct {
	Path        string `json:"path"`         // the root-hash dir: <UserCacheDir>/meridian/<roothash>
	VersionDirs int    `json:"version_dirs"` // one per md version that has run against this root (Decision 10: they accumulate)
	Shards      int    `json:"shards"`       // total shard-XX.gob files across all version dirs
	Entries     int    `json:"entries"`      // total cached documents (best-effort gob decode; corrupt shard counts 0)
	Bytes       int64  `json:"bytes"`        // total shard bytes on disk
}

// CleanReport is what `md cache clean` removed: the same entry/byte totals as a
// stat, plus the path acted on. Removing an absent cache is a valid no-op
// (zero/zero), not an error.
type CleanReport struct {
	Path    string `json:"path"`
	Entries int    `json:"entries"`
	Bytes   int64  `json:"bytes"`
}

// rootHashDir resolves the root-hash directory for a scan root —
// <UserCacheDir>/meridian/<roothash>, the parent of the per-version shard dirs —
// and the meridian base it must sit directly under. It is the shared resolution
// behind stat and clean.
func rootHashDir(scanRoot string) (rootDir, meridianBase string, err error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", "", err
	}
	versionDir, err := CacheDirForRoot(scanRoot)
	if err != nil {
		return "", "", err
	}
	return filepath.Dir(versionDir), filepath.Join(base, "meridian"), nil
}

// assertSafeRootDir refuses any path that is not exactly one non-empty segment
// below <UserCacheDir>/meridian. This is the guard that makes `md cache clean`'s
// recursive removal safe: a bug or manipulated scan root that resolved the cache
// dir to "/", a home directory, or the meridian base itself must never be handed
// to os.RemoveAll. The one legal shape is meridianBase/<roothash>.
func assertSafeRootDir(rootDir, meridianBase string) error {
	rel, err := filepath.Rel(meridianBase, rootDir)
	if err != nil {
		return fmt.Errorf("refusing to act: %q is not under %q", rootDir, meridianBase)
	}
	if rel == "." || rel == "" || strings.HasPrefix(rel, "..") || strings.ContainsRune(rel, filepath.Separator) {
		return fmt.Errorf("refusing to act: %q is not a single roothash segment under %q", rootDir, meridianBase)
	}
	return nil
}

// versionDirs lists the version subdirectories under a root-hash dir. A missing
// root dir yields an empty list (no cache yet), never an error.
func versionDirs(rootDir string) ([]string, error) {
	ents, err := os.ReadDir(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	for _, e := range ents {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(rootDir, e.Name()))
		}
	}
	return dirs, nil
}

// countDir sums the shard count, entry count, and byte total for one version
// directory. Entry counting decodes each shard best-effort: a corrupt or
// undecodable shard contributes its bytes and shard count but zero entries
// (mirrors the store's corrupt-shard-is-a-miss rule).
func countDir(dir string) (shards, entries int, bytes int64) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, 0
	}
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "shard-") || !strings.HasSuffix(e.Name(), ".gob") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		shards++
		bytes += info.Size()
		entries += countShardEntries(p)
	}
	return shards, entries, bytes
}

// countShardEntries decodes one shard file and returns its entry count, or 0 on
// any error (missing, corrupt, or an entry whose Facts type is not registered in
// this process). Counting is inspection tooling — it must never fail the command.
func countShardEntries(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var m map[string]entry
	if err := gob.NewDecoder(f).Decode(&m); err != nil {
		return 0
	}
	return len(m)
}

// StatForRoot reports the on-disk cache inventory for a scan root, aggregated
// across every accumulated version directory. It is read-only and returns zeros
// (with the resolved Path) when no cache exists yet.
func StatForRoot(scanRoot string) (StatsReport, error) {
	rootDir, _, err := rootHashDir(scanRoot)
	if err != nil {
		return StatsReport{}, err
	}
	rep := StatsReport{Path: rootDir}
	dirs, err := versionDirs(rootDir)
	if err != nil {
		return StatsReport{}, err
	}
	rep.VersionDirs = len(dirs)
	for _, d := range dirs {
		s, e, b := countDir(d)
		rep.Shards += s
		rep.Entries += e
		rep.Bytes += b
	}
	return rep, nil
}

// CleanForRoot removes the ENTIRE root-hash directory for a scan root — every
// accumulated version's shards (Decision 10: clean removes all versions, there is
// no auto-prune) — after counting what it will remove. It refuses unless the
// resolved path is a single roothash segment under <UserCacheDir>/meridian
// (assertSafeRootDir). Removing an absent cache is a zero/zero no-op.
func CleanForRoot(scanRoot string) (CleanReport, error) {
	rootDir, meridianBase, err := rootHashDir(scanRoot)
	if err != nil {
		return CleanReport{}, err
	}
	if err := assertSafeRootDir(rootDir, meridianBase); err != nil {
		return CleanReport{}, err
	}

	rep := CleanReport{Path: rootDir}
	dirs, err := versionDirs(rootDir)
	if err != nil {
		return CleanReport{}, err
	}
	for _, d := range dirs {
		s, e, b := countDir(d)
		_ = s
		rep.Entries += e
		rep.Bytes += b
	}

	if err := os.RemoveAll(rootDir); err != nil {
		return CleanReport{}, err
	}
	return rep, nil
}
