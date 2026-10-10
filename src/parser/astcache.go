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
// deterministic node walk order. Returns nil when there is nothing to store.
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
// (already-decoded) program in the same order used by SerializeNodeSem.
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

// walkProgramNodes visits every Node reachable from prog.Statements in a
// deterministic pre-order, calling visit exactly once per unique node. The walk
// is reflection-based so it stays correct as node types are added; gob preserves
// the tree structure, so the encode-time and decode-time walk orders are
// identical and the uint32 indices line up.
func walkProgramNodes(prog *Program, visit func(Node)) {
	if prog == nil {
		return
	}
	seen := make(map[Node]bool)
	for _, s := range prog.Statements {
		walkValue(reflect.ValueOf(s), visit, seen)
	}
}

func walkValue(v reflect.Value, visit func(Node), seen map[Node]bool) {
	switch v.Kind() {
	case reflect.Invalid:
		return
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		walkValue(v.Elem(), visit, seen)
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		if v.CanInterface() {
			if n, ok := v.Interface().(Node); ok {
				if !seen[n] {
					seen[n] = true
					visit(n)
				}
			}
		}
		walkValue(v.Elem(), visit, seen)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), visit, seen)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			walkValue(v.Field(i), visit, seen)
		}
	default:
		// basic kinds (int, string, bool, token, position) and maps: no Node
		// descendants to recurse into.
	}
}
