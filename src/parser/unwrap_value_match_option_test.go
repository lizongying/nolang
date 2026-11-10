package parser

// `v ?= subj: { … }`——`?=` 的右值是**值位 match** 时的语义。
//
// 值位 match 的臂自己就处理了 nil/err（`-> 0` 兜底），整体产出一个裸 `T`，
// `?=` 在右侧找不到可解包的 option。此时 `?=` 只剩「把 match 的结果当 option
// 值赋给目标」这一层意思：目标保持 option 型别 `?T`，赋值点做隐式 `ok()` 包装。
//
//	flags-hi ?= data[2]: { ok -> it  -> 0 }   ; == flags-hi ?byte = …
//
// 修复前它落到通用解包路径：合成的 `__unwrap_N` 没有型别标注 ⇒ MIR 取不到
// option payload ⇒ 目标恒 undef/0。src/std/net/dns.no 的 `flags-hi` 因此恒 0，
// rcode 判定全走错分支（`v = data[2]: {…}` 那种普通赋值则一切正常）。
//
// 断言：目标在语义副表里被登记成 `?T`（T 取 match 主语的 option payload），
// 且语句没有被降成「解包 + match」的 BlockStatement（LLVM 路径保留糖节点，
// 结构展开交给 H0，故这里只钉副表事实与「不是 BlockStatement」）。
//
// 端到端的「打印 20 / 值是 ok(…)、仍可三态 match」由 fmt 包
// TestFormatUnwrapAssignValueMatchAfterTHIRView（CLI 等价路径）与
// test/std/unwrap-value-match.no 把守。

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
)

func TestUnwrapAssignValueMatchBecomesOptionLet(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantTyp string
	}{
		{
			name: "identSubject",
			src: "f = (o ?i64) (out str) {\n" +
				"    v ?= o: {\n" +
				"        ok -> it\n" +
				"\n" +
				"        -> 0\n" +
				"    }\n" +
				"    out = ''\n" +
				"}\n",
			wantTyp: "?i64",
		},
		{
			name: "indexSubject",
			src: "f = (data []byte) (out str) {\n" +
				"    v ?= data[2]: {\n" +
				"        ok -> it\n" +
				"\n" +
				"        -> 0\n" +
				"    }\n" +
				"    out = ''\n" +
				"}\n",
			wantTyp: "?byte",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.src))
			prog := p.ParseProgram()
			if errs := p.Errors(); len(errs) > 0 {
				t.Fatalf("parse failed: %v", errs)
			}
			fd, ok := prog.Statements[0].(*FunctionDefinition)
			if !ok || fd.Body == nil || len(fd.Body.Statements) == 0 {
				t.Fatalf("unexpected program shape: %T", prog.Statements[0])
			}
			if bs, isBlock := fd.Body.Statements[0].(*BlockStatement); isBlock {
				t.Fatalf("`?=` with a value-match RHS must not be desugared into an unwrap block, got %T", bs)
			}
			if prog.Sem == nil {
				t.Fatalf("ParseProgram did not attach Sem")
			}
			got, ok := prog.Sem.FuncVarType("f", "v")
			if !ok {
				t.Fatalf("`v` has no registered type; it must be %s", tc.wantTyp)
			}
			if got != tc.wantTyp {
				t.Fatalf("`v` type = %q, want %q", got, tc.wantTyp)
			}
		})
	}
}
