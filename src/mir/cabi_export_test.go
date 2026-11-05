package mir

import (
	"strings"
	"testing"
)

// The C ABI export path is a compiler-native replacement for the old
// "no build -v -> hand-written shim" workaround. These tests pin the four
// properties that workaround needed external scripts for:
//
//   1. an exported function survives LowerHIR reachability elimination even
//      when NOTHING in the program calls it;
//   2. the emitted wrapper has a normal return value and a STABLE symbol;
//   3. the generated header matches the wrapper signatures exactly;
//   4. non-scalar signatures are rejected instead of silently corrupting C
//      callers that assume a scalar ABI.
//
// lowerSourceForTest (arg_type_guard_test.go) parses + ASTToHIR + LowerHIR,
// which is the same HIR shape build/transpiler_codegen.go feeds emitMIR.

const cabiThreeExports = `#{cabi='nolang_add'}
nl-add = (a i64, b i64) (r i64) {
  r = a + b
}
#{cabi='nolang_scale'}
nl-scale = (x f64, k f64) (r f64) {
  r = x * k
}
#{cabi='nolang_sum3'}
nl-sum3 = (a i64, b i64, c i64) (r i64) {
  r = a + b + c
}
`

func TestCABIExportSeedsUnreferencedFunctions(t *testing.T) {
	mod := lowerSourceForTest(t, cabiThreeExports)
	if got, want := len(mod.CABIExports), 3; got != want {
		t.Fatalf("CABIExports = %d, want %d: %+v", got, want, mod.CABIExports)
	}
	for i, want := range []CABIExport{
		{Symbol: "nolang_add", FuncName: "nl-add"},
		{Symbol: "nolang_scale", FuncName: "nl-scale"},
		{Symbol: "nolang_sum3", FuncName: "nl-sum3"},
	} {
		if mod.CABIExports[i] != want {
			t.Fatalf("CABIExports[%d] = %+v, want %+v", i, mod.CABIExports[i], want)
		}
		// The decisive assertion: no top-level statement calls any of these
		// functions. Without the extra reachability roots in LowerHIR this map
		// has no entry and the wrapper has no body to call.
		if _, ok := mod.FuncByName[want.FuncName]; !ok {
			t.Errorf("exported function %q was dropped by reachability analysis", want.FuncName)
		}
	}
}

func TestCABIExportEmitsNormalReturnWrappers(t *testing.T) {
	mod := lowerSourceForTest(t, cabiThreeExports)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	for _, want := range []string{
		"define i64 @nolang_add(i64 %a0, i64 %a1) {",
		"define double @nolang_scale(double %a0, double %a1) {",
		"define i64 @nolang_sum3(i64 %a0, i64 %a1, i64 %a2) {",
		"call void @nl_add(i64 %a0, i64 %a1, i64* %cabi.r)",
		"call void @nl_scale(double %a0, double %a1, double* %cabi.r)",
		"ret i64 %cabi.v",
		"ret double %cabi.v",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("emitted IR is missing %q", want)
		}
	}
	if strings.Count(ir, "define i64 @nolang_add(") != 1 {
		t.Error("expected exactly one nolang_add wrapper")
	}
}

func TestCABISharedModuleOmitsCMainButKeepsArgGlobals(t *testing.T) {
	// Executable builds must stay byte-identical (the C `main` is still
	// emitted), while `no build --shared` sets NoMainEntry so a dynamic
	// library cannot interpose on the host executable's own main.
	exe := lowerSourceForTest(t, cabiThreeExports)
	exeIR, err := exe.EmitLLVM()
	if err != nil {
		t.Fatalf("executable EmitLLVM: %v", err)
	}
	if !strings.Contains(exeIR, "define i32 @main(i32 %0, i8** %1) {") {
		t.Error("executable module lost its C entry point")
	}

	lib := lowerSourceForTest(t, cabiThreeExports)
	lib.NoMainEntry = true
	libIR, err := lib.EmitLLVM()
	if err != nil {
		t.Fatalf("shared EmitLLVM: %v", err)
	}
	if strings.Contains(libIR, "define i32 @main(") {
		t.Error("shared library must not export a C main symbol")
	}
	// os.args / os.arg read these globals directly from builtin_call.go;
	// dropping them together with the entry would produce unverifiable IR.
	for _, want := range []string{
		"@nolang_argc = hidden global i32 0",
		"@nolang_argv = hidden global i8** null",
		"define default i64 @nolang_add(i64 %a0, i64 %a1) {",
	} {
		if !strings.Contains(libIR, want) {
			t.Errorf("shared IR is missing %q", want)
		}
	}
}

