package mir

import (
	"strings"
	"testing"
)

// Vec builtins must emit binary LLVM subtraction instructions. A chained
// subtraction written as one `sub` produces three operands and is rejected by
// the LLVM verifier before the program can run.
func TestVecBuiltinArithmeticEmitsBinarySubtractions(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		marker string
	}{
		{
			name:   "reverse",
			marker: "%rvj",
			src: `Values []i64 = [1, 2, 3, 4]

run = () {
    Values.reverse()
}

run()
`,
		},
		{
			name:   "remove",
			marker: "%rmrc",
			src: `Values []i64 = [1, 2, 3, 4]

run = () {
    removed = Values.remove(1)
}

run()
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ir, err := lowerSourceForTest(t, tc.src).EmitLLVM()
			if err != nil {
				t.Fatalf("vec.%s lowering failed: %v", tc.name, err)
			}
			if !strings.Contains(ir, tc.marker) {
				t.Fatalf("vec.%s builtin was not lowered (%s missing)", tc.name, tc.marker)
			}
			subtractions := 0
			for _, line := range strings.Split(ir, "\n") {
				if !strings.Contains(line, "= sub i64 ") {
					continue
				}
				subtractions++
				operands := strings.SplitN(line, "= sub i64 ", 2)[1]
				if strings.Count(operands, ",") != 1 {
					t.Errorf("vec.%s emitted a non-binary subtraction: %s", tc.name, strings.TrimSpace(line))
				}
			}
			if subtractions < 2 {
				t.Errorf("vec.%s emitted %d subtractions; chained arithmetic was not lowered", tc.name, subtractions)
			}
		})
	}
}
