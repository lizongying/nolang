package mir

import (
	"regexp"
	"strings"
	"testing"
)

// TestI128MoveWidensInsteadOfTypePunning pins the two 128-bit lane rules that
// together made i128.MIN.sqrt return garbage at -O0 (test/std/number.no):
//
//	(a) emitMove must WIDEN a narrow source into a 128-bit slot, never type-pun
//	    the slot. MIR flattens every integer narrower than 128 bits to i64, so
//	    `r = 0` with an i128 `r` used to emit
//	        alloca i64 %vN.s ; store i64 0, i64* %vN.s ; load i128, i128* %vN.s
//	    — a 16-byte read of an 8-byte slot, i.e. whatever the neighbouring stack
//	    bytes held. -O2 promoted the allocas and hid it; -O0 did not.
//	(b) a 128-bit OPERAND must make the arithmetic 128-bit. typeOfNode takes an
//	    infix's type from its LEFT operand (see its KInfix case), so `0 - d` with
//	    an i128 `d` inferred i64 and BOTH operands were `trunc`-ed to 64 bits:
//	    the result was silently wrong for any value that does not fit
//	    (18446744073709551609 instead of -7). std's `int.to-str__i128` has the
//	    same shape in `d = 0 - d`.
//
// (c)/(d)/(e) are the supporting and negative-control cases: a purely 64-bit
// subtraction must stay 64-bit (the width rule cannot be a blanket widening),
// and a narrow source must reach a 128-bit slot through an explicit sext.
func TestI128MoveWidensInsteadOfTypePunning(t *testing.T) {
	const src = `sub-zero = () (r i128) {
    d i128 = 7
    r = 0 - d
}
narrow-sub = () (r i64) {
    a i64 = 9
    b i64 = 4
    r = a - b
}
assign-narrow = () (r i128) {
    a i64 = 3
    r = a
}
lit-into-128 = () (r i128) {
    r = 5
}
main = () () {
    print(sub-zero())
    print(narrow-sub())
    print(assign-narrow())
    print(lit-into-128())
}
`
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	// (a) A 128-bit load must read a 128-bit slot. Slot names are FUNCTION-LOCAL
	// (%v0.s is re-used by every function), so each define block must be scanned
	// on its own — a module-wide map silently lets one function's `alloca i64`
	// shadow another's `alloca i128`.
	allocaRe := regexp.MustCompile(`(?m)^\s*(%[A-Za-z0-9_.]+)\s*=\s*alloca\s+(.+?)\s*$`)
	loadRe := regexp.MustCompile(`(?m)load\s+i128,\s*i128\*\s*(%[A-Za-z0-9_.]+)`)

	blocks := eachDefineBlock(ll)
	if len(blocks) == 0 {
		t.Fatal("no define block found in the emitted IR — the overread check would pass vacuously")
	}
	checked := 0
	for _, blk := range blocks {
		slotType := map[string]string{}
		for _, m := range allocaRe.FindAllStringSubmatch(blk, -1) {
			slotType[m[1]] = m[2]
		}
		for _, m := range loadRe.FindAllStringSubmatch(blk, -1) {
			checked++
			slot := m[1]
			if ty, ok := slotType[slot]; ok && ty != "i128" {
				t.Errorf("%s: load i128 reads 16 bytes from %s, declared `alloca %s` — the move must WIDEN the value (sext/zext), not type-pun the slot",
					defineName(blk), slot, ty)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no `load i128` found in any define block — the overread check would pass vacuously")
	}

	// (b) `0 - d` with an i128 `d` must be computed in 128 bits.
	subZero := extractDefineBlock(t, ll, "@sub_zero")
	if !strings.Contains(subZero, "sub i128") {
		t.Errorf("@sub_zero must compute `0 - d` (d i128) in 128 bits, but the emitted body has no `sub i128`:\n%s", subZero)
	}

	// (c) ... while a purely 64-bit subtraction must NOT be widened.
	narrowSub := extractDefineBlock(t, ll, "@narrow_sub")
	if !strings.Contains(narrowSub, "sub i64") {
		t.Errorf("@narrow_sub must keep computing `a - b` (i64 operands) in 64 bits:\n%s", narrowSub)
	}
	if strings.Contains(narrowSub, "sub i128") {
		t.Errorf("@narrow_sub widened a 64-bit subtraction to 128 bits — the width rule must only widen when an operand really is 128-bit:\n%s", narrowSub)
	}

	// (d) A narrowing source -> 128-bit destination must sign-extend explicitly
	// rather than type-pun the slot (the i64 -> i128 case).
	assignNarrow := extractDefineBlock(t, ll, "@assign_narrow")
	if !strings.Contains(assignNarrow, "sext i64") || !strings.Contains(assignNarrow, "to i128") {
		t.Errorf("@assign_narrow must sext an i64 into the i128 slot:\n%s", assignNarrow)
	}

	// (e) The same for a LITERAL assigned to a 128-bit result — this is exactly
	// the `r = 0` arm of i128.sqrt's negative case. The literal is allowed to
	// stay i64 as long as the move into the i128 slot widens it; what must never
	// happen is a 128-bit read of the i64 slot (already asserted in (a)).
	litInto128 := extractDefineBlock(t, ll, "@lit_into_128")
	if !strings.Contains(litInto128, "sext i64") || !strings.Contains(litInto128, "to i128") {
		t.Errorf("@lit_into_128 must widen the literal into the i128 slot via sext:\n%s", litInto128)
	}
}

// eachDefineBlock returns every top-level `define ... { ... }` block, closing
// brace included. Top-level braces are the only ones at column 0, so a `^\}`
// search terminates each block correctly.
func eachDefineBlock(ir string) []string {
	header := regexp.MustCompile(`(?m)^define[^\n]*\{$`)
	close := regexp.MustCompile(`(?m)^\}`)
	var out []string
	for _, loc := range header.FindAllStringIndex(ir, -1) {
		rest := ir[loc[0]:]
		end := close.FindStringIndex(rest)
		if end == nil {
			continue
		}
		out = append(out, rest[:end[1]])
	}
	return out
}

// defineName pulls the symbol out of a define block, for error messages.
func defineName(block string) string {
	if m := regexp.MustCompile(`@([A-Za-z0-9_.]+)\s*\(`).FindStringSubmatch(block); m != nil {
		return "@" + m[1]
	}
	return "<unknown define>"
}
