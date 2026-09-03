package hir

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// TestNodeIsPointerFree guards the central design invariant: a Node must be a
// plain value type. The whole reason HIR exists as a flat arena is that a
// package's GC cost becomes proportional to the handful of slices it owns
// rather than to its node count. One pointer, string or interface field in
// Node silently gives that back, because the GC then has to scan every element
// of Package.Nodes.
func TestNodeIsPointerFree(t *testing.T) {
	rt := reflect.TypeOf(Node{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		switch f.Type.Kind() {
		case reflect.Int32, reflect.Int64, reflect.Uint32:
			// fine: fixed-width scalars
		default:
			t.Errorf("Node.%s has kind %s; Node must contain only fixed-width "+
				"scalars so the arena stays pointer-free (use an interned "+
				"string id instead of a string/pointer/interface field)",
				f.Name, f.Type.Kind())
		}
	}
	// 10 int32 fields + one int64, all naturally aligned.
	if got, want := unsafe.Sizeof(Node{}), uintptr(48); got != want {
		t.Errorf("unsafe.Sizeof(Node{}) = %d, want %d; a node grew, which "+
			"multiplies arena memory across every module", got, want)
	}
}

// TestKindNamesComplete keeps KindNames in sync with the Kind const block.
// A missing entry would make Dump and every test failure message print
// "Kind(37)" instead of a name.
func TestKindNamesComplete(t *testing.T) {
	seen := map[string]Kind{}
	for k := Kind(0); k < kindCount; k++ {
		name := KindNames[k]
		if name == "" {
			t.Errorf("Kind(%d) has no entry in KindNames", int32(k))
			continue
		}
		if prev, dup := seen[name]; dup {
			t.Errorf("KindNames[%d] and KindNames[%d] are both %q; names must "+
				"be unique to stay useful in Dump output", int32(prev), int32(k), name)
		}
		seen[name] = k
		if k.String() != name {
			t.Errorf("Kind(%d).String() = %q, want %q", int32(k), k.String(), name)
		}
	}
}

func TestKindStringOutOfRange(t *testing.T) {
	if got := kindCount.String(); !strings.HasPrefix(got, "Kind(") {
		t.Errorf("out-of-range Kind should render as Kind(n), got %q", got)
	}
	if got := Kind(-1).String(); got != "Kind(-1)" {
		t.Errorf("Kind(-1).String() = %q, want Kind(-1)", got)
	}
}

// TestZeroValueNodeIsALeaf pins down the invariant that motivated NoID == 0.
//
// Regression test for a real bug: with NoID == -1, a node built without
// explicitly setting First and Next (which is every node in the AST -> HIR
// lowering that has a single child, e.g. KReturn) got First = Next = 0, i.e.
// it claimed node 0 as both its first child and its next sibling. The first
// traversal then looped forever. Reserving slot 0 as the nil node makes the
// zero value mean "childless leaf, no next sibling".
func TestZeroValueNodeIsALeaf(t *testing.T) {
	if NoID != 0 {
		t.Fatalf("NoID = %d; this test and the zero-value invariant assume 0", NoID)
	}
	b := NewBuilder("m")
	// A single child NOT routed through List, so its Next is never assigned.
	child := b.Add(Node{Kind: KIntLit, Val: 7})
	parent := b.Add(Node{Kind: KReturn, First: child})
	b.AddTop(parent)
	p := b.Package()

	if got := p.Children(parent); len(got) != 1 || got[0] != child {
		t.Fatalf("Children = %v, want exactly [%d]; an unset Next must terminate "+
			"the sibling chain, not point back into the arena", got, child)
	}
	var visited int
	p.WalkTop(func(int32, *Node) bool { visited++; return visited < 100 })
	if visited != 2 {
		t.Errorf("walk visited %d nodes, want 2 (parent + child); more means the "+
			"sibling chain is cyclic", visited)
	}
}

// TestReservedSlotZero documents that index 0 of both tables is a sentinel and
// is never handed out by Add or Intern.
func TestReservedSlotZero(t *testing.T) {
	b := NewBuilder("m")
	if id := b.Add(Node{Kind: KIdent}); id == NoID {
		t.Error("Add must never return NoID; slot 0 is the reserved nil node")
	}
	if id := b.Intern("x"); id == NoID {
		t.Error("Intern must never return NoID for a non-empty string")
	}
	p := b.Package()
	if p.Node(NoID) != nil {
		t.Error("Node(NoID) must be nil so callers can pass an absent id safely")
	}
	if p.Str(NoID) != "" {
		t.Error("Str(NoID) must be the empty string")
	}
	if len(p.Nodes) < 1 || len(p.Strings) < 1 {
		t.Fatal("both tables must reserve slot 0")
	}
}

func TestInternDedupesAndMapsEmptyToNoID(t *testing.T) {
	b := NewBuilder("m")
	if id := b.Intern(""); id != NoID {
		t.Errorf("Intern(\"\") = %d, want NoID so absent payloads stay distinguishable", id)
	}
	a := b.Intern("hello")
	again := b.Intern("hello")
	other := b.Intern("world")
	if a != again {
		t.Errorf("Intern is not deduplicating: %d != %d", a, again)
	}
	if a == other {
		t.Error("distinct strings interned to the same id")
	}
	p := b.Package()
	if p.Str(a) != "hello" || p.Str(other) != "world" {
		t.Errorf("round trip failed: %q / %q", p.Str(a), p.Str(other))
	}
	if p.Str(NoID) != "" || p.Str(9999) != "" {
		t.Error("out-of-range string ids must resolve to \"\"")
	}
}

// TestAddAssignsSequentialIDs checks that a node's Id always equals its arena
// index, which is what lets a rewrite pass record old -> new ids by number.
// Ids start at 1 because slot 0 is the reserved nil node.
func TestAddAssignsSequentialIDs(t *testing.T) {
	b := NewBuilder("m")
	for i := 1; i <= 5; i++ {
		if id := b.Add(Node{Kind: KIdent}); id != int32(i) {
			t.Fatalf("Add #%d returned id %d, want %d", i, id, i)
		}
	}
	p := b.Package()
	for i := range p.Nodes {
		if p.Nodes[i].Id != int32(i) {
			t.Errorf("Nodes[%d].Id = %d, want %d", i, p.Nodes[i].Id, i)
		}
	}
}

func TestListLinksSiblingsAndDropsNoID(t *testing.T) {
	b := NewBuilder("m")
	a := b.Add(Node{Kind: KIdent, S: b.Intern("a")})
	c := b.Add(Node{Kind: KIdent, S: b.Intern("c")})

	// NoID entries are dropped, which is what lets a caller pass optional
	// operands positionally without pre-filtering them.
	head := b.List([]int32{NoID, a, NoID, c, NoID})
	if head != a {
		t.Fatalf("List head = %d, want %d", head, a)
	}
	parent := b.Add(Node{Kind: KBlock, First: head})
	p := b.Package()

	if got := names(p, parent); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Errorf("children = %v, want [a c]", got)
	}
	if p.Nodes[c].Next != NoID {
		t.Error("last sibling's Next must be NoID")
	}
	if b.List(nil) != NoID || b.List([]int32{NoID}) != NoID {
		t.Error("an all-empty list must produce NoID")
	}
}

