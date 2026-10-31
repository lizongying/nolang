package mir

import (
	"regexp"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Match arms on an option whose PAYLOAD IS A STRUCT.
//
// THE TRIGGER IS NARROWER THAN "A STRUCT PAYLOAD"
// -----------------------------------------------
// Measured with `no run`, four shapes, all identical except for how the `?struct`
// is produced:
//
//	mk  = () (r ?node)              m = mk()      -> ok
//	node.mk = (b node) (r ?node)    m = k.mk()    -> ok
//	node.mod-x = (b node) (r ?node) m = a.mod-x(k)-> ok
//	node._mod = (b node) (r ?node)  m = a._mod(k) -> icmp eq %node  (BREAKS)
//
// Only the last one fails, and the last one is the only one whose name is a
// reserved operator-overload name (`_add/_sub/_mul/_div/_mod`). parser.
// OperatorOverloadReturnType strips its `?`, so the CALLER binds `m` as a plain
// `node` while the CALLEE still returns an `%option`. hir2mir then peels the
// subject and lowers the bare `ok` keyword as a zero struct constant, because
// variantCtx only engages on an option typeHint.
//
// That is also why std cannot consume `%` on a bigint at all, and why
// `str.str-to-f64` reached around the public `_mod` and called bigint's private
// `_mod-i64` instead.
//
// THE BUG
// -------
// `o: { ok -> ... }` desugars to `o == ok`, and emitCmp's `aOpt && bOpt` branch
// turns that into a comparison of the two DISCRIMINANTS (Inst.TagOnly). For a
// scalar payload the arm works. For a struct payload (bigint, and every other
// std struct wrapped in `?T`) the operands that reach emitCmp are already the
// PAYLOADS — `%bigint_bigint` and a zero `%bigint_bigint` — so neither side is
// an `%option`, the discriminant branch never runs, and the emitter falls
// through to a whole-struct `icmp`:
//
//	%c22 = icmp eq %bigint_bigint %lv48, %lv49
//	error: icmp requires integer operands
//
// Measured on `no run` with std linked, for a `?bigint` from `n % k` matched
// with `ok -> it.to-str()`. The consequence for std is that no caller can
// consume a `?struct` through a match at all, which is what pushed
// `str.str-to-f64` to reach around the public `_mod` operator and call bigint's
// private `_mod-i64` fast path instead.
//
// WHY THE ASSERTION IS ON THE IR, NOT ON STDOUT
// ---------------------------------------------
// There is no executable to run here: the defect is an ill-typed instruction,
// and the only honest pin is "no comparison may take aggregate operands, and an
// `ok` arm must be decided by the discriminant".
// ─────────────────────────────────────────────────────────────────────────────

const structPayloadMatchSrc = `; node mirrors bigint's shape: 40 bytes, i.e. ABOVE the
; option inline threshold (24), so its payload is HEAP-BOXED. An inline struct
; payload lowers correctly; the boxed one is what breaks.
node {
    limbs []i64
    len i64
    sign i64
}

arm-ok = (o ?node) (out i64) {
    r = o: {
        ok -> 1
        err -> 2
        nil -> 3
    }
    out = r
}

arm-nil = (o ?node) (out i64) {
    r = o: {
        nil -> 7
        -> 9
    }
    out = r
}

arm-it = (o ?node) (out i64) {
    r = o: {
        ok -> it.len
        err -> 0
        nil -> 0
    }
    out = r
}

mk = () (r ?node) {
    n node = node { limbs: [1], len: 1, sign: 1 }
    r = ok(n)
}

a = arm-ok(mk())
b = arm-nil(mk())
c = arm-it(mk())
print(a)
print(b)
print(c)
`

// operatorOverloadOptionSrc is the RED case, kept out of the default gate (see
// TestOperatorOverloadOptionKeepsTag). It is the same shape as mk() above, only
// reached through a reserved operator-overload name — and that alone is enough
// to lose the option.
const operatorOverloadOptionSrc = `node {
    limbs []i64
    len i64
    sign i64
}

node._mod = (b node) (r ?node) {
    r = ok(b)
}

arm-ov = (a node, b node) (out i64) {
    m = a._mod(b)
    v = m: {
        ok -> 1
        err -> 2
        nil -> 3
    }
    out = v
}

d = arm-ov(node { limbs: [1], len: 1, sign: 1 }, node { limbs: [2], len: 1, sign: 1 })
print(d)
`

// optCmpIcmpRE captures the operand type of every `icmp` in a function body.
var optCmpIcmpRE = regexp.MustCompile(`icmp [a-z]+ (%[\w.]+) `)

// integerLLVMTypes are the only types `icmp` accepts.
var integerLLVMTypes = map[string]bool{
	"i1": true, "i8": true, "i16": true, "i32": true, "i64": true, "i128": true,
}

// assertOptionArmComparesTag pins the two halves of the contract on one
// function body: no `icmp` may take an aggregate operand, and the arm decision
// must actually read the option discriminant.
func assertOptionArmComparesTag(t *testing.T, ir, sym string) {
	fn := extractDefineBlock(t, ir, sym)
	for _, m := range optCmpIcmpRE.FindAllStringSubmatch(fn, -1) {
		if !integerLLVMTypes[m[1]] {
			t.Errorf("%s compares with icmp on a non-integer operand %s — an option "+
				"arm must be decided by its discriminant, not by the payload: %s", sym, m[1], m[0])
		}
	}
	// The arm decision must READ the discriminant. If the struct payload
	// is being compared instead, the tag never appears in the IR.
	if !strings.Contains(fn, "extractvalue %option") {
		t.Errorf("%s never reads the option discriminant:\n%s", sym, fn)
	}
}

func TestMatchOnStructPayloadOptionComparesTag(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, structPayloadMatchSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	for _, sym := range []string{"@arm_ok", "@arm_nil", "@arm_it"} {
		t.Run(sym, func(t *testing.T) {
			assertOptionArmComparesTag(t, ir, sym)
		})
	}
}

// TestOperatorOverloadOptionKeepsTag pins the operator-overload half: a
// `?node` reached through a reserved operator-overload name (`_mod`) must keep
// its option on the caller side, so the `ok` arm compares the tag instead of
// degenerating into a whole-struct `icmp` (which LLVM rejects, failing
// EmitLLVM outright).
//
// The regression this guards was the frontend trimming `?` off every
// `_add/_sub/_mul/_div/_mod` result before it reached the signature tables.
// The trim now happens only at consumption positions — see
// parser/operator_overload.go OperatorOverloadReturnType and
// parser/type.go resolveReceiverType — so the subject of a match keeps `?T`.
func TestOperatorOverloadOptionKeepsTag(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, operatorOverloadOptionSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("operator-overload `?struct` lost its option: %v", err)
	}
	assertOptionArmComparesTag(t, ir, "@arm_ov")
}
