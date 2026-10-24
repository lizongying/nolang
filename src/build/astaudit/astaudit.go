// Package astaudit implements the AST immutability gate for the Nolang
// compiler's strict-range invariant (docs/design/nolang-parser-lowering-
// invariants.md §1.4):
//
//	ParseProgram 返回之后，任何阶段不得写入任何 AST 节点字段。
//
// The gate works by value-snapshotting the entry AST immediately after
// ParseProgram returns and again when the assembly pipeline finishes. Both
// snapshots are taken on the *same* tree, so node identity (pointer value) is
// stable across the two walks and the diff localises every write to a concrete
// (node type, field) pair — which is exactly the shape §13.5 needs.
//
// Design notes
//
//   - Nodes are keyed by pointer identity, never by index path. Assembly
//     legitimately appends to / removes from parser.Program.Statements
//     (stripBuiltinStubs, injectEnterLeave), so index paths would drift and
//     turn a single removal into hundreds of phantom field writes.
//   - Slice fields are recorded as (length, sorted member set) rather than
//     per-index entries, for the same reason: a slice membership change shows
//     up as one diff on the slice field instead of N shifted indices.
//   - Only struct pointers declared in package parser count as nodes.
//     lexer.Position, comment payload strings, etc. are ordinary values.
//   - Unexported fields cannot be read through reflect. Each node records how
//     many it has; a change in that count is still reported as a write.
//   - Program-level side tables (Sem, FmtTrivia, Warnings, Errors,
//     TrailingComments, FreeComments, BuiltinFuncNames) are deliberately out of
//     scope: §1.4 permits them. FmtTrivia (added 2026-10-23) is the formatter's
//     own node-keyed table (RTFlags / opening-brace comments); like Sem it is a
//     side table, not part of the node tree.
//     Program contributes exactly one audited field: Statements.
package astaudit

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// maxDepth guards against a pathological cycle in a non-node struct value
// (node cycles are already cut by the visited set in walkNode).
const maxDepth = 96

// unexportedField is the pseudo field path under which a node's count of
// unexported (reflect-invisible) fields is recorded.
const unexportedField = "<unexported>"

// NodeRecord is the audited state of one AST node: its concrete type plus a
// flat map of field path -> rendered value.
type NodeRecord struct {
	Type   string
	Fields map[string]string
}

// Snapshot is a full value snapshot of an AST, keyed by node pointer identity.
type Snapshot struct {
	nodes map[uintptr]*NodeRecord
	order []uintptr
	// keep pins every captured node so the GC cannot free it and let a later
	// allocation reuse its address. Diff matches records by uintptr, so without
	// this a freed node's record would be compared against an unrelated node
	// that happened to land on the same address: phantom field writes, and — when
	// the two nodes have different types — a "<type>" write on the old record.
	// Observed in practice as a sporadic *parser.NamedType.<type> delta.
	keep []any
}

// Write is one observed field write: a node field whose value changed between
// the two captures.
type Write struct {
	Type   string // concrete node type, e.g. "*parser.LetStatement"
	Field  string // field path within the node, e.g. "SourceFile"
	Before string
	After  string
}

// SliceDelta reports a slice whose membership changed between captures. It is
// reported separately from Write because §1.4 forbids writing node *fields*;
// appending a freshly synthesised node is assembly, not a field write.
type SliceDelta struct {
	Type   string
	Field  string
	Before string
	After  string
}

// NodeDelta reports a node present in only one capture.
type NodeDelta struct {
	Type string
	ID   uintptr
}

// Diff is the full result of comparing two captures.
type Delta struct {
	Writes       []Write
	Slices       []SliceDelta
	AddedNodes   []NodeDelta
	RemovedNodes []NodeDelta
}

// Empty reports whether the two captures are identical, i.e. whether the
// strict-range invariant held across the audited span.
func (d Delta) Empty() bool {
	return len(d.Writes) == 0 && len(d.Slices) == 0 &&
		len(d.AddedNodes) == 0 && len(d.RemovedNodes) == 0
}

// Merge folds other into d (used to aggregate per-root deltas).
func (d *Delta) Merge(other Delta) {
	d.Writes = append(d.Writes, other.Writes...)
	d.Slices = append(d.Slices, other.Slices...)
	d.AddedNodes = append(d.AddedNodes, other.AddedNodes...)
	d.RemovedNodes = append(d.RemovedNodes, other.RemovedNodes...)
}

