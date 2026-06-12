package run

import "testing"

func TestParseWikilink(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    Wikilink
		wantErr bool
	}{
		{"bare note", "[[file]]", Wikilink{Target: "file"}, false},
		{"block ref", "[[file#^id]]", Wikilink{Target: "file", BlockID: "id"}, false},
		{"heading ref", "[[file#Heading]]", Wikilink{Target: "file", Heading: "Heading"}, false},
		{"subdir target", "[[dir/file#^block-1]]", Wikilink{Target: "dir/file", BlockID: "block-1"}, false},
		{"alias stripped", "[[file|alias]]", Wikilink{Target: "file"}, false},
		{"block ref with alias", "[[file#^id|alias]]", Wikilink{Target: "file", BlockID: "id"}, false},
		{"same-file heading", "[[#Heading]]", Wikilink{Heading: "Heading"}, false},
		{"same-file block", "[[#^id]]", Wikilink{BlockID: "id"}, false},
		{"whitespace tolerated", "  [[file#^id]]  ", Wikilink{Target: "file", BlockID: "id"}, false},
		{"not a wikilink", "file#^id", Wikilink{}, true},
		{"unclosed", "[[file", Wikilink{}, true},
		{"empty", "", Wikilink{}, true},
		{"invalid block id chars", "[[file#^bad id]]", Wikilink{}, true},
		{"empty block id", "[[file#^]]", Wikilink{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseWikilink(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseWikilink(%q) = %+v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseWikilink(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseWikilink(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}
