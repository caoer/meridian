package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"
	"strings"

	"github.com/caoer/meridian/internal/rules"
)

// MerkleTree holds per-file combined hashes and a root hash
// derived from sorted file hashes.
type MerkleTree struct {
	RootHash   string
	FileHashes map[string]string // path → combined hash
}

// BuildTree walks the filesystem, hashes each .md file combined with
// applicable rule hashes, and computes a Merkle root.
func BuildTree(fsys fs.FS, ruleList []rules.Rule) (*MerkleTree, error) {
	// Pre-compute rule hashes.
	ruleH := make([]string, len(ruleList))
	for i, r := range ruleList {
		ruleH[i] = RuleHash(r)
	}

	fileHashes := make(map[string]string)

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil // skip unreadable
		}

		fh := FileHash(data)
		fileHashes[path] = CombinedHash(fh, ruleH)
		return nil
	})
	if err != nil {
		return nil, err
	}

	root := computeRoot(fileHashes)
	return &MerkleTree{RootHash: root, FileHashes: fileHashes}, nil
}

// Changed returns file paths whose hashes differ from previous tree.
// If previous is nil, all files are returned.
func (t *MerkleTree) Changed(previous *MerkleTree) []string {
	if previous == nil {
		return t.allPaths()
	}

	var changed []string
	for path, hash := range t.FileHashes {
		prevHash, ok := previous.FileHashes[path]
		if !ok || prevHash != hash {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

func (t *MerkleTree) allPaths() []string {
	paths := make([]string, 0, len(t.FileHashes))
	for p := range t.FileHashes {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// computeRoot hashes sorted (path + hash) pairs into a single root.
func computeRoot(fileHashes map[string]string) string {
	paths := make([]string, 0, len(fileHashes))
	for p := range fileHashes {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte(":"))
		h.Write([]byte(fileHashes[p]))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
