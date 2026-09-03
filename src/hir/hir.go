// Package hir defines the Nolang high-level intermediate representation.
//
// HIR is a flat arena. Every node is a fixed-size struct living in a single
// []Node slice; children are referenced by integer id through a
// first-child/next-sibling linked list; every string payload is interned into
// a side table and referenced by index. There are no Go pointers between
// nodes, so the GC cost of a package is proportional to the handful of slices
// it owns rather than to the node count.
//
// The package deliberately does NOT import parser. hir must stay independent
// of the surface syntax it is lowered from; the dependency runs one way,
// parser -> hir.
//
// Design note: an earlier sketch used `Data any` per node. That was rejected
// because an interface field adds two words to every node and allocates a
// heap object per payload, which defeats the point of storing the tree in
// flat slices. Kind + fixed integer fields keeps the node a plain value type.
package hir

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Kind identifies the shape of a Node.
type Kind int32

// Node kinds. Statements, expressions, types and auxiliary syntax nodes all
// share one arena; KindNames must stay in sync (TestKindNamesComplete).
const (
	KUnknown Kind = iota

	// ---- program + statements ----
	KProgram
	KUse
	KExport
	KMultiAssign
	KUnwrapAssign
	KLet
	KReturn
	KExprStmt
	KBlock
	KFuncDef
	KExtern
	KAnnotation
	KFor
	KBreak
	KContinue
	KEnumDef
	KTypeAlias
	KTaggedEnumDef
	KInterfaceDef
	KStructDef

	// ---- expressions ----
	KIdent
	KIntLit
	KByteLit
	KFloatLit
	KStrLit
	KCharLit
	KRegexLit
	KBoolLit
	KNilLit
	KPrefix
	KInfix
	KRun
	KAwait
	KIf
	KRange
	KSlice
	KIndex
	KAssign
	KCond
	KCast
	KIter
	KArrayLit
	KMapLit
	KSliceLit
	KStructLit
	KCall
	KDot
	KFuncLit
	KGrouped

	// ---- types ----
	TNamed
	TArray
	TSlice
	TMap
	TNullable
	TPointer
	TFunc
	TUnion

	// ---- auxiliary ----
	KParam
	KResult
	KStructField
	KEnumValue
	KVariant
	KInterfaceMethod
	KMapPair
	KName
	KAnnEntry
	KGeneric
	KUnionName
	// KSlot labels an optional or repeatable named child so that consumers
	// never have to guess which positional slot an id landed in. S holds the
	// label ("cond", "then", "arg", ...); First holds the wrapped child.
	KSlot

	kindCount
)

// KindNames maps every Kind to a stable, human-readable name used by Dump and
// by test failure messages. Index order must match the const block above.
var KindNames = [kindCount]string{
	KUnknown: "unknown",

	KProgram: "program", KUse: "use", KExport: "export",
	KMultiAssign: "multi-assign", KUnwrapAssign: "unwrap-assign",
	KLet: "let", KReturn: "return", KExprStmt: "expr-stmt",
	KBlock: "block", KFuncDef: "fn", KExtern: "extern",
	KAnnotation: "annotation", KFor: "for", KBreak: "break",
	KContinue: "continue", KEnumDef: "enum", KTypeAlias: "alias",
	KTaggedEnumDef: "tagged-enum", KInterfaceDef: "interface",
	KStructDef: "struct",

	KIdent: "ident", KIntLit: "int", KByteLit: "byte",
	KFloatLit: "float", KStrLit: "str", KCharLit: "char",
	KRegexLit: "regex", KBoolLit: "bool", KNilLit: "nil",
	KPrefix: "prefix", KInfix: "infix", KRun: "run", KAwait: "awy",
	KIf: "if", KRange: "range", KSlice: "slice", KIndex: "index",
	KAssign: "assign", KCond: "cond", KCast: "cast", KIter: "iter",
	KArrayLit: "array-lit", KMapLit: "map-lit", KSliceLit: "slice-lit",
	KStructLit: "struct-lit", KCall: "call", KDot: "dot",
	KFuncLit: "fn-lit", KGrouped: "grouped",

	TNamed: "t-name", TArray: "t-array", TSlice: "t-slice",
	TMap: "t-map", TNullable: "t-nullable", TPointer: "t-pointer",
	TFunc: "t-fn", TUnion: "t-union",

	KParam: "param", KResult: "result", KStructField: "field",
	KEnumValue: "enum-value", KVariant: "variant",
	KInterfaceMethod: "iface-method", KMapPair: "map-pair",
	KName: "name", KAnnEntry: "ann-entry",
	KGeneric: "generic", KUnionName: "union-name", KSlot: "slot",
}

