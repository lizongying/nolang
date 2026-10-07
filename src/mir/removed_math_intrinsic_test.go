package mir

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/builtin"
)

// ---------------------------------------------------------------------------
// No registered builtin may name an LLVM intrinsic that LLVM 21 REMOVED.
//
// Why this is a test and not just a comment: the whole point of the
// "refactor(math): remove libm dependency" change is that the toolchain links NO
// math library. `emitBuiltin`'s LLVMIntrinsic branch used to silently paper over
// the removal by emitting `declare double @sin(...)` and calling the libm
// function of the same name — which puts `-lm` back on the link line and
// resurrects the exact cross-platform failure the refactor removes
// (`undefined reference to log10/floor/exp` when linking on Linux x86_64).
//
// That branch is now a hard `c.fail` tripwire (see removedMathIntrinsic), and
// this test is the OTHER half: it catches the same mistake at the registry, i.e.
// at the moment someone re-adds `LLVMIntrinsic: "llvm.sin.f64"` to
// src/builtin/math_f64.go, without needing a program that calls it.
//
// Non-vacuity is asserted below: the registry must be non-empty AND must
// actually contain `llvm.sqrt.f64` (the one intrinsic that is allowed, because
// it is a hardware instruction with no libm involvement). Without that half the
// test would pass on an empty registry.
// ---------------------------------------------------------------------------

func TestNoBuiltinUsesRemovedMathIntrinsic(t *testing.T) {
	if len(builtin.BuiltinMethodList) == 0 {
		t.Fatal("BuiltinMethodList is empty — this test would be vacuous")
	}

	// The allowed intrinsic: llvm.sqrt.f64 lowers to the hardware fsqrt.
	const allowed = "llvm.sqrt.f64"
	seenAllowed := false

	for i := range builtin.BuiltinMethodList {
		bm := &builtin.BuiltinMethodList[i]
		if bm.LLVMIntrinsic == "" {
			continue
		}
		if bm.LLVMIntrinsic == allowed {
			seenAllowed = true
		}
		if libm, gone := removedMathIntrinsic[bm.LLVMIntrinsic]; gone {
			t.Errorf("builtin %q is registered with LLVM intrinsic %q, which LLVM 21 removed. "+
				"Lowering it would emit `declare double @%s(...)` and put -lm back on the link line. "+
				"Implement it in pure Nolang (src/std/number.no / src/std/math.no) and drop the "+
				"LLVMIntrinsic registration.", bm.MethodName, bm.LLVMIntrinsic, libm)
		}
	}

	if !seenAllowed {
		t.Errorf("no builtin registers %q — either the registry lost its only LLVM intrinsic "+
			"(fine, delete this assertion) or this test stopped looking at the real registry", allowed)
	}
}

// TestRemovedMathIntrinsicCoversTranscendentals pins the tripwire's own contents.
// An EMPTY map would make TestNoBuiltinUsesRemovedMathIntrinsic vacuous, and a
// map missing the name a developer is most likely to reach for (sin/cos/pow) is
// the same thing with extra steps.
func TestRemovedMathIntrinsicCoversTranscendentals(t *testing.T) {
	want := []string{
		"llvm.sin.f64", "llvm.cos.f64", "llvm.tan.f64",
		"llvm.asin.f64", "llvm.acos.f64", "llvm.atan.f64", "llvm.atan2.f64",
		"llvm.sinh.f64", "llvm.cosh.f64", "llvm.tanh.f64",
		"llvm.exp.f64", "llvm.exp2.f64", "llvm.exp10.f64",
		"llvm.log.f64", "llvm.log2.f64", "llvm.log10.f64",
		"llvm.pow.f64",
	}
	for _, k := range want {
		if _, ok := removedMathIntrinsic[k]; !ok {
			t.Errorf("removedMathIntrinsic is missing %q", k)
		}
	}
	// llvm.sqrt.f64 is a hardware instruction and must NOT be in the tripwire.
	if libm, ok := removedMathIntrinsic["llvm.sqrt.f64"]; ok {
		t.Errorf("llvm.sqrt.f64 must not be in the tripwire (it is a hardware instruction), got libm=%q", libm)
	}
	// Every entry must name a plausible libm symbol (guards a typo'd value).
	for k, v := range removedMathIntrinsic {
		if v == "" || strings.ContainsAny(v, " .@") {
			t.Errorf("removedMathIntrinsic[%q] = %q is not a plausible C symbol", k, v)
		}
	}
}
