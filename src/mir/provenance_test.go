package mir

import (
	"strings"
	"testing"
)

// These tests pin the PROVENANCE contract that replaced the "MIR flattens
// everything to i64, so guess from the spelling" model:
//
//	Value.Type    the type the backend materialises (i64 for every integer)
//	Value.Origin  the type the SOURCE declared, when a promotion erased it
//
// Each case below used to be answerable only by string-prefix guessing at the
// use site, and each was wrong somewhere: `[]i8` elements zero-extended, a u64
// parameter divided signed, char printing as a decimal number.

// findIndexOrigins returns the pre-promotion spelling of every OpIndex result.
func findIndexOrigins(mod *Module) []string {
	var out []string
	for i := 1; i < len(mod.Insts); i++ {
		inst := &mod.Insts[i]
		if inst.Op != OpIndex || inst.Dst == NoVal {
			continue
		}
		raw := ""
		if o := mod.ValueOrigin(inst.Dst); o != nil {
			raw = o.Raw
		}
		out = append(out, raw)
	}
	return out
}

func TestNarrowIndexReadKeepsElementProvenance(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	const src = `signed-read = (a []i8, i i64) (r i64) {
    r = a[i]
}
byte-read = (a []byte, i i64) (r i64) {
    r = a[i]
}
u8-read = (a []u8, i i64) (r i64) {
    r = a[i]
}
main = () () {
    print(signed-read([], 0))
    print(byte-read([], 0))
    print(u8-read([], 0))
}
`
	mod, _ := lowerSrcWithDiags(t, src)
	origins := findIndexOrigins(mod)
	seen := map[string]bool{}
	for _, o := range origins {
		seen[o] = true
	}
	for _, want := range []string{"i8", "byte", "u8"} {
		if !seen[want] {
			t.Fatalf("no OpIndex carries origin %q (got %v)", want, origins)
		}
	}
	// The MIR type of the read is still the flat i64 the backend computes in.
	for i := 1; i < len(mod.Insts); i++ {
		inst := &mod.Insts[i]
		if inst.Op != OpIndex || inst.Dst == NoVal {
			continue
		}
		if ty := mod.Type(mod.Values[inst.Dst].Type); ty != nil && ty.Raw != "i64" {
			t.Fatalf("promoted read kept MIR type %q, want i64", ty.Raw)
		}
	}

	// The IR must reflect the difference the provenance exists for: signed
	// narrow reads sign-extend, byte/u8 reads zero-extend.
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	if !strings.Contains(ll, "sext i8") {
		t.Error("`[]i8` read did not sign-extend (fix #1)")
	}
	if !strings.Contains(ll, "zext i8") {
		t.Error("`[]byte`/`[]u8` read did not zero-extend")
	}
}

// unsignedOps returns the set of arithmetic/comparison opcodes used by mod.
func unsignedOps(mod *Module) map[Op]bool {
	out := map[Op]bool{}
	for i := 1; i < len(mod.Insts); i++ {
		out[mod.Insts[i].Op] = true
	}
	return out
}

func TestUnsignedDivisionUsesProvenanceNotNames(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	// Provenance, not the name table, decides (fix #2): the declared type of `d`
	// reaches the division through the lowered value, so the answer does not
	// depend on the spelling being remembered under a local's name.
	const src = `unsigned-half = (d u64) (r u64) {
    r = d / 2
}
signed-half = (d i64) (r i64) {
    r = d / 2
}
main = () () {
    print(unsigned-half(10))
    print(signed-half(10))
}
`
	mod, _ := lowerSrcWithDiags(t, src)
	ops := unsignedOps(mod)
	if !ops[OpUDiv] {
		t.Error("u64 parameter division did not route to udiv (fix #2)")
	}
	if !ops[OpDiv] {
		t.Error("i64 division must stay signed")
	}

	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	if !strings.Contains(ll, "udiv i64") {
		t.Error("IR has no udiv for the u64 division")
	}
}

