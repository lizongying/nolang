package build

import (
	"testing"

	"github.com/lizongying/nolang/mir"
)

// TestFirstFatalLowerDiagAcceptsDeprecated pins the backstop gate for removed
// print-family builtins.
//
// printf / eprintf are rejected by the checker (ValidateDeprecatedPrintf) before
// lowering, so the MIR-level guard in lowerNamedFormat is a backstop. A backstop
// that is silently ignored is worse than none: the build would emit a call whose
// target does not exist. `firstFatalLowerDiag` is what turns a lower diagnostic
// into a refused build, so "deprecated" must be one of its fatal kinds.
//
// The same argument covers "map-index" (D9 map subscript lowering): every such
// diagnostic means the get/put was NOT emitted — the read yields nothing and the
// write silently does not happen. A map write that quietly does nothing is a
// wrong-but-exit-0 program, which is worse than a build error.
func TestFirstFatalLowerDiagAcceptsDeprecated(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want bool
	}{
		{"interp (pre-existing fatal kind)", "interp", true},
		{"deprecated (removed builtin backstop)", "deprecated", true},
		{"map-index (dropped map subscript)", "map-index", true},
		{"an unrelated non-fatal kind", "for-range", false},
		{"another unrelated non-fatal kind", "unsupported", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags := []mir.LowerDiag{
				// A non-fatal diag first, so the test also pins that the scan
				// does not simply look at diags[0].
				{Func: "f", Kind: "for-range", Msg: "noise"},
				{Func: "f", Kind: tc.kind, Msg: "payload"},
			}
			got, ok := firstFatalLowerDiag(diags)
			if ok != tc.want {
				t.Fatalf("firstFatalLowerDiag ok=%v, want %v (kind=%q)", ok, tc.want, tc.kind)
			}
			if ok && got.Kind != tc.kind {
				t.Errorf("returned kind %q, want %q", got.Kind, tc.kind)
			}
		})
	}
}

// TestFirstFatalLowerDiagEmpty pins the trivial case: no diagnostics, no verdict.
func TestFirstFatalLowerDiagEmpty(t *testing.T) {
	if d, ok := firstFatalLowerDiag(nil); ok {
		t.Fatalf("firstFatalLowerDiag(nil) = (%+v, true), want (zero, false)", d)
	}
}
