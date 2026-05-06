package vfs

import (
	"errors"
	"io"
	"io/fs"
	"sort"
	"testing"
)

func TestMemFS_WriteReadRoundTrip(t *testing.T) {
	m := NewMemFS()
	want := "hello world"

	if err := m.WriteFile("greeting.txt", []byte(want), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	f, err := m.Open("greeting.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestMemFS_WriteFile_CopiesData(t *testing.T) {
	m := NewMemFS()
	buf := []byte("original")
	m.WriteFile("f.txt", buf, 0644)

	// Mutate the original buffer.
	buf[0] = 'X'

	f, _ := m.Open("f.txt")
	defer f.Close()
	got, _ := io.ReadAll(f)

	if string(got) != "original" {
		t.Errorf("WriteFile did not copy data; got %q", got)
	}
}

func TestMemFS_Remove(t *testing.T) {
	m := NewMemFS()
	m.WriteFile("tmp.txt", []byte("gone"), 0644)

	if err := m.Remove("tmp.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	_, err := m.Open("tmp.txt")
	if err == nil {
		t.Fatal("Open after Remove should fail")
	}
}

func TestMemFS_RemoveNotExist(t *testing.T) {
	m := NewMemFS()

	err := m.Remove("nonexistent.txt")
	if err == nil {
		t.Fatal("Remove nonexistent should error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error = %v, want fs.ErrNotExist", err)
	}
}

func TestMemFS_Rename(t *testing.T) {
	m := NewMemFS()
	want := "content"
	m.WriteFile("old.txt", []byte(want), 0644)

	if err := m.Rename("old.txt", "new.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Old name should be gone.
	if _, err := m.Open("old.txt"); err == nil {
		t.Error("Open old name should fail after Rename")
	}

	// New name should have the content.
	f, err := m.Open("new.txt")
	if err != nil {
		t.Fatalf("Open new name: %v", err)
	}
	defer f.Close()

	got, _ := io.ReadAll(f)
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestMemFS_RenameNotExist(t *testing.T) {
	m := NewMemFS()

	err := m.Rename("ghost.txt", "dest.txt")
	if err == nil {
		t.Fatal("Rename nonexistent should error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error = %v, want fs.ErrNotExist", err)
	}
}

func TestMemFS_MkdirAll(t *testing.T) {
	m := NewMemFS()

	if err := m.MkdirAll("a/b/c", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	for _, dir := range []string{"a", "a/b", "a/b/c"} {
		info, err := fs.Stat(m, dir)
		if err != nil {
			t.Errorf("Stat(%q): %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", dir)
		}
	}
}

func TestMemFS_AddFile(t *testing.T) {
	m := NewMemFS()
	want := "quick content"

	m.AddFile("easy.txt", want)

	f, err := m.Open("easy.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	got, _ := io.ReadAll(f)
	if string(got) != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestMemFS_WalkDir(t *testing.T) {
	m := NewMemFS()
	m.AddFile("a/one.txt", "1")
	m.AddFile("a/two.txt", "2")
	m.AddFile("b/three.txt", "3")

	var found []string
	err := fs.WalkDir(m, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}

	sort.Strings(found)
	want := []string{"a/one.txt", "a/two.txt", "b/three.txt"}
	if len(found) != len(want) {
		t.Fatalf("found %v, want %v", found, want)
	}
	for i := range want {
		if found[i] != want[i] {
			t.Errorf("found[%d] = %q, want %q", i, found[i], want[i])
		}
	}
}

func TestMemFS_ReadDir(t *testing.T) {
	m := NewMemFS()
	m.AddFile("dir/alpha.txt", "a")
	m.AddFile("dir/beta.txt", "b")

	entries, err := fs.ReadDir(m, "dir")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	want := []string{"alpha.txt", "beta.txt"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

// Compile-time interface check.
var _ WriteFS = (*MemFS)(nil)