func (k Kind) String() string {
	if k < 0 || k >= kindCount {
		return fmt.Sprintf("Kind(%d)", int32(k))
	}
	if n := KindNames[k]; n != "" {
		return n
	}
	return fmt.Sprintf("Kind(%d)", int32(k))
}

// Flag bits. These carry the boolean fields of the surface AST that have
// semantic meaning downstream (codegen, validators, formatter), so that HIR
// consumers never need to reach back into the AST.
const (
	FlagMethod uint32 = 1 << iota // fn is a method definition
	FlagVariadic
	FlagSynthetic // compiler-injected binding, not from source
	FlagModuleConst
	FlagInline
	FlagColon // `foo: (a i64) { }` colon syntax
	FlagSkipNaming
	FlagCondWrapper
	FlagWasSlice
	FlagExplicit // enum value written as `= <int>`
	FlagUnion    // alias binds a union
	FlagInferred
	FlagReadOnly
	FlagSealed
	FlagImplicitGeneric
	FlagGenericReceiver
	FlagLeftInc
	FlagRightInc
	FlagAsKeyword // `use path.fn as alias`
	FlagSlice     // StructField.IsSlice (legacy slice modifier)
	FlagFuncType  // declared type renders as a function type
)

// NoID marks an absent child, sibling, string or type reference.
//
// It is deliberately 0, not -1. Index 0 of Package.Nodes is a reserved nil
// node and index 0 of Package.Strings is the empty string, so the zero value
// of a Node means "leaf with no next sibling" and the zero value of S / S2 /
// Type means "no payload".
//
// The alternative (-1) was tried and rejected: it makes the zero value of
// Node claim node 0 as both its first child and its next sibling, so any node
// built without explicitly writing First and Next produces a cyclic tree that
// hangs the first traversal. Reserving slot 0 makes the safe state the default
// one, which is the whole point of a sentinel.
const NoID int32 = 0

// Node is a single HIR node. Id equals its index in Package.Nodes at build
// time; rewrite passes allocate new nodes and record an old -> new mapping
// instead of mutating nodes in place (see Package.Remap).
type Node struct {
	Kind  Kind
	Id    int32
	First int32 // first child, NoID when leaf
	Next  int32 // next sibling, NoID when last
	S     int32 // primary interned string: name, literal text, operator
	S2    int32 // secondary interned string: property, alias, receiver, lang
	Type  int32 // interned type-string id, NoID when unknown
	Flags uint32
	Line  int32
	Col   int32
	Val   int64 // int/bool/enum ordinal, or IEEE-754 bits of a float
}

// Float returns Val reinterpreted as a float64.
func (n *Node) Float() float64 { return math.Float64frombits(uint64(n.Val)) }

// SetFloat stores f's bit pattern in Val.
func (n *Node) SetFloat(f float64) { n.Val = int64(math.Float64bits(f)) }

// Bool returns Val as a boolean.
func (n *Node) Bool() bool { return n.Val != 0 }

// Has reports whether all the given flags are set.
func (n *Node) Has(f uint32) bool { return n.Flags&f == f }

// Embed carries compile-time embedded bytes / directory files for a node.
//
// `#{embed=...}` (a single file) and `#{embed=dir}` (a directory map) are not
// statements in the surface AST: the parser files them in a semantic side table
// keyed by AST pointer identity. HIR has no pointers, so lowering re-keys them
// onto node ids here. build/llvm reads these to inline embedded assets into the
// binary, so a HIR that loses them would silently compile an embed-free program.
type Embed struct {
	Owner int32             // annotated node id
	Data  []byte            // single-file embed bytes (nil when only a dir map)
	Files map[string][]byte // directory embed: relative path -> content
}

// Ann attaches an annotation group to a node.
//
// #{...} annotations do not exist as statements in the surface AST: the parser
// records them in a semantic side table keyed by AST pointer identity. HIR has
// no pointers, so lowering re-keys them onto node ids here. Owner is the
// annotated node; Node is a KAnnotation whose children are KAnnEntry, and an
// entry with no child is a bare boolean key such as #{linux-amd64}.
//
// Carrying these is not cosmetic. build/llvm.FilterByPlatform drops every
// statement whose platform annotation does not match the target, so an HIR
// that loses annotations would compile another platform's code with no
// diagnostic at all.
type Ann struct {
	Owner int32 // annotated node id
	Node  int32 // KAnnotation node id
}

