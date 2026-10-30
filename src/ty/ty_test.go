package ty

import "testing"

// testResolver stands in for a stage registry: `fd` is a newtype alias of i64,
// `code` is an integer newtype, `Point` is a struct and `color` an enum.
type testResolver struct{}

func (testResolver) Alias(name string) (string, bool) {
	switch name {
	case "fd":
		return "i64", true
	case "code":
		return "i32", true
	case "point-alias":
		return "Point", true
	}
	return "", false
}

func (testResolver) IsTypeName(name string) bool {
	switch name {
	case "fd", "code", "Point", "color", "point-alias":
		return true
	}
	return false
}

func newTestPool() *Pool { return NewPool(testResolver{}) }

func TestParseScalars(t *testing.T) {
	p := newTestPool()
	cases := []struct {
		raw    string
		kind   Kind
		bits   int16
		signed bool
		llvm   string
	}{
		{"void", Void, 0, false, "void"},
		{"bool", Bool, 1, false, "i1"},
		{"byte", Int, 8, false, "i8"},
		{"i8", Int, 8, true, "i8"},
		{"u8", Int, 8, false, "i8"},
		{"i16", Int, 16, true, "i16"},
		{"u32", Int, 32, false, "i32"},
		{"i64", Int, 64, true, "i64"},
		{"u64", Int, 64, false, "i64"},
		{"i128", Int, 128, true, "i128"},
		{"u128", Int, 128, false, "i128"},
		{"char", Char, 32, false, "i32"},
		{"f32", Float, 32, false, "double"},
		{"f64", Float, 64, false, "double"},
		{"str", Str, 0, false, "%str-long"},
		{"txt", Txt, 0, false, "%txt"},
		// std aliases become explicit structure instead of a lucky fallback.
		{"int", Int, 64, true, "i64"},
		{"uint", Int, 64, false, "i64"},
		{"float", Float, 64, false, "double"},
		{"vec", Slice, 0, false, "%vec"},
	}
	for _, c := range cases {
		got := p.Parse(c.raw)
		if got.Kind != c.kind {
			t.Errorf("%s: kind = %v, want %v", c.raw, got.Kind, c.kind)
		}
		if got.Bits != c.bits && c.kind != Slice {
			t.Errorf("%s: bits = %d, want %d", c.raw, got.Bits, c.bits)
		}
		if c.kind == Int && got.Signed != c.signed {
			t.Errorf("%s: signed = %v, want %v", c.raw, got.Signed, c.signed)
		}
		if llvm := got.LLVM(); llvm != c.llvm {
			t.Errorf("%s: llvm = %q, want %q", c.raw, llvm, c.llvm)
		}
		if got.String() != c.raw {
			t.Errorf("%s: String() = %q", c.raw, got.String())
		}
	}
}

