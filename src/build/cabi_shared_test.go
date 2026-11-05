package build

import (
	"strings"
	"testing"
)

// The build layer owns the `--shared` wiring: SetShared must reach
// mir.Module.NoMainEntry before EmitLLVM, and CABIHeader must be available to
// BuildProgram (which refuses a shared build with no exports). The MIR tests
// cover wrapper/header generation; these cover the flag propagation.
const cabiSharedSrc = `#{cabi='nolang_add'}
nl-add = (a i64, b i64) (r i64) {
  r = a + b
}
`

func TestSharedTranspilerSuppressesCMain(t *testing.T) {
	shared := NewTranspiler(nil)
	shared.SetShared(true)
	ir, err := shared.Compile(cabiSharedSrc)
	if err != nil {
		t.Fatalf("shared Compile: %v", err)
	}
	if strings.Contains(ir, "define i32 @main(") {
		t.Error("a shared-library module must not export a C main entry")
	}
	for _, want := range []string{
		"@nolang_argc = hidden global i32 0",
		"define default i64 @nolang_add(i64 %a0, i64 %a1) {",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("shared IR is missing %q", want)
		}
	}
	// Internal helpers must be hidden, so the dylib does not export them.
	if !strings.Contains(ir, "define hidden void @nl_add(") {
		t.Error("internal function must be hidden in a shared-library build")
	}
	if strings.Contains(ir, "@nolang_argc = global i32 0") {
		t.Error("argc global must be hidden, not default-visible")
	}
	hdr := shared.CABIHeader()
	if !strings.Contains(hdr, "int64_t nolang_add(int64_t a0, int64_t a1);") {
		t.Errorf("shared build did not expose a C header: %q", hdr)
	}

	// Executable builds are unchanged: SetShared is opt-in and defaults off.
	exe := NewTranspiler(nil)
	exeIR, err := exe.Compile(cabiSharedSrc)
	if err != nil {
		t.Fatalf("executable Compile: %v", err)
	}
	if !strings.Contains(exeIR, "define i32 @main(") {
		t.Error("executable build lost its C entry point")
	}
}

func TestSharedTranspilerHeaderEmptyWithoutExports(t *testing.T) {
	tr := NewTranspiler(nil)
	tr.SetShared(true)
	if _, err := tr.Compile("print('hi')\n"); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if hdr := tr.CABIHeader(); hdr != "" {
		t.Fatalf("header for a program without #{cabi} exports = %q, want empty", hdr)
	}
}