func TestCABISharedModuleHidesInternalSymbols(t *testing.T) {
	// `no build --shared` must export ONLY the C ABI wrappers. Every other
	// function and module global is hidden, so the produced dylib/.so/.dll does
	// not leak __nolang_main, _nolang_rc_*, _nolang_async_*, @nolang_argc, etc.
	lib := lowerSourceForTest(t, cabiThreeExports)
	lib.NoMainEntry = true
	libIR, err := lib.EmitLLVM()
	if err != nil {
		t.Fatalf("shared EmitLLVM: %v", err)
	}
	// The three wrappers are explicitly default-visible (exported).
	for _, sym := range []string{"nolang_add", "nolang_scale", "nolang_sum3"} {
		if !strings.Contains(libIR, "define default i64 @"+sym+"(") &&
			!strings.Contains(libIR, "define default double @"+sym+"(") {
			t.Errorf("wrapper %s must be default-visible, got:\n%s", sym, libIR)
		}
	}
	// The internal functions they call must be hidden, not exported.
	for _, internal := range []string{
		"define hidden void @nl_add(",
		"define hidden void @nl_scale(",
		"define hidden void @nl_sum3(",
		"define hidden void @print_i64(",
	} {
		if !strings.Contains(libIR, internal) {
			t.Errorf("internal symbol not hidden: want %q in IR", internal)
		}
	}
	// No `define` line may be left unmarked, EXCEPT local-linkage functions
	// (internal/private) which are already non-exported and keep default
	// visibility. Every other function is either the default-visible wrapper or
	// a hidden (external-linkage) internal helper.
	for _, line := range strings.Split(libIR, "\n") {
		if !strings.HasPrefix(line, "define ") {
			continue
		}
		if strings.Contains(line, " internal ") || strings.Contains(line, " private ") {
			continue
		}
		if !strings.Contains(line, "default ") && !strings.Contains(line, "hidden ") {
			t.Errorf("unmarked function in shared IR: %q", line)
		}
	}
	// Module globals must be hidden too.
	if !strings.Contains(libIR, "@nolang_argc = hidden global") {
		t.Error("argc global must be hidden in a shared library")
	}
}

func TestCABIExecutableModuleStaysByteIdentical(t *testing.T) {
	// The visibility pass runs only for shared builds; an ordinary executable
	// module must not gain any `default`/`hidden` keyword.
	exe := lowerSourceForTest(t, cabiThreeExports)
	exeIR, err := exe.EmitLLVM()
	if err != nil {
		t.Fatalf("executable EmitLLVM: %v", err)
	}
	if strings.Contains(exeIR, "define default ") || strings.Contains(exeIR, "define hidden ") {
		t.Error("executable module must not be touched by the shared visibility pass")
	}
	if !strings.Contains(exeIR, "define i64 @nolang_add(i64 %a0, i64 %a1) {") {
		t.Error("executable wrapper lost its normal-return shape")
	}
}

