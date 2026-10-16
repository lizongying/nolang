package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/parser/dump"
)

func parseFile(t *testing.T, path string) *parser.Program {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	p := parser.New(lexer.New(string(src)))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse %s: %v", path, errs)
	}
	return prog
}

// TestASTTreeRoundTripStd：對 std 語料逐模組做 編碼→解碼，用 L2 oracle
// （dump 文本）做逐位元組等價判據——含註解副表，所以 nodeSem 的往返也被覆蓋。
func TestASTTreeRoundTripStd(t *testing.T) {
	root := filepath.Join("..", "std")
	var files []string
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && filepath.Ext(p) == ".no" {
			files = append(files, p)
		}
		return nil
	})
	if len(files) < 50 {
		t.Fatalf("expected the std corpus, got %d files", len(files))
	}

	opts := dump.Options{Annotations: true}
	failed := 0
	for _, f := range files {
		prog := parseFile(t, f)
		nt, err := parser.EncodeASTTree(prog, "")
		if err != nil {
			t.Errorf("%s: encode: %v", f, err)
			failed++
			continue
		}
		back, err := parser.DecodeASTTree(nt)
		if err != nil {
			t.Errorf("%s: decode: %v", f, err)
			failed++
			continue
		}
		want := dump.DumpWithOptions(prog, opts)
		got := dump.DumpWithOptions(back, opts)
		if want != got {
			t.Errorf("%s: dump mismatch (%d vs %d bytes)", f, len(want), len(got))
			if failed < 3 {
				a, b := firstDiff(want, got)
				t.Errorf("  first divergence:\n  want: %s\n  got : %s", a, b)
			}
			failed++
		}
		_ = strings.TrimSpace
	}
	if failed != 0 {
		t.Fatalf("%d/%d modules failed the mm ast-tree round trip", failed, len(files))
	}
	t.Logf("round-tripped %d std modules", len(files))
}

// TestASTTreeStrictEqual：比 dump 更嚴的判據——對 std 語料逐模組做 編碼→解碼，
// 用反射逐欄位比較**所有已導出欄位**（含 Token 的 Type/Raw/Line/Column、
// slice 的 nil vs 空、map 內容），要求與原始 parse 產物完全一致。
func TestASTTreeStrictEqual(t *testing.T) {
	root := filepath.Join("..", "std")
	var files []string
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && filepath.Ext(p) == ".no" {
			files = append(files, p)
		}
		return nil
	})
	if len(files) < 50 {
		t.Fatalf("expected the std corpus, got %d files", len(files))
	}
	failed := 0
	for _, f := range files {
		prog := parseFile(t, f)
		nt, err := parser.EncodeASTTree(prog, "")
		if err != nil {
			t.Errorf("%s: encode: %v", f, err)
			failed++
			continue
		}
		back, err := parser.DecodeASTTree(nt)
		if err != nil {
			t.Errorf("%s: decode: %v", f, err)
			failed++
			continue
		}
		var diffs []string
		strictDiff(reflect.ValueOf(prog).Elem(), reflect.ValueOf(back).Elem(), "", &diffs, 12)
		if len(diffs) != 0 {
			t.Errorf("%s: %d strict diffs", f, len(diffs))
			for _, d := range diffs {
				t.Errorf("  %s", d)
			}
			failed++
		}
	}
	if failed != 0 {
		t.Fatalf("%d/%d modules failed strict AST equality", failed, len(files))
	}
	t.Logf("strictly equal on %d std modules", len(files))
}

// strictDiff 遞迴比較兩個值的所有已導出欄位；Token/Position 用 DeepEqual 整體比。
func strictDiff(a, b reflect.Value, path string, diffs *[]string, max int) {
	if len(*diffs) >= max || !a.IsValid() || !b.IsValid() {
		return
	}
	if a.Type() != b.Type() {
		*diffs = append(*diffs, fmt.Sprintf("%s: type %s vs %s", path, a.Type(), b.Type()))
		return
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() != b.IsNil() {
			*diffs = append(*diffs, fmt.Sprintf("%s: nil %v vs %v", path, a.IsNil(), b.IsNil()))
			return
		}
		if a.IsNil() {
			return
		}
		strictDiff(strictDeref(a), strictDeref(b), path, diffs, max)
	case reflect.Struct:
		if t := a.Type(); t == reflect.TypeOf(lexer.Position{}) || t == reflect.TypeOf(lexer.Token{}) {
			if !reflect.DeepEqual(a.Interface(), b.Interface()) {
				*diffs = append(*diffs, fmt.Sprintf("%s: %v %v vs %v", path, t, a.Interface(), b.Interface()))
			}
			return
		}
		for i := 0; i < a.NumField(); i++ {
			f := a.Type().Field(i)
			if f.PkgPath != "" { // 未導出（本就不序列化）
				continue
			}
			strictDiff(a.Field(i), b.Field(i), path+"."+f.Name, diffs, max)
		}
	case reflect.Slice, reflect.Array:
		if a.Kind() == reflect.Slice && a.IsNil() != b.IsNil() {
			*diffs = append(*diffs, fmt.Sprintf("%s: slice nil %v vs %v", path, a.IsNil(), b.IsNil()))
		}
		if a.Len() != b.Len() {
			*diffs = append(*diffs, fmt.Sprintf("%s: len %d vs %d", path, a.Len(), b.Len()))
			return
		}
		for i := 0; i < a.Len(); i++ {
			strictDiff(a.Index(i), b.Index(i), fmt.Sprintf("%s[%d]", path, i), diffs, max)
		}
	case reflect.Map:
		if a.IsNil() != b.IsNil() {
			*diffs = append(*diffs, fmt.Sprintf("%s: map nil %v vs %v", path, a.IsNil(), b.IsNil()))
		}
		if a.Len() != b.Len() {
			*diffs = append(*diffs, fmt.Sprintf("%s: maplen %d vs %d", path, a.Len(), b.Len()))
			return
		}
		for _, k := range a.MapKeys() {
			bv := b.MapIndex(k)
			if !bv.IsValid() {
				*diffs = append(*diffs, fmt.Sprintf("%s[%v]: missing", path, k))
				continue
			}
			strictDiff(a.MapIndex(k), bv, fmt.Sprintf("%s[%v]", path, k), diffs, max)
		}
	default:
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			*diffs = append(*diffs, fmt.Sprintf("%s: %v=%v vs %v", path, a.Kind(), a.Interface(), b.Interface()))
		}
	}
}

func strictDeref(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v
		}
		v = v.Elem()
	}
	return v
}

// TestASTTreeVersionGuard：版本不符必須硬錯，不靜默解出結構不同的樹。
func TestASTTreeVersionGuard(t *testing.T) {
	prog := parseFile(t, filepath.Join("..", "std", "vec.no"))
	nt, err := parser.EncodeASTTree(prog, "")
	if err != nil {
		t.Fatal(err)
	}
	nt.Ver = "1900-01-01.0"
	if _, err := parser.DecodeASTTree(nt); err == nil {
		t.Fatal("expected wire version mismatch error")
	}
	nt.Ver = parser.MMWireVersion
	nt.AstVer = "1900-01-01.0"
	if _, err := parser.DecodeASTTree(nt); err == nil {
		t.Fatal("expected AST version mismatch error")
	}
}

// firstDiff 返回 want/got 第一個不同行之附近的上下文（便於定位）。
func firstDiff(want, got string) (string, string) {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return a, b
		}
	}
	return "", ""
}
