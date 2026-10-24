package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnwrapMethodCallRHS pins the Gap-3 regression: `?=` whose RHS is a
// *method* call returning an option (here `s.to-i64()` → ?i64), with the
// unwrap target being a non-result local (the propagate() branch).
//
// Root cause (2026-10-24): the H0 scratch parser reconstructs its type
// tables from SemanticContext.ExternFuncSigs / ExternMethodSigs, but those
// snapshots were (a) dropped by SemanticContext.Merge and (b) never
// serialized into the Tier-1 user-AST disk cache. So on the H0 path the
// scratch parser had empty signature tables, inferTypeFromCallExpr could not
// resolve `str.to-i64`'s ?i64 return type, the `__unwrap_N` temp was emitted
// with a void type, and MIR codegen failed with "EmitLLVM: move src slot …".
//
// The fix (SemanticContext.MergeExternSigs + re-injection at the transpiler
// merge point) restores the builtin signatures for both fresh-parse and
// cache-hit paths. Compilation succeeding here is the regression gate: a
// relapse re-triggers the "move src slot" compile error.
func TestUnwrapMethodCallRHS(t *testing.T) {
	src := `get-val = (s str) (r ?i64) {
    r = nil
    v ?= s.to-i64()
    r = v
}

x ?i64 = get-val('123')
_ = x
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "workspace.jsonc"), []byte(`{"test":"."}`), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.no")
	if err := os.WriteFile(mainPath, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	trans := NewTranspiler(nil)
	trans.sourcePath = mainPath
	ir, err := trans.Compile(src)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	// The unwrap temp must carry the ?i64 option payload, not a void slot.
	// str.to-i64 lowers to @str_to_i64(%str-long*, %option*): its ?i64
	// result must land in an %option-typed slot.
	if !strings.Contains(ir, "@str_to_i64") {
		t.Fatalf("expected IR to reference @str_to_i64 (str.to-i64), got:\n%s", ir)
	}
}
