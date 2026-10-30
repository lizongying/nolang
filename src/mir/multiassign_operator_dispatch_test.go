package mir

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Operator-overload dispatch for a variable introduced by MULTI-RETURN
// DESTRUCTURING (`q, r = a.div-mod(b)`).
// ─────────────────────────────────────────────────────────────────────────────
// Measured with `no run` on std (tests tmp/bx/d1.no):
//
//	f = (a bigint, b bigint) (out str) {
//	    q, r = a.div-mod(b)
//	    m = q % 2          -> LLVM: '%lv50' defined with type 'i64'
//	    out = m.to-str()          but expected '%bigint_bigint'
//	}
//
// The same `q` is NOT mistyped for the checker — `q.abs-cmp(r)` vets clean — so
// this is not a type-inference bug. It is a REGISTRATION gap: the parser's
// operator dispatch resolves a receiver through its own function-scope table
// (`sem.FuncVarType` / VarTypes), and a `MultiAssignStatement` target never gets
// written into that table. Every other binding form does: LetStatement (its own
// type or inference), match arm bindings, `#{index-out}` temporaries, for-range
// variables. So `resolveReceiverType(q)` answers "", `OperatorOverloadReturnType`
// answers "", lowering declines the `q % 2` → `q._mod(2)` rewrite, and the
// InfixExpression survives to MIR, which emits NATIVE arithmetic on a struct —
// hence an operand-type error from LLVM rather than a diagnostic.
//
// The same silent fallback is what makes `bigint + (c - 1)` abort the toolchain
// (tmp/bx/d2.no): the receiver is unresolvable, so no rewrite happens, and
// native `add` on an aggregate is a hard crash instead of an error.
//
// WHY THE ASSERTION IS A CALL, NOT A TYPE STRING
// ----------------------------------------------
// The contract at MIR level is "the infix became a method call". Asserting that
// `@…_mod` appears in the body, and that no native remainder was emitted for a
// struct operand, pins both halves: dispatch happened, and the aggregate never
// reached machine arithmetic.
// ─────────────────────────────────────────────────────────────────────────────

const multiAssignOperandSrc = `node {
    limbs []i64
    len i64
    sign i64
}

; 多返回值方法：镜像 bigint.div-mod = (b bigint) (q bigint, r bigint)
node.div-mod = (b node) (q node, r node) {
    q = .
    q.len = b.len
    r = .
    r.sign = b.sign
}

node._mod = (b node) (c node) {
    c = .
    c.sign = b.sign
}

node.is-zero = () (z bool) {
    z = .len == 0
}

; 接收者来自解构绑定 —— 这是派发的盲点
from-destructure = (a node, b node) (out node) {
    q, r = a.div-mod(b)
    out = q % b
}

; 对照组：普通参数接收者本来就正常
from-param = (a node, b node) (out node) {
    out = a % b
}

z = from-destructure(node { limbs: [1], len: 1, sign: 1 }, node { limbs: [2], len: 2, sign: 1 })
print(z.len)
p = from-param(node { limbs: [1], len: 1, sign: 1 }, node { limbs: [2], len: 2, sign: 1 })
print(p.len)
`

// TestMultiAssignTargetDispatchesOperatorOverload pins the rewrite: a variable
// bound by `q, r = call()` must be visible to operator-overload dispatch, so
// `q % b` becomes a call to the `_mod` method instead of native srem/add on a
// struct value.
func TestMultiAssignTargetDispatchesOperatorOverload(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, multiAssignOperandSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed — the infix on a destructured binding fell back to "+
			"native arithmetic on a struct (dispatch never saw its type): %v", err)
	}

	fn := extractDefineBlock(t, ir, "@from_destructure")
	if !strings.Contains(fn, "_mod") {
		t.Errorf("@from_destructure never calls the `_mod` overload — the infix on the "+
			"destructured binding was not rewritten, so it went to machine arithmetic on "+
			"an aggregate:\n%s", fn)
	}
	// Native remainder on a struct operand is the signature of the fallback.
	for _, bad := range []string{"srem", "urem"} {
		for _, line := range strings.Split(fn, "\n") {
			if strings.Contains(line, bad) && strings.Contains(line, "%node") {
				t.Errorf("@from_destructure emitted native %s on a struct operand: %s", bad, strings.TrimSpace(line))
			}
		}
	}

	// The control group proves the assertion has teeth: the identical infix on a
	// plain parameter dispatches today, so a broken table would show up as BOTH
	// functions failing rather than as a real destructuring-specific gap.
	paramFn := extractDefineBlock(t, ir, "@from_param")
	if !strings.Contains(paramFn, "_mod") {
		t.Errorf("control @from_param also lost dispatch — the gate is reporting an "+
			"unrelated breakage, not the destructuring gap:\n%s", paramFn)
	}
}
