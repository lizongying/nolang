package main

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// mkFix 構造一條替換 span 的修復（1-based 行列，與診斷口徑一致）。
func mkFix(snippet string, sl, sc, el, ec int) parser.DiagnosticFix {
	return parser.DiagnosticFix{
		Snippet:   snippet,
		StartLine: sl, StartCol: sc,
		EndLine: el, EndCol: ec,
	}
}

func TestApplyFixesToSourceSingle(t *testing.T) {
	// 位元組列語意：Column 是 1-based 位元組列。
	src := "x = get-env('PATH')\nprint(x)\n"
	fixes := []parser.DiagnosticFix{
		mkFix("os.get-env", 1, 5, 1, 12), // 覆蓋 "get-env"
	}
	got, err := applyFixesToSource(src, fixes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "x = os.get-env('PATH')\nprint(x)\n"
	if got != want {
		t.Fatalf("single fix:\n got %q\nwant %q", got, want)
	}
}

func TestApplyFixesToSourceBackToFront(t *testing.T) {
	// 同一檔案多條不相交修復，必須從後往前套用，避免偏移錯位。
	src := "a = f()\nb = g()\n"
	fixes := []parser.DiagnosticFix{
		mkFix("m.f", 1, 5, 1, 6), // 覆蓋 "f"
		mkFix("m.g", 2, 5, 2, 6), // 覆蓋 "g"
	}
	got, err := applyFixesToSource(src, fixes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a = m.f()\nb = m.g()\n"
	if got != want {
		t.Fatalf("back-to-front:\n got %q\nwant %q", got, want)
	}
}

func TestApplyFixesToSourceMultiByteIsByteColumn(t *testing.T) {
	// Column 是位元組列：一行開頭的中文（多位元組）讓 rune 列與位元組列不同，
	// 修復仍應精準替換到 byte 偏移處。
	src := "中文 = foo()\n"
	// 位元組：中(0-2) 文(3-5) 空格(6) =(7) 空格(8) f(9) o(10) o(11) (12) )(13)
	// 故 "foo" 在 1-based 位元組列 10/11/12；half-open EndCol = 13。
	fixes := []parser.DiagnosticFix{
		mkFix("bar", 1, 10, 1, 13),
	}
	got, err := applyFixesToSource(src, fixes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "中文 = bar()\n"
	if got != want {
		t.Fatalf("multibyte byte-column:\n got %q\nwant %q", got, want)
	}
}

func TestApplyFixesToSourceRejectsOverlap(t *testing.T) {
	src := "abcde\n"
	fixes := []parser.DiagnosticFix{
		mkFix("X", 1, 1, 1, 3), // 覆蓋 "ab"
		mkFix("Y", 1, 2, 1, 4), // 覆蓋 "bc" —— 與上一條相交
	}
	_, err := applyFixesToSource(src, fixes)
	if err == nil {
		t.Fatal("expected overlap error, got nil")
	}
}

func TestApplyFixesToSourceDedupesIdentical(t *testing.T) {
	src := "get-env\n"
	fixes := []parser.DiagnosticFix{
		mkFix("os.get-env", 1, 1, 1, 8),
		mkFix("os.get-env", 1, 1, 1, 8), // 相同 span+snippet：應去重，只套用一次
	}
	got, err := applyFixesToSource(src, fixes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "os.get-env\n"
	if got != want {
		t.Fatalf("dedupe:\n got %q\nwant %q", got, want)
	}
}

func TestApplyFixesToSourceOutOfRange(t *testing.T) {
	src := "abc\n"
	fixes := []parser.DiagnosticFix{
		mkFix("X", 1, 1, 1, 99), // 超出列範圍
	}
	if _, err := applyFixesToSource(src, fixes); err == nil {
		t.Fatal("expected out-of-range error, got nil")
	}
}

func TestApplyFixesToSourceEmpty(t *testing.T) {
	src := "untouched\n"
	got, err := applyFixesToSource(src, nil)
	if err != nil || got != src {
		t.Fatalf("empty fixes should return src unchanged, got %q err %v", got, err)
	}
}
