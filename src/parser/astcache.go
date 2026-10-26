package parser

import (
	"reflect"
)

// CachedProgram is the gob-serializable form of a *Program used by the
// incremental-build AST cache (Tier 0 baked std AST + Tier 1 on-disk
// user-module cache).
//
// 🔴 **它只有一個欄位，這是架構約束，不是巧合。**
//
// 這個 blob 是「結構化的原始碼」：它未來要當**分發格式**用，所以裡面只能有
// 語法本身，不能夾帶任何「某個版本的編譯器對這段原始碼的結論」。衍生事實
// （推斷型別、索引證明、簽名表）一律在解碼後由 `BuildSemViews`（見
// semviews.go）重走一遍補回來。理由與三個已踩過的坑寫在 semviews.go 的
// 檔案頭。
//
// 歷史：這裡曾經有 `TypeFacts` / `NodeFacts` 兩個 `map[uint32]...` 欄位
// （`encoding/gob` 不保留 interface 型別 map 鍵的指標身分，所以節點鍵表得靠
// `walkProgramNodes` 的 occurrence 編號攤平）。2026-10-26 整條刪除：能從樹
// 算出來的東西不該有第二份真相來源。同一天稍早，`#{...}` 註解也從副表
// （`AnnTable`）搬回 AST 節點上（`AnnotatedNode`）—— 註解本來就是語法。
//
// `Program.FmtTrivia` 仍是節點鍵表且仍不序列化：fmt / LSP 一律 fresh-parse，
// 從不讀快取。
type CachedProgram struct {
	Prog *Program
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
// node and the restored side-table entries land on unrelated statements.
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

// CountProgramNodes 回傳 walkProgramNodes 走查會觸及的節點 occurrence 總數
// （打包統計用；口徑與三張 node-keyed 副表的索引空間一致）。
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
