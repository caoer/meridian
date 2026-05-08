package cache

import (
	"testing"
	"testing/fstest"

	"github.com/caoer/meridian/internal/rules"
)

func testRules() []rules.Rule {
	return []rules.Rule{
		{
			ID:       "r1",
			Check:    "frontmatter_exists",
			Severity: rules.SeverityError,
			On:       rules.OnFilter{Paths: []string{"**/*.md"}},
			Params:   map[string]any{"field": "title"},
		},
	}
}

func TestBuildTree_IdenticalFS(t *testing.T) {
	fs := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
		"b.md": {Data: []byte("world")},
	}
	rl := testRules()

	t1, err := BuildTree(fs, rl)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := BuildTree(fs, rl)
	if err != nil {
		t.Fatal(err)
	}
	if t1.RootHash != t2.RootHash {
		t.Fatalf("identical FS produced different root hashes: %s vs %s", t1.RootHash, t2.RootHash)
	}
}

func TestBuildTree_FileChanged(t *testing.T) {
	fs1 := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
		"b.md": {Data: []byte("world")},
	}
	fs2 := fstest.MapFS{
		"a.md": {Data: []byte("changed")},
		"b.md": {Data: []byte("world")},
	}
	rl := testRules()

	t1, _ := BuildTree(fs1, rl)
	t2, _ := BuildTree(fs2, rl)

	if t1.RootHash == t2.RootHash {
		t.Fatal("changed file should change root hash")
	}
}

func TestBuildTree_FileAdded(t *testing.T) {
	fs1 := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
	}
	fs2 := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
		"c.md": {Data: []byte("new")},
	}
	rl := testRules()

	t1, _ := BuildTree(fs1, rl)
	t2, _ := BuildTree(fs2, rl)

	if t1.RootHash == t2.RootHash {
		t.Fatal("added file should change root hash")
	}
}

func TestBuildTree_FileRemoved(t *testing.T) {
	fs1 := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
		"b.md": {Data: []byte("world")},
	}
	fs2 := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
	}
	rl := testRules()

	t1, _ := BuildTree(fs1, rl)
	t2, _ := BuildTree(fs2, rl)

	if t1.RootHash == t2.RootHash {
		t.Fatal("removed file should change root hash")
	}
}

func TestChanged_IdenticalFS(t *testing.T) {
	fs := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
		"b.md": {Data: []byte("world")},
	}
	rl := testRules()

	t1, _ := BuildTree(fs, rl)
	t2, _ := BuildTree(fs, rl)

	changed := t2.Changed(t1)
	if len(changed) != 0 {
		t.Fatalf("identical FS should have no changes, got: %v", changed)
	}
}

func TestChanged_OneFileModified(t *testing.T) {
	fs1 := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
		"b.md": {Data: []byte("world")},
	}
	fs2 := fstest.MapFS{
		"a.md": {Data: []byte("changed")},
		"b.md": {Data: []byte("world")},
	}
	rl := testRules()

	t1, _ := BuildTree(fs1, rl)
	t2, _ := BuildTree(fs2, rl)

	changed := t2.Changed(t1)
	if len(changed) != 1 || changed[0] != "a.md" {
		t.Fatalf("expected [a.md] changed, got: %v", changed)
	}
}

func TestChanged_NewFile(t *testing.T) {
	fs1 := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
	}
	fs2 := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
		"b.md": {Data: []byte("new")},
	}
	rl := testRules()

	t1, _ := BuildTree(fs1, rl)
	t2, _ := BuildTree(fs2, rl)

	changed := t2.Changed(t1)
	if len(changed) != 1 || changed[0] != "b.md" {
		t.Fatalf("expected [b.md] changed, got: %v", changed)
	}
}

func TestChanged_SubdirectoryUnchanged(t *testing.T) {
	fs1 := fstest.MapFS{
		"dir/a.md":  {Data: []byte("hello")},
		"dir/b.md":  {Data: []byte("world")},
		"root.md":   {Data: []byte("root")},
	}
	fs2 := fstest.MapFS{
		"dir/a.md":  {Data: []byte("hello")},
		"dir/b.md":  {Data: []byte("world")},
		"root.md":   {Data: []byte("changed root")},
	}
	rl := testRules()

	t1, _ := BuildTree(fs1, rl)
	t2, _ := BuildTree(fs2, rl)

	changed := t2.Changed(t1)
	// Only root.md changed; dir/ subtree untouched.
	if len(changed) != 1 || changed[0] != "root.md" {
		t.Fatalf("expected [root.md] changed, got: %v", changed)
	}
}

func TestChanged_NilPrevious(t *testing.T) {
	fs := fstest.MapFS{
		"a.md": {Data: []byte("hello")},
	}
	rl := testRules()

	tree, _ := BuildTree(fs, rl)
	changed := tree.Changed(nil)
	// All files should be "changed" when no previous tree.
	if len(changed) != 1 || changed[0] != "a.md" {
		t.Fatalf("nil previous should mark all files changed, got: %v", changed)
	}
}

func TestBuildTree_NonMdFilesIgnored(t *testing.T) {
	fs := fstest.MapFS{
		"a.md":  {Data: []byte("hello")},
		"b.txt": {Data: []byte("ignored")},
	}
	rl := testRules()

	tree, err := BuildTree(fs, rl)
	if err != nil {
		t.Fatal(err)
	}
	// Only .md files in the tree.
	if _, ok := tree.FileHashes["b.txt"]; ok {
		t.Fatal("non-.md file should not be in tree")
	}
	if _, ok := tree.FileHashes["a.md"]; !ok {
		t.Fatal("a.md should be in tree")
	}
}
