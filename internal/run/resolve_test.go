package run

import (
	"reflect"
	"testing"
	"testing/fstest"
)

func TestResolveNote(t *testing.T) {
	fsys := fstest.MapFS{
		"notes/abc.md":         {Data: []byte("a")},
		"other/abc.md":         {Data: []byte("b")},
		"unique.md":            {Data: []byte("c")},
		"deep/dir/special.md":  {Data: []byte("d")},
		".obsidian/skipme.md":  {Data: []byte("x")},
		".git/abc.md":          {Data: []byte("x")},
		"notes/.hidden/abc.md": {Data: []byte("x")},
	}

	t.Run("multiple matches sorted", func(t *testing.T) {
		got, err := ResolveNote(fsys, "abc")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"notes/abc.md", "other/abc.md"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ResolveNote(abc) = %v, want %v (dot-dirs skipped)", got, want)
		}
	})

	t.Run("unique", func(t *testing.T) {
		got, err := ResolveNote(fsys, "unique")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{"unique.md"}) {
			t.Errorf("ResolveNote(unique) = %v", got)
		}
	})

	t.Run("path-qualified target", func(t *testing.T) {
		got, err := ResolveNote(fsys, "dir/special")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{"deep/dir/special.md"}) {
			t.Errorf("ResolveNote(dir/special) = %v", got)
		}
	})

	t.Run("no match", func(t *testing.T) {
		if _, err := ResolveNote(fsys, "missing"); err == nil {
			t.Fatal("missing note should fail")
		}
	})
}
