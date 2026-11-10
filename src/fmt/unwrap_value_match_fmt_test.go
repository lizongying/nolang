package fmt

// `v ?= subj: { … }` 的值位 match 曾被 `no fmt -w` 寫壞成不可再解析的形態：
//
//	flags-hi ?= data[2]: { ok -> it  -> 0 }              ← 原文
//	flags-hi ?= {: { __match_subj_512_26 == ok: … } }     ← 被寫出
//	v ?= o: { … }  →  v ?= o == ok: { it } else: { 0 }
//
// 根因不在 formatter，而在 CLI 的取捨順序：`no fmt` 先 parse 一次拿糖樹，
// 再對**同一棵樹**跑 checker.BuildTHIRView（─ H0 直出 `?=`）計算溢出相關性，
// 最後才把這棵樹交給 formatter。H0 的 lowerUnwrapAssign 一開頭就用
// `l.walk(&uas.Value)` 就地展開右值的 SurfaceMatch，於是 formatter 看到的是
// 降糖後的 IfExpression 鏈（手臂條件 `subj == ok`、合成名 `__match_subj_L_C`），
// 印回原始碼就再也 parse 不回來。src/std/net/dns.no 因此整檔格式化失敗。
//
// 修法在 parser/tohir.go：lowerUnwrapAssignToHIR 傳副本給 lowerUnwrapAssign
// （C6：H0 不動 AST）。本檔同時釘住兩條路徑：
//   - 純 formatter（FormatFile）：本來就該正確，防未來的 formatting 回歸；
//   - CLI 等價路徑（parse → BuildTHIRView → OverflowAnnotationRelevance →
//     FormatProgramWithOverflow）：這條才是 dns.no 真正踩到的路徑。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

const unwrapValueMatchCases = "f = (o ?i64, data []byte) (r ?i64) {\n" +
	"    a ?= o: {\n" +
	"        ok -> it\n" +
	"\n" +
	"        -> 0\n" +
	"    }\n" +
	"    b ?= data[2]: {\n" +
	"        ok -> it\n" +
	"\n" +
	"        -> 0\n" +
	"    }\n" +
	"    r = a + b\n" +
	"}\n"

// assertUnwrapValueMatchIntact 檢查格式化輸出仍保留原始主語與臂模式，且不含
// 任何降糖痕跡；再驗冪等與可重解析。
func assertUnwrapValueMatchIntact(t *testing.T, src, out string) {
	t.Helper()
	for _, want := range []string{"a ?= o: {", "b ?= data[2]: {", "ok -> it"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted output lost %q:\n--- in ---\n%s\n--- out ---\n%s", want, src, out)
		}
	}
	for _, bad := range []string{"__match_subj", "== ok", "else:"} {
		if strings.Contains(out, bad) {
			t.Fatalf("formatted output leaked desugared form %q:\n--- out ---\n%s", bad, out)
		}
	}
	if second := FormatFile(out); second != out {
		t.Fatalf("formatter not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, second)
	}
	p := parser.New(lexer.New(out))
	p.SkipLowering = true
	p.ParseProgram()
	if perrs := p.Errors(); len(perrs) > 0 {
		t.Fatalf("formatted output no longer parses: %v\n--- output ---\n%s", perrs, out)
	}
}

// TestFormatUnwrapAssignValueMatchSubject 純 formatter 路徑。
func TestFormatUnwrapAssignValueMatchSubject(t *testing.T) {
	assertUnwrapValueMatchIntact(t, unwrapValueMatchCases, FormatFile(unwrapValueMatchCases))
}

// TestFormatUnwrapAssignValueMatchAfterTHIRView 是 `no fmt` CLI 的等價路徑。
//
// BuildTHIRView 跑 H0 是為了給 FixStringConcatOperators / 溢出相關性計算提供
// 型別視圖；它必須對 AST **零副作用**，否則同一棵樹交到 formatter 手上時
// 糖節點已被展開。這條斷言就是 C6（H0 不動 AST）在 `?=` 上的門禁。
func TestFormatUnwrapAssignValueMatchAfterTHIRView(t *testing.T) {
	lx := lexer.New(unwrapValueMatchCases)
	p := parser.New(lx)
	p.SkipLowering = true
	prog := p.ParseProgram()
	if perrs := p.Errors(); len(perrs) > 0 {
		t.Fatalf("parse failed: %v", perrs)
	}

	checker.FixStringConcatOperators(prog, checker.BuildTHIRView(prog))
	relevant, governed := checker.OverflowAnnotationRelevance(prog)
	out := FormatProgramWithOverflow(prog, unwrapValueMatchCases, relevant, governed)

	assertUnwrapValueMatchIntact(t, unwrapValueMatchCases, out)
}
