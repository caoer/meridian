package chainblock

import "testing"

// refs collapses a Result to its edge ref/hash pairs — the count-and-identity
// facts the callers care about.
func refs(r Result) [][2]string {
	out := make([][2]string, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, [2]string{it.Ref, it.Hash})
	}
	return out
}

func eq(a, b [][2]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestParse_PureSequence: the plain shape — two edges, one born-null.
func TestParse_PureSequence(t *testing.T) {
	res, problem := Parse("- ref: '[[dep#Sec]]'\n  hash: abc123\n- ref: '[[#Self]]'\n  hash: null\n")
	if problem != "" {
		t.Fatalf("unexpected problem: %s", problem)
	}
	want := [][2]string{{"[[dep#Sec]]", "abc123"}, {"[[#Self]]", ""}}
	if got := refs(res); !eq(got, want) {
		t.Errorf("refs = %v, want %v", got, want)
	}
}

// TestParse_TrailingHashAlgo: the mixed shape (sequence + top-level scalar) a
// single yaml decode rejects. The sequence still parses; hash-algo is captured.
func TestParse_TrailingHashAlgo(t *testing.T) {
	res, problem := Parse("- ref: '[[dep#Sec]]'\n  hash: abc123\nhash-algo: v1  # spec version\n")
	if problem != "" {
		t.Fatalf("unexpected problem: %s", problem)
	}
	if len(res.Items) != 1 || res.Items[0].Ref != "[[dep#Sec]]" {
		t.Fatalf("items = %+v, want one [[dep#Sec]] edge", res.Items)
	}
	if res.HashAlgo != "v1" {
		t.Errorf("HashAlgo = %q, want v1 (inline comment stripped)", res.HashAlgo)
	}
}

// TestParse_ClaimBlockScalarDash is the core of the class: a dash-bulleted
// `claim: |` block scalar is prose — exactly one edge, hash on its own entry.
func TestParse_ClaimBlockScalarDash(t *testing.T) {
	content := "- ref: '[[dep#Sec]]'\n" +
		"  claim: |\n" +
		"    provides:\n" +
		"    - one\n" +
		"    - two\n" +
		"  hash: abc123\n"
	res, problem := Parse(content)
	if problem != "" {
		t.Fatalf("unexpected problem: %s", problem)
	}
	want := [][2]string{{"[[dep#Sec]]", "abc123"}}
	if got := refs(res); !eq(got, want) {
		t.Errorf("refs = %v, want %v (dash lines in a block scalar are prose)", got, want)
	}
}

// TestParse_HashPositions: the writer's surgical coordinates — the hash key's
// content line and column, bound from node position (not a raw-line regex).
func TestParse_HashPositions(t *testing.T) {
	content := "- ref: '[[dep#Sec]]'\n" +
		"  claim: |\n" +
		"    - a dash that is NOT the hash line\n" +
		"  hash: abc123\n"
	res, problem := Parse(content)
	if problem != "" {
		t.Fatalf("unexpected problem: %s", problem)
	}
	it := res.Items[0]
	if it.RefLine != 1 {
		t.Errorf("RefLine = %d, want 1", it.RefLine)
	}
	if !it.HasHash || it.HashLine != 4 || it.HashCol != 2 {
		t.Errorf("hash pos = (has=%v line=%d col=%d), want (true, 4, 2)", it.HasHash, it.HashLine, it.HashCol)
	}
}

// TestParse_BareDashFailsClosed: a top-level bare `-` is a non-mapping entry —
// the writer's fail-closed case (non-empty problem), while every well-formed
// entry parsed BEFORE it is still returned for the tolerant reader.
func TestParse_BareDashFailsClosed(t *testing.T) {
	res, problem := Parse("- ref: '[[dep#Sec]]'\n  hash: abc123\n-\n")
	if problem == "" {
		t.Fatalf("bare `-` must fail closed, got no problem (items=%+v)", res.Items)
	}
	if len(res.Items) != 1 || res.Items[0].Ref != "[[dep#Sec]]" {
		t.Errorf("items = %+v, want the one well-formed entry before the bare dash", res.Items)
	}
}

// TestParse_HashTwiceFailsClosed: a duplicate hash key is ambiguous — refuse.
func TestParse_HashTwiceFailsClosed(t *testing.T) {
	_, problem := Parse("- ref: '[[dep#Sec]]'\n  hash: abc\n  hash: def\n")
	if problem == "" {
		t.Fatal("duplicate hash must fail closed")
	}
}

// TestParse_NotASequenceFailsClosed: a mapping where a sequence is required.
func TestParse_NotASequenceFailsClosed(t *testing.T) {
	_, problem := Parse("ref: '[[dep#Sec]]'\nhash: abc\n")
	if problem == "" {
		t.Fatal("a top-level mapping must fail closed (not a sequence)")
	}
}

// TestParse_Empty: an empty block is not a problem — the caller reports "empty".
func TestParse_Empty(t *testing.T) {
	res, problem := Parse("\n  \n")
	if problem != "" || len(res.Items) != 0 {
		t.Errorf("empty content = (%q, %+v), want no problem, no items", problem, res.Items)
	}
}
