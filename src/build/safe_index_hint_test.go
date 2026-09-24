package build

import (
	"strings"
	"testing"
)

// TestSafeIndexStrComputedIndex verifies that a bounds-checked safe read of a
// []str element with a COMPUTED integer index — `a-line = lines[pre + k]` under
// `#{index-out=0}` — compiles. Regression for a lowering bug where the read's
// `str` target typeHint leaked into the index sub-expression, so `pre + k` was
// emitted as `add %str-long` and the bounds-check `ge`/`lt` received string
// operands, failing codegen with "ordering comparison ge with a string operand".
// It surfaced as `no build` failing on notools' sdiff-run over a []str.
func TestSafeIndexStrComputedIndex(t *testing.T) {
	src := `main = () () {
  lines []str
  lines.push('a')
  lines.push('b')
  pre = 0
  n = lines.len()
  k <- [0..n): {
    #{overflow=wrap}
    #{index-out=0}
    a-line = lines[pre + k]
    print(a-line)
  }
}
`
	t2 := NewTranspiler(nil)
	_, err := t2.Compile(src)
	if err != nil {
		if strings.Contains(err.Error(), "ordering comparison") ||
			strings.Contains(err.Error(), "%str-long") {
			t.Fatalf("regression: safe-index with computed integer index produced a string-typed "+
				"bounds check (typeHint leaked into the index expression): %v", err)
		}
		t.Fatalf("unexpected compile error: %v", err)
	}
}