// Summary renders a compact one-line-per-delta report.
func (d Delta) Summary() string {
	var b strings.Builder
	for _, w := range d.Writes {
		fmt.Fprintf(&b, "write    %s.%s: %s -> %s\n", w.Type, w.Field, w.Before, w.After)
	}
	for _, s := range d.Slices {
		fmt.Fprintf(&b, "slice    %s.%s: %s -> %s\n", s.Type, s.Field, s.Before, s.After)
	}
	for _, a := range d.AddedNodes {
		fmt.Fprintf(&b, "added    %s@%d\n", a.Type, a.ID)
	}
	for _, r := range d.RemovedNodes {
		fmt.Fprintf(&b, "removed  %s@%d\n", r.Type, r.ID)
	}
	return b.String()
}

var parserPkgPath = reflect.TypeOf(parser.Program{}).PkgPath()

// Capture snapshots prog by value. It never mutates prog.
func Capture(prog *parser.Program) *Snapshot {
	c := &Snapshot{nodes: map[uintptr]*NodeRecord{}}
	if prog == nil {
		return c
	}
	pv := reflect.ValueOf(prog)
	rec := &NodeRecord{Type: pv.Type().String(), Fields: map[string]string{}}
	id := pv.Pointer()
	c.nodes[id] = rec
	c.order = append(c.order, id)
	c.keep = append(c.keep, prog)
	// Program is the root container, not an AST node. §1.4 permits writes to
	// its side tables (Sem / Warnings / Errors / TrailingComments /
	// FreeComments / BuiltinFuncNames), so exactly one field is audited:
	// Statements.
	c.walkValue(pv.Elem().FieldByName("Statements"), "Statements", rec, 0, "Statements")
	return c
}

// Diff compares two captures taken on the same tree.
func Diff(before, after *Snapshot) Delta {
	var d Delta
	if before == nil || after == nil {
		return d
	}
	for _, id := range before.order {
		br, ok := before.nodes[id]
		if !ok {
			continue
		}
		ar, ok := after.nodes[id]
		if !ok {
			d.RemovedNodes = append(d.RemovedNodes, NodeDelta{Type: br.Type, ID: id})
			continue
		}
		if br.Type != ar.Type {
			d.Writes = append(d.Writes, Write{Type: br.Type, Field: "<type>",
				Before: br.Type, After: ar.Type})
			continue
		}
		for _, f := range sortedKeys(br.Fields) {
			bv := br.Fields[f]
			av, present := ar.Fields[f]
			if !present {
				d.RemovedNodes = append(d.RemovedNodes, NodeDelta{Type: br.Type, ID: id})
				continue
			}
			if bv == av {
				continue
			}
			if isSliceField(bv) || isSliceField(av) {
				d.Slices = append(d.Slices, SliceDelta{Type: br.Type, Field: f,
					Before: bv, After: av})
				continue
			}
			d.Writes = append(d.Writes, Write{Type: br.Type, Field: f,
				Before: bv, After: av})
		}
		for _, f := range sortedKeys(ar.Fields) {
			if _, ok := br.Fields[f]; !ok {
				d.AddedNodes = append(d.AddedNodes, NodeDelta{Type: ar.Type, ID: id})
				break
			}
		}
	}
	for _, id := range after.order {
		if _, ok := before.nodes[id]; !ok {
			d.AddedNodes = append(d.AddedNodes, NodeDelta{Type: after.nodes[id].Type, ID: id})
		}
	}
	return d
}

// isSliceField reports whether a rendered field value looks like a slice
// membership rendering ("len=N [...]").
func isSliceField(v string) bool {
	return strings.HasPrefix(v, "len=")
}

// IsParserNode reports whether v is a pointer to a struct declared in package
// parser (possibly held in an interface), i.e. an AST node.
func IsParserNode(v reflect.Value) bool {
	_, ok := concreteNode(v)
	return ok
}

// concreteNode unwraps interfaces and reports the underlying *T pointer when
// T is a struct declared in package parser.
func concreteNode(v reflect.Value) (reflect.Value, bool) {
	for i := 0; i < 8; i++ {
		if !v.IsValid() {
			return reflect.Value{}, false
		}
		switch v.Kind() {
		case reflect.Interface:
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		case reflect.Ptr:
			if v.IsNil() {
				return reflect.Value{}, false
			}
			et := v.Type().Elem()
			if et.PkgPath() != parserPkgPath || et.Kind() != reflect.Struct {
				return reflect.Value{}, false
			}
			return v, true
		default:
			return reflect.Value{}, false
		}
	}
	return reflect.Value{}, false
}

func (c *Snapshot) walkNode(v reflect.Value) {
	cv, ok := concreteNode(v)
	if !ok {
		return
	}
	id := cv.Pointer()
	if _, seen := c.nodes[id]; seen {
		return
	}
	rec := &NodeRecord{Type: cv.Type().String(), Fields: map[string]string{}}
	c.nodes[id] = rec
	c.order = append(c.order, id)
	// Pin the node: the record is keyed by address, so the address must stay
	// unique for the lifetime of this snapshot. See Snapshot.keep.
	c.keep = append(c.keep, cv.Interface())
	c.walkFields(cv.Elem(), "", rec, 0)
}

