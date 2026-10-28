package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// hasLoweringTempBinding reports whether the parsed program contains a
// lowering-synthesized `__`-prefixed binding. Used as a non-vacuity guard: the
// exemption below only means anything if the option-capture pass really did
// desugar the source into `__cap_N`.
func hasLoweringTempBinding(prog *parser.Program) bool {
	found := false
	var walk func(stmts []parser.Statement)
	walk = func(stmts []parser.Statement) {
		for _, s := range stmts {
			switch v := s.(type) {
			case *parser.LetStatement:
				if v.Name != nil && strings.HasPrefix(v.Name.Value, "__") {
					found = true
				}
			case *parser.BlockStatement:
				walk(v.Statements)
			case *parser.ExpressionStatement:
				if ifx, ok := v.Expression.(*parser.IfExpression); ok {
					if ifx.Consequence != nil {
						walk(ifx.Consequence.Statements)
					}
					if ifx.Alternative != nil {
						walk(ifx.Alternative.Statements)
					}
				}
			}
		}
	}
	if prog != nil {
		walk(prog.Statements)
	}
	return found
}

// TestBareOptionArgLoweringTempExempt pins the exemption of
// lowering-synthesized temporaries from the bare-option-argument rule
// (fxxoptarg).
//
// `r0 = g(3, j - 1)` carries an option source (`j - 1`) inside a call argument,
// so the option-capture pass rewrites it to
//
//	__cap_1 ?i64 = j - 1
//	match __cap_1 { nil -> r0 = __cap_1
//	                err -> r0 = __cap_1
//	                ok  -> r0 = g(3, __cap_1) }
//
// The argument the checker then sees is the `?i64` temp. Inside that `ok` arm
// the temp denotes the UNWRAPPED payload — codegen emits
// `call void @g(i64 %lv80, …)` and the program prints the correct value — so
// reporting it as a bare option argument is a false positive. A `__`-prefixed
// name cannot be written in Nolang source, so the exemption cannot mask a
// user's own binding.
//
// The second half is the control: a genuine `?i64` VARIABLE argument must still
// be rejected, or the exemption would have been widened into "never check
// arguments".
func TestBareOptionArgLoweringTempExempt(t *testing.T) {
	// 本測試守護的是 AST 軌的「降級合成臨時變數豁免 fxxoptarg」不變量：該豁免
	// 依賴捕獲 pass 真的把引數 desugar 成 `__cap_N` 臨時（H0 軌不注入，故無此
	// 豁免對象）。顯式鎖定 AST 軌（false），使其非真空守衛有意義；H0 軌的行為
	// 由 dual-track 等價測試與 checker parity 守護。
	parser.HIRCaptureEnabled = false
	defer func() { parser.HIRCaptureEnabled = true }()

	desugared := `g = (x i64, y i64) (r i64) {
	r = x - y
}
j = 5
r0 = g(3, j - 1)
`
	l := lexer.New(desugared)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if !hasLoweringTempBinding(prog) {
		t.Fatal("non-vacuity: the capture pass did not desugar the argument; " +
			"this test would pass for the wrong reason")
	}
	n := 0
	for _, r := range ValidateFuncArgs(prog, "") {
		if r.TraceID == "fxxoptarg" {
			n++
		}
	}
	if n != 0 {
		t.Fatalf("lowering temp argument wrongly rejected: got %d fxxoptarg errors", n)
	}
}
