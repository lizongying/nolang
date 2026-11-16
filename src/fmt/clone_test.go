package fmt

import (
	"strings"
	"testing"
)

// clone_test.go 釘住「複用已解析 AST」的保真前提：CloneProgramForFormat 的
// 輸出必須與 fresh parse 的格式化結果逐位元組一致，否則 LSP 省掉的那一次
// parse 會悄悄改變排版。

func TestCloneProgramForFormatMatchesFreshParse(t *testing.T) {
	src := "# std/fs\n\n; keep this comment\nf = () {\n    ok = fs.is-empty()\n\n    ok == true -> return\n}\n"
	cfg := ParseConfig{
		FuncSigs:     map[string][]string{"fs.is-empty": {"bool"}},
		MethodSigs:   map[string][]string{"str.starts-with": {"bool"}},
		StructFields: map[string]map[string]string{"Foo": {"x": "i64"}},
		EnumVariants: map[string][]string{"E": {"a", "b"}},
	}
	fresh, errs := ParseSourceWithConfig(src, cfg)
	if len(errs) > 0 || fresh == nil {
		t.Fatalf("fresh parse: prog=%v errs=%v", fresh, errs)
	}
	clone := CloneProgramForFormat(fresh)
	if clone == nil || clone == fresh {
		t.Fatal("CloneProgramForFormat must return a distinct program")
	}

	// 實證：parser.CloneProgram 的 remapSem 走 Merge，只搬函式 / 方法兩張表；
	// ExternStructFields 不補會整張消失。
	if len(clone.Sem.ExternFuncSigs) != len(fresh.Sem.ExternFuncSigs) ||
		len(clone.Sem.ExternMethodSigs) != len(fresh.Sem.ExternMethodSigs) {
		t.Fatalf("extern signature tables lost: %#v / %#v", clone.Sem.ExternFuncSigs, clone.Sem.ExternMethodSigs)
	}
	if got := clone.Sem.ExternStructFields["Foo"]["x"]; got != "i64" {
		t.Fatalf("ExternStructFields lost by CloneProgram: %#v", clone.Sem.ExternStructFields)
	}
	if len(clone.Sem.EnumVariants) != len(fresh.Sem.EnumVariants) {
		t.Fatalf("EnumVariants lost: %#v", clone.Sem.EnumVariants)
	}

	want, ferrs := FormatFileWithConfig(src, cfg, LoopStylePrefix)
	if len(ferrs) > 0 {
		t.Fatalf("fresh format: %v", ferrs)
	}
	got := FormatProgram(clone, src)
	if got != want {
		t.Fatalf("clone changed formatting:\nfresh: %q\nclone: %q", want, got)
	}
	// FmtTrivia 是 node-keyed 表，clone 必須連鍵一起換過去；註解消失就是漏了。
	if !strings.Contains(got, "; keep this comment") || !strings.Contains(got, "ok -> return") {
		t.Fatalf("trivia or bool evidence lost:\n%s", got)
	}
}

func TestCloneProgramForFormatHandlesEmptyAndZeroConfig(t *testing.T) {
	if CloneProgramForFormat(nil) != nil {
		t.Fatal("nil program must stay nil")
	}
	src := "f = () {\n    x = 1 + 2\n}\n"
	prog, errs := ParseSourceWithConfig(src, ParseConfig{})
	if len(errs) > 0 || prog == nil {
		t.Fatalf("parse: errs=%v", errs)
	}
	clone := CloneProgramForFormat(prog)
	want, _ := FormatFileWithConfig(src, ParseConfig{}, LoopStylePrefix)
	if got := FormatProgram(clone, src); got != want {
		t.Fatalf("zero-config clone changed formatting:\nwant: %q\n got: %q", want, got)
	}
}