func TestAppendChildGrowsTheChain(t *testing.T) {
	b := NewBuilder("m")
	parent := b.Add(Node{Kind: KBlock, First: NoID})
	for _, s := range []string{"a", "b", "c"} {
		b.AppendChild(parent, b.Add(Node{Kind: KIdent, S: b.Intern(s)}))
	}
	// No-ops rather than panics, so lowering passes can append unconditionally.
	b.AppendChild(parent, NoID)
	b.AppendChild(NoID, parent)

	p := b.Package()
	if got := names(p, parent); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("children = %v, want [a b c]", got)
	}
}

func TestWalkVisitsPreOrder(t *testing.T) {
	b := NewBuilder("m")
	leafA := b.Add(Node{Kind: KIdent, S: b.Intern("a")})
	leafB := b.Add(Node{Kind: KIdent, S: b.Intern("b")})
	inner := b.Add(Node{Kind: KBlock, S: b.Intern("inner"), First: b.List([]int32{leafA, leafB})})
	leafC := b.Add(Node{Kind: KIdent, S: b.Intern("c")})
	root := b.Add(Node{Kind: KBlock, S: b.Intern("root"), First: b.List([]int32{inner, leafC})})
	b.AddTop(root)
	p := b.Package()

	var order []string
	p.WalkTop(func(_ int32, n *Node) bool {
		order = append(order, p.Str(n.S))
		return true
	})
	want := []string{"root", "inner", "a", "b", "c"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("pre-order = %v, want %v", order, want)
	}

	// Returning false prunes the subtree but not the siblings after it.
	order = nil
	p.WalkTop(func(_ int32, n *Node) bool {
		order = append(order, p.Str(n.S))
		return n.Kind != KBlock || p.Str(n.S) != "inner"
	})
	if want := []string{"root", "inner", "c"}; !reflect.DeepEqual(order, want) {
		t.Errorf("pruned walk = %v, want %v", order, want)
	}
}

