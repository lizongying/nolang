package fmt

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// REGRESSION GUARD (ineffective #{overflow=...} is removed by `no fmt`).
//
// When a `#{overflow = ...}` annotation governs a statement that contains no
// integer-overflow-prone arithmetic (e.g. a line that is purely string
// concatenation where the `-` is concat, not subtraction), the annotation does
// nothing and must be stripped. A whole annotation line that becomes empty after
// the overflow entry is dropped (e.g. `#{overflow=wrap}` alone, or the overflow
// portion of `#{intrinsic, overflow=wrap}`) must also disappear, and the result
// must stay idempotent (no dangling blank line).
//
// The formatter receives the relevance info from the caller (checker), matching
// the `no fmt` CLI path; it never imports the checker itself.
func TestFormatRemovesIneffectiveOverflow(t *testing.T) {
	input := "main = () {\n" +
		"    #{overflow=wrap}\n" +
		"    s = \"a\" - \"b\"\n" +
		"    #{overflow=wrap}\n" +
		"    n = 1 + 2\n" +
		"    #{intrinsic, overflow=wrap}\n" +
		"    t = \"x\" - \"y\"\n" +
		"}\n"
	expected := "main = () {\n" +
		"    s = \"a\" - \"b\"\n" +
		"\n" +
		"    #{overflow=wrap}\n" +
		"    n = 1 + 2\n" +
		"\n" +
		"    #{intrinsic}\n" +
		"    t = \"x\" - \"y\"\n" +
		"}\n"

	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if out != expected {
		t.Errorf("FormatProgramWithOverflow()\n--- got ---\n%s\n--- want ---\n%s", out, expected)
	}

	// Idempotency: a second pass (re-parsing) must not change anything.
	program2 := parseForTest(out)
	if program2 == nil {
		t.Fatal("failed to re-parse formatted output")
	}
	relevant2, governed2 := checker.OverflowAnnotationRelevance(program2)
	out2 := FormatProgramWithOverflow(program2, out, relevant2, governed2)
	if out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// TestFormatRemovesIneffectiveAttachedOverflow covers the *attached* (IDENT-
// initial) annotation path: when the ineffective overflow is attached directly
// to a string-concat statement, no dangling blank line is left behind.
func TestFormatRemovesIneffectiveAttachedOverflow(t *testing.T) {
	input := "g = () {\n" +
		"    #{overflow=wrap}\n" +
		"    s = \"a\" - \"b\"\n" +
		"}\n" +
		"\n" +
		"h = () {\n" +
		"    #{mac-arm64, overflow=wrap}\n" +
		"    s = \"x\" - \"y\"\n" +
		"}\n"
	expected := "g = () {\n" +
		"    s = \"a\" - \"b\"\n" +
		"}\n" +
		"\n" +
		"h = () {\n" +
		"\n" +
		"    #{mac-arm64}\n" +
		"    s = \"x\" - \"y\"\n" +
		"}\n"

	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if out != expected {
		t.Errorf("FormatProgramWithOverflow()\n--- got ---\n%s\n--- want ---\n%s", out, expected)
	}
}

// TestFormatPreservesEffectiveOverflow confirms that a *valid* overflow
// annotation (governing real integer arithmetic) is never stripped — the
// over-approximation is conservative, so we only remove provably-ineffective ones.
func TestFormatPreservesEffectiveOverflow(t *testing.T) {
	input := "f = (a i64, b i64) (c i64) {\n" +
		"    #{overflow=wrap}\n" +
		"    c = a - b\n" +
		"}\n"
	expected := "f = (a i64, b i64) (c i64) {\n" +
		"\n" +
		"    #{overflow=wrap}\n" +
		"    c = a - b\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if out != expected {
		t.Errorf("effective overflow was stripped (should be preserved):\n--- got ---\n%s\n--- want ---\n%s", out, expected)
	}
}

