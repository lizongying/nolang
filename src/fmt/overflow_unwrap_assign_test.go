package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// `?=`（UnwrapAssignStatement）上的註解曾被 `no fmt` 整批刪掉：formatter 的
// attachedAnnotations 型別白名單沒有 UnwrapAssignStatement，該型別附加的所有
// 註解因此「查不到」→ 直接不輸出。src/std 有 77 處 `#{overflow=wrap}` 寫在
// `p ?= p + 1` 之上，全被刪掉，且刪掉後語意由「溢出回繞」變成「溢出回 err」。
//
// 本測試釘住三件事：
//  1. IDENT 目標的 `?=` 上方的註解必須保留；
//  2. 索引/欄位目標（解析為 ExpressionStatement 包住 UnwrapAssignStatement）
//     上方的註解必須保留；
//  3. 二次格式化冪等（不能每跑一次多印/少印一次）。
func TestFormatPreservesAnnotationsOnUnwrapAssign(t *testing.T) {
	cases := map[string]string{
		// IDENT 目標：parser 直接把註解附加到 UnwrapAssignStatement。
		"identTarget": "f = (d i64) (cp i64) {\n" +
			"    cp = 0\n" +
			"    #{overflow=wrap}\n" +
			"    cp ?= cp * 16 + d\n" +
			"}\n",
		// 索引目標：`.payload[total + i] ?= ...`，註解仍必須留在語句上方
		// （目標的下標算式本身就要 wrap 才有意義）。
		"indexTarget": "Frame.fill = (n i64) {\n" +
			"    i <- [0..n): {\n" +
			"        #{overflow=wrap}\n" +
			"        .payload[.total + i] ?= .byte(i)\n" +
			"    }\n" +
			"}\n",
		// 平台註解不是 overflow，走的是白名單的同一條路徑：一起釘住，避免只修
		// overflow 分支而留下同類漏洞。
		"platformOnUnwrapAssign": "g = (n i64) (v i64) {\n" +
			"    #{mac-arm64}\n" +
			"    v ?= n + 1\n" +
			"}\n",
	}
	for name, src := range cases {
		name, src := name, src
		t.Run(name, func(t *testing.T) {
			first := FormatFile(src)
			if !strings.Contains(first, "#{") {
				t.Fatalf("annotation on `?=` was dropped:\n--- in ---\n%s\n--- out ---\n%s", src, first)
			}
			// 冪等：註解若被重複附加到合成節點上，第二輪會多印一次。
			if second := FormatFile(first); second != first {
				t.Fatalf("formatter not idempotent:\n--- first ---\n%s\n--- second ---\n%s", first, second)
			}
			// 輸出必須可再解析（否則 `no fmt -w` 會寫出讀不回來的檔案）。
			p := parser.New(lexer.New(first))
			p.SkipLowering = true
			p.ParseProgram()
			if perrs := p.Errors(); len(perrs) > 0 {
				t.Fatalf("formatted output no longer parses: %v\n--- output ---\n%s", perrs, first)
			}
		})
	}
}

// `no fmt` 的 CLI/LSP 路徑（FormatProgramWithOverflow）會先向 checker 要
// 「這條註解管轄的陳述裡有沒有整數運算」的判定（relevant 集合）。索引/欄位目標的
// `?=`（`.payload[.total + i] ?= ...`）以**裸 UnwrapAssignStatement 陳述**的形態
// 出現（目標以 `.` 開頭，parser 無法把註解附加到它，只能留成獨立 AnnotationStatement
// 再靠 governed 對應）。checker 的 stmtHasIntOverflow 若只看 Value、不看 Target，
// **目標的索引算式**就完全不可見 ⇒ 判成「無整數運算」⇒ 註解被刪。
// 實例：src/std/net/ws.no、sse.no、http2.no、tree-map.no、tree-set.no。
//
// 判別力：把 stmtHasIntOverflow 的 `exprHasIntOverflow(s.Target, ...)` 去掉，
// 本測試立刻失敗（輸出只剩 `.payload[.total + i] ?= .byte(i)`，註解消失）。
func TestFormatKeepsOverflowOnIndexUnwrapAssign(t *testing.T) {
	input := "Frame.fill = (n i64) {\n" +
		"    i <- [0..n): {\n" +
		"        #{overflow=wrap}\n" +
		"        .payload[.total + i] ?= .byte(i)\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if !strings.Contains(out, "#{overflow=wrap}") {
		t.Fatalf("overflow annotation governing an index-target `?=` was stripped:\n%s", out)
	}
	// 冪等：第二次仍走同一套判定，不得再變。
	program2 := parseForTest(out)
	if program2 == nil {
		t.Fatal("failed to re-parse formatted output")
	}
	relevant2, governed2 := checker.OverflowAnnotationRelevance(program2)
	if out2 := FormatProgramWithOverflow(program2, out, relevant2, governed2); out2 != out {
		t.Fatalf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// 語意面（`#{overflow=wrap}` 是否真的隨 `?=` 的右式搬進合成 let、進而讓 MIR
// 設 Inst.OvfAnnotated）在 src/mir/unwrap_overflow_annot_test.go 釘住 ——
// 這裡的 ParseProgram 走 H0 直出，AST 上的 `?=` 節點不會被改寫成 `__unwrap_N`，
// 看不出那一步。
