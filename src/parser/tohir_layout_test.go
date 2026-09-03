package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/hir"
	"github.com/lizongying/nolang/lexer"
)

// The layout conventions in ASTToHIR's header comment are a contract: every
// consumer (signature extraction today, type inference and codegen later)
// reads children by position or by slot label. A comment cannot enforce that.
// These tests do — each one pins one line of that header.

func lowerSrc(t *testing.T, src string) *hir.Package {
	t.Helper()
	p := New(lexer.NewCached("layout_test.no", src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parsing %q failed: %v", src, errs)
	}
	return ASTToHIR(prog)
}

// topNode returns the i-th top-level node, failing if it is absent or not of
// the expected kind.
func topNode(t *testing.T, p *hir.Package, i int, want hir.Kind) (int32, *hir.Node) {
	t.Helper()
	if i >= len(p.Top) {
		t.Fatalf("wanted top-level node #%d, package only has %d", i, len(p.Top))
	}
	id := p.Top[i]
	n := p.Node(id)
	if n == nil {
		t.Fatalf("top-level node #%d resolves to nil", i)
	}
	if n.Kind != want {
		t.Fatalf("top-level node #%d is %s, want %s", i, n.Kind, want)
	}
	return id, n
}

// slotOf returns the child wrapped in the KSlot labelled label, or NoID.
func slotOf(p *hir.Package, parent int32, label string) int32 {
	for _, c := range p.Children(parent) {
		n := p.Node(c)
		if n != nil && n.Kind == hir.KSlot && p.Str(n.S) == label {
			return n.First
		}
	}
	return hir.NoID
}

// slotsOf returns every child wrapped in a KSlot labelled label, in order.
func slotsOf(p *hir.Package, parent int32, label string) []int32 {
	var out []int32
	for _, c := range p.Children(parent) {
		n := p.Node(c)
		if n != nil && n.Kind == hir.KSlot && p.Str(n.S) == label {
			out = append(out, n.First)
		}
	}
	return out
}

// childKinds renders the direct children as "kind" or "slot:label" so a layout
// mismatch reports the whole shape instead of one wrong field.
func childKinds(p *hir.Package, parent int32) string {
	var parts []string
	for _, c := range p.Children(parent) {
		n := p.Node(c)
		if n == nil {
			parts = append(parts, "<nil>")
			continue
		}
		if n.Kind == hir.KSlot {
			parts = append(parts, "slot:"+p.Str(n.S))
			continue
		}
		parts = append(parts, n.Kind.String())
	}
	return strings.Join(parts, " ")
}

func TestUseLayout(t *testing.T) {
	// use: S=path, S2=function, FlagAsKeyword when written with `as`,
	// slot "alias" holding a KName.
	p := lowerSrc(t, "# std/math.add as m\n")
	id, n := topNode(t, p, 0, hir.KUse)

	if got := p.Str(n.S); got != "std/math" {
		t.Errorf("path = %q, want %q", got, "std/math")
	}
	if got := p.Str(n.S2); got != "add" {
		t.Errorf("function = %q, want %q", got, "add")
	}
	if !n.Has(hir.FlagAsKeyword) {
		t.Error("FlagAsKeyword not set for `as` form")
	}
	alias := p.Node(slotOf(p, id, "alias"))
	if alias == nil || alias.Kind != hir.KName {
		t.Fatalf("alias slot missing or wrong kind; children = %q", childKinds(p, id))
	}
	if got := p.Str(alias.S); got != "m" {
		t.Errorf("alias = %q, want %q", got, "m")
	}
}

func TestExportLayoutWithoutAsKeyword(t *testing.T) {
	// The bare-alias form must lower to the same shape but leave
	// FlagAsKeyword clear, because the formatter round-trips on that flag.
	p := lowerSrc(t, "@ std/math.add plus\n")
	id, n := topNode(t, p, 0, hir.KExport)

	if got := p.Str(n.S); got != "std/math" {
		t.Errorf("path = %q, want %q", got, "std/math")
	}
	if n.Has(hir.FlagAsKeyword) {
		t.Error("FlagAsKeyword set for the bare-alias form")
	}
	alias := p.Node(slotOf(p, id, "alias"))
	if alias == nil || p.Str(alias.S) != "plus" {
		t.Fatalf("alias = %v, want KName plus; children = %q", alias, childKinds(p, id))
	}
}

func TestLetLayout(t *testing.T) {
	// let: S=name, Type=declared type, single unlabelled child = value.
	p := lowerSrc(t, "x i64 = 42\n")
	id, n := topNode(t, p, 0, hir.KLet)

	if got := p.Str(n.S); got != "x" {
		t.Errorf("name = %q, want x", got)
	}
	if got := p.Type(n.Type); got != "i64" {
		t.Errorf("declared type = %q, want i64", got)
	}
	kids := p.Children(id)
	if len(kids) != 1 {
		t.Fatalf("let has %d children (%q), want exactly the value",
			len(kids), childKinds(p, id))
	}
	val := p.Node(kids[0])
	if val.Kind != hir.KIntLit || val.Val != 42 {
		t.Errorf("value = %s val=%d, want int 42", val.Kind, val.Val)
	}
}

func TestFuncDefLayout(t *testing.T) {
	// fn: S=name, children = param* result* generic* then slot "body".
	p := lowerSrc(t, "add = (a i64, b i64) (r i64) {\n    r = a + b\n}\n")
	id, n := topNode(t, p, 0, hir.KFuncDef)

	if got := p.Str(n.S); got != "add" {
		t.Errorf("name = %q, want add", got)
	}
	if got := childKinds(p, id); got != "param param result slot:body" {
		t.Fatalf("children = %q, want %q", got, "param param result slot:body")
	}

	kids := p.Children(id)
	for i, want := range []struct{ name, typ string }{{"a", "i64"}, {"b", "i64"}} {
		prm := p.Node(kids[i])
		if p.Str(prm.S) != want.name || p.Type(prm.Type) != want.typ {
			t.Errorf("param %d = %s %s, want %s %s",
				i, p.Str(prm.S), p.Type(prm.Type), want.name, want.typ)
		}
	}
	res := p.Node(kids[2])
	if p.Str(res.S) != "r" || p.Type(res.Type) != "i64" {
		t.Errorf("result = %s %s, want r i64", p.Str(res.S), p.Type(res.Type))
	}
	if body := p.Node(slotOf(p, id, "body")); body == nil || body.Kind != hir.KBlock {
		t.Errorf("body slot = %v, want a block", body)
	}
}

func TestCallLayoutKeepsArgumentOrder(t *testing.T) {
	// call: slot "fn" first, then one slot "arg" per argument, in source
	// order. Argument order is the one thing a flat arena can silently
	// scramble, so assert it explicitly rather than just counting.
	p := lowerSrc(t, "f(1, 2, 3)\n")
	stmtID, _ := topNode(t, p, 0, hir.KExprStmt)
	callID := p.Node(stmtID).First
	call := p.Node(callID)
	if call == nil || call.Kind != hir.KCall {
		t.Fatalf("expression statement wraps %v, want a call", call)
	}

	fn := p.Node(slotOf(p, callID, "fn"))
	if fn == nil || fn.Kind != hir.KIdent || p.Str(fn.S) != "f" {
		t.Fatalf("callee = %v, want ident f; children = %q", fn, childKinds(p, callID))
	}
	args := slotsOf(p, callID, "arg")
	if len(args) != 3 {
		t.Fatalf("%d arg slots, want 3; children = %q", len(args), childKinds(p, callID))
	}
	for i, want := range []int64{1, 2, 3} {
		if got := p.Node(args[i]).Val; got != want {
			t.Errorf("arg %d = %d, want %d", i, got, want)
		}
	}
}

func TestArrayLiteralSizeAndElementSlots(t *testing.T) {
	// A typed fixed-size array `a [N] = [...]` lowers to KArrayLit (with a
	// "size" slot + "elem" slots). The bare form `a = [1, 2]` is a slice and
	// lowers to KSliceLit instead, which is covered by the slice-lit test.
	p := lowerSrc(t, "a [2] = [1, 2]\n")
	letID, _ := topNode(t, p, 0, hir.KLet)
	arrID := p.Node(letID).First
	arr := p.Node(arrID)
	if arr == nil || arr.Kind != hir.KArrayLit {
		t.Fatalf("let value = %v, want array literal", arr)
	}
	// First child is the "size" slot; the rest are "elem" slots.
	if p.Str(p.Node(p.Node(arrID).First).S) != "size" {
		t.Fatalf("first child = %q, want \"size\" slot", childKinds(p, arrID))
	}
	elems := slotsOf(p, arrID, "elem")
	if len(elems) != 2 {
		t.Fatalf("%d elem slots, want 2; children = %q", len(elems), childKinds(p, arrID))
	}
	if p.Node(elems[0]).Val != 1 || p.Node(elems[1]).Val != 2 {
		t.Errorf("elements = %d,%d want 1,2", p.Node(elems[0]).Val, p.Node(elems[1]).Val)
	}
}

func TestSliceLiteralHasNoSizeSlot(t *testing.T) {
	// The untyped `a = [1, 2]` form is a slice literal: KSliceLit with only
	// element children (no "size" slot), distinct from a typed array literal.
	p := lowerSrc(t, "a = [1, 2]\n")
	letID, _ := topNode(t, p, 0, hir.KLet)
	slID := p.Node(letID).First
	sl := p.Node(slID)
	if sl == nil || sl.Kind != hir.KSliceLit {
		t.Fatalf("let value = %v, want slice literal", sl)
	}
	elems := p.Children(slID)
	if len(elems) != 2 {
		t.Fatalf("%d children, want 2; children = %q", len(elems), childKinds(p, slID))
	}
	if p.Node(elems[0]).Val != 1 || p.Node(elems[1]).Val != 2 {
		t.Errorf("elements = %d,%d want 1,2", p.Node(elems[0]).Val, p.Node(elems[1]).Val)
	}
}

func TestIntLiteralKeepsTokenLiteralForOutOfRangeValues(t *testing.T) {
	// Module export values render S2 (the token literal), not Val, because a
	// u64 constant above the int64 range would otherwise display as -1.
	const big = "18446744073709551615"
	p := lowerSrc(t, "max-u64 = "+big+"\n")
	letID, _ := topNode(t, p, 0, hir.KLet)
	lit := p.Node(p.Node(letID).First)
	if lit == nil || lit.Kind != hir.KIntLit {
		t.Fatalf("value = %v, want int literal", lit)
	}
	if got := p.Str(lit.S2); got != big {
		t.Errorf("S2 (token literal) = %q, want %q", got, big)
	}
}

func TestAnnotationsAttachToTheAnnotatedStatement(t *testing.T) {
	// Platform annotations are the reason Package.Anns exists: codegen drops
	// statements whose platform key does not match the target.
	p := lowerSrc(t, "#{linux-amd64}\nx = 1\ny = 2\n")
	annotated, _ := topNode(t, p, 0, hir.KLet)
	plain, _ := topNode(t, p, 1, hir.KLet)

	keys, hasValue := p.AnnotationKeys(annotated)
	if len(keys) != 1 || keys[0] != "linux-amd64" {
		t.Fatalf("annotation keys = %v, want [linux-amd64]", keys)
	}
	if hasValue[0] {
		t.Error("bare platform key reported as having a value; matchesPlatform " +
			"only considers valueless entries, so this distinction is load-bearing")
	}
	if k, _ := p.AnnotationKeys(plain); len(k) != 0 {
		t.Errorf("unannotated statement carries %v", k)
	}
}

func TestAnnotationWithValueIsNotABareKey(t *testing.T) {
	p := lowerSrc(t, "#{generic=[k, v]}\nf = (a k) (r v) {\n    r = a\n}\n")
	id, _ := topNode(t, p, 0, hir.KFuncDef)
	keys, hasValue := p.AnnotationKeys(id)
	if len(keys) != 1 || keys[0] != "generic" {
		t.Fatalf("annotation keys = %v, want [generic]", keys)
	}
	if !hasValue[0] {
		t.Error("#{generic=[...]} reported as a bare key")
	}
}

func TestCastStoresTargetTypeAsString(t *testing.T) {
	p := lowerSrc(t, "p = nil\nq = p as *byte\n")
	letID, _ := topNode(t, p, 1, hir.KLet)
	cast := p.Node(p.Node(letID).First)
	if cast == nil || cast.Kind != hir.KCast {
		t.Fatalf("value = %v, want a cast", cast)
	}
	if got := p.Type(cast.Type); got != "*byte" {
		t.Errorf("cast target = %q, want *byte", got)
	}
}

func TestMapLiteralPairsKeepKeyThenValue(t *testing.T) {
	p := lowerSrc(t, "m [str]i64 = {\"a\": 1}\n")
	letID, _ := topNode(t, p, 0, hir.KLet)
	mapID := p.Node(letID).First
	m := p.Node(mapID)
	if m == nil || m.Kind != hir.KMapLit {
		t.Fatalf("value = %v, want a map literal", m)
	}
	pairs := p.Children(mapID)
	if len(pairs) != 1 {
		t.Fatalf("%d pairs, want 1; children = %q", len(pairs), childKinds(p, mapID))
	}
	kv := p.Children(pairs[0])
	if len(kv) != 2 {
		t.Fatalf("pair has %d children, want key then value", len(kv))
	}
	if v := p.Node(kv[1]); v.Kind != hir.KIntLit || v.Val != 1 {
		t.Errorf("pair value = %s val=%d, want int 1", v.Kind, v.Val)
	}
}

func TestExternLayout(t *testing.T) {
	p := lowerSrc(t, "#{c}\nputs = (s *byte) (r i32)\n")
	id, n := topNode(t, p, 0, hir.KExtern)
	if got := p.Str(n.S); got != "puts" {
		t.Errorf("name = %q, want puts", got)
	}
	if got := p.Str(n.S2); got != "c" {
		t.Errorf("lang = %q, want c", got)
	}
	if got := childKinds(p, id); got != "param result" {
		t.Errorf("children = %q, want %q", got, "param result")
	}
}

// TestVariadicUnionIsEmptyAtLoweringTime documents an ordering constraint that
// the HIR migration has to respect: FunctionDefinition.VariadicUnion and
// GenericUnion are written by the *checker* (checker.go:662 / :679), long after
// ParseProgram returns. Lowering at the end of parsing therefore cannot see
// them, and codegen's union monomorphisation must read them from a stage-2
// side table keyed by node id rather than from HIR's S2.
func TestVariadicUnionIsEmptyAtLoweringTime(t *testing.T) {
	p := lowerSrc(t, "num = i64 | f64\nabs = (a num) (r num) {\n    r = a\n}\n")
	for i := range p.Top {
		n := p.Node(p.Top[i])
		if n.Kind != hir.KFuncDef {
			continue
		}
		if got := p.Str(n.S2); got != "" {
			t.Fatalf("S2 (variadic union) = %q at lowering time; if the parser now "+
				"fills it, this test and the KUnionName exemption in "+
				"hir/golden_test.go are both stale", got)
		}
		return
	}
	t.Fatal("no function definition lowered; the snippet no longer exercises this")
}
