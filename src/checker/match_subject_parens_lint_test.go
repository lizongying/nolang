package checker

import "testing"

// countMatchSubjectParens 回傳 ValidateMatchSubjectParens 對 src 產出的
// msubjparen HINT 數。
func countMatchSubjectParens(t *testing.T, src string) int {
	t.Helper()
	prog := parseProg(t, src)
	n := 0
	for _, r := range ValidateMatchSubjectParens(prog, "src/app.no") {
		if r.TraceID == matchSubjectParensTraceID {
			n++
		}
	}
	return n
}

// TestMatchSubjectParensHint：`v = (expr): { arms }` 主語括號多餘，應提示一次。
func TestMatchSubjectParensHint(t *testing.T) {
	src := `f = (a i64, b i64) (out i64) {
    out = (a + b): {
        ok -> it

        -> 0
    }
}
`
	if got := countMatchSubjectParens(t, src); got != 1 {
		t.Fatalf("expected 1 msubjparen hint for parenthesised subject, got %d", got)
	}
}

// TestMatchSubjectParensNested：多層括號仍只提示一次（同一個 GroupedExpression 鏈）。
func TestMatchSubjectParensNested(t *testing.T) {
	src := `f = (a i64, b i64) (out i64) {
    out = ((a + b)): {
        ok -> it

        -> 0
    }
}
`
	if got := countMatchSubjectParens(t, src); got != 1 {
		t.Fatalf("expected 1 msubjparen hint for nested parentheses, got %d", got)
	}
}

// TestMatchSubjectParensNoHintBare：不加括號是建議寫法，不得提示。
func TestMatchSubjectParensNoHintBare(t *testing.T) {
	src := `f = (a i64, b i64) (out i64) {
    out = a + b: {
        ok -> it

        -> 0
    }
}
`
	if got := countMatchSubjectParens(t, src); got != 0 {
		t.Fatalf("bare subject must not be hinted, got %d", got)
	}
}

// TestMatchSubjectParensNoHintIdentifier：識別字主語手動加括號也提示（同屬多餘）。
func TestMatchSubjectParensNoHintIdentifier(t *testing.T) {
	src := `f = (o ?i64) (out i64) {
    out = (o): {
        ok -> it

        -> 0
    }
}
`
	if got := countMatchSubjectParens(t, src); got != 1 {
		t.Fatalf("parenthesised identifier subject must be hinted too, got %d", got)
	}
}