// TestFormatPreservesEffectiveOverflowInMatchArm 回归钉：match 臂体内的有效
// #{overflow=wrap} 不得被误删（src/std/vec.no `[]t.insert` 实报）。
//
// 两个独立缺陷叠加造成误删：
//
//	(a) checker.exprHasIntOverflow 在 IfExpression 分支遇首个命中子语句即
//	    `return true`，短路了后续兄弟语句的遍历；而 relevant 集合是靠递归
//	    过程中的副作用逐个登记的，未被访问的语句（如第二臂的 `last = last - 1`）
//	    永远不在集合中 → 其附加注解被 formatter 判为无效而删除。
//	(b) OverflowAnnotationRelevance.collect() 不下钻 ExpressionStatement→IfExpression
//	    的臂块，臂内「独立成行」的 overflow 注解（被管辖语句以 `.` 开头、parser
//	    无法附加时，如 `.len = cur + 1`）拿不到 governed → gov=nil → 被删除，
//	    即使被管辖语句本身已在 relevant 中。
func TestFormatPreservesEffectiveOverflowInMatchArm(t *testing.T) {
	input := "[]t.insert = (i i64, val t) {\n" +
		"    {\n" +
		"        i >= .len() -> {\n" +
		"            #{overflow=wrap}\n" +
		"            .len = cur + 1\n" +
		"        }\n" +
		"\n" +
		"        -> {\n" +
		"            #{overflow=wrap}\n" +
		"            last = last - 1\n" +
		"        }\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	// 两条注解都管辖真实整数运算（`cur + 1` / `last - 1`），均必须保留。
	if got := strings.Count(out, "#{overflow=wrap}"); got != 2 {
		t.Errorf("match-arm effective overflow annotations stripped: kept %d, want 2:\n%s", got, out)
	}
	// 幂等：二次格式化不得再变动。
	program2 := parseForTest(out)
	if program2 == nil {
		t.Fatal("failed to re-parse formatted output")
	}
	relevant2, governed2 := checker.OverflowAnnotationRelevance(program2)
	out2 := FormatProgramWithOverflow(program2, out, relevant2, governed2)
	if out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// TestFormatPreservesEffectiveOverflowGenericElem 回归钉：泛型容器的元素运算
// （`[]t.sum` 的 `acc = acc + .[i]`，`.[i]` 推断为型别参数 "t"）必须被 relevant
// 集合保守视为可能整数，其 #{overflow=wrap} 注解不得被 `no fmt` 误删
// （单态化为 i64 vec 后该运算确实可能溢出，wrap 是作者显式选择的语义）。
func TestFormatPreservesEffectiveOverflowGenericElem(t *testing.T) {
	input := "[]t.sum = () (sum t) {\n" +
		"    acc = 0\n" +
		"    i <- [0...len()): {\n" +
		"        #{overflow=wrap}\n" +
		"        acc = acc + .[i]\n" +
		"    }\n" +
		"    sum = acc\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if got := strings.Count(out, "#{overflow=wrap}"); got != 1 {
		t.Errorf("generic-element effective overflow annotation stripped: kept %d, want 1:\n%s", got, out)
	}
}

// TestFormatPreservesEffectiveOverflowSelfDot 回归钉：方法体内裸 `.`（隱式
// self）参与的整数算术（src/std/char.no `char.to-upper` 的 `result = . - 32`）
// 其 #{overflow=wrap} 不得被误删。parser 将 `.` 脱糖为 Value=="self" 的
// Identifier，method 的 declared 表把 self 登记为接收者型别（char）→
// operandIntKind/isDirectOverflowValue 均判为「确定非整族」→ 注解误删；但
// codegen 对 char self 算术实际产生 option，删后换来编译期 ovfhndld 硬错。
// checker.infixFlaggedByOvfLint 对裸 `.` 保守改判为整数后修复。
func TestFormatPreservesEffectiveOverflowSelfDot(t *testing.T) {
	input := "char.to-upper = () (result char) {\n" +
		"    {\n" +
		"        . >= 97 && . <= 122 -> {\n" +
		"            #{overflow=wrap}\n" +
		"            result = . - 32\n" +
		"        }\n" +
		"\n" +
		"        -> result = .\n" +
		"    }\n" +
		"}\n"
	program := parseForTest(input)
	if program == nil {
		t.Fatal("failed to parse test input")
	}
	relevant, governed := checker.OverflowAnnotationRelevance(program)
	out := FormatProgramWithOverflow(program, input, relevant, governed)
	if got := strings.Count(out, "#{overflow=wrap}"); got != 1 {
		t.Errorf("self-dot effective overflow annotation stripped: kept %d, want 1:\n%s", got, out)
	}
	// 幂等：二次格式化不得再变动。
	program2 := parseForTest(out)
	if program2 == nil {
		t.Fatal("failed to re-parse formatted output")
	}
	relevant2, governed2 := checker.OverflowAnnotationRelevance(program2)
	out2 := FormatProgramWithOverflow(program2, out, relevant2, governed2)
	if out2 != out {
		t.Errorf("not idempotent:\n--- first ---\n%s\n--- second ---\n%s", out, out2)
	}
}

// parseForTest parses source with the same options the `no fmt` CLI uses.
func parseForTest(src string) *parser.Program {
	lx := lexer.New(src)
	p := parser.New(lx)
	p.SkipUnwrapLowering = true
	p.SkipSafeIndexLowering = true
	return p.ParseProgram()
}