func TestParseComposites(t *testing.T) {
	p := newTestPool()

	sl := p.Parse("[]byte")
	if sl.Kind != Slice || sl.Elem == nil || sl.Elem.Raw() != "byte" {
		t.Fatalf("[]byte = %#v", sl)
	}
	if sl.String() != "[]byte" {
		t.Errorf("round trip = %q", sl.String())
	}

	arr := p.Parse("[32]i64")
	if arr.Kind != Array || arr.Len != 32 || arr.LenText != "" || arr.Elem.Raw() != "i64" {
		t.Fatalf("[32]i64 = %#v", arr)
	}

	unknown := p.Parse("[?]i64")
	if unknown.Kind != Array || unknown.Len != -1 || unknown.LenText != "?" {
		t.Fatalf("[?]i64 = %#v", unknown)
	}

	// A single letter is an implicit generic size parameter, not a key type.
	sym := p.Parse("[n]t")
	if sym.Kind != Array || sym.LenText != "n" {
		t.Fatalf("[n]t = %#v", sym)
	}

	// A known type name in the brackets is a key: the `[fd]i64` case that the
	// three old parsers disagreed about.
	m := p.Parse("[fd]i64")
	if m.Kind != Map || m.Key == nil || m.Key.Raw() != "fd" || m.Elem.Raw() != "i64" {
		t.Fatalf("[fd]i64 = %#v", m)
	}
	if km := p.Parse("[str]i64"); km.Kind != Map {
		t.Fatalf("[str]i64 = %v", km.Kind)
	}
	if km := p.Parse("[Point]i64"); km.Kind != Map {
		t.Fatalf("[Point]i64 = %v", km.Kind)
	}

	// Nested brackets must keep their inner structure.
	nested := p.Parse("[[2]i64]str")
	if nested.Kind != Map || nested.Key.Kind != Array || nested.Key.Len != 2 {
		t.Fatalf("[[2]i64]str = %#v (key %#v)", nested, nested.Key)
	}

	opt := p.Parse("?i64")
	if opt.Kind != Option || opt.Elem.Raw() != "i64" {
		t.Fatalf("?i64 = %#v", opt)
	}
	optView := p.Parse("?&i64")
	if optView.Kind != Option || optView.Elem.Kind != View || optView.Elem.Elem.Raw() != "i64" {
		t.Fatalf("?&i64 = %#v", optView)
	}
	if v := p.Parse("&i64"); v.Kind != View {
		t.Fatalf("&i64 = %v", v.Kind)
	}
	// Both pointer spellings are Ptr and each keeps its own text.
	star := p.Parse("*byte")
	ptrw := p.Parse("ptr byte")
	if star.Kind != Ptr || ptrw.Kind != Ptr {
		t.Fatalf("*byte=%v ptr byte=%v", star.Kind, ptrw.Kind)
	}
	if star.Raw() != "*byte" || ptrw.Raw() != "ptr byte" {
		t.Fatalf("pointer spellings mutated: %q %q", star.Raw(), ptrw.Raw())
	}

	fn := p.Parse("fn(i64,[]byte)(str)")
	if fn.Kind != Func || len(fn.Params) != 2 || len(fn.Results) != 1 {
		t.Fatalf("fn(...) = %#v", fn)
	}
	if fn.Params[1].Kind != Slice || fn.Results[0].Kind != Str {
		t.Fatalf("fn(...) components = %#v", fn)
	}
	if nf := p.Parse("fn()"); nf.Kind != Func || len(nf.Params) != 0 || len(nf.Results) != 0 {
		t.Fatalf("fn() = %#v", nf)
	}
	if nf := p.Parse("fn(i64)"); nf.Kind != Func || len(nf.Params) != 1 || len(nf.Results) != 0 {
		t.Fatalf("fn(i64) = %#v", nf)
	}

	u := p.Parse("i64|f64")
	if u.Kind != Union || len(u.Members) != 2 {
		t.Fatalf("i64|f64 = %#v", u)
	}
	if !u.HasMember("f64") || u.HasMember("f32") {
		t.Error("HasMember mismatch")
	}
	spaced := p.Parse("i64 | f64")
	if spaced.Kind != Union || len(spaced.Members) != 2 {
		t.Fatalf("i64 | f64 = %#v", spaced)
	}
	if spaced.Raw() != "i64 | f64" {
		t.Errorf("union spelling mutated: %q", spaced.Raw())
	}

	named := p.Parse("os.utsname")
	if named.Kind != Named || named.Name != "os.utsname" {
		t.Fatalf("os.utsname = %#v", named)
	}
}

func TestParseAliasesAndCanonical(t *testing.T) {
	p := newTestPool()

	fd := p.Parse("fd")
	if fd.Kind != Named || fd.Name != "fd" {
		t.Fatalf("fd = %#v", fd)
	}
	if un := fd.Underlying(); un == nil || un.Raw() != "i64" {
		t.Fatalf("fd.Underlying() = %#v", un)
	}
	if c := fd.Canonical(); c.Kind != Int || c.Bits != 64 {
		t.Fatalf("fd.Canonical() = %#v", c)
	}
	// The alias keeps its own spelling as identity.
	if fd.Raw() != "fd" {
		t.Errorf("fd raw = %q", fd.Raw())
	}

	chain := p.Parse("point-alias")
	if chain.Kind != Named || chain.Underlying() == nil {
		t.Fatalf("point-alias = %#v", chain)
	}
	// A cycle-free walk terminates on the struct name, which has no Under.
	if c := chain.Canonical(); c.Kind != Named || c.Name != "Point" {
		t.Fatalf("point-alias.Canonical() = %#v", c)
	}

	// `num` and `string` are structural unions.
	num := p.Parse("num")
	if num.Kind != Union || len(num.Members) != 2 || !num.HasMember("i64") || !num.HasMember("f64") {
		t.Fatalf("num = %#v", num)
	}
	str := p.Parse("string")
	if str.Kind != Union || !str.HasMember("str") || !str.HasMember("txt") {
		t.Fatalf("string = %#v", str)
	}
}

func TestParseTotal(t *testing.T) {
	p := NewPool(nil)
	for _, raw := range []string{"", " ", "?", "&", "*", "[]", "[", "[]]", "fn(", "fn(i64", "fn(i64)(", "[i64]", "|i64", "i64|", "??"} {
		got := p.Parse(raw)
		if got == nil {
			t.Fatalf("%q: nil", raw)
		}
		if got.String() != raw {
			t.Errorf("%q: String() = %q", raw, got.String())
		}
	}
	// A resolver-less pool must not resolve aliases.
	if fd := p.Parse("fd"); fd.Underlying() != nil {
		t.Errorf("nil resolver produced Under = %#v", fd.Underlying())
	}
}

