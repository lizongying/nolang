package mir

import (
	"strings"
	"testing"
)

// Implicit comptime: the compiler folds a pure expression to a constant at
// compile time, with NO annotation from the user. These tests pin both
// directions of that behaviour:
//
//   - with comptime ON  the folded constant must be materialised;
//   - with comptime OFF the same constant must NOT appear.
//
// The OFF arm is what makes the ON arm meaningful. Asserting only that a
// constant exists would pass even if some unrelated lowering produced it, so
// every case here also proves the constant is absent when the evaluator is
// disabled — i.e. that the fold is what produced it.

// comptimePair lowers src twice: once with the evaluator enabled (its default)
// and once switched off via NOLANG_COMPTIME=0.
func comptimePair(t *testing.T, src string) (on, off *Module) {
	t.Helper()
	t.Setenv("NOLANG_COMPTIME", "1")
	on = lowerSrc(t, src)
	t.Setenv("NOLANG_COMPTIME", "0")
	off = lowerSrc(t, src)
	return on, off
}

// assertFoldedInt checks that `want` is materialised exactly when comptime is
// on. It is only valid when `want` is NOT already a literal in the source
// (`1 < 2 ? 3 + 4 : 5` -> 7 is fine; `1 < 2 ? 100 : 200` -> 100 is not,
// because the OFF module legitimately holds the literal 100 too).
func assertFoldedInt(t *testing.T, src string, want int64) {
	t.Helper()
	on, off := comptimePair(t, src)
	if !constInts(on)[want] {
		t.Errorf("comptime on: folded constant %d missing; consts=%v", want, constInts(on))
	}
	if constInts(off)[want] {
		t.Errorf("comptime off: constant %d should not exist; consts=%v", want, constInts(off))
	}
}

func hasCallSym(m *Module, substr string) bool {
	for sym := range callSyms(m) {
		if strings.Contains(sym, substr) {
			return true
		}
	}
	return false
}

// runInsts counts the OpRun (task launch) instructions in the module. Spawns
// are not OpCall, so callSyms cannot see them.
func runInsts(m *Module) int {
	n := 0
	for i := range m.Insts {
		if m.Insts[i].Op == OpRun {
			n++
		}
	}
	return n
}

// TestComptimeFoldsPureCall is the interprocedural case: a call to a PURE
// function with all-constant arguments is evaluated at compile time, so the
// call disappears and only the result constant remains.
//
// This is also the regression test for the KLet result-binding bug: Nolang
// returns by binding the result parameter, and `r = <expr>` lowers to KLet
// (not KAssign). evalConstBody used to record env["r"] but never mark it as the
// return value, so the correctly computed 5 was discarded at the end of the
// body walk and no pure call ever folded.
func TestComptimeFoldsPureCall(t *testing.T) {
	src := `add = (a i64, b i64) (r i64) {
    r = a + b
}
main = () () {
    r = add(2, 3)
    print(r)
}
`
	assertFoldedInt(t, src, 5)
	on, _ := comptimePair(t, src)
	if hasCallSym(on, "add") {
		t.Errorf("add(2,3) kept its call instead of folding; syms=%v", callSyms(on))
	}
}

// TestComptimeFoldsConstLetFn: a zero-argument pure function whose whole body
// is constant folds too, proving constant locals propagate through the body
// (`k = 10` -> `r = k * 2 + 1` -> 21).
func TestComptimeFoldsConstLetFn(t *testing.T) {
	src := `propagate = () (r i64) {
    k = 10
    r = k * 2 + 1
}
main = () () {
    r = propagate()
    print(r)
}
`
	assertFoldedInt(t, src, 21)
	on, _ := comptimePair(t, src)
	if hasCallSym(on, "propagate") {
		t.Errorf("propagate() kept its call instead of folding; syms=%v", callSyms(on))
	}
}