// walkFields records every exported field of struct value v onto rec. An
// anonymous embedded struct is flattened (no extra path segment) so promoted
// fields report as e.g. "Doc" rather than "CommentedNode.Doc".
//
// Flattening is load-bearing for the census: a write site is identified by
// (node type, field), so reporting "CommentedNode.Doc" for a
// *parser.LetStatement would name a type that never appears in the inventory
// and split one logical field across every embedder.
func (c *Snapshot) walkFields(v reflect.Value, prefix string, rec *NodeRecord, depth int) {
	if depth > maxDepth || v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	unexported := 0
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" { // unexported: reflect cannot read it
			unexported++
			continue
		}
		// Path of this field's own value, and the prefix handed to nested
		// fields. For an anonymous embedded struct the two differ: the embed
		// contributes no segment of its own.
		own := sf.Name
		child := sf.Name
		if prefix != "" {
			own = prefix + "." + sf.Name
			child = own
		}
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			child = prefix
		}
		c.walkValue(v.Field(i), own, rec, depth+1, child)
	}
	if unexported > 0 {
		rec.Fields[unexportedField] = strconv.Itoa(unexported)
	}
}

func (c *Snapshot) walkValue(v reflect.Value, path string, rec *NodeRecord, depth int, childPrefix string) {
	if !v.IsValid() {
		rec.Fields[path] = "<invalid>"
		return
	}
	if depth > maxDepth {
		rec.Fields[path] = "<depth-limit>"
		return
	}
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			rec.Fields[path] = "<nil>"
			return
		}
		if IsParserNode(v) {
			c.walkNode(v)
			rec.Fields[path] = nodeRef(v)
			return
		}
		// Non-parser pointer: inline its fields so they stay diffable.
		c.walkFields(v.Elem(), childPrefix, rec, depth+1)
	case reflect.Struct:
		c.walkFields(v, childPrefix, rec, depth+1)
	case reflect.Slice, reflect.Array:
		refs := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			ev := v.Index(i)
			if IsParserNode(ev) {
				c.walkNode(ev)
				refs = append(refs, nodeRef(ev))
				continue
			}
			refs = append(refs, renderScalar(ev, path, rec, depth+1))
		}
		sort.Strings(refs)
		rec.Fields[path] = fmt.Sprintf("len=%d [%s]", v.Len(), strings.Join(refs, " "))
	case reflect.Map:
		rec.Fields[path] = renderMap(v)
	default:
		rec.Fields[path] = fmt.Sprintf("%v", v.Interface())
	}
}

// renderScalar folds a non-node value into a single string. Nested structs
// recurse so their interior stays diffable.
func renderScalar(v reflect.Value, path string, rec *NodeRecord, depth int) string {
	if !v.IsValid() {
		return "<invalid>"
	}
	if depth > maxDepth {
		return "<depth-limit>"
	}
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return "<nil>"
		}
		if IsParserNode(v) {
			return nodeRef(v)
		}
		return renderScalar(v.Elem(), path, rec, depth+1)
	case reflect.Struct:
		return fmt.Sprintf("%v", v.Interface())
	case reflect.Slice, reflect.Array:
		parts := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			parts = append(parts, renderScalar(v.Index(i), path, rec, depth+1))
		}
		return "[" + strings.Join(parts, " ") + "]"
	case reflect.Map:
		return renderMap(v)
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

func renderMap(v reflect.Value) string {
	if v.IsNil() {
		return "<nil-map>"
	}
	keys := v.MapKeys()
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%v=%v", k.Interface(), v.MapIndex(k).Interface()))
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, " ") + "}"
}

func nodeRef(v reflect.Value) string {
	cv, ok := concreteNode(v)
	if !ok {
		return "<non-node>"
	}
	return fmt.Sprintf("%s@%d", cv.Type().String(), cv.Pointer())
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Census counts distinct (node type, field) write keys observed across a run.
type Census map[string]int

// Key renders a stable "Type.Field" key.
func Key(w Write) string { return w.Type + "." + w.Field }

// Add records one write.
func (c Census) Add(w Write) { c[Key(w)]++ }

// Keys returns the census keys in sorted order.
func (c Census) Keys() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Report renders one "key<TAB>count" line per census entry.
func (c Census) Report() string {
	var b strings.Builder
	for _, k := range c.Keys() {
		b.WriteString(k)
		b.WriteByte('\t')
		b.WriteString(strconv.Itoa(c[k]))
		b.WriteByte('\n')
	}
	return b.String()
}