// Package is a fully built, immutable HIR unit: plain data slices plus an
// optional symbol table. It owns no maps, so copying and caching it is cheap.
type Package struct {
	Name    string
	Nodes   []Node
	Strings []string
	Types   []string
	Top     []int32 // top-level node ids, in source order
	Anns    []Ann   // annotation groups, sorted by Owner
	Embeds  []Embed // embed data groups, sorted by Owner
	// Inferred holds the checker's inferred type strings, keyed by node id.
	// HIR nodes only carry *declared* types in Node.Type; the rich inferred
	// types (which the checker computes by mutating the AST) live here so
	// codegen can read them without reaching back into the AST. Filled by
	// parser.PopulateInferredTypes after type checking. Nil until populated.
	Inferred map[int32]string
	Syms     *SymbolTable
}

// Node returns a pointer into the arena, or nil when id is absent (NoID) or
// out of range. The pointer is only valid for the lifetime of the Package and
// must not be retained across a rewrite.
func (p *Package) Node(id int32) *Node {
	if id <= NoID || int(id) >= len(p.Nodes) {
		return nil
	}
	return &p.Nodes[id]
}

// Str resolves an interned string id. NoID and out-of-range ids return "".
func (p *Package) Str(id int32) string {
	if id <= NoID || int(id) >= len(p.Strings) {
		return ""
	}
	return p.Strings[id]
}

// Type resolves an interned type-string id.
func (p *Package) Type(id int32) string { return p.Str(id) }

// Children returns the direct children of id as a freshly allocated slice.
// Hot paths should walk First/Next directly to avoid the allocation.
func (p *Package) Children(id int32) []int32 {
	n := p.Node(id)
	if n == nil {
		return nil
	}
	var out []int32
	for c := n.First; c != NoID; c = p.Nodes[c].Next {
		out = append(out, c)
	}
	return out
}

// AnnotationsOf returns the KAnnotation node id attached to id, or NoID when
// the node carries no annotations. Anns is kept sorted by Owner so this is a
// binary search rather than a map lookup, which keeps Package map-free.
func (p *Package) AnnotationsOf(id int32) int32 {
	if id <= NoID {
		return NoID
	}
	i := sort.Search(len(p.Anns), func(i int) bool { return p.Anns[i].Owner >= id })
	if i < len(p.Anns) && p.Anns[i].Owner == id {
		return p.Anns[i].Node
	}
	return NoID
}

// EmbedDataOf returns the embedded bytes attached to id, or nil when the node
// carries no single-file embed. Embeds is kept sorted by Owner so this is a
// binary search, which keeps Package map-free.
func (p *Package) EmbedDataOf(id int32) []byte {
	if id <= NoID {
		return nil
	}
	i := sort.Search(len(p.Embeds), func(i int) bool { return p.Embeds[i].Owner >= id })
	if i < len(p.Embeds) && p.Embeds[i].Owner == id {
		return p.Embeds[i].Data
	}
	return nil
}

// EmbedFilesOf returns the directory embed map attached to id, or nil when the
// node carries no directory embed.
func (p *Package) EmbedFilesOf(id int32) map[string][]byte {
	if id <= NoID {
		return nil
	}
	i := sort.Search(len(p.Embeds), func(i int) bool { return p.Embeds[i].Owner >= id })
	if i < len(p.Embeds) && p.Embeds[i].Owner == id {
		return p.Embeds[i].Files
	}
	return nil
}

// InferredType returns the inferred type string recorded for id, or "" when no
// type has been inferred for that node. Codegen reads this instead of the AST's
// mutated Type field.
func (p *Package) InferredType(id int32) string {
	if p == nil || id <= NoID || id >= int32(len(p.Nodes)) {
		return ""
	}
	return p.Inferred[id]
}

// SetInferred records the inferred type string for id. Safe to call multiple
// times; later calls overwrite. Used by parser.PopulateInferredTypes.
func (p *Package) SetInferred(id int32, typ string) {
	if id <= NoID || id >= int32(len(p.Nodes)) || typ == "" {
		return
	}
	if p.Inferred == nil {
		p.Inferred = make(map[int32]string, 64)
	}
	p.Inferred[id] = typ
}

// AnnotationKeys returns the annotation keys attached to id, in source order.
// A key whose entry has no child value is a bare flag (#{linux-amd64}); the
// second return value reports, per key, whether it had a value.
func (p *Package) AnnotationKeys(id int32) (keys []string, hasValue []bool) {
	group := p.AnnotationsOf(id)
	if group == NoID {
		return nil, nil
	}
	for _, e := range p.Children(group) {
		n := p.Node(e)
		if n == nil || n.Kind != KAnnEntry {
			continue
		}
		keys = append(keys, p.Str(n.S))
		hasValue = append(hasValue, n.First != NoID)
	}
	return keys, hasValue
}