// TestComptimeFoldsNestedCalls: folding recurses through a call chain
// (compose -> add -> square). The intermediate `s` only exists inside the
// callee's environment, so this also proves arguments evaluated in one frame
// are visible to the next one.
func TestComptimeFoldsNestedCalls(t *testing.T) {
	src := `add = (a i64, b i64) (r i64) {
    r = a + b
}
square = (a i64) (r i64) {
    r = a * a
}
compose = (a i64, b i64) (r i64) {
    s = add(a, b)
    r = square(s)
}
main = () () {
    r = compose(2, 3)
    print(r)
}
`
	assertFoldedInt(t, src, 25)
	on, _ := comptimePair(t, src)
	if hasCallSym(on, "compose") {
		t.Errorf("compose(2,3) kept its call instead of folding; syms=%v", callSyms(on))
	}
}

// TestComptimeFoldsIfBranch: a pure function whose body has a constant-condition
// `if` folds — only the taken branch runs, so the result is the branch's value.
// The result (51 = 3*17) is NOT a literal in the source, so the OFF arm of
// assertFoldedInt holds: the unoptimised module still performs the 3*17 multiply
// and never materialises 51.
func TestComptimeFoldsIfBranch(t *testing.T) {
	src := `clamp = (a i64) (r i64) {
    a > 10 -> { r = 7 * 13 }
    -> { r = 3 * 17 }
}
main = () () {
    r = clamp(5)
    print(r)
}
`
	assertFoldedInt(t, src, 51)
	on, _ := comptimePair(t, src)
	if hasCallSym(on, "clamp") {
		t.Errorf("clamp(5) kept its call instead of folding; syms=%v", callSyms(on))
	}
}

// TestComptimeFoldsMatchConstant: a pure function that match-es on a constant
// subject folds too. `match` desugars to the same if-chain evalConstBody now
// walks (each arm's condition compares the subject, not the synthetic `it`
// binding, which lives inside the arm body), so a constant subject selects the
// right arm. The result (72 = 8*9) is not a literal in the source.
func TestComptimeFoldsMatchConstant(t *testing.T) {
	src := `classify = (n i64) (r i64) {
    n: {
        0 -> r = 6 * 7
        1 -> r = 8 * 9
        _ -> r = 5 * 5
    }
}
main = () () {
    r = classify(1)
    print(r)
}
`
	assertFoldedInt(t, src, 72)
	on, _ := comptimePair(t, src)
	if hasCallSym(on, "classify") {
		t.Errorf("classify(1) kept its call instead of folding; syms=%v", callSyms(on))
	}
}

// TestComptimeLeavesRuntimeCondIfAlone is the reverse guard: an `if` whose
// condition reads a RUNTIME variable (a range-loop index) cannot fold, so the
// call survives and no constant is hoisted out of it.
func TestComptimeLeavesRuntimeCondIfAlone(t *testing.T) {
	on, _ := comptimePair(t, `pick = (a i64) (r i64) {
    a > 0 -> { r = 7 * 13 }
    -> { r = 3 * 17 }
}
main = () () {
    i <- [0..3) {
        r = pick(i)
        print(r)
    }
}
`)
	if !hasCallSym(on, "pick") {
		t.Errorf("call with a runtime condition was folded away; syms=%v", callSyms(on))
	}
}

// TestComptimeLeavesLoopBodyAlone: a pure function whose body contains a loop is
// not foldable (folding a loop would need unrolling or an invariant proof). The
// call must survive even with constant arguments.
func TestComptimeLeavesLoopBodyAlone(t *testing.T) {
	on, _ := comptimePair(t, `sumto = (n i64) (r i64) {
    r = 0
    k <- [1..n] {
        r = r + k
    }
}
main = () () {
    r = sumto(5)
    print(r)
}
`)
	if !hasCallSym(on, "sumto") {
		t.Errorf("loop body was folded away; syms=%v", callSyms(on))
	}
}

// TestComptimeFoldsMethodCall: a METHOD call folds when its receiver and its
// arguments are compile-time constants. The receiver is the method's first
// argument, so it has to be constant-evaluable too — hence the aggregate
// constant (`point { x: 3, y: 4 }`) that the receiver is bound to.
//
// The method's result (25 = 3*3 + 4*4) is NOT a literal in the source, so the
// OFF arm of assertFoldedInt holds: with the evaluator off, the field reads
// stay runtime loads and 25 is never materialised.
func TestComptimeFoldsMethodCall(t *testing.T) {
	src := `point {
    x i64
    y i64
}
point.dist = () (r i64) {
    r = .x * .x + .y * .y
}
main = () () {
    p = point { x: 3, y: 4 }
    r = p.dist()
    print(r)
}
`
	assertFoldedInt(t, src, 25)
	on, _ := comptimePair(t, src)
	if hasCallSym(on, "dist") {
		t.Errorf("p.dist() kept its call instead of folding; syms=%v", callSyms(on))
	}
}

