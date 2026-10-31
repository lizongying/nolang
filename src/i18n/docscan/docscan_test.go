package docscan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestScanCollectsOnlySemicolonRuns pins the eligibility rule. The exclusions are
// not stylistic: the generator reads raw text, so a block comment's interior lines
// carry no `;` prefix and a `;;` line carries two. If any of them were collected,
// the block would differ from the one the LSP sees in the parsed Doc group.
func TestScanCollectsOnlySemicolonRuns(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.no", strings.Join([]string{
		"; counted: a plain semicolon doc line",
		"; [counted](#a.counted)",
		";; not counted: double semicolon",
		"; [skipped](#a.skipped)",
		"plain = () () {",
		"    s = '[x](#nope) is not a comment here'",
		"}",
	}, "\n"))

	entries, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 1 {
		for _, e := range entries {
			t.Logf("collected #%s %s:%d %q", e.Anchor, e.File, e.Line, e.Text)
		}
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Anchor != "a.counted" || entries[0].Text != "counted: a plain semicolon doc line" {
		t.Fatalf("entry = %+v, want anchor a.counted with the prose above it", entries[0])
	}
	if entries[0].Line != 2 {
		t.Fatalf("Line = %d, want 2 (the link line)", entries[0].Line)
	}
}

// TestScanMultiLineBlock proves the block is every line above the link, not just
// the one before it. This is the whole reason the marker is a separate last line:
// a two-sentence paragraph translates as one unit.
func TestScanMultiLineBlock(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "b.no", strings.Join([]string{
		"; get: 獲取第 i 個參數",
		"; 委派至 os.arg(i) builtin（讀取 main 的 argv[i]）；越界回傳 nil",
		"; [get](#args.get)",
		"get = (i i64) (val ?str) {",
		"    val = os.arg(i)",
		"}",
	}, "\n"))
	entries, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	want := "get: 獲取第 i 個參數\n委派至 os.arg(i) builtin（讀取 main 的 argv[i]）；越界回傳 nil"
	if entries[0].Text != want {
		t.Fatalf("Text = %q, want %q", entries[0].Text, want)
	}
}

// TestScanRequiresCodeBelow: a `;` run followed by a blank line is a
// free-standing note, not a doc comment, so the parser never hangs it on a node
// and the LSP would never look it up. Collecting it would create a catalog entry
// nothing reads.
func TestScanRequiresCodeBelow(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "c.no", strings.Join([]string{
		"; detached note",
		"; [note](#a.note)",
		"",
		"x = 1",
	}, "\n"))
	entries, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %+v, want no entries", entries)
	}
}

// TestScanIgnoresExternalLinks: `[docs](https://...)` is prose a human wrote for
// a human. Treating it as a key would silently replace the sentence.
func TestScanIgnoresExternalLinks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "d.no", strings.Join([]string{
		"; see the manual",
		"; [docs](https://example.com/manual)",
		"x = 1",
	}, "\n"))
	entries, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("got %+v, want no entries", entries)
	}
}

// TestTextsDedupesIdenticalBlocks: the same paragraph under one anchor in two
// files is one message, not a conflict.
func TestTextsDedupesIdenticalBlocks(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "e.no", "; same sentence\n; [same](#a.same)\nx = 1\n")
	write(t, dir, "sub/f.no", "; same sentence\n; [same](#a.same)\nx = 1\n")
	entries, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	m, err := Texts(entries)
	if err != nil {
		t.Fatalf("Texts: %v", err)
	}
	if len(entries) != 2 || len(m) != 1 {
		t.Fatalf("entries=%d map=%d, want 2 and 1", len(entries), len(m))
	}
}

// TestTextsRejectsAnchorCollision is the falsification case: one anchor reused for
// different prose must fail loudly. A translation is keyed by the anchor, so the
// two blocks would otherwise silently share one translation and one would be wrong
// with no signal anywhere.
func TestTextsRejectsAnchorCollision(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "g.no", "; first\n; [x](#a.dup)\nx = 1\n")
	write(t, dir, "h.no", "; second\n; [x](#a.dup)\nx = 1\n")
	entries, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, err := Texts(entries); err == nil {
		t.Fatal("Texts accepted one anchor with two texts; it must fail")
	}
}

// TestScanSkipsVendorDirs keeps the walk out of generated output: a stale copy
// under bin/ or dist/ would contribute keys that no source line actually has.
func TestScanSkipsVendorDirs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bin/old.no", "; stale\n; [x](#a.stale)\nx = 1\n")
	write(t, dir, "dist/old.no", "; stale\n; [x](#a.stale)\nx = 1\n")
	write(t, dir, "keep.no", "; fresh\n; [x](#a.fresh)\nx = 1\n")
	entries, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 1 || entries[0].Text != "fresh" {
		t.Fatalf("got %+v, want only the keep.no entry", entries)
	}
}
