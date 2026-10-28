package mir

import (
	"regexp"
	"strings"
	"testing"
)

// TestByteAccessorReturnsBoundsCheckedOption pins the 2026-10-21 semantics of
// `str.byte(i)` / `txt.byte(i)`: the accessor returns `?byte`, NOT a bare i64:
//
//	tag = ok  (0), payload = zext(byte)   when 0 <= i < len
//	tag = nil (1), payload = 0            otherwise
//
// The old reader had NO bounds check: an out-of-range index was a GEP+load on
// arbitrary memory. `byte` is a PUBLIC method, so it now fails like `arr[i]`.
// The load itself stays memory-safe on the nil path via a `select` on the data
// pointer (an empty str has data == NULL, so clamping the index alone would
// still dereference NULL).
//
// Every assertion below discriminates against the old emitter: the old IR had
// an unconditional `getelementptr i8` + `load i8` + `store i64`, with no
// icmp, no select on a pointer, and no `%option` store. The negative control
// proves the pattern does not appear in a function that never calls `.byte`.
func TestByteAccessorReturnsBoundsCheckedOption(t *testing.T) {
	// Comptime OFF: `plain` is a pure zero-argument constant function, so
	// folding would replace its only call with 42 and the function would never
	// be emitted — the negative control in (c) needs the body.
	t.Setenv("NOLANG_COMPTIME", "0")
	const src = `sprobe = (s str, i i64) (b ?byte) {
    b = s.byte(i)
}

tprobe = (t txt, i i64) (b ?byte) {
    b = t.byte(i)
}

plain = () (r i64) {
    r = 42
}

s str = 'abc'
u txt = 'hi'
y = sprobe(s, 1)
y: {
    ok -> print(it)
    -> print('nil')
}
z = tprobe(u, 1)
z: {
    ok -> print(it)
    -> print('nil')
}
print(plain())
`
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}

	// --- str receiver: bounds check + safe load + option result ---
	sp := extractDefineBlock(t, ll, "@sprobe")
	if sp == "" {
		t.Fatal("@sprobe not found")
	}
	checks := []struct {
		name string
		re   *regexp.Regexp
	}{
		// unsigned compare: a negative i64 is a huge unsigned value, so
		// `ult` rejects it without a separate `i >= 0` check.
		{"bounds check (icmp ult)", regexp.MustCompile(`icmp ult i64 %\w+, %\w+`)},
		// the data pointer is selected between the real buffer and a
		// fallback, so the nil path never dereferences.
		{"guarded pointer (select i8*)", regexp.MustCompile(`select i1 %\w+, i8\* %\w+, i8\* %\w+`)},
		{"fallback slot bitcast", regexp.MustCompile(`bitcast %option\* %[\w.]+ to i8\*`)},
		{"byte payload widened", regexp.MustCompile(`zext i8 %\w+ to i64`)},
		{"tag select (ok/nil)", regexp.MustCompile(`select i1 %\w+, i64 0, i64 1`)},
		{"payload select", regexp.MustCompile(`select i1 %\w+, i64 %\w+, i64 0`)},
		{"option result store", regexp.MustCompile(`store %option`)},
	}
	for _, c := range checks {
		if !c.re.MatchString(sp) {
			t.Errorf("@sprobe: missing %s\n%s", c.name, indented(sp))
		}
	}
	// The option store must write the tag AND the payload: the tag field
	// gep (i32 0, i32 0) and the payload slot gep (i32 0, i32 1) are what
	// distinguish a real option construction from a bare scalar store.
	if !strings.Contains(sp, "getelementptr inbounds %option, %option* %") {
		t.Errorf("@sprobe: no option field gep — the result is not an option\n%s", indented(sp))
	}

	// --- txt receiver: len is the i8 field 1, so it must be zext'd ---
	tp := extractDefineBlock(t, ll, "@tprobe")
	if tp == "" {
		t.Fatal("@tprobe not found")
	}
	if !regexp.MustCompile(`getelementptr inbounds %txt, %txt\* %[\w.]+, i32 0, i32 1`).MatchString(tp) {
		t.Errorf("@tprobe: len not loaded from the txt i8 field (i32 0, i32 1)\n%s", indented(tp))
	}
	if !regexp.MustCompile(`icmp ult i64`).MatchString(tp) {
		t.Errorf("@tprobe: missing bounds check\n%s", indented(tp))
	}
	// at least two zexts: the txt length (i8 -> i64) and the payload byte.
	if n := strings.Count(tp, "zext i8"); n < 2 {
		t.Errorf("@tprobe: expected >= 2 zext i8 (len + payload), got %d\n%s", n, indented(tp))
	}

	// --- negative control: a function without .byte grows none of this ---
	pl := extractDefineBlock(t, ll, "@plain")
	if pl == "" {
		t.Fatal("@plain not found")
	}
	for _, bad := range []string{"icmp ult i64", "select i1", "store %option", "getelementptr i8, i8*"} {
		if strings.Contains(pl, bad) {
			t.Errorf("@plain: unexpected %q in a function that never calls .byte\n%s", bad, indented(pl))
		}
	}
}

func indented(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
