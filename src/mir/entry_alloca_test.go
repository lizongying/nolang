package mir

import (
	"strings"
	"testing"
)

// entryAllocaLoopSrc calls a function with an OUT-PARAMETER from inside a loop
// body. Every out-param needs a result temporary (`%cresN = alloca i64`), and
// the emitter writes it at the call site — i.e. inside the loop.
//
// This is not a stylistic concern: in LLVM an `alloca` is an instruction that
// reserves stack space every time it EXECUTES, and the space is reclaimed only
// when the function returns. So one in a loop body grows the stack once per
// iteration without bound. Measured before hoistEntryAllocas existed
// (darwin/arm64, 8 MB stack):
//
//	m [str]i64 = {}
//	i <- [0..200000) { m.put('key', 1) }
//
// segfaulted (rc=139) after ~180k iterations with peak RSS still only 15.6 MB.
// The optimized IR still carried `%carg12 = alloca %str-long` and
// `%cres14 = alloca i1` in the loop body — LLVM's LICM does not hoist allocas,
// and mem2reg/SROA cannot promote one that is not in the entry block.
//
// The same shape is harmless when the callee is inlined (a 300k-iteration loop
// calling a tiny local function keeps every alloca out of the body after opt),
// which is why this only shows up on calls to non-inlined functions.
const entryAllocaLoopSrc = `
g = () (r i64) {
    r = 1
}

main = () {
    n = 0
    i <- [0..4) {
        #{overflow=wrap}
        n = n + g()
    }
    print(n)
}
`

// entryAllocaStraightSrc is the control: the same call, but not in a loop. Its
// temporaries were always in the entry block, so this half passes with or
// without the hoist — without it the test could not tell "hoisted" apart from
// "no temporaries emitted at all".
const entryAllocaStraightSrc = `
g = () (r i64) {
    r = 1
}

main = () {
    n = g()
    print(n)
}
`

// TestCallTemporariesAreAllocatedInTheEntryBlock pins the invariant that every
// static alloca in an emitted function lives in its entry block.
//
// The assertion is on the emitted IR rather than on a runtime counter because
// the failure mode is a stack overflow that needs ~180k iterations to reach —
// far too slow for a unit test, and it depends on the machine's stack limit.
func TestCallTemporariesAreAllocatedInTheEntryBlock(t *testing.T) {
	ir := mustEmit(t, entryAllocaLoopSrc)
	main := irFunc(ir, "_nolang_main")
	if main == "" {
		t.Fatalf("no @_nolang_main in the emitted module")
	}

	// Vacuity guard: the body must actually contain a call result temporary.
	// If codegen ever stopped emitting out-param temporaries altogether, the
	// assertion below would pass for the wrong reason.
	if !strings.Contains(main, "%cres") {
		t.Fatalf("the loop body emits no %%cres temporary at all; the test no longer exercises the call path:\n%s", main)
	}

	if got := allocasOutsideEntry(main); len(got) > 0 {
		t.Errorf("%d alloca(s) still sit outside the entry block of _nolang_main; each one grows the stack on every iteration:\n  %s\n---\n%s",
			len(got), strings.Join(got, "\n  "), main)
	}
}

// TestStraightLineCallStillEmitsItsTemporary is the other half of the vacuity
// guard: out-param temporaries must still be emitted at all. A rewrite that
// simply dropped them would make the test above pass while breaking every call
// in the language.
func TestStraightLineCallStillEmitsItsTemporary(t *testing.T) {
	main := irFunc(mustEmit(t, entryAllocaStraightSrc), "_nolang_main")
	if !strings.Contains(main, "%cres") {
		t.Fatalf("no %%cres temporary for a straight-line out-param call:\n%s", main)
	}
	if got := allocasOutsideEntry(main); len(got) > 0 {
		t.Errorf("straight-line call left %d alloca(s) outside the entry block:\n  %s", len(got), strings.Join(got, "\n  "))
	}
}

// TestHoistEntryAllocasLeavesDynamicAllocasInPlace is the negative control for
// the rewriter itself. A DYNAMIC alloca takes its element count from a register
// that a later block may define, and the entry block does not dominate that
// definition — hoisting it would emit invalid IR. So the pass must move the
// static alloca and leave the dynamic one exactly where it was.
func TestHoistEntryAllocasLeavesDynamicAllocasInPlace(t *testing.T) {
	const src = `define void @f(i64 %p0) {
f.bb0:
  %v0.s = alloca i64
  br label %f.bb1
f.bb1:
  %carg1 = alloca %str-long, align 16
  %dyn2 = alloca i8, i64 %p0, align 8
  %dyn3 = alloca [4 x i8], align 1
  ret void
}
`
	got := hoistEntryAllocas(src)

	for _, want := range []string{"%carg1", "%dyn3"} {
		if !strings.Contains(blockOf(got, want), "f.bb0") {
			t.Errorf("%s was not hoisted into the entry block:\n%s", want, got)
		}
	}
	if blk := blockOf(got, "%dyn2"); blk != "f.bb1" {
		t.Errorf("the dynamic alloca %%dyn2 must stay in f.bb1 (its size operand is a register), got %q:\n%s", blk, got)
	}
	// The return must still be there and the function must stay balanced.
	if !strings.Contains(got, "ret void") || strings.Count(got, "ret void") != 1 {
		t.Errorf("rewriting lost or duplicated the terminator:\n%s", got)
	}
}

// blockOf returns the label of the block that contains the first line defining
// reg (`%name = ...`), or "" if it is not found.
func blockOf(fnIR, reg string) string {
	cur := ""
	for _, ln := range strings.Split(fnIR, "\n") {
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") && strings.HasSuffix(ln, ":") {
			cur = strings.TrimSuffix(ln, ":")
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(ln), reg+" = ") {
			return cur
		}
	}
	return ""
}

// allocasOutsideEntry lists the hoistable allocas of fnIR that sit in a block
// other than the entry block (block 0). See hoistEntryAllocas for why that is
// an error and isHoistableAlloca for which forms qualify.
func allocasOutsideEntry(fnIR string) []string {
	var out []string
	block := -1
	for _, ln := range strings.Split(fnIR, "\n") {
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") && strings.HasSuffix(ln, ":") {
			block++
			continue
		}
		if block > 0 && isHoistableAlloca(ln) {
			out = append(out, strings.TrimSpace(ln))
		}
	}
	return out
}
