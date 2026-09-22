package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFixRedundantTypeInFile 回歸：no fmt --fix=redundant 只能移除「標註型別 == 推斷型別」
// 的型別標註；隱式轉換（如 i16 <- i64 字面量）、無標註、跨型別（?i64 / txt 等）一律保留。
func TestFixRedundantTypeInFile(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// want == "" 表示期望「無可移除」（ok=false，呼叫方保留原檔）；
		// 否則表示期望移除後的結果且 ok=true。
		want string
	}{
		{
			name: "named type equal to inferred (str)",
			src:  "main = () () {\n  a str = ''\n  b i64 = 5\n}\n",
			want: "main = () () {\n  a = ''\n  b = 5\n}\n",
		},
		{
			name: "named type equal to inferred (function call)",
			src:  "main = () () {\n  e str = greet()\n  f i64 = getv()\n}\n" +
				"greet = () (s str) {\n  s = 'hi'\n}\n" +
				"getv = () (v i64) {\n  v = 7\n}\n",
			want: "main = () () {\n  e = greet()\n  f = getv()\n}\n" +
				"greet = () (s str) {\n  s = 'hi'\n}\n" +
				"getv = () (v i64) {\n  v = 7\n}\n",
		},
		{
			name: "map type equal to inferred",
			src:  "main = () () {\n  m [str]i64 = make-map()\n}\n" +
				"make-map = () (r [str]i64) {\n  r = [:]\n}\n",
			want: "main = () () {\n  m = make-map()\n}\n" +
				"make-map = () (r [str]i64) {\n  r = [:]\n}\n",
		},
		{
			name: "hyphenated var name",
			src:  "main = () () {\n  last-array str = ''\n}\n",
			want: "main = () () {\n  last-array = ''\n}\n",
		},
		{
			name: "implicit numeric conversion preserved (i16 <- i64 literal)",
			src:  "main = () () {\n  c i16 = 5\n}\n",
			want: "",
		},
		{
			name: "no annotation preserved",
			src:  "main = () () {\n  d = 'x'\n}\n",
			want: "",
		},
		{
			name: "nullable preserved (?i64 <- nil)",
			src:  "main = () () {\n  p ?i64 = nil\n}\n",
			want: "",
		},
		// 回歸：可空型別 ?T 的 NamedType.Pos() 指向 `?` 之後的 T，移除時必須連 `?`
		// 一起吃掉，否則會留下懸空 `?`（`c ?conn = f()` -> `c ? = f()`，語法錯）。
		{
			name: "nullable equal to inferred (?i64 <- fn returning ?i64)",
			src:  "main = () () {\n  v ?i64 = maybe()\n}\n" +
				"maybe = () (r ?i64) {\n  r = 5\n}\n",
			want: "main = () () {\n  v = maybe()\n}\n" +
				"maybe = () (r ?i64) {\n  r = 5\n}\n",
		},
		{
			name: "nullable equal to inferred (?str <- fn returning ?str)",
			src:  "main = () () {\n  w ?str = maybe-s()\n}\n" +
				"maybe-s = () (r ?str) {\n  r = 'x'\n}\n",
			want: "main = () () {\n  w = maybe-s()\n}\n" +
				"maybe-s = () (r ?str) {\n  r = 'x'\n}\n",
		},
		// 回歸：linter 把十六進位字面量推斷為 byte（啟發式），與編譯器「預設 i64」不一致，
		// 故 `t [4]byte = [0x01, ...]` 會被誤判冗餘；移除後字面量退化成 i64 元素而壞掉。
		// 此處必須保守保留標註。
		{
			name: "hex literal array preserved (annotation load-bearing)",
			src:  "main = () () {\n  t [4]byte = [0x01, 0x02, 0x03, 0x04]\n}\n",
			want: "",
		},
		{
			name: "hex literal slice preserved (annotation load-bearing)",
			src:  "main = () () {\n  s []byte = [0x50, 0x4b, 0x03, 0x04]\n}\n",
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "t-redundant.no")
			if err := os.WriteFile(p, []byte(c.src), 0644); err != nil {
				t.Fatal(err)
			}
			got, ok := fixRedundantTypeInFile(p)
			if c.want == "" {
				if ok {
					t.Errorf("expected NO fix (ok=false), got ok=true result=%q", got)
				}
				return
			}
			if !ok {
				t.Fatalf("expected a fix (ok=true), got ok=false")
			}
			if got != c.want {
				t.Errorf("fixRedundantTypeInFile =\n%q\nwant\n%q", got, c.want)
			}
		})
	}
}

// TestFixRedundantTypeFixesDirectory 回歸：目錄模式只對有變動的 .no 檔產出修復。
func TestFixRedundantTypeFixesDirectory(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.no")
	p2 := filepath.Join(dir, "b.no")
	if err := os.WriteFile(p1, []byte("main = () () {\n  x str = ''\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("main = () () {\n  y i16 = 5\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fixes := fixRedundantTypeFixes(dir)
	abs1, _ := filepath.Abs(p1)
	abs2, _ := filepath.Abs(p2)
	if _, ok := fixes[filepath.Clean(abs1)]; !ok {
		t.Errorf("expected a fix for %s", p1)
	}
	if _, ok := fixes[filepath.Clean(abs2)]; ok {
		t.Errorf("did not expect a fix for %s (implicit conversion)", p2)
	}
}