// Walk visits id and all of its descendants in pre-order.
func (p *Package) Walk(id int32, fn func(id int32, n *Node) bool) {
	n := p.Node(id)
	if n == nil {
		return
	}
	if !fn(id, n) {
		return
	}
	for c := n.First; c != NoID; c = p.Nodes[c].Next {
		p.Walk(c, fn)
	}
}

// WalkTop visits every top-level node and its descendants in source order.
func (p *Package) WalkTop(fn func(id int32, n *Node) bool) {
	for _, id := range p.Top {
		p.Walk(id, fn)
	}
}

// KindCounts returns how many nodes of each kind exist, excluding the reserved
// nil node at index 0. Used by coverage tests to prove a lowering pass
// exercised a representative corpus, and to assert no KUnknown was emitted.
func (p *Package) KindCounts() map[Kind]int {
	m := make(map[Kind]int, kindCount)
	for i := 1; i < len(p.Nodes); i++ {
		m[p.Nodes[i].Kind]++
	}
	return m
}

// Remap applies an old -> new id mapping produced by a rewrite pass. Nodes
// created by that pass are appended to the arena and referenced through the
// map; ids absent from the map are left alone. This is how lowering replaces a
// node without mutating the original tree.
//
// Contract: Remap only rewrites references *to* a remapped id. A replacement
// node must carry the outgoing links it needs itself — in particular it should
// copy the replaced node's Next, or it will terminate the sibling chain early.
func (p *Package) Remap(m map[int32]int32) {
	fix := func(id *int32) {
		if *id == NoID {
			return
		}
		if to, ok := m[*id]; ok {
			*id = to
		}
	}
	for i := range p.Nodes {
		fix(&p.Nodes[i].First)
		fix(&p.Nodes[i].Next)
	}
	for i := range p.Top {
		fix(&p.Top[i])
	}
	// Annotations are keyed by node id, so a rewrite that replaces an
	// annotated statement must carry its annotations over to the replacement,
	// or platform filtering silently stops applying to it.
	for i := range p.Anns {
		fix(&p.Anns[i].Owner)
		fix(&p.Anns[i].Node)
	}
	sort.SliceStable(p.Anns, func(a, b int) bool { return p.Anns[a].Owner < p.Anns[b].Owner })
	// Embeds are keyed by node id, same contract as Anns.
	for i := range p.Embeds {
		fix(&p.Embeds[i].Owner)
	}
	sort.SliceStable(p.Embeds, func(a, b int) bool { return p.Embeds[a].Owner < p.Embeds[b].Owner })
}

// Dump renders the tree as indented text. Intended for golden tests and for
// debugging a lowering pass; it is not a source formatter.
func (p *Package) Dump() string {
	var sb strings.Builder
	for _, id := range p.Top {
		p.dumpNode(&sb, id, 0)
	}
	return sb.String()
}

func (p *Package) dumpNode(sb *strings.Builder, id int32, depth int) {
	n := p.Node(id)
	if n == nil {
		return
	}
	sb.WriteString(strings.Repeat("  ", depth))
	sb.WriteString(n.Kind.String())
	if s := p.Str(n.S); s != "" {
		sb.WriteString(" s=")
		sb.WriteString(s)
	}
	if s := p.Str(n.S2); s != "" {
		sb.WriteString(" s2=")
		sb.WriteString(s)
	}
	if n.Type != NoID {
		sb.WriteString(" t=")
		sb.WriteString(p.Type(n.Type))
	}
	if n.Flags != 0 {
		fmt.Fprintf(sb, " flags=%#x", n.Flags)
	}
	if n.Val != 0 {
		fmt.Fprintf(sb, " val=%d", n.Val)
	}
	if n.Line != 0 {
		fmt.Fprintf(sb, " @%d:%d", n.Line, n.Col)
	}
	sb.WriteByte('\n')
	// Annotations are not part of the child chain, so render them explicitly;
	// otherwise a lowering pass could drop them without any golden test
	// noticing.
	if a := p.AnnotationsOf(id); a != NoID {
		p.dumpNode(sb, a, depth+1)
	}
	for c := n.First; c != NoID; c = p.Nodes[c].Next {
		p.dumpNode(sb, c, depth+1)
	}
}