// TestComptimeLeavesRuntimeReceiverMethodAlone is the reverse guard: the SAME
// method called on a receiver that is only known at run time cannot fold, so
// the call survives. The receiver's field is bound to the loop index, so the
// struct literal is not a compile-time constant either.
func TestComptimeLeavesRuntimeReceiverMethodAlone(t *testing.T) {
	on, _ := comptimePair(t, `point {
    x i64
    y i64
}
point.dist = () (r i64) {
    r = .x * .x + .y * .y
}
main = () () {
    i <- [0..3) {
        p = point { x: i, y: 4 }
        r = p.dist()
        print(r)
    }
}
`)
	if !hasCallSym(on, "dist") {
		t.Errorf("method call with a runtime receiver was folded away; syms=%v", callSyms(on))
	}
}

// TestComptimeLeavesMutatingMethodAlone is the soundness guard that matters
// most for methods: a method that WRITES to its receiver must never be
// folded, because folding it would drop the write. The purity analysis sees
// the field assignment (a KAssign on a dot LHS) and refuses.
func TestComptimeLeavesMutatingMethodAlone(t *testing.T) {
	on, _ := comptimePair(t, `point {
    x i64
    y i64
}
point.bump = () (r i64) {
    .x = .x + 1
    r = .x
}
main = () () {
    p = point { x: 3, y: 4 }
    r = p.bump()
    print(r)
}
`)
	if !hasCallSym(on, "bump") {
		t.Errorf("method that mutates its receiver was folded away; syms=%v", callSyms(on))
	}
}

// TestComptimeLeavesGlobalWriteAlone is the soundness guard for a side effect
// the purity analysis has to see: a function that WRITES a module global.
// Folding it would take the store with it, and the write is observable —
// `counter = 0` + `bump = () { counter = 5 }` printed 5 with the evaluator
// off and 0 with it on.
//
// The write is a `let` on an identifier, so it looks exactly like a harmless
// local binding; only the GLOBAL table tells them apart.
func TestComptimeLeavesGlobalWriteAlone(t *testing.T) {
	on, _ := comptimePair(t, `counter = 0
bump = () (r i64) {
    counter = 5
    r = 7
}
main = () () {
    r = bump()
    print(r)
    print(counter)
}
`)
	if !hasCallSym(on, "bump") {
		t.Errorf("function that writes a global was folded away; syms=%v", callSyms(on))
	}
}

// TestComptimeFoldsConstExpr: an all-literal arithmetic expression folds even
// when it is not inside a function call.
func TestComptimeFoldsConstExpr(t *testing.T) {
	assertFoldedInt(t, `main = () () {
    r = (2 + 3) * 4 - 1
    print(r)
}
`, 19)
}

// TestComptimeFoldsTernary: a constant condition selects one arm at compile
// time. The arm is `3 + 4` rather than a bare literal so that 7 is not already
// present in the unoptimised module.
func TestComptimeFoldsTernary(t *testing.T) {
	assertFoldedInt(t, `main = () () {
    r = 1 < 2 ? 3 + 4 : 5
    print(r)
}
`, 7)
}

// TestComptimeFoldsStringConcat: string literals joined with `-` form one
// constant. NOTE: unlike the integer cases this fold is also performed by the
// pre-existing string-concat folder, so it is asserted in the ON direction
// only — the OFF arm of assertFoldedInt would not hold here.
func TestComptimeFoldsStringConcat(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "1")
	on := lowerSrc(t, `main = () () {
    s = 'a=' - 'b'
    print(s)
}
`)
	if strConsts(on)["a=b"] == 0 {
		t.Errorf("joined string \"a=b\" missing; consts=%v", strConsts(on))
	}
}

// TestComptimeFoldsBitwiseAndShift: bitwise operators and shifts are pure and
// fold like arithmetic. (3&5)|(1<<3) == 1|8 == 9.
func TestComptimeFoldsBitwiseAndShift(t *testing.T) {
	assertFoldedInt(t, `main = () () {
    r = (3 & 5) | (1 << 3)
    print(r)
}
`, 9)
}

