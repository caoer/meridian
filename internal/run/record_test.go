package run

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const recordDoc = `---
md-hello: "[[note#^hello]]"
md-fail: "[[note#^fail]]"
md-fency: "[[note#^fency]]"
---

# Note

` + "```bash" + `
echo "hello stdout"
echo "hello stderr" >&2
` + "```" + `

^hello

` + "```bash" + `
echo "about to break" >&2
exit 3
` + "```" + `

^fail

` + "```bash" + `
printf '%s\n' '` + "```" + `bash' 'echo nested' '` + "```" + `'
` + "```" + `

^fency
`

// runRecorded is the test harness: run tasks with recording on, return results
// and the record path.
func runRecorded(t *testing.T, mdPath string, names []string) ([]TaskResult, string) {
	t.Helper()
	var out, errW bytes.Buffer
	results, _, err := RunTasks(mdPath, names, nil, 0, &out, &errW, Record())
	if err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	rec, err := WriteRecord(mdPath, results)
	if err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	return results, rec
}

func TestRecordCreatesSidecarAndLeavesSourceUntouched(t *testing.T) {
	mdPath := writeRepo(t, "note.md", recordDoc)
	before, _ := os.ReadFile(mdPath)

	results, rec := runRecorded(t, mdPath, []string{"hello"})

	after, _ := os.ReadFile(mdPath)
	if !bytes.Equal(before, after) {
		t.Fatal("source file mutated — record must never touch the executable doc")
	}
	want := filepath.Join(filepath.Dir(mdPath), "note.runs.md")
	if rec != want {
		t.Fatalf("record path = %s, want %s", rec, want)
	}
	data, err := os.ReadFile(rec)
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	s := string(data)
	for _, needle := range []string{"type/run-record", "hello stdout", "hello stderr", "^hello", "block_sha: "} {
		if !strings.Contains(s, needle) {
			t.Errorf("sidecar missing %q\n---\n%s", needle, s)
		}
	}
	if len(results) != 1 || results[0].Output == "" {
		t.Fatalf("expected captured output on result, got %+v", results)
	}
}

func TestRecordBlockSHAIsGitBlobID(t *testing.T) {
	// git blob id of "hi\n": echo "hi" | git hash-object --stdin
	const want = "45b983be36b73c0788dc9cbcb76cbb80fc7bb057"
	if got := BlobSHA("hi\n"); got != want {
		t.Fatalf("BlobSHA = %s, want %s", got, want)
	}
}

func TestRecordSHAMatchesExecutedBlock(t *testing.T) {
	mdPath := writeRepo(t, "note.md", recordDoc)
	results, _ := runRecorded(t, mdPath, []string{"hello"})
	b, err := FindBlock(recordDoc, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].BlockSHA != BlobSHA(b.Code) {
		t.Fatalf("BlockSHA = %s, want blob sha of block code %s", results[0].BlockSHA, BlobSHA(b.Code))
	}
}

func TestRecordRerunReplacesOnlyThatTask(t *testing.T) {
	mdPath := writeRepo(t, "note.md", recordDoc)
	runRecorded(t, mdPath, []string{"hello"})
	_, rec := runRecorded(t, mdPath, []string{"fency"})

	data, _ := os.ReadFile(rec)
	s := string(data)
	if !strings.Contains(s, "hello stdout") {
		t.Error("re-run of another task dropped hello's record")
	}
	if !strings.Contains(s, "^fency") || !strings.Contains(s, "echo nested") {
		t.Errorf("fency record missing:\n%s", s)
	}
	if strings.Count(s, "^hello") != 1 {
		t.Errorf("hello section duplicated:\n%s", s)
	}
}

func TestRecordFailedTaskStillRecorded(t *testing.T) {
	mdPath := writeRepo(t, "note.md", recordDoc)
	var out, errW bytes.Buffer
	results, _, err := RunTasks(mdPath, []string{"fail"}, nil, 0, &out, &errW, Record())
	if err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	rec, err := WriteRecord(mdPath, results)
	if err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	data, _ := os.ReadFile(rec)
	s := string(data)
	if !strings.Contains(s, "exit: 3") {
		t.Errorf("failed exit code not recorded:\n%s", s)
	}
	if !strings.Contains(s, "about to break") {
		t.Errorf("failing task's output not recorded:\n%s", s)
	}
}

func TestRecordOutputFenceSurvivesNestedFences(t *testing.T) {
	mdPath := writeRepo(t, "note.md", recordDoc)
	_, rec := runRecorded(t, mdPath, []string{"fency"})
	data, _ := os.ReadFile(rec)
	// The record must itself be block-addressable: FindBlock on the record
	// content must return exactly the fency output fence even though the
	// captured output contains a ``` fence.
	b, err := FindBlock(string(data), "fency")
	if err != nil {
		t.Fatalf("record not block-addressable: %v\n%s", err, data)
	}
	if !strings.Contains(b.Code, "echo nested") {
		t.Errorf("fency block content wrong: %q", b.Code)
	}
}

func TestRecordWithoutOptionIsByteIdenticalBehavior(t *testing.T) {
	mdPath := writeRepo(t, "note.md", recordDoc)
	var out, errW bytes.Buffer
	results, _, err := RunTasks(mdPath, []string{"hello"}, nil, 0, &out, &errW)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Output != "" || results[0].BlockSHA != "" {
		t.Fatalf("capture fields populated without Record(): %+v", results[0])
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(mdPath), "note.runs.md")); !os.IsNotExist(err) {
		t.Fatal("sidecar written without record option")
	}
}

func TestRecordTextModeStillStreamsLive(t *testing.T) {
	mdPath := writeRepo(t, "note.md", recordDoc)
	var out, errW bytes.Buffer
	_, _, err := RunTasks(mdPath, []string{"hello"}, nil, 0, &out, &errW, Record())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hello stdout") {
		t.Error("stdout no longer streams to caller writer when recording")
	}
	if !strings.Contains(errW.String(), "hello stderr") {
		t.Error("stderr no longer streams to caller writer when recording")
	}
}

func TestRecordOutputCappedWithTruncationFlag(t *testing.T) {
	doc := `---
md-spam: "[[note#^spam]]"
---

` + "```bash" + `
yes x | head -c 200000
` + "```" + `

^spam
`
	mdPath := writeRepo(t, "note.md", doc)
	var out, errW bytes.Buffer
	results, _, err := RunTasks(mdPath, []string{"spam"}, nil, 0, &out, &errW, Record())
	if err != nil {
		t.Fatal(err)
	}
	if len(results[0].Output) > recordOutputMax+1 {
		t.Fatalf("captured output not capped: %d bytes", len(results[0].Output))
	}
	if !results[0].OutputTruncated {
		t.Error("truncation not flagged")
	}
	rec, err := WriteRecord(mdPath, results)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(rec)
	if !strings.Contains(string(data), "truncated: true") {
		t.Error("truncated flag not in record frontmatter")
	}
}

func TestRecordDurationAndTimestamp(t *testing.T) {
	mdPath := writeRepo(t, "note.md", recordDoc)
	results, rec := runRecorded(t, mdPath, []string{"hello"})
	if results[0].StartedAt.IsZero() {
		t.Error("StartedAt not set")
	}
	if time.Since(results[0].StartedAt) > time.Minute {
		t.Error("StartedAt implausible")
	}
	data, _ := os.ReadFile(rec)
	s := string(data)
	if !strings.Contains(s, "duration_ms: ") || !strings.Contains(s, "at: ") {
		t.Errorf("duration/timestamp missing from record:\n%s", s)
	}
}
