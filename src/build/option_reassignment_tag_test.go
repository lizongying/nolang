package build

import (
	"strings"
	"testing"
)

// Reassigning an option constructor to an existing option slot must preserve the
// constructor's tag. This runs the checked, merged pipeline: parser-only HIR
// lowering does not reproduce the std alias collision that mistypes the target.
func TestOptionConstructorReassignmentPreservesTagInFullPipeline(t *testing.T) {
	skipWithoutStdSigKey(t)
	t.Setenv("NOLANG_AST_CACHE_OFF", "1")
	t.Setenv("NOLANG_COMPTIME", "0")

	const src = `err-probe = (s str) (out ?i64) {
    out = err('invalid number')
}

ok-probe = (n i64) (out ?i64) {
    out = ok(n)
}

run = () {
    a = err-probe('3.5')
    b = ok-probe(7)
    print(a)
    print(b)
}

run()
`

	tr := NewTranspiler(nil)
	ir, err := tr.Compile(src)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	for _, tc := range []struct {
		name string
		tag  string
	}{
		{name: "err_probe", tag: "2"},
		{name: "ok_probe", tag: "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := llvmFunctionBody(t, ir, "@"+tc.name+"(")
			want := "store %option_i64 { i8 " + tc.tag + ","
			if got := strings.Count(body, want); got != 1 {
				t.Fatalf("constructor tag %s emitted %d times, want exactly one:\n%s", tc.tag, got, body)
			}
			otherTag := "0"
			if tc.tag == "0" {
				otherTag = "2"
			}
			if got := strings.Count(body, "store %option_i64 { i8 "+otherTag+","); got != 0 {
				t.Fatalf("constructor was rewrapped with tag %s %d time(s):\n%s", otherTag, got, body)
			}
		})
	}
}

func llvmFunctionBody(t *testing.T, ir, signature string) string {
	t.Helper()
	for offset := 0; offset < len(ir); {
		rel := strings.Index(ir[offset:], signature)
		if rel < 0 {
			break
		}
		start := offset + rel
		lineStart := strings.LastIndex(ir[:start], "\n") + 1
		if strings.HasPrefix(ir[lineStart:start], "define ") {
			endRel := strings.Index(ir[start:], "\n}\n")
			if endRel < 0 {
				t.Fatalf("LLVM function %s has no closing brace", signature)
			}
			return ir[start : start+endRel+3]
		}
		offset = start + len(signature)
	}
	t.Fatalf("LLVM IR is missing function definition %s", signature)
	return ""
}