func TestInterning(t *testing.T) {
	p := newTestPool()
	a := p.Parse("[]i64")
	b := p.Parse("[]i64")
	if a != b {
		t.Fatal("same spelling must intern to one pointer")
	}
	if a.Elem != p.Parse("i64") {
		t.Fatal("nested components must be interned too")
	}
	if !Equal(a, b) {
		t.Fatal("Equal disagrees with interning")
	}
	if Equal(p.Parse("i64"), p.Parse("u64")) {
		t.Fatal("Equal must distinguish i64 from u64")
	}
	if p.Len() == 0 {
		t.Fatal("Len = 0 after Parse")
	}
}

func TestConcurrentParse(t *testing.T) {
	p := newTestPool()
	raws := []string{"i64", "[]byte", "[fd]i64", "?str", "fn(i64)(i64)", "num", "[n]t", "ptr byte", "i64|f64"}
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for n := 0; n < 200; n++ {
				raw := raws[(i+n)%len(raws)]
				got := p.Parse(raw)
				if got.String() != raw {
					t.Errorf("%q -> %q", raw, got.String())
					return
				}
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	for _, raw := range raws {
		if p.Parse(raw) != p.Parse(raw) {
			t.Fatalf("%q: racing intern produced two instances", raw)
		}
	}
}

func TestConvert(t *testing.T) {
	p := newTestPool()
	cases := []struct {
		from, to string
		want     Conv
	}{
		{"i64", "i64", ConvNone},
		{"fd", "i64", ConvNone},
		{"i8", "i64", ConvSExt},
		{"byte", "i64", ConvZExt},
		{"u8", "i64", ConvZExt},
		{"bool", "i64", ConvZExt},
		{"char", "i64", ConvZExt},
		{"i32", "i16", ConvTrunc},
		{"i64", "f64", ConvSIToFP},
		{"u64", "f64", ConvUIToFP},
		{"f64", "i64", ConvSatFPToI},
		{"f32", "f64", ConvFPExt},
		{"f64", "f32", ConvFPTrunc},
		{"i32", "u32", ConvNone},
		{"[]i64", "[]i64", ConvNone},
		{"Point", "[]i64", ConvNone},
	}
	for _, c := range cases {
		got := Convert(p.Parse(c.from), p.Parse(c.to))
		if got != c.want {
			t.Errorf("Convert(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
	if Convert(nil, p.Parse("i64")) != ConvNone {
		t.Error("nil source must be ConvNone")
	}
	if got := ConvertExact(p.Parse("f64"), p.Parse("u64")); got != ConvFPToUI {
		t.Errorf("ConvertExact(f64, u64) = %v", got)
	}
}

func TestPromote(t *testing.T) {
	p := newTestPool()
	cases := []struct {
		l, r string
		want string
	}{
		{"i8", "i64", "i64"},
		{"i64", "i8", "i64"},
		{"i64", "i64", "i64"},
		{"u32", "i64", "i64"},
		{"i64", "f64", "f64"},
		{"f32", "f64", "f64"},
		{"byte", "char", "u32"},
		{"char", "char", "char"},
		{"bool", "i64", "i64"},
		{"i64", "Point", ""},
		{"[]i64", "[]i64", "[]i64"},
	}
	for _, c := range cases {
		got := p.Promote(p.Parse(c.l), p.Parse(c.r))
		switch {
		case c.want == "":
			if got != nil {
				t.Errorf("Promote(%s, %s) = %v, want nil", c.l, c.r, got)
			}
		case got == nil:
			t.Errorf("Promote(%s, %s) = nil, want %s", c.l, c.r, c.want)
		case got.Raw() != c.want:
			t.Errorf("Promote(%s, %s) = %s, want %s", c.l, c.r, got.Raw(), c.want)
		}
	}
	if got := p.Promote(p.Parse("bool"), p.Parse("bool")); got == nil || got.Raw() != "u1" {
		t.Errorf("Promote(bool, bool) = %v", got)
	}
}

func TestMakeOnAndChecked(t *testing.T) {
	p := newTestPool()
	// Stage-precise knowledge wins, and the conflict is reported.
	got, ok := p.Checked("Point", Type{Kind: Named, Name: "Point"})
	if !ok {
		t.Fatalf("Point should agree with the inferred structure: %#v", got)
	}
	_, conflict := p.Checked("i64", Type{Kind: Named, Name: "i64"})
	if conflict {
		t.Error("i64 reported as struct should conflict")
	}

	q := NewPool(nil)
	made := q.MakeOn("Thing", Type{Kind: Named, Name: "Thing"})
	if made.Raw() != "Thing" || made.Kind != Named {
		t.Fatalf("MakeOn = %#v", made)
	}
	if q.Parse("Thing") != made {
		t.Error("MakeOn must be visible to later Parse calls")
	}
}
