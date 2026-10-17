package checker

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// countTrace 回傳 results 中 trace 為 id 的條數。
func countTrace(results []ValidateResult, id string) int {
	n := 0
	for _, r := range results {
		if r.TraceID == id {
			n++
		}
	}
	return n
}

// TestCatchAllArmUnreachForLiteralArith：`ao = 1 + 2` 可證明不溢出 → 空 `->` 臂是
// 死臂，須報 arm-unreach（D9）。這是用戶提出的原型場景。
func TestCatchAllArmUnreachForLiteralArith(t *testing.T) {
	src := `f = () (res i64) {
    ao = 1 + 2
    a = ao: {
        ok -> it
        ->
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armUnreachTraceID); got != 1 {
		t.Fatalf("expected 1 %s, got %d: %+v", armUnreachTraceID, got, res)
	}
	if got := countTrace(res, armShouldTraceID); got != 0 {
		t.Errorf("expected 0 %s, got %d", armShouldTraceID, got)
	}
	if !strings.Contains(res[0].Message, "ao") {
		t.Errorf("message should name the subject, got: %s", res[0].Message)
	}
}

// TestCatchAllArmShouldForVariableArith：操作數含變數 → 無法證明 → 空 `->` 臂可能
// 被執行，須報 arm-should（WARNING）提示用戶決定該做什麼。
func TestCatchAllArmShouldForVariableArith(t *testing.T) {
	src := `f = (x i64, y i64) (res i64) {
    ao = x + y
    a = ao: {
        ok -> it
        ->
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armShouldTraceID); got != 1 {
		t.Fatalf("expected 1 %s, got %d: %+v", armShouldTraceID, got, res)
	}
	if got := countTrace(res, armUnreachTraceID); got != 0 {
		t.Errorf("expected 0 %s, got %d", armUnreachTraceID, got)
	}
	// 信息必須可操作：告訴用戶該做什麼。
	if !strings.Contains(res[0].Message, "log") {
		t.Errorf("arm-should message should suggest log/print, got: %s", res[0].Message)
	}
}

// TestCatchAllArmWithUserActionNotReported：兜底臂寫了實際動作 → 用戶已經決定失敗
// 時做什麼，完全不提示。
func TestCatchAllArmWithUserActionNotReported(t *testing.T) {
	src := `f = (x i64, y i64) (res i64) {
    ao = x + y
    a = ao: {
        ok -> it
        -> log('overflow')
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if len(res) != 0 {
		t.Fatalf("expected no D9 diagnostic for an arm with a user action, got %+v", res)
	}
}

// TestCatchAllArmWithExplicitZeroStillReported：`-> 0` 與 `->` 在 AST 上無法區分
// （D4：空臂語義就是默認零值），因此同樣提示。這是已知且可接受的等價。
func TestCatchAllArmWithExplicitZeroStillReported(t *testing.T) {
	src := `f = (x i64, y i64) (res i64) {
    ao = x + y
    a = ao: {
        ok -> it
        -> 0
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armShouldTraceID); got != 1 {
		t.Fatalf("expected 1 %s for `-> 0` (same shape as empty), got %d: %+v",
			armShouldTraceID, got, res)
	}
}

// TestCatchAllArmTracedThroughIdentifier：標識符主體追蹤到唯一初始器。
// 注意 `bo = ao` 不會推導出 `?i64`（matched expression type is unknown），
// 因此這裡用顯式 `?i64` 型別宣告， D9 才追得到。
func TestCatchAllArmTracedThroughIdentifier(t *testing.T) {
	src := `f = () (res i64) {
    ao = 1 + 2
    bo ?i64 = ao
    a = bo: {
        ok -> it
        ->
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armUnreachTraceID); got != 1 {
		t.Fatalf("expected 1 %s through identifier chain, got %d: %+v",
			armUnreachTraceID, got, res)
	}
}

// TestCatchAllArmNonZeroLiteralDivisor：除數為非零字面量 → 可證明不除零 →
// 報 arm-unreach。這是「除 0」的安全方向，與越界同一套思路。
func TestCatchAllArmNonZeroLiteralDivisor(t *testing.T) {
	src := `f = (x i64) (res i64) {
    q = x / 2
    a = q: {
        ok -> it
        ->
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armUnreachTraceID); got != 1 {
		t.Fatalf("expected 1 %s for non-zero literal divisor, got %d: %+v",
			armUnreachTraceID, got, res)
	}
}

// TestCatchAllArmLiteralDivisorModulo：`n % 8` 同理。
func TestCatchAllArmLiteralDivisorModulo(t *testing.T) {
	src := `f = (n i64) (res i64) {
    q = n % 8
    a = q: {
        ok -> it
        ->
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armUnreachTraceID); got != 1 {
		t.Fatalf("expected 1 %s for literal modulo divisor, got %d: %+v",
			armUnreachTraceID, got, res)
	}
}

// TestCatchAllArmVariableDivisor：除數是變數 → 無法證明不除零 → 報 arm-should。
func TestCatchAllArmVariableDivisor(t *testing.T) {
	src := `f = (x i64, y i64) (res i64) {
    q = x / y
    a = q: {
        ok -> it
        ->
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armShouldTraceID); got != 1 {
		t.Fatalf("expected 1 %s for variable divisor, got %d: %+v",
			armShouldTraceID, got, res)
	}
}

// TestCatchAllArmDivisorMinusOne：除數為 -1 時 `MIN / -1` 會溢位，必須保守地
// 當成「無法證明」——不可把會失敗的臂說成死臂。
//
// 只測單元層：`q = x / -1` 之後對 `q` 做 match 會撞上既有的型別推斷缺口
// （前綴 `-` 的除數讓 inferOptionArith 認不出整數運算元，報
// "empty match arm cannot determine a value"），那是 D9 之前的問題，與本規則無關。
func TestCatchAllArmDivisorMinusOne(t *testing.T) {
	src := "f = (x i64, y i64) (r i64) {\n    z = -1\n    r = z\n}\n"
	fd := parseProg(t, src).Statements[0].(*parser.FunctionDefinition)
	var rhs parser.Expression
	for _, s := range fd.Body.Statements {
		if ls, ok := s.(*parser.LetStatement); ok && ls.Name.Value == "z" {
			rhs = ls.Value
		}
	}
	if nonOverflowingDivisorLiteral(rhs) {
		t.Errorf("`-1` must NOT count as a provably-safe divisor (MIN / -1 overflows)")
	}
	// 反向對照：-2 安全。
	src2 := "f = (x i64, y i64) (r i64) {\n    z = -2\n    r = z\n}\n"
	fd2 := parseProg(t, src2).Statements[0].(*parser.FunctionDefinition)
	for _, s := range fd2.Body.Statements {
		if ls, ok := s.(*parser.LetStatement); ok && ls.Name.Value == "z" {
			rhs = ls.Value
		}
	}
	if !nonOverflowingDivisorLiteral(rhs) {
		t.Errorf("`-2` must count as a provably-safe divisor")
	}
}

// TestCatchAllArmOverflowingLiteral：`a * b` 全字面量但乘積會溢位 → 不可證明 →
// 報 arm-should（不能把「其實会失败」的臂說成死臂 —— 安全方向必须保守）。
func TestCatchAllArmOverflowingLiteral(t *testing.T) {
	src := `f = () (res i64) {
    ao = 3037000500 * 3037000500
    a = ao: {
        ok -> it
        ->
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armShouldTraceID); got != 1 {
		t.Fatalf("expected 1 %s for overflowing literal product, got %d: %+v",
			armShouldTraceID, got, res)
	}
	if got := countTrace(res, armUnreachTraceID); got != 0 {
		t.Errorf("must NOT claim an overflowing arm is unreachable, got %d", got)
	}
}

// TestCatchAllArmReboundVarNotTraced：變數被綁定兩次 → 無法確定唯一初始器 → 保守
// 報 arm-should。
func TestCatchAllArmReboundVarNotTraced(t *testing.T) {
	src := `f = (z i64) (res i64) {
    ao = 1 + 2
    ao = z + 1
    a = ao: {
        ok -> it
        ->
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armShouldTraceID); got != 1 {
		t.Fatalf("expected 1 %s for rebound var, got %d: %+v", armShouldTraceID, got, res)
	}
}

// TestCatchAllArmSeverityRouting：經 RunAllLints 時，arm-unreach 必須是 HINT、
// arm-should 必須是 WARNING（D9 的雙態分級由 lint.go 逐條依 trace 決定）。
func TestCatchAllArmSeverityRouting(t *testing.T) {
	dead := `f = () (res i64) {
    ao = 1 + 2
    a = ao: {
        ok -> it
        ->
    }
    res = a
}
`
	live := `f = (x i64, y i64) (res i64) {
    ao = x + y
    a = ao: {
        ok -> it
        ->
    }
    res = a
}
`
	for _, c := range []struct {
		name string
		src  string
		id   string
		want LintSeverity
	}{
		{"dead arm is a HINT", dead, armUnreachTraceID, LintHint},
		{"live arm is a WARNING", live, armShouldTraceID, LintWarning},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := RunAllLints(parseProg(t, c.src), LintOptions{SourcePath: "t.no"})
			seen := 0
			for _, r := range out {
				if r.TraceID != c.id {
					continue
				}
				seen++
				if r.Severity != c.want {
					t.Errorf("trace %s: got severity %q, want %q", c.id, r.Severity, c.want)
				}
			}
			if seen != 1 {
				t.Fatalf("expected exactly 1 %s from RunAllLints, got %d", c.id, seen)
			}
		})
	}
}

// TestCatchAllArmNotForValueMatch：值 match（`1 -> ...`）不是 option match，
// 不報任何 D9 診斷。
func TestCatchAllArmNotForValueMatch(t *testing.T) {
	src := `f = (x i64) (res i64) {
    a = x: {
        1 -> 10
        -> 20
    }
    res = a
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if len(res) != 0 {
		t.Fatalf("value match must not produce D9 diagnostics, got %+v", res)
	}
}

// TestCatchAllArmNotForNonExhaustive：沒有兜底臂的 match 由
// CollectNonExhaustiveMatches 負責，D9 不管。
func TestCatchAllArmNotForNonExhaustive(t *testing.T) {
	src := `f = () () {
    size = stat-size("x")
    size: {
        ok -> {
            n = 1
        }
    }
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armShouldTraceID) + countTrace(res, armUnreachTraceID); got != 0 {
		t.Fatalf("match without a catch-all arm must not produce D9 diagnostics, got %+v", res)
	}
	if n := countMatchNonex(ValidateNonExhaustiveMatch(parseProg(t, src))); n != 1 {
		t.Fatalf("expected the non-exhaustive rule to own that case, got %d", n)
	}
}

// TestCatchAllArmIndexSubjectLimitation：以索引為主體的 match（`x = a[0]: {...}`）
// 降級後的 IfExpression 沒有 MatchedExpr（D7-R1 豁免後讀取不再是 option 基底），
// collectMatchRoots 因此看不見它 —— 這是已知限制，暫不提示。
// 釘住現況，避免日後誤以為已經支援。
func TestCatchAllArmIndexSubjectLimitation(t *testing.T) {
	src := `f = () (res i64) {
    a [4]i64 = [11, 22, 33, 44]
    x = a[0]: {
        ok -> it
        ->
    }
    res = x
}
`
	roots, _, _ := collectMatchRoots(parseProg(t, src))
	if len(roots) != 0 {
		t.Errorf("expected 0 visible match roots for an index subject, got %d", len(roots))
	}
	if res := ValidateCatchAllArms(parseProg(t, src)); len(res) != 0 {
		t.Errorf("index-subject match is a known limitation; got %+v", res)
	}
}

// TestFoldIntArithDivZeroAndOverflow 釘住 foldIntArith 的失敗方向。
func TestFoldIntArithDivZeroAndOverflow(t *testing.T) {
	cases := []struct {
		expr string
		ok   bool
		want int64
	}{
		{"1 + 2", true, 3},
		{"2 * 3", true, 6},
		{"10 - 3", true, 7},
		{"10 / 3", true, 3},
		{"10 % 3", true, 1},
		{"1 / 0", false, 0},
		{"1 % 0", false, 0},
		{"3037000500 * 3037000500", false, 0},
		{"9223372036854775807 + 1", false, 0},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			src := "f = (x i64) (r i64) {\n    y = " + c.expr + "\n    r = y\n}\n"
			fd := parseProg(t, src).Statements[0].(*parser.FunctionDefinition)
			var inf *parser.InfixExpression
			for _, s := range fd.Body.Statements {
				if ls, ok := s.(*parser.LetStatement); ok && ls.Name.Value == "y" {
					inf, _ = ls.Value.(*parser.InfixExpression)
				}
			}
			if inf == nil {
				t.Fatalf("no infix initializer for y in %q", c.expr)
			}
			got, ok := foldIntArith(inf, 0)
			if ok != c.ok {
				t.Fatalf("foldIntArith(%q) ok=%v, want %v (value %d)", c.expr, ok, c.ok, got)
			}
			if ok && got != c.want {
				t.Errorf("foldIntArith(%q) = %d, want %d", c.expr, got, c.want)
			}
		})
	}
}

// TestNonOverflowingDivisorLiteral 釘住「除數是否可證明安全」的判定。
func TestNonOverflowingDivisorLiteral(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"2", true},
		{"8", true},
		{"1", true},
		{"-2", true},
		{"0", false},
		{"-1", false},
		{"y", false},
		{"1 + 1", false}, // 只收字面量，表達式不追
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			src := "f = (x i64, y i64) (r i64) {\n    z = " + c.expr + "\n    r = z\n}\n"
			fd := parseProg(t, src).Statements[0].(*parser.FunctionDefinition)
			var rhs parser.Expression
			for _, s := range fd.Body.Statements {
				if ls, ok := s.(*parser.LetStatement); ok && ls.Name.Value == "z" {
					rhs = ls.Value
				}
			}
			if got := nonOverflowingDivisorLiteral(rhs); got != c.want {
				t.Errorf("nonOverflowingDivisorLiteral(%q) = %v, want %v", c.expr, got, c.want)
			}
		})
	}
}

