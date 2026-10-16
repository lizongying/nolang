package parser

import (
	"reflect"
)

// CachedProgram is the gob-serializable form of a *Program used by the
// incremental-build AST cache (Tier 0 baked std AST + Tier 1 on-disk
// user-module cache).
//
// encoding/gob does NOT preserve pointer identity for interface-typed map
// keys, so SemanticContext.nodeSem (keyed by Node pointers) cannot be encoded
// as a map directly — the decoded keys would be different pointers than the
// decoded AST nodes, and every prog.Sem.XxxOf(node) lookup would miss.
//
// Instead nodeSem is serialized as a separate map[uint32]*NodeSemantics keyed
// by a deterministic reflection-based node walk order (uint32 keys survive gob
// trivially), and rebuilt into prog.Sem.nodeSem after decode via the same walk
// over the decoded tree. The Program itself is encoded with nodeSem left
// unexported so gob ignores it.
type CachedProgram struct {
	Prog    *Program
	NodeSem map[uint32]*NodeSemantics
}

// SerializeNodeSem flattens prog.Sem.nodeSem into a uint32-keyed map using a
// deterministic node-OCCURRENCE walk order (see walkProgramNodes: the index is
// the position of the occurrence in that walk, not a unique-node id). Returns
// nil when there is nothing to store.
func SerializeNodeSem(prog *Program) map[uint32]*NodeSemantics {
	if prog == nil || prog.Sem == nil {
		return nil
	}
	m := make(map[uint32]*NodeSemantics)
	idx := uint32(0)
	walkProgramNodes(prog, func(n Node) {
		if ns, ok := prog.Sem.nodeSem[n]; ok {
			m[idx] = ns
		}
		idx++
	})
	if len(m) == 0 {
		return nil
	}
	return m
}

// RestoreNodeSem rebuilds prog.Sem.nodeSem from a serialized map, walking the
// (already-decoded) program in the same occurrence order used by
// SerializeNodeSem. The two walks agree because both enumerate occurrences of a
// tree whose SHAPE gob preserves — pointer aliasing may differ after a
// round-trip and must therefore not influence the index.
func RestoreNodeSem(prog *Program, m map[uint32]*NodeSemantics) {
	if prog == nil || len(m) == 0 {
		return
	}
	if prog.Sem == nil {
		prog.Sem = NewSemanticContext()
	}
	if prog.Sem.nodeSem == nil {
		prog.Sem.nodeSem = make(map[Node]*NodeSemantics)
	}
	idx := uint32(0)
	walkProgramNodes(prog, func(n Node) {
		if ns, ok := m[idx]; ok {
			prog.Sem.nodeSem[n] = ns
		}
		idx++
	})
}

// walkProgramNodes visits every Node OCCURRENCE reachable from prog.Statements
// in a deterministic pre-order, calling visit once per occurrence (not once per
// unique node). The walk is reflection-based so it stays correct as node types
// are added.
//
// 🔴 It MUST enumerate occurrences, not unique nodes. An earlier version
// deduplicated by pointer identity (`seen map[Node]bool`), on the assumption
// that "gob preserves the tree structure, so the encode-time and decode-time
// walk orders are identical". That assumption is FALSE: encoding/gob flattens
// pointers, so a node shared by two fields (the parser's lowering shares
// several — e.g. `maybeIndexOutAssign` reuses one default-value literal in both
// match arms and one temp identifier in two nodes) is decoded as TWO distinct
// objects. The decode-time walk then skips fewer duplicates than the encode-time
// walk, so every uint32 index after the first divergence points at the wrong
// node and the restored nodeSem entries land on unrelated statements.
//
// Measured (src/std/archive/bzip2.no, 2281 lines): the dedup walk yields 2166
// nodes at encode time and 2343 after decode; the occurrence walk yields 2343
// on both sides with zero divergence. The symptom of the old behaviour was a
// `no vet` false positive — `nolang-overflow-ineffective` on a perfectly valid
// `#{overflow=wrap}` — that appeared only on a cache HIT and vanished with
// NOLANG_AST_CACHE_OFF=1.
//
// Two rules keep the two walks in lockstep:
//   - enumerate occurrences (no global dedup); the occurrence sequence is a
//     function of the tree SHAPE alone, which gob does preserve;
//   - skip unexported fields, because gob does not encode them, so descending
//     into them would add occurrences at encode time that do not exist after
//     decode.
//
// `onPath` is a cycle guard, not a dedup set: it holds only the nodes on the
// current root-to-leaf path, so a shared node is still visited at each of its
// occurrences while a self-referential node cannot recurse forever.
func walkProgramNodes(prog *Program, visit func(Node)) {
	if prog == nil {
		return
	}
	onPath := make(map[Node]bool)
	for _, s := range prog.Statements {
		walkValue(reflect.ValueOf(s), visit, onPath)
	}
}

// CountProgramNodes 回傳 SerializeNodeSem 走查會觸及的節點 occurrence 總數
// （打包統計用；口徑與 nodeSem 副表的索引空間一致）。
func CountProgramNodes(prog *Program) int {
	n := 0
	walkProgramNodes(prog, func(Node) { n++ })
	return n
}

func walkValue(v reflect.Value, visit func(Node), onPath map[Node]bool) {
	switch v.Kind() {
	case reflect.Invalid:
		return
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		walkValue(v.Elem(), visit, onPath)
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		if v.CanInterface() {
			if n, ok := v.Interface().(Node); ok {
				if onPath[n] {
					return // already being visited: cycle, stop here
				}
				onPath[n] = true
				visit(n)
				walkValue(v.Elem(), visit, onPath)
				delete(onPath, n)
				return
			}
		}
		walkValue(v.Elem(), visit, onPath)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), visit, onPath)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			// Unexported fields are NOT gob-encoded, so the decoded tree has
			// them zeroed. Walking them would make the encode-time occurrence
			// sequence longer than the decode-time one.
			if !f.CanInterface() {
				continue
			}
			walkValue(f, visit, onPath)
		}
	default:
		// basic kinds (int, string, bool, token, position) and maps: no Node
		// descendants to recurse into.
	}
}
