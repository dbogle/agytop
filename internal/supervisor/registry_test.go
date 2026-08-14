package supervisor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadLastLines(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test log: %v", err)
	}

	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("failed to open log: %v", err)
	}
	defer f.Close()

	lines, size, err := readLastLines(f, 3)
	if err != nil {
		t.Fatalf("readLastLines failed: %v", err)
	}

	expected := []string{"line 3", "line 4", "line 5"}
	if !reflect.DeepEqual(lines, expected) {
		t.Errorf("expected %v, got %v", expected, lines)
	}
	if size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), size)
	}
}

// Forces multiple backward ReadAt calls (~250KB > the 64KB tailChunkSize), so
// the partial-first-line drop is exercised and the first returned line is
// asserted to be complete rather than truncated.
func TestReadLastLinesAcrossChunkBoundary(t *testing.T) {
	var buf bytes.Buffer
	const totalLines = 5000
	for i := 0; i < totalLines; i++ {
		fmt.Fprintf(&buf, "line %04d %s\n", i, strings.Repeat("x", 40))
	}

	logFile := filepath.Join(t.TempDir(), "big.log")
	if err := os.WriteFile(logFile, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	lines, size, err := readLastLines(f, 3)
	if err != nil {
		t.Fatalf("readLastLines: %v", err)
	}
	if size != int64(buf.Len()) {
		t.Errorf("size = %d, want %d", size, buf.Len())
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for i, got := range lines {
		want := fmt.Sprintf("line %04d %s", totalLines-3+i, strings.Repeat("x", 40))
		if got != want {
			t.Errorf("line %d = %q, want %q", i, got, want)
		}
	}
}

// An empty log must tail to nothing, not to a single blank line.
func TestReadLastLinesEmptyFile(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(logFile, nil, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	lines, size, err := readLastLines(f, 10)
	if err != nil {
		t.Fatalf("readLastLines: %v", err)
	}
	if size != 0 {
		t.Errorf("size = %d, want 0", size)
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines (%q), want 0", len(lines), lines)
	}
}

// A file with no trailing newline must still return its last line complete,
// not silently dropped or truncated by the TrimRight/Split logic.
func TestReadLastLinesNoTrailingNewline(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "no-trailing-newline.log")
	content := "alpha\nbravo\ncharlie"
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	lines, size, err := readLastLines(f, 10)
	if err != nil {
		t.Fatalf("readLastLines: %v", err)
	}
	expected := []string{"alpha", "bravo", "charlie"}
	if !reflect.DeepEqual(lines, expected) {
		t.Errorf("expected %v, got %v", expected, lines)
	}
	if size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), size)
	}
}

// A file of nothing but newlines has no content to report, so it tails to
// nothing rather than to a run of blank lines. This pins the second half of
// the documented empty-content contract, which TrimRight collapses onto the
// same path as a 0-byte file.
func TestReadLastLinesOnlyNewlines(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "blank.log")
	content := "\n\n\n"
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	lines, size, err := readLastLines(f, 10)
	if err != nil {
		t.Fatalf("readLastLines: %v", err)
	}
	if size != int64(len(content)) {
		t.Errorf("size = %d, want %d", size, len(content))
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines (%q), want 0", len(lines), lines)
	}
}

// Requesting more lines than the file contains must return everything
// available, unpadded, without panicking on the length arithmetic.
func TestReadLastLinesMaxLinesExceedsAvailable(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "short.log")
	content := "only\ntwo lines\n"
	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	lines, size, err := readLastLines(f, 100)
	if err != nil {
		t.Fatalf("readLastLines: %v", err)
	}
	expected := []string{"only", "two lines"}
	if !reflect.DeepEqual(lines, expected) {
		t.Errorf("expected %v, got %v", expected, lines)
	}
	if size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), size)
	}
}