// TestCatchAllArmDividendArithStillRisky：`(a + b) / 2` 的除零已被除數消掉，但
// `a + b` 自己會溢位 → 仍不可證明 → 報 arm-should。
// 迴歸釘：一度把「被除數是普通變數」誤擴成「被除數也必須可證明」，導致 `x / 2`
// 被漏報；本條與 TestCatchAllArmNonZeroLiteralDivisor 構成方向對照。
func TestCatchAllArmDividendArithStillRisky(t *testing.T) {
	src := `f = (a i64, b i64) (res i64) {
    q = (a + b) / 2
    v = q: {
        ok -> it
        ->
    }
    res = v
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armShouldTraceID); got != 1 {
		t.Fatalf("expected 1 %s for a risky dividend, got %d: %+v",
			armShouldTraceID, got, res)
	}
	if got := countTrace(res, armUnreachTraceID); got != 0 {
		t.Errorf("must NOT claim a `(a+b)/2` arm is unreachable, got %d", got)
	}
}

// TestCatchAllArmPlainVariableDividendSafe：`x / 2` 的被除數是普通變數，變數求值
// 不會失敗 → 可證明 → 報 arm-unreach。這是上面那條的正向對照。
func TestCatchAllArmPlainVariableDividendSafe(t *testing.T) {
	src := `f = (x i64) (res i64) {
    q = x / 2
    v = q: {
        ok -> it
        ->
    }
    res = v
}
`
	res := ValidateCatchAllArms(parseProg(t, src))
	if got := countTrace(res, armUnreachTraceID); got != 1 {
		t.Fatalf("expected 1 %s for a plain-variable dividend, got %d: %+v",
			armUnreachTraceID, got, res)
	}
}
