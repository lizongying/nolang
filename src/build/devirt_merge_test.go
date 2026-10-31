package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// parseDevirtSrc 解析一段 nolang 原始碼；parser 報錯直接失敗。
func parseDevirtSrc(t *testing.T, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("ParseProgram returned nil\n--- src ---\n%s", src)
	}
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v\n--- src ---\n%s", errs, src)
	}
	return prog
}

// TestDevirtualizeInterfaceMerge 鎖定「實現子介面的型別同時算作父介面的實作者」：
//
//	json { t.to-json() (s str) }
//	noml { t.to-noml() (s str) }
//	serialize { json⏎ noml }          ; 接口合併
//	user serialize { … }
//	s serialize = u
//
// 少了父閉包展開，`impls["json"]` 為空 ⇒ `s.to-json()` 攤平時仍會找
// `serialize.to-json`（不存在的函式）；而少了「主程序也掃」則 `interfaces` 全空、
// 函式直接 return，`s` 保留介面型別，接收者傳下去是一塊零值。
func TestDevirtualizeInterfaceMerge(t *testing.T) {
	src := `json {
    t.to-json() (s str)
}
noml {
    t.to-noml() (s str)
}
serialize {
    json
    noml
}
roundtrip {
    serialize
    t.roundtrip() (s str)
}
user serialize, roundtrip {
    name str
}
u = user{
    name: 'Alice'
}
s serialize = u
r roundtrip = u
`
	prog := parseDevirtSrc(t, src)
	varTypes := map[string]string{}
	devirtualizeInterfaceVars(prog, prog, varTypes, map[string]string{})

	for _, v := range []string{"s", "r"} {
		if got, want := varTypes[v], "user"; got != want {
			t.Fatalf("varTypes[%q] = %q, want %q（實作型別未具象化）", v, got, want)
		}
	}
	// 父介面也必須認得 user：resolveMethodCall 的介面回退按「介面名」查表。
	for _, iface := range []string{"serialize", "roundtrip", "json", "noml"} {
		impls := prog.Sem.IfaceImplsOf(iface)
		if len(impls) != 1 || impls[0] != "user" {
			t.Fatalf("IfaceImplsOf(%q) = %v, want [user]", iface, impls)
		}
	}
}

// TestDevirtualizeTypeOverrideReachesMergedSem 鎖定「具象化寫點必須同時落
// mainProg.Sem 與 merged.Sem」：裝配順序是 prefixMethodsAndTypes（本函式）先於
// appendTopLevelStatements，而 HIR/MIR 用 merged + merged.Sem（另一份副表）。
// 只寫 mainProg.Sem 時，`v json = u` 的 LetStatement 在 merged 眼裡仍是介面型別，
// `v.to-json()` 傳下去的接收者是一塊零值（實測輸出 `json:` 而不是 `json:Alice`）。
func TestDevirtualizeTypeOverrideReachesMergedSem(t *testing.T) {
	src := `json {
    t.to-json() (s str)
}
user json {
    name str
}
u = user{
    name: 'Alice'
}
v json = u
`
	mainProg := parseDevirtSrc(t, src)
	// 模擬 newCompileAssembly：merged 是獨立的一份 Sem，只 Merge 了當下的語義。
	merged := &parser.Program{Statements: mainProg.Statements}
	merged.Sem = parser.NewSemanticContext()
	merged.Sem.Merge(mainProg.Sem)

	varTypes := map[string]string{}
	devirtualizeInterfaceVars(mainProg, merged, varTypes, map[string]string{})

	var let *parser.LetStatement
	for _, stmt := range merged.Statements {
		if ls, ok := stmt.(*parser.LetStatement); ok && ls.Name != nil && ls.Name.Value == "v" {
			let = ls
		}
	}
	if let == nil {
		t.Fatalf("找不到 v 的 LetStatement")
	}
	if got, want := merged.Sem.TypeOverrideOf(let), "user"; got != want {
		t.Fatalf("merged.Sem.TypeOverrideOf(v) = %q, want %q", got, want)
	}
	if got, want := mainProg.Sem.TypeOverrideOf(let), "user"; got != want {
		t.Fatalf("mainProg.Sem.TypeOverrideOf(v) = %q, want %q", got, want)
	}
}