// TestComptimeLeavesImpureCallAlone is the reverse guard for purity: a function
// that prints is NOT pure, so its call must survive even with constant
// arguments. Two things are checked — the call is still emitted, and the
// function's result (5 + 1 == 6) was not folded into a constant.
func TestComptimeLeavesImpureCallAlone(t *testing.T) {
	on, _ := comptimePair(t, `impure = (a i64) (r i64) {
    print(a)
    r = a + 1
}
main = () () {
    r = impure(5)
    print(r)
}
`)
	if !hasCallSym(on, "impure") {
		t.Errorf("impure call was folded away; syms=%v", callSyms(on))
	}
	if constInts(on)[6] {
		t.Errorf("impure call was folded to its result 6; consts=%v", constInts(on))
	}
}

// TestComptimeLeavesRuntimeCallAlone is the reverse guard for constant
// ARGUMENTS: a pure function called with a runtime value (a range-loop
// variable) must keep its call. If the loop variable were wrongly recorded as a
// compile-time constant, this is the test that would catch it.
func TestComptimeLeavesRuntimeCallAlone(t *testing.T) {
	on, _ := comptimePair(t, `add = (a i64, b i64) (r i64) {
    r = a + b
}
main = () () {
    i <- [0..3) {
        r = add(i, 3)
        print(r)
    }
}
`)
	if !hasCallSym(on, "add") {
		t.Errorf("call with a runtime argument was folded away; syms=%v", callSyms(on))
	}
}

// TestComptimeLeavesAsyncSpawnAlone: a spawn CREATES A TASK, so it must never
// be replaced by a constant — not even when the callee is pure and every
// argument is a literal. Folding `run work-async(21)` to 42 would delete the
// task and with it every spawn-graph / tier verdict derived from it.
func TestComptimeLeavesAsyncSpawnAlone(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "1")
	on := lowerSrc(t, `work-async = (n i64) (r i64) {
    #{overflow=wrap}
    r = n * 2
}
main = () {
    h = run work-async(21)
    v = awy h
    print(v)
}
`)
	if n := runInsts(on); n != 1 {
		t.Errorf("the spawn was folded away (or duplicated): %d OpRun instruction(s), want 1", n)
	}
	if constInts(on)[42] {
		t.Errorf("the spawn was folded to its result 42; consts=%v", constInts(on))
	}
}

// TestComptimeLeavesOptionResultAlone: an Option is a {tag, payload} pair, and
// comptimeVal models neither. Folding `mk-ok()` (result `?i64`) to 11 emitted a
// bare i64 where the caller expected an option, so `p: { ok(v) -> … }` fell into
// the nil arm (caught by test/std/option.no). Option results keep their call.
func TestComptimeLeavesOptionResultAlone(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "1")
	on := lowerSrc(t, `mk-ok = () (v ?i64) {
    v = 11
}
main = () () {
    p = mk-ok()
    p: {
        ok(v) -> print(v)
        nil -> print('nil')
        err -> print('err')
    }
}
`)
	if !hasCallSym(on, "mk-ok") {
		t.Errorf("a call returning an option was folded away; syms=%v", callSyms(on))
	}
}

// TestComptimeSwitchDefaultsToOn pins the default: comptime is an implicit
// feature, so an unset NOLANG_COMPTIME must behave like "on". (lowerWithOpt in
// opt_test.go is what switches it off, and only there.)
func TestComptimeSwitchDefaultsToOn(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "")
	if !comptimeEnabled() {
		t.Error("NOLANG_COMPTIME unset: comptime must be enabled by default")
	}
	t.Setenv("NOLANG_COMPTIME", "0")
	if comptimeEnabled() {
		t.Error("NOLANG_COMPTIME=0: comptime must be disabled")
	}
	t.Setenv("NOLANG_COMPTIME", "off")
	if comptimeEnabled() {
		t.Error("NOLANG_COMPTIME=off: comptime must be disabled")
	}
	t.Setenv("NOLANG_COMPTIME", "1")
	if !comptimeEnabled() {
		t.Error("NOLANG_COMPTIME=1: comptime must be enabled")
	}
}