func TestCABIHeaderMatchesWrappers(t *testing.T) {
	mod := lowerSourceForTest(t, cabiThreeExports)
	if _, err := mod.EmitLLVM(); err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	hdr, err := mod.CABIHeader()
	if err != nil {
		t.Fatalf("CABIHeader: %v", err)
	}
	for _, want := range []string{
		"#ifndef NOLANG_CABI_H",
		"#include <stdint.h>",
		`extern "C" {`,
		"int64_t nolang_add(int64_t a0, int64_t a1);",
		"double nolang_scale(double a0, double a1);",
		"int64_t nolang_sum3(int64_t a0, int64_t a1, int64_t a2);",
	} {
		if !strings.Contains(hdr, want) {
			t.Errorf("header is missing %q\n--- header ---\n%s", want, hdr)
		}
	}
	// A program with no exports must not produce a header, so executable
	// builds stay byte-identical and `no build --shared` can reject emptiness.
	plain := lowerSourceForTest(t, "f = (a i64) (r i64) {\n  r = a\n}\n")
	empty, err := plain.CABIHeader()
	if err != nil || empty != "" {
		t.Fatalf("header for a program without exports = %q, %v; want empty", empty, err)
	}
}

func TestCABIExportBareFlagDerivesSymbol(t *testing.T) {
	mod := lowerSourceForTest(t, `#{cabi}
nl-add = (a i64, b i64) (r i64) {
  r = a + b
}
`)
	if len(mod.CABIExports) != 1 || mod.CABIExports[0].Symbol != "nolang_nl_add" {
		t.Fatalf("derived symbol = %+v, want nolang_nl_add", mod.CABIExports)
	}
}

func TestCABIExportBoolWidensToU8(t *testing.T) {
	mod := lowerSourceForTest(t, `#{cabi='nolang_flip'}
flip = (a bool) (r bool) {
  r = !a
}
`)
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	for _, want := range []string{
		"define i8 @nolang_flip(i8 %a0) {",
		"%cabi.t0 = trunc i8 %a0 to i1",
		"call void @flip(i1 %cabi.t0, i1* %cabi.r)",
		"%cabi.z = zext i1 %cabi.v to i8",
		"ret i8 %cabi.z",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("emitted IR is missing %q", want)
		}
	}
	hdr, err := mod.CABIHeader()
	if err != nil {
		t.Fatalf("CABIHeader: %v", err)
	}
	if !strings.Contains(hdr, "uint8_t nolang_flip(uint8_t a0);") {
		t.Errorf("bool must be exposed as uint8_t, got:\n%s", hdr)
	}
}

func TestCABIExportRejectsNonScalarSignatures(t *testing.T) {
	cases := map[string]string{
		"str result": `#{cabi='bad_str'}
f = (a i64) (r str) {
  r = 'x'
}
`,
		"?T result": `#{cabi='bad_opt'}
f = (a i64) (r ?i64) {
  r = a
}
`,
		"vec result": `#{cabi='bad_vec'}
f = (a i64) (r []i64) {
  r = [a]
}
`,
		"i128 result": `#{cabi='bad_i128'}
f = (a i128) (r i128) {
  r = a
}
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			mod := lowerSourceForTest(t, src)
			if _, err := mod.EmitLLVM(); err == nil {
				t.Fatal("expected EmitLLVM to reject a non-scalar #{cabi} signature")
			} else if !strings.Contains(err.Error(), "unsupported") {
				t.Fatalf("expected a scalar-only diagnostic, got: %v", err)
			}
		})
	}
}

func TestCABIExportRejectsDuplicateAndInvalidSymbols(t *testing.T) {
	src := `#{cabi='dup'}
f = (a i64) (r i64) {
  r = a
}
#{cabi='dup'}
g = (a i64) (r i64) {
  r = a
}
#{cabi='9bad-name'}
h = (a i64) (r i64) {
  r = a
}
`
	_, _, diags := LowerHIR(parseHIR(t, src), nil)
	joined := ""
	for _, d := range diags {
		if d.Kind == "cabi" {
			joined += d.Msg + "\n"
		}
	}
	if !strings.Contains(joined, "duplicate C symbol") {
		t.Errorf("missing duplicate-symbol diagnostic, got:\n%s", joined)
	}
	if !strings.Contains(joined, "valid C identifier") {
		t.Errorf("missing invalid-symbol diagnostic, got:\n%s", joined)
	}
}
