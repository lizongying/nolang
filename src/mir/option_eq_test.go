package mir

import (
	"regexp"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// `==` / `!=` on two OPTION values: tag AND payload.
//
// The defect this pins: emitCmp's `aOpt && bOpt` branch extracted only the
// discriminant from both sides, so `==` on two options degenerated into "do both
// hold the same tag". Measured on the compiler before the fix:
//
//	ok(1) == ok(2)       -> true     (WRONG)
//	ok(1) != ok(2)       -> false    (WRONG)
//	err('a') == err('b') -> true     (WRONG)
//
// The fix has to walk a line that is easy to get wrong in the OTHER direction:
// the SAME branch also carries every match arm, because the parser desugars
// `o: { ok -> ... }` into `o == ok`. The keyword side of that comparison carries
// no payload (a zero constant, or none at all for `nil`), so a structural
// comparison would answer `false` for every arm and silently break every match
// on a ?T in std. Inst.TagOnly is what separates the two meanings, and both
// directions are asserted here:
//
//  1. two option VALUES compare structurally (tag icmp AND payload equality,
//     combined with `and`);
//  2. a bare variant KEYWORD compares the discriminant only — no payload read,
//     no `and`.
//
// The assertions are structural rather than numeric, because a numeric test
// would pass again the moment some later layer papered over the wrong answer.
//
// Distinguishing the two comparisons in the IR is the subtle part: for a scalar
// payload BOTH are `icmp eq i64`, so the test cannot key on the instruction. It
// keys on the OPERANDS instead — a tag comparison compares two `extractvalue
// %option <v>, 0` results, a payload comparison compares two values loaded from
// the payload slot. That difference is the semantics, not a naming convention.
// ─────────────────────────────────────────────────────────────────────────────

const optionEqSrc = `val-eq = (a ?i64, b ?i64) (out bool) {
    out = a == b
}

val-ne = (a ?i64, b ?i64) (out bool) {
    out = a != b
}

spair-eq = (a ?str, b ?str) (out bool) {
    out = a == b
}

arm = (o ?i64) (out i64) {
    r = o: {
        ok -> 1
        err -> 2
        nil -> 3
    }
    out = r
}

x ?i64 = ok(1)
y ?i64 = ok(2)
p = val-eq(x, y)
q = val-ne(x, y)
s1 ?str = ok('a')
s2 ?str = ok('b')
t = spair-eq(s1, s2)
u = arm(x)
`

var (
	andRE    = regexp.MustCompile(`(%[\w.]+) = and i1 (%[\w.]+), (%[\w.]+)`)
	tagSrcRE = regexp.MustCompile(`(%[\w.]+) = extractvalue %option %[\w.]+, 0`)
	icmpRE   = regexp.MustCompile(`(%[\w.]+) = icmp eq (?:i64|i32|i16|i8|i1) (%[\w.]+), (%[\w.]+)`)
	fcmpRE   = regexp.MustCompile(`(%[\w.]+) = fcmp oeq`)
	strEqRE  = regexp.MustCompile(`(%[\w.]+) = call i1 @str_eq`)
)

// cmpShape classifies every comparison in one emitted function body.
type cmpShape struct {
	// tagCmp holds the comparisons of two discriminants (both operands come
	// from `extractvalue %option <v>, 0`).
	tagCmp map[string]bool
	// payloadCmp holds the comparisons that are NOT discriminant tests.
	payloadCmp map[string]bool
}

func classifyComparisons(fn string) cmpShape {
	tagSrcs := map[string]bool{}
	for _, m := range tagSrcRE.FindAllStringSubmatch(fn, -1) {
		tagSrcs[m[1]] = true
	}
	s := cmpShape{tagCmp: map[string]bool{}, payloadCmp: map[string]bool{}}
	for _, m := range icmpRE.FindAllStringSubmatch(fn, -1) {
		if tagSrcs[m[2]] && tagSrcs[m[3]] {
			s.tagCmp[m[1]] = true
		} else {
			s.payloadCmp[m[1]] = true
		}
	}
	for _, m := range fcmpRE.FindAllStringSubmatch(fn, -1) {
		s.payloadCmp[m[1]] = true
	}
	for _, m := range strEqRE.FindAllStringSubmatch(fn, -1) {
		s.payloadCmp[m[1]] = true
	}
	return s
}

// TestOptionValueComparisonComparesPayloads proves the VALUE reading: a tag
// comparison AND a payload comparison, combined. A tag-only comparison has no
// `and` at all, so its absence is what the assertion catches.
func TestOptionValueComparisonComparesPayloads(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, optionEqSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	for _, sym := range []string{"@val_eq", "@val_ne", "@spair_eq"} {
		t.Run(sym, func(t *testing.T) {
			fn := extractDefineBlock(t, ll, sym)
			and := andRE.FindStringSubmatch(fn)
			if and == nil {
				t.Fatalf("%s: the comparison was not combined from a tag and a payload "+
					"test (no `and i1`): the payload is being ignored\n---\n%s", sym, fn)
			}
			lhs, rhs := and[2], and[3]
			s := classifyComparisons(fn)

			switch {
			case s.tagCmp[lhs] && s.payloadCmp[rhs], s.tagCmp[rhs] && s.payloadCmp[lhs]:
				// exactly what we want
			case s.tagCmp[lhs] && s.tagCmp[rhs]:
				t.Errorf("%s: `and i1 %s, %s` combines TWO discriminant tests — the payload "+
					"is never compared, so ok(1) == ok(2) is still true\n---\n%s", sym, lhs, rhs, fn)
			default:
				t.Errorf("%s: `and i1 %s, %s` does not combine a discriminant test with a "+
					"payload test\n---\n%s", sym, lhs, rhs, fn)
			}

			if sym == "@spair_eq" {
				if len(s.payloadCmp) == 0 || !strings.Contains(fn, "call i1 @str_eq") {
					t.Errorf("@spair_eq: a ?str comparison must compare the payload through "+
						"@str_eq, not an icmp on the %%str-long struct\n---\n%s", fn)
				}
			}
			if sym == "@val_ne" {
				if !regexp.MustCompile(`xor i1 %[\w.]+, true`).MatchString(fn) {
					t.Errorf("@val_ne: `!=` must NEGATE the combined equality, not just invert "+
						"the tag test\n---\n%s", fn)
				}
			}
		})
	}
}

// TestOptionVariantTestComparesTagsOnly is the other half, and the one that
// would take down every match in std if it regressed: a comparison against a
// BARE variant keyword (`o == nil`, `o == ok`, `o == err`, and every match arm)
// must stay a discriminant test. The keyword carries a zero/absent payload, so
// a structural comparison would compare the subject's real payload against zero
// and the arm would never fire.
func TestOptionVariantTestComparesTagsOnly(t *testing.T) {
	mod, diags := lowerSrcWithDiags(t, optionEqSrc)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	fn := extractDefineBlock(t, ll, "@arm")
	if m := andRE.FindStringSubmatch(fn); m != nil {
		t.Errorf("@arm: an arm test was combined from a tag and a payload test "+
			"(`%s = and i1 %s, %s`) — a bare variant keyword carries no payload, so "+
			"every arm of every match on a ?T would stop firing\n---\n%s",
			m[1], m[2], m[3], fn)
	}
	s := classifyComparisons(fn)
	if len(s.tagCmp) != 3 {
		t.Errorf("@arm: expected 3 discriminant tests (ok / err / nil), got %d\n---\n%s",
			len(s.tagCmp), fn)
	}
	if len(s.payloadCmp) != 0 {
		t.Errorf("@arm: an arm test read the payload (%d payload comparisons)\n---\n%s",
			len(s.payloadCmp), fn)
	}

	// MIR level: the arm comparisons are the ONLY TagOnly instructions, and the
	// three value comparisons must NOT be marked. Counting both directions is
	// what makes this non-vacuous — a flag that is never set and a flag that is
	// always set are equally useless.
	tagOnly, notTagOnly := 0, 0
	for _, inst := range mod.Insts {
		if inst.Op != OpEq && inst.Op != OpNe {
			continue
		}
		if inst.TagOnly {
			tagOnly++
		} else {
			notTagOnly++
		}
	}
	if tagOnly != 3 {
		t.Errorf("expected exactly 3 tag-only comparisons (the ok/err/nil arm tests), got %d", tagOnly)
	}
	if notTagOnly != 3 {
		t.Errorf("expected exactly 3 structural comparisons (val-eq / val-ne / str-eq), got %d", notTagOnly)
	}
}

// TestOptionEqualityPayloadDispatchIsTyped guards the fallback contract: only
// payload types with a real equality are compared structurally. A slice payload
// has no element-wise equality in the compiler, and comparing its {ptr,len,cap}
// BYTES would compare heap pointers — answering on the discriminant is
// well-defined, comparing pointer identity is not.
func TestOptionEqualityPayloadDispatchIsTyped(t *testing.T) {
	src := `vec-eq = (a ?[]i64, b ?[]i64) (out bool) {
    out = a == b
}

x ?[]i64 = [1, 2]
y ?[]i64 = [3, 4]
r = vec-eq(x, y)
`
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	fn := extractDefineBlock(t, ll, "@vec_eq")
	if m := andRE.FindStringSubmatch(fn); m != nil {
		t.Errorf("@vec_eq: a slice payload has no structural equality, so the comparison "+
			"must stay on the discriminant — it was combined with a payload test "+
			"(`%s = and i1 %s, %s`), which compares heap POINTERS\n---\n%s",
			m[1], m[2], m[3], fn)
	}
	s := classifyComparisons(fn)
	if len(s.tagCmp) != 1 || len(s.payloadCmp) != 0 {
		t.Errorf("@vec_eq: expected a single discriminant comparison, got %d tag / %d payload\n---\n%s",
			len(s.tagCmp), len(s.payloadCmp), fn)
	}
}
