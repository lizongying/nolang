package mir

import (
	"testing"
)

// A variadic callee (`mx (a ..i64) (r i64)`) is monomorphized with the spread as
// ONE slice parameter, so the argument count can never distinguish the two
// legal call shapes:
//
//	v = mx(p, q)      // LHS form — BOTH arguments are spread elements
//	mx(1, 2, r)       // arg form — the trailing actual is the out-parameter
//
// Both lowerings therefore keyed on "the trailing kResult arguments are
// identifiers", which is true for `mx(p, q)` as well. The LHS form then lost its
// last spread element (the %vec was built with ONE element out of two, and the
// callee's `a[1]` read past the end) and the result was additionally written into
// the caller's `q` slot — a silent corruption that turned into SIGTRAP for
// `number.max(a, b)` with f64 arguments.
//
// The remaining discriminator is the position: only a call whose value is
// DISCARDED (a bare expression statement) can bind out-parameters by trailing
// argument. Anywhere else the call is used as a value, so every argument is a
// spread element.

const variadicSpreadPrelude = `mx = (a ..i64) (r i64) {
    #{index-out=zero}
    r = a[0]
    n = len(a)
    i <- [1..n): {
        a[i] > r -> {
            #{index-out=zero}
            r = a[i]
        }
    }
}
`

// callArgCounts reports, per OpCall to `sym`, how many arguments the lowered
// call carries.
func callArgCounts(t *testing.T, mod *Module, sym string) [][]ValueID {
	t.Helper()
	var got [][]ValueID
	for _, ins := range mod.Insts {
		if ins.Op == OpCall && ins.Sym == sym {
			got = append(got, ins.Args)
		}
	}
	if len(got) == 0 {
		t.Fatalf("no OpCall to %q was lowered", sym)
	}
	return got
}

// resultMoves reports the destinations of every `move dst=0 args=[src dst]`
// (the EmitMoveInto encoding, see analysis.go moveDst) whose source is a call to
// `sym`'s own result value — i.e. the out-parameter bindings the lowerer chose.
func resultMoves(mod *Module, sym string) []ValueID {
	var dsts []ValueID
	for _, ins := range mod.Insts {
		if ins.Op != OpCall || ins.Sym != sym || len(ins.Results) == 0 {
			continue
		}
		res := ins.Results[0]
		for _, m := range mod.Insts {
			if m.Op != OpMove || m.Dst != NoVal || len(m.Args) != 2 || m.Args[0] != res {
				continue
			}
			dsts = append(dsts, m.Args[1])
		}
	}
	return dsts
}

// TestVariadicLHSFormKeepsEverySpreadArgument: `v = mx(p, q)` must lower with
// BOTH identifiers as spread arguments — the trailing identifier is a value, not
// an out-parameter actual — and the result must not be written back over it.
func TestVariadicLHSFormKeepsEverySpreadArgument(t *testing.T) {
	src := variadicSpreadPrelude + `
main = () () {
    p i64 = 3
    q i64 = 7
    v = mx(p, q)
    print(v)
}
`
	mod := lowerSourceForTest(t, src)
	for _, args := range callArgCounts(t, mod, "mx") {
		if len(args) != 2 {
			t.Fatalf("LHS-form variadic call lowered %d argument(s) %v; want 2 (both spread elements)",
				len(args), args)
		}
	}
	// The other half of the same misjudgement: the result was ALSO moved into
	// the trailing argument's slot, silently overwriting the caller's `q`.
	if dsts := resultMoves(mod, "mx"); len(dsts) != 0 {
		t.Fatalf("LHS-form variadic call bound %d out-parameter move(s) %v; want none — the result must not be written over a spread argument",
			len(dsts), dsts)
	}
}

// TestVariadicNestedCallKeepsEverySpreadArgument: the same call nested inside
// another call's argument list is still a value position — the enclosing
// statement's call (`print`) owns the statement mark, not `mx`.
func TestVariadicNestedCallKeepsEverySpreadArgument(t *testing.T) {
	src := variadicSpreadPrelude + `
main = () () {
    p i64 = 3
    q i64 = 7
    print(mx(p, q))
}
`
	mod := lowerSourceForTest(t, src)
	for _, args := range callArgCounts(t, mod, "mx") {
		if len(args) != 2 {
			t.Fatalf("nested variadic call lowered %d argument(s) %v; want 2", len(args), args)
		}
	}
	if dsts := resultMoves(mod, "mx"); len(dsts) != 0 {
		t.Fatalf("nested variadic call bound %d out-parameter move(s) %v; want none", len(dsts), dsts)
	}
}

// TestVariadicStatementFormDropsOutParamActual: `mx(1, 2, r)` as a bare
// statement DOES bind `r` to the result parameter, so the spread the callee
// receives is [1, 2], `r` must not be packed into the %vec (it would corrupt
// the slice), and the result must land in `r`.
func TestVariadicStatementFormDropsOutParamActual(t *testing.T) {
	src := variadicSpreadPrelude + `
main = () () {
    r i64 = 0
    mx(1, 2, r)
    print(r)
}
`
	mod := lowerSourceForTest(t, src)
	for _, args := range callArgCounts(t, mod, "mx") {
		if len(args) != 2 {
			t.Fatalf("arg-form variadic call lowered %d argument(s) %v; want 2 (out-parameter stripped)",
				len(args), args)
		}
	}
	if dsts := resultMoves(mod, "mx"); len(dsts) == 0 {
		t.Fatal("statement-form variadic call bound no out-parameter; the result never reaches `r`")
	}
}
