package checker

import (
	"strings"
	"testing"
)

// undefinedFor returns the "is not defined" diagnostic emitted for the given
// bare identifier, or "" when none was reported.
func undefinedFor(results []ValidateResult, name string) string {
	wanted := "'" + name + "' is not defined"
	for _, r := range results {
		if strings.Contains(r.Message, wanted) {
			return r.Message
		}
	}
	return ""
}

// TestUndefinedVarsCrossModuleStdRequiresPrefix is the core global.no contract:
// a std function owned by a module (async.cancelled) must NOT be callable bare
// from another module, and the diagnostic carries the module-qualified hint.
func TestUndefinedVarsCrossModuleStdRequiresPrefix(t *testing.T) {
	prog := mustParse(t, "x = cancelled()\n")
	results := ValidateUndefinedVars(prog, "")
	msg := undefinedFor(results, "cancelled")
	if msg == "" {
		t.Fatalf("expected bare 'cancelled' to be flagged as undefined, got: %+v", results)
	}
	if !strings.Contains(msg, "async.cancelled") {
		t.Fatalf("expected hint to name 'async.cancelled', got: %q", msg)
	}
}

// TestUndefinedVarsModuleQualifiedStdCallIsAccepted asserts the qualified form
// (async.cancelled()) resolves cleanly — the DotExpression receiver is a known
// module name and must never be reported undefined.
func TestUndefinedVarsModuleQualifiedStdCallIsAccepted(t *testing.T) {
	prog := mustParse(t, "x = async.cancelled()\n")
	results := ValidateUndefinedVars(prog, "")
	if msg := undefinedFor(results, "cancelled"); msg != "" {
		t.Fatalf("unexpected undefined for qualified async.cancelled: %q\nall: %+v", msg, results)
	}
	if msg := undefinedFor(results, "async"); msg != "" {
		t.Fatalf("module name 'async' should be defined: %q", msg)
	}
}

// TestUndefinedVarsLocalFunctionDefAllowsBareCall verifies the "own module"
// carve-out: a locally defined function named 'cancelled' is callable bare.
func TestUndefinedVarsLocalFunctionDefAllowsBareCall(t *testing.T) {
	prog := mustParse(t, "cancelled = () (r bool) {\n    r = false\n}\nx = cancelled()\n")
	results := ValidateUndefinedVars(prog, "")
	if msg := undefinedFor(results, "cancelled"); msg != "" {
		t.Fatalf("locally defined 'cancelled' must be bare-callable, got: %q\nall: %+v", msg, results)
	}
}

// TestUndefinedVarsTrulyGlobalFunctionsStayBare asserts the 6 global.no
// functions (here print/format) remain bare-callable.
func TestUndefinedVarsTrulyGlobalFunctionsStayBare(t *testing.T) {
	prog := mustParse(t, "print('hi')\ns = format('hi')\n")
	results := ValidateUndefinedVars(prog, "")
	for _, fn := range []string{"print", "format"} {
		if msg := undefinedFor(results, fn); msg != "" {
			t.Fatalf("truly-global %q must stay bare-callable, got: %q\nall: %+v", fn, msg, results)
		}
	}
}

// TestUndefinedVarsStdConstantStaysBare verifies a module-level std constant
// (bigint.no MASK) is still reachable bare — constants are grandfathered to
// preserve existing behavior.
func TestUndefinedVarsStdConstantStaysBare(t *testing.T) {
	prog := mustParse(t, "x = MASK\n")
	results := ValidateUndefinedVars(prog, "")
	if msg := undefinedFor(results, "MASK"); msg != "" {
		t.Fatalf("std constant 'MASK' must stay bare, got: %q\nall: %+v", msg, results)
	}
}

// TestUndefinedVarsUnionMethodShortFormStaysBare verifies union methods
// (num.abs → bare abs) keep their short-name alias.
func TestUndefinedVarsUnionMethodShortFormStaysBare(t *testing.T) {
	prog := mustParse(t, "x = abs(-1)\n")
	results := ValidateUndefinedVars(prog, "")
	if msg := undefinedFor(results, "abs"); msg != "" {
		t.Fatalf("union-method short form 'abs' must stay bare, got: %q\nall: %+v", msg, results)
	}
}

// TestUndefinedVarsSelfModuleBareIsAllowed verifies the module-aware carve-out:
// a std module calling its OWN function bare (math.no calling f64-to-i64) is
// legal, while the same call from another module is still flagged. The entry
// file resolves to the current module; the merged self-module statements carry
// an empty SourceFile and fall back to it.
func TestUndefinedVarsSelfModuleBareIsAllowed(t *testing.T) {
	// Within math.no (owner == curModule) -> no diagnostic.
	prog := mustParse(t, "x = f64-to-i64(1.5)\n")
	results := ValidateUndefinedVars(prog, "", "/repo/src/std/math.no")
	if msg := undefinedFor(results, "f64-to-i64"); msg != "" {
		t.Fatalf("self-module bare 'f64-to-i64' inside math.no must be allowed, got: %q\nall: %+v", msg, results)
	}
	// From a different module (str.no) -> still a cross-module violation.
	prog2 := mustParse(t, "x = f64-to-i64(1.5)\n")
	results2 := ValidateUndefinedVars(prog2, "", "/repo/src/std/str.no")
	msg2 := undefinedFor(results2, "f64-to-i64")
	if msg2 == "" {
		t.Fatalf("cross-module bare 'f64-to-i64' from str.no must be flagged, got: %+v", results2)
	}
	if !strings.Contains(msg2, "math.f64-to-i64") {
		t.Fatalf("expected hint to name 'math.f64-to-i64', got: %q", msg2)
	}
}

// TestUndefinedVarsMultiOwnerNameStaysBare verifies that a function name owned
// by 2+ std modules (now-ms lives in both os.no and time.no) has no canonical
// prefix, so it is excluded from the gate and stays bare-permissive rather than
// being forced to an arbitrary single module's qualified form.
func TestUndefinedVarsMultiOwnerNameStaysBare(t *testing.T) {
	prog := mustParse(t, "x = now-ms()\n")
	results := ValidateUndefinedVars(prog, "")
	if msg := undefinedFor(results, "now-ms"); msg != "" {
		t.Fatalf("multi-owner bare 'now-ms' must stay permissive, got: %q\nall: %+v", msg, results)
	}
}
