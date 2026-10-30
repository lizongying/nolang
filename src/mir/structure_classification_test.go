package mir

import "testing"

// TestBracketClassificationIsStructural pins the single authority for the
// array/map/slice ambiguity at the MIR boundary.
//
// The same spelling used to get three different answers depending on who was
// asked: parser.buildType called `[fd]i64` a MAP (bracketed type name), MIR's
// hand-rolled `arrayElemRaw` called it an ARRAY (it only searched for ']' and
// then trimmed `raw[closeB+1:]`), and codegen's `parseSliceElem` disagreed with
// both on the fixed-size form. All three are now `ty`, so this test exists to
// keep the rule in one place -- and to keep `ElemRaw` the only element accessor,
// since the deleted helper was exactly a second, subtly different one.
func TestBracketClassificationIsStructural(t *testing.T) {
	m := NewModule("t")
	// `fd` is a struct whose layout this module knows: a bracketed NAME, so it
	// has to be read as a key type no matter which stage asks.
	m.StructNames["fd"] = true

	cases := []struct {
		raw                           string
		wantMap, wantArray, wantSlice bool
		why                           string
	}{
		{"[fd]i64", true, false, false, "non-numeric key => map (the case the deleted MIR helper got wrong)"},
		{"[str]i64", true, false, false, "builtin scalar key => map"},
		{"[3]i64", false, true, false, "digits => fixed-size array"},
		{"[n]t", false, true, false, "single letter => implicit generic length"},
		{"[?]i64", false, true, false, "inferred length"},
		{"[]i64", false, false, true, "empty brackets => slice"},
	}
	for _, c := range cases {
		st := m.tyOf(c.raw)
		if st == nil {
			t.Fatalf("%s: nil type", c.raw)
		}
		if got := st.IsMap(); got != c.wantMap {
			t.Errorf("%s: IsMap=%v, want %v -- %s", c.raw, got, c.wantMap, c.why)
		}
		if got := st.IsArray(); got != c.wantArray {
			t.Errorf("%s: IsArray=%v, want %v -- %s", c.raw, got, c.wantArray, c.why)
		}
		if got := st.IsSlice(); got != c.wantSlice {
			t.Errorf("%s: IsSlice=%v, want %v -- %s", c.raw, got, c.wantSlice, c.why)
		}
	}

	// LAYOUT kind is a separate question from syntax kind, but for containers
	// they must agree; this is what kindOfTy replaced `KindOfRaw` with.
	if k := kindOfTy(m.tyOf("[fd]i64")); k != KindMap {
		t.Errorf("kindOfTy([fd]i64) = %v, want KindMap", k)
	}
	if k := kindOfTy(m.tyOf("[3]i64")); k != KindArray {
		t.Errorf("kindOfTy([3]i64) = %v, want KindArray", k)
	}
	if k := kindOfTy(m.tyOf("[]i64")); k != KindSlice {
		t.Errorf("kindOfTy([]i64) = %v, want KindSlice", k)
	}

	// ElemRaw is the single element accessor for arrays, slices and maps alike,
	// so no caller needs to slice the raw spelling to find out what is inside.
	for raw, want := range map[string]string{
		"[3]i64":    "i64",
		"[]str":     "str",
		"[str]?i64": "?i64",
	} {
		if got := m.tyOf(raw).ElemRaw(); got != want {
			t.Errorf("ElemRaw(%s) = %q, want %q", raw, got, want)
		}
	}
	if k := m.tyOf("[str]?i64").Key.Raw(); k != "str" {
		t.Errorf("key of [str]?i64 = %q, want str", k)
	}
}
