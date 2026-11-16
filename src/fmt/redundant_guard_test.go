package fmt

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

// firstLet 解析 src 並回傳第一個函式體內的第一條 let 陳述（測試輔助）。
func firstLet(t *testing.T, src string) *parser.LetStatement {
	t.Helper()
	prog, errs := ParseSourceWithConfig(src, ParseConfig{})
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	for _, stmt := range prog.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || fd.Body == nil {
			continue
		}
		for _, s := range fd.Body.Statements {
			if let, ok := s.(*parser.LetStatement); ok {
				return let
			}
		}
	}
	t.Fatalf("no let statement in source:\n%s", src)
	return nil
}

// TestRedundantTypeAnnotationSafe 釘住「哪類型別標註不可省」，因為 no fmt
// 的文字區間路徑與 LSP 的節點表路徑現在共用這一份判定。
func TestRedundantTypeAnnotationSafe(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "plain named type hint is removable",
			src:  "f = () () {\n  a str = ''\n}\n",
			want: true,
		},
		{
			name: "slice annotation is removable (type still inferrable)",
			src:  "f = () () {\n  k []byte = 'x'.to-bytes()\n}\n",
			want: true,
		},
		{
			name: "map annotation on a call value is removable",
			src:  "f = () () {\n  m [str]i64 = make-map()\n}\n",
			want: true,
		},
		{
			name: "fixed-size array annotation is load-bearing",
			src:  "f = () () {\n  a [3]i64 = [1, 2, 3]\n}\n",
			want: false,
		},
		{
			name: "map literal annotation is load-bearing",
			src:  "f = () () {\n  m [str]i64 = { 'a': 1 }\n}\n",
			want: false,
		},
		{
			name: "hex payload annotation is load-bearing (byte vs i64)",
			src:  "f = () () {\n  h []byte = [0x01, 0x02]\n}\n",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RedundantTypeAnnotationSafe(firstLet(t, tc.src)); got != tc.want {
				t.Fatalf("RedundantTypeAnnotationSafe = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRedundantTypeAnnotationSafeNil 防呆：空節點 / 缺欄位一律不安全。
func TestRedundantTypeAnnotationSafeNil(t *testing.T) {
	if RedundantTypeAnnotationSafe(nil) {
		t.Fatal("nil node must not be considered safe")
	}
	if RedundantTypeAnnotationSafe(&parser.LetStatement{}) {
		t.Fatal("incomplete node must not be considered safe")
	}
}