// A divided operand that is NOT a bare name — an index read or a struct field —
// has no entry in the declared-type table at all, so the only thing that can
// answer "was this unsigned?" is the value's own provenance. This is the half of
// fix #2 the name-table heuristic could never reach (it also covers map values
// and cross-module bindings, which lower to the same shapes).
func TestUnsignedDivisionOfNonNameOperandUsesProvenance(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	const src = `pair {
    hi u64
    lo i64
}
signed-read = (a []i8, i i64) (r i64) {
    r = a[i] / 2
}
field-hi = (p pair) (r u64) {
    r = p.hi / 2
}
field-lo = (p pair) (r i64) {
    r = p.lo / 2
}
main = () () {
    print(signed-read([], 0))
    print(field-hi(pair { hi: 8, lo: 8 }))
    print(field-lo(pair { hi: 8, lo: 8 }))
}
`
	mod, _ := lowerSrcWithDiags(t, src)
	ops := unsignedOps(mod)
	if !ops[OpUDiv] {
		t.Error("u64 struct field division did not route to udiv (fix #2)")
	}
	if !ops[OpDiv] {
		t.Error("i64 struct field / `[]i8` element division must stay signed")
	}

	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	if strings.Count(ll, "udiv i64") < 1 {
		t.Error("IR has no udiv for the u64 field division")
	}
	if strings.Count(ll, "sdiv i64") < 1 {
		t.Error("IR has no sdiv: every i64 division became unsigned")
	}
}

func TestUnsignedCompareUsesProvenance(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	const src = `unsigned-below = (d u64, lim u64) (r bool) {
    r = d < lim
}
signed-below = (d i64, lim i64) (r bool) {
    r = d < lim
}
main = () () {
    print(unsigned-below(1, 2))
    print(signed-below(1, 2))
}
`
	mod, _ := lowerSrcWithDiags(t, src)
	ops := unsignedOps(mod)
	if !ops[OpULt] {
		t.Error("u64 comparison did not route to ult")
	}
	if !ops[OpLt] {
		t.Error("i64 comparison must stay signed")
	}
}

func TestCharKeepsCodePointIdentity(t *testing.T) {
	t.Setenv("NOLANG_COMPTIME", "0")
	// A char travels as i32; only its SOURCE spelling says it is a code point,
	// so printing it must go through @str_from_cp (UTF-8) rather than the
	// decimal-text path @str_from_i64 (which rendered U+4E2D as "20013").
	const src = `as-str = (c char) (r str) {
    r = c
}
main = () () {
    print(as-str("A"))
}
`
	mod, _ := lowerSrcWithDiags(t, src)
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	if !strings.Contains(ll, "@str_from_cp") {
		t.Error("char did not keep its code-point identity (fix #3)")
	}
}

func TestUnsignedPrintingUsesSignednessNotSpellingPrefix(t *testing.T) {
	// isUnsignedRaw is the single place signedness is decided now; it must
	// answer from the parsed type, not from a hand-maintained name list.
	cases := map[string]bool{
		"u8": true, "u16": true, "u32": true, "u64": true, "u128": true,
		"byte": true, "char": true, "bool": true,
		"i8": false, "i16": false, "i32": false, "i64": false, "i128": false,
		"int": false, "f64": false, "str": false, "[]u8": false,
	}
	for raw, want := range cases {
		if got := isUnsignedRaw(raw); got != want {
			t.Errorf("isUnsignedRaw(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestTypeExplicitReportsKindConflict(t *testing.T) {
	// The strict mode asserts the OPPOSITE of this test, so it must be off here
	// even when the suite itself is run under `NOLANG_TY_STRICT=1`.
	t.Setenv("NOLANG_TY_STRICT", "0")
	b := NewBuilder("conflict")
	// Intern the spelling first through the normal classifier: `i64` is an int.
	b.Type("i64")
	id := b.TypeExplicit("i64", KindStruct, false, NoType)
	if ex := b.Mod.Type(id); ex == nil || ex.Kind != KindInt {
		t.Fatalf("TypeExplicit overrode the interned kind: %#v", ex)
	}
	if !b.Mod.tyWarned["i64\x00int\x00struct"] {
		t.Error("kind conflict was not reported (fix #6)")
	}
	// A matching claim stays quiet.
	b2 := NewBuilder("agree")
	b2.TypeExplicit("Point", KindStruct, false, NoType)
	b2.TypeExplicit("Point", KindStruct, false, NoType)
	if len(b2.Mod.tyWarned) != 0 {
		t.Errorf("agreeing TypeExplicit warned: %v", b2.Mod.tyWarned)
	}
}

// The conflict must be UNIGNORABLE when the caller asks for it: a silent
// disagreement between "what a stage claims" and "what is interned" is the exact
// failure class this assertion exists for, and a warning in a passing build is
// how it stayed hidden before.
func TestTypeExplicitPanicsInStrictMode(t *testing.T) {
	t.Setenv("NOLANG_TY_STRICT", "1")
	b := NewBuilder("strict")
	b.Type("i64")
	defer func() {
		if recover() == nil {
			t.Error("NOLANG_TY_STRICT=1 did not panic on a kind conflict")
		}
	}()
	b.TypeExplicit("i64", KindStruct, false, NoType)
}
