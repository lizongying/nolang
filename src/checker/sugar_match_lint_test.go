package checker

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// 本檔案是 S4 的「非空洞」形態門禁：把每個 linter 的觸發器**放進 match 臂內**，
// 比較 sugar（SkipLowering=true，match 是 SurfaceMatch）與 lowered（match 降糖為
// IfExpression）兩形態下的命中數。若某個 walker 的 switch 漏了 `case *parser.SurfaceMatch`，
// sugar 形態會靜默漏報，sugar 計數就會比 lowered 少——此處直接斷言兩者相等且非零。
//
// 這是對 checker_on_sugar_test.go / sugar_lint_ab_test.go（531/532）的必要補強：
// 那兩份 corpus 門禁是「語料級空洞」的——532 份語料從沒把這些觸發器放進 match 臂內，
// 所以即使 5 個 walker 有盲區，corpus 門禁也過得去（0==0）。本檔的臂內觸發器
// 才是真正能抓到盲區的非空洞尺子（cf. S4 紀律：防 `0 == 0`）。

func parseShape(t *testing.T, src string, sugar bool) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	p.SkipLowering = sugar
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse(%s) errors: %v", map[bool]string{true: "sugar", false: "lowered"}[sugar], errs)
	}
	return prog
}

// deprecated_printf：printf 在 match 臂內。
func TestSugarMatchLintDeprecatedPrintf(t *testing.T) {
	src := `f = () () {
    v = get-v()
    v: {
        ok(x) -> {
            printf('hi')
        }
    }
}
`
	sugar := len(ValidateDeprecatedPrintf(parseShape(t, src, true)))
	lowered := len(ValidateDeprecatedPrintf(parseShape(t, src, false)))
	if sugar == 0 || lowered == 0 {
		t.Fatalf("printf-in-match-arm must be reported on both shapes; got sugar=%d lowered=%d", sugar, lowered)
	}
	if sugar != lowered {
		t.Fatalf("shape mismatch: sugar=%d lowered=%d", sugar, lowered)
	}
}

// listdir：fs.list-dir + fs.remove 在 match 臂內。
func TestSugarMatchLintListDir(t *testing.T) {
	src := `walk = (path str) () {
    entries = fs.list-dir(path)
    entries: {
        ok(es) -> {
            fs.remove(es)
        }
    }
}
`
	sugar := len(ValidateListDirUnfiltered(parseShape(t, src, true)))
	lowered := len(ValidateListDirUnfiltered(parseShape(t, src, false)))
	if sugar == 0 || lowered == 0 {
		t.Fatalf("listdir-in-match-arm must be reported on both shapes; got sugar=%d lowered=%d", sugar, lowered)
	}
	if sugar != lowered {
		t.Fatalf("shape mismatch: sugar=%d lowered=%d", sugar, lowered)
	}
}

// option_compare：?point 與 struct 比較在 match 臂內。
func TestSugarMatchLintOptionCompare(t *testing.T) {
	src := `point = (x i64, y i64) {}
f = (v ?point) () {
    q point = point { x: 1, y: 2 }
    v: {
        ok(p) -> {
            b = v == q
        }
        nil -> {}
        err -> {}
    }
}
`
	sugar := len(ValidateOptionComparison(parseShape(t, src, true)))
	lowered := len(ValidateOptionComparison(parseShape(t, src, false)))
	if sugar == 0 || lowered == 0 {
		t.Fatalf("option-compare-in-match-arm must be reported on both shapes; got sugar=%d lowered=%d", sugar, lowered)
	}
	if sugar != lowered {
		t.Fatalf("shape mismatch: sugar=%d lowered=%d", sugar, lowered)
	}
}

// range_ellipsis：[0...count)（隱式 self 上界）在 match 臂內。
func TestSugarMatchLintRangeEllipsis(t *testing.T) {
	src := `jar { count i64 }
jar.set = (v ?i64) () {
    v: {
        ok(x) -> {
            i <- [0...count): {
                i = i
            }
        }
        nil -> {}
        err -> {}
    }
}
`
	sugar := 0
	for _, r := range ValidateRangeSelfEllipsis(parseShape(t, src, true), "src/app.no") {
		if r.TraceID == rngEllipsisTraceID {
			sugar++
		}
	}
	lowered := 0
	for _, r := range ValidateRangeSelfEllipsis(parseShape(t, src, false), "src/app.no") {
		if r.TraceID == rngEllipsisTraceID {
			lowered++
		}
	}
	if sugar != 1 || lowered != 1 {
		t.Fatalf("rng-ellipsis-in-match-arm must report exactly 1 on both shapes; got sugar=%d lowered=%d", sugar, lowered)
	}
}

// funcargs：foo()（少參）在 match 臂內——經 ValidateFuncArgCount 的 checkArgCountInExpr。
func TestSugarMatchLintFuncArgs(t *testing.T) {
	src := `foo = (a i64) () {}
f = (v ?i64) () {
    v: {
        ok(x) -> {
            foo()
        }
        nil -> {}
        err -> {}
    }
}
`
	sugar := len(ValidateFuncArgCount(parseShape(t, src, true)))
	lowered := len(ValidateFuncArgCount(parseShape(t, src, false)))
	if sugar == 0 || lowered == 0 {
		t.Fatalf("funcarg-undercount-in-match-arm must be reported on both shapes; got sugar=%d lowered=%d", sugar, lowered)
	}
	if sugar != lowered {
		t.Fatalf("shape mismatch: sugar=%d lowered=%d", sugar, lowered)
	}
}
