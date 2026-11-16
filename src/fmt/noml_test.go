package fmt

import (
	"strings"
	"testing"
)

func TestNomlFmtBasicShape(t *testing.T) {
	src := `{
     name:   'x'
  compiler: {
        version: 'v0.1.0'
     link-libs: []
  }
}
`
	want := `{
  name: 'x'
  compiler: {
    version: 'v0.1.0'
    link-libs: []
  }
}
`
	got, probs := FormatNomlFile(src)
	if len(probs) > 0 {
		t.Fatalf("unexpected problems: %+v", probs)
	}
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNomlFmtKeepsCommentsAndKeyOrder(t *testing.T) {
	src := `; 頂部說明
;   - 縮進的子項
{
    zeta: 1
    ; 中間註解
    alpha: 2
}
`
	// 鍵序不動（zeta 在 alpha 之前），註釋行保持原縮進。
	want := `; 頂部說明
;   - 縮進的子項
{
  zeta: 1
    ; 中間註解
  alpha: 2
}
`
	got, probs := FormatNomlFile(src)
	if len(probs) > 0 {
		t.Fatalf("unexpected problems: %+v", probs)
	}
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// 塊註釋的內文逐位元組保留：內容是散文，重縮進等於改資料。
func TestNomlFmtBlockCommentBodyUntouched(t *testing.T) {
	src := `{
      ;;
  這行刻意不縮進
        這行刻意多縮進
      ;;
  a: 1
}
`
	// 定界符與內文都原樣：只有成員 a 的縮進被規範化。
	want := `{
      ;;
  這行刻意不縮進
        這行刻意多縮進
      ;;
  a: 1
}
`
	got, _ := FormatNomlFile(src)
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// 原始串內容同樣逐位元組保留（縮進在原始串裡是資料）。
func TestNomlFmtRawStringBodyUntouched(t *testing.T) {
	src := "{\n" +
		"    definition: `\n" +
		"未縮進的散文\n" +
		"    四空格縮進的程式碼\n" +
		"`\n" +
		"    other: 1\n" +
		"}\n"
	want := "{\n" +
		"  definition: `\n" +
		"未縮進的散文\n" +
		"    四空格縮進的程式碼\n" +
		"`\n" +
		"  other: 1\n" +
		"}\n"
	got, probs := FormatNomlFile(src)
	if len(probs) > 0 {
		t.Fatalf("unexpected problems: %+v", probs)
	}
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

// 字串裡的括號不能影響縮進計算。
func TestNomlFmtBracesInsideStringsIgnored(t *testing.T) {
	src := `{
  a: '}'
  b: '{ ['
  c: 'x; y'
  d: 'a:b'
}
`
	got, probs := FormatNomlFile(src)
	if len(probs) > 0 {
		t.Fatalf("unexpected problems: %+v", probs)
	}
	if got != src {
		t.Fatalf("got:\n%s\nwant unchanged:\n%s", got, src)
	}
}

func TestNomlFmtMultilineArrayIndent(t *testing.T) {
	src := `{
    deps: [
        'a',
          'b',
    ]
      tail: 1
}
`
	want := `{
  deps: [
    'a',
    'b',
  ]
  tail: 1
}
`
	got, _ := FormatNomlFile(src)
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNomlFmtBlankLinesAndTrailingNewline(t *testing.T) {
	src := "\n\n{\n  a: 1\n\n\n\n  b: 2\n}\n\n\n"
	want := "{\n  a: 1\n\n  b: 2\n}\n"
	got, _ := FormatNomlFile(src)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNomlFmtTrailingWhitespace(t *testing.T) {
	src := "{\n  a: 1   \n  b: 2\t\n}\n"
	want := "{\n  a: 1\n  b: 2\n}\n"
	got, _ := FormatNomlFile(src)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// 注解行 `#{...}` 跟著成員同樣縮進。
func TestNomlFmtAnnotationIndent(t *testing.T) {
	src := `{
      #{type = '[]str'}
    tags: []
}
`
	want := `{
  #{type = '[]str'}
  tags: []
}
`
	got, _ := FormatNomlFile(src)
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// 已有檔案必須是固定點：格式化器對規範輸入只能原樣返回。
func TestNomlFmtIdempotent(t *testing.T) {
	src := "; 說明\n{\n  name: 'x'\n  terms: {\n    'a-b': {\n" +
		"      title: '標題'\n      list: [\n        1,\n        2,\n      ]\n" +
		"      body: `\n散文\n      `\n    }\n  }\n}\n"
	once, probs := FormatNomlFile(src)
	if len(probs) > 0 {
		t.Fatalf("unexpected problems: %+v", probs)
	}
	if once != src {
		t.Fatalf("formatter is not a fixed point:\ngot:\n%s\nwant:\n%s", once, src)
	}
	twice, _ := FormatNomlFile(once)
	if twice != once {
		t.Fatalf("second pass changed the output:\n%s\n---\n%s", once, twice)
	}
}

func TestNomlFmtUnterminatedBlockComment(t *testing.T) {
	src := "{\n  a: 1\n  ;;\n  未閉合\n"
	got, probs := FormatNomlFile(src)
	if len(probs) == 0 {
		t.Fatal("unterminated block comment must be reported")
	}
	if got != src {
		t.Fatalf("output must fall back to the source, got %q", got)
	}
	if !strings.Contains(probs[0].Reason, "block comment") {
		t.Errorf("reason = %q", probs[0].Reason)
	}
}

func TestNomlFmtUnterminatedRawString(t *testing.T) {
	src := "{\n  a: `\n未閉合\n"
	got, probs := FormatNomlFile(src)
	if len(probs) == 0 {
		t.Fatal("unterminated raw string must be reported")
	}
	if got != src {
		t.Fatalf("output must fall back to the source, got %q", got)
	}
}

// `;; 有內容` 是行註解，不是塊註釋開始：後續行必須照常格式化。
func TestNomlFmtDoubleSemicolonWithContentIsLineComment(t *testing.T) {
	src := `{
      ;; 只是行註解
    a: 1
}
`
	// 註釋行的縮進原樣保留（見 src/fmt/noml.go 檔頭），所以 `;; 只是行註解`
	// 仍停在原來的 6 個空格；關鍵是它**不是**塊註釋開始，a 沒有被吃掉。
	want := `{
      ;; 只是行註解
  a: 1
}
`
	got, probs := FormatNomlFile(src)
	if len(probs) > 0 {
		t.Fatalf("unexpected problems: %+v", probs)
	}
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestNomlFmtEmptyAndBlankOnly(t *testing.T) {
	for _, src := range []string{"", "\n", "\n\n\n"} {
		got, probs := FormatNomlFile(src)
		if len(probs) > 0 {
			t.Fatalf("%q: unexpected problems: %+v", src, probs)
		}
		if got != "" {
			t.Fatalf("%q: got %q, want empty", src, got)
		}
	}
}

func TestIsNomlPath(t *testing.T) {
	cases := map[string]bool{
		"a.noml":           true,
		"pkg/package.noml": true,
		"x.NOML":           true,
		"a.no":             false,
		"a.noml.bak":       false,
		"noml":             false,
		"dir.noml/a.txt":   false,
	}
	for p, want := range cases {
		if got := IsNomlPath(p); got != want {
			t.Errorf("IsNomlPath(%q) = %v, want %v", p, got, want)
		}
	}
}