// TestRemapRedirectsWithoutMutatingOldNodes is the property that makes
// lowering auditable: a pass appends replacement nodes and records old -> new,
// so the original nodes survive intact and the rewrite is inspectable rather
// than being an in-place pointer mutation.
func TestRemapRedirectsWithoutMutatingOldNodes(t *testing.T) {
	b := NewBuilder("m")
	old := b.Add(Node{Kind: KIntLit, Val: 1})
	sib := b.Add(Node{Kind: KIntLit, Val: 2})
	root := b.Add(Node{Kind: KBlock, First: b.List([]int32{old, sib})})
	b.AddTop(root)
	p := b.Package()

	// A lowering pass replaces `old` with a freshly appended node. The
	// replacement carries the old node's outgoing links itself: Remap only
	// rewrites references *to* the old id, it does not splice the new node
	// into the chain.
	fresh := int32(len(p.Nodes))
	p.Nodes = append(p.Nodes, Node{
		Kind: KIntLit, Id: fresh, Val: 42,
		First: NoID, Next: p.Nodes[old].Next,
	})
	p.Remap(map[int32]int32{old: fresh})

	kids := p.Children(root)
	if len(kids) != 2 || kids[0] != fresh || kids[1] != sib {
		t.Fatalf("children after remap = %v, want [%d %d]", kids, fresh, sib)
	}
	if p.Nodes[old].Val != 1 {
		t.Error("Remap must not mutate the replaced node; the old tree stays readable")
	}
	if p.Nodes[fresh].Next != sib {
		t.Errorf("replacement's Next = %d, want %d (sibling chain must be preserved)",
			p.Nodes[fresh].Next, sib)
	}
}

func TestRemapRewritesTopLevel(t *testing.T) {
	b := NewBuilder("m")
	old := b.Add(Node{Kind: KLet, S: b.Intern("x")})
	b.AddTop(old)
	p := b.Package()

	fresh := int32(len(p.Nodes))
	p.Nodes = append(p.Nodes, Node{Kind: KLet, Id: fresh, S: p.Nodes[old].S, First: NoID, Next: NoID})
	p.Remap(map[int32]int32{old: fresh})

	if len(p.Top) != 1 || p.Top[0] != fresh {
		t.Errorf("Top = %v, want [%d]", p.Top, fresh)
	}
}

func TestFloatAndBoolPayloadsRoundTrip(t *testing.T) {
	for _, f := range []float64{0, 1, -1, 3.141592653589793, math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(1)} {
		var n Node
		n.SetFloat(f)
		if got := n.Float(); got != f {
			t.Errorf("float round trip: got %v, want %v", got, f)
		}
	}
	var nan Node
	nan.SetFloat(math.NaN())
	if !math.IsNaN(nan.Float()) {
		t.Error("NaN did not survive the Val bit-pattern round trip")
	}
	if (&Node{Val: 0}).Bool() || !(&Node{Val: 1}).Bool() {
		t.Error("Bool must report Val != 0")
	}
}

func TestHasRequiresAllFlags(t *testing.T) {
	n := Node{Flags: FlagMethod | FlagVariadic}
	if !n.Has(FlagMethod) || !n.Has(FlagMethod|FlagVariadic) {
		t.Error("Has must be true when every requested flag is set")
	}
	if n.Has(FlagInline) || n.Has(FlagMethod|FlagInline) {
		t.Error("Has must be false unless ALL requested flags are set")
	}
}

func TestFlagsAreDistinctBits(t *testing.T) {
	all := []uint32{
		FlagMethod, FlagVariadic, FlagSynthetic, FlagModuleConst, FlagInline,
		FlagColon, FlagSkipNaming, FlagCondWrapper, FlagWasSlice, FlagExplicit,
		FlagUnion, FlagInferred, FlagReadOnly, FlagSealed, FlagImplicitGeneric,
		FlagGenericReceiver, FlagLeftInc, FlagRightInc, FlagAsKeyword,
		FlagSlice, FlagFuncType,
	}
	var acc uint32
	for i, f := range all {
		if f == 0 || f&(f-1) != 0 {
			t.Errorf("flag #%d = %#x is not a single bit", i, f)
		}
		if acc&f != 0 {
			t.Errorf("flag #%d = %#x collides with an earlier flag", i, f)
		}
		acc |= f
	}
}

func TestNodeAndStrOutOfRangeAreSafe(t *testing.T) {
	p := NewBuilder("m").Package()
	if p.Node(NoID) != nil || p.Node(0) != nil || p.Node(-7) != nil {
		t.Error("Node must return nil for out-of-range ids rather than panicking")
	}
	// Walk on a missing node must be a no-op, so callers can pass NoID freely.
	p.Walk(NoID, func(int32, *Node) bool {
		t.Error("Walk visited a node in an empty package")
		return true
	})
}

func TestKindCountsTalliesArena(t *testing.T) {
	b := NewBuilder("m")
	b.Add(Node{Kind: KIdent})
	b.Add(Node{Kind: KIdent})
	b.Add(Node{Kind: KBlock})
	got := b.Package().KindCounts()
	if got[KIdent] != 2 || got[KBlock] != 1 || got[KLet] != 0 {
		t.Errorf("KindCounts = %v, want 2 idents and 1 block", got)
	}
}

// names returns the interned S of each direct child of id, for readable
// assertions on sibling chains.
func names(p *Package, id int32) []string {
	var out []string
	for _, c := range p.Children(id) {
		out = append(out, p.Str(p.Node(c).S))
	}
	return out
}