// ---- builder ----

// Builder accumulates nodes and interns payload strings. It is not safe for
// concurrent use; build each package on its own Builder.
type Builder struct {
	nodes   []Node
	strings []string
	strIdx  map[string]int32
	top     []int32
	anns    []Ann
	annSeen map[int32]bool // dedup for AddAnn; discarded when Package is built
	embeds  []Embed
	embedSeen map[int32]bool // dedup for AddEmbed; discarded when Package is built
	name    string
}

// NewBuilder returns an empty Builder sized for a typical module.
//
// Slot 0 of both the node arena and the string table is reserved so that NoID
// can be 0; see the comment on NoID for why that matters.
func NewBuilder(name string) *Builder {
	b := &Builder{
		nodes:   make([]Node, 1, 1024),
		strings: make([]string, 1, 128),
		strIdx:  make(map[string]int32, 256),
		name:    name,
	}
	b.strIdx[""] = NoID
	return b
}

// Intern returns the id of s in the string table, adding it when unseen.
// NoID is returned for "" so that empty payloads stay distinguishable from
// present-but-empty ones.
func (b *Builder) Intern(s string) int32 {
	if id, ok := b.strIdx[s]; ok {
		return id
	}
	id := int32(len(b.strings))
	b.strings = append(b.strings, s)
	b.strIdx[s] = id
	return id
}

// InternType interns a rendered type string into the same table as Intern.
// Types share the table so that Dump and Str work uniformly.
func (b *Builder) InternType(s string) int32 { return b.Intern(s) }

// Add appends n, assigns its Id, and returns the new id.
func (b *Builder) Add(n Node) int32 {
	id := int32(len(b.nodes))
	n.Id = id
	b.nodes = append(b.nodes, n)
	return id
}

// List ties ids together as a sibling chain and returns the head, dropping
// any NoID entries. Returns NoID for an empty list.
func (b *Builder) List(ids []int32) int32 {
	head := NoID
	var prev int32 = NoID
	for _, id := range ids {
		if id == NoID {
			continue
		}
		if prev == NoID {
			head = id
		} else {
			b.nodes[prev].Next = id
		}
		prev = id
	}
	return head
}

// AppendChild links child as the last child of parent. Used by lowering
// passes that grow a node's operand list after it was created.
func (b *Builder) AppendChild(parent, child int32) {
	if parent == NoID || child == NoID {
		return
	}
	if b.nodes[parent].First == NoID {
		b.nodes[parent].First = child
		return
	}
	last := b.nodes[parent].First
	for b.nodes[last].Next != NoID {
		last = b.nodes[last].Next
	}
	b.nodes[last].Next = child
}

// AddTop records a top-level statement node id.
func (b *Builder) AddTop(id int32) {
	if id != NoID {
		b.top = append(b.top, id)
	}
}

// AddAnn attaches an annotation group to owner. Later calls for the same owner
// are ignored: the parser stores one annotation list per node, so a second
// group would mean the caller lowered the same node twice.
func (b *Builder) AddAnn(owner, node int32) {
	if owner == NoID || node == NoID {
		return
	}
	if b.annSeen == nil {
		b.annSeen = make(map[int32]bool, 16)
	} else if b.annSeen[owner] {
		return
	}
	b.annSeen[owner] = true
	b.anns = append(b.anns, Ann{Owner: owner, Node: node})
}

// AddEmbed attaches compile-time embedded bytes / directory files to owner.
// Later calls for the same owner are ignored, mirroring AddAnn: the parser
// stores one embed group per node.
func (b *Builder) AddEmbed(owner int32, data []byte, files map[string][]byte) {
	if owner == NoID || (len(data) == 0 && len(files) == 0) {
		return
	}
	if b.embedSeen == nil {
		b.embedSeen = make(map[int32]bool, 8)
	} else if b.embedSeen[owner] {
		return
	}
	b.embedSeen[owner] = true
	b.embeds = append(b.embeds, Embed{Owner: owner, Data: data, Files: files})
}

// Package finalises the builder into an immutable Package.
func (b *Builder) Package() *Package {
	sort.SliceStable(b.anns, func(i, j int) bool { return b.anns[i].Owner < b.anns[j].Owner })
	sort.SliceStable(b.embeds, func(i, j int) bool { return b.embeds[i].Owner < b.embeds[j].Owner })
	return &Package{
		Name:    b.name,
		Nodes:   b.nodes,
		Strings: b.strings,
		Top:     b.top,
		Anns:    b.anns,
		Embeds:  b.embeds,
	}
}
