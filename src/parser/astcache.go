package parser

import (
	"reflect"
)

// CachedProgram is the gob-serializable form of a *Program used by the
// incremental-build AST cache (Tier 0 baked std AST + Tier 1 on-disk
// user-module cache).
//
// encoding/gob does NOT preserve pointer identity for interface-typed map
// keys, so the node-keyed side-tables (SemanticContext.anns / typeFacts /
// nodeFacts, all keyed by Node pointers) cannot be encoded as maps directly —
// the decoded keys would be different pointers than the decoded AST nodes, and
// every prog.Sem.XxxOf(node) lookup would miss.
//
// Instead each is serialized as its own map[uint32]... keyed by a deterministic
// reflection-based node walk order (uint32 keys survive gob trivially), and
// rebuilt after decode via the same walk over the decoded tree. The Program
// itself is encoded with the side-tables left unexported so gob ignores them.
//
// `Program.FmtTrivia` is node-keyed too but is deliberately NOT serialized:
// fmt and LSP always fresh-parse.
//
// (A fourth node-keyed table, `SemanticContext.nodeSem`, used to live here as
// `CachedProgram.NodeSem`. Stage 2–5 of the THIR plan moved all 18 of its
// fields into FmtTrivia / AnnTable / TypeFacts / NodeFacts and Stage 7 deleted
// the empty shell.)
type CachedProgram struct {
	Prog *Program
	// Anns 是 Program 級註解表（Stage 3）的序列化形式，與另兩張表用同一套
	// occurrence 編號鍵（`walkProgramNodes` 的第 idx 次造訪），故兩邊的鍵空間
	// 一致、可在同一趟走查裡一起重建。見 SerializeAnns / RestoreAnns。
	Anns map[uint32][]*AnnotationEntry

	// TypeFacts 是 Program 級型別事實表（Stage 4）的序列化形式，與 Anns 用同一套
	// occurrence 編號鍵。見 SerializeTypeFacts / RestoreTypeFacts。
	//
	// 三欄（`InferredType` / `ByOptionArith` / `TypeOverride`）**全部**鏡像。
	// 丟失的後果是靜默的——`?T` 推斷消失會讓 KLet 的 HIR Type 變空，checker 的
	// 四處 option 豁免失效，而 Assembly 期的有效型別覆寫丟失會退回宣告型別。
	TypeFacts map[uint32]*TypeFact

	// NodeFacts 是 Program 級節點事實表（Stage 5）的序列化形式，與 Anns /
	// TypeFacts 用同一套 occurrence 編號鍵。見 SerializeNodeFacts /
	// RestoreNodeFacts。
	//
	// 🔴 9 欄**全部**鏡像，理由有兩層：
	//   1. C9（`ASTVersion` 註解）明確點名 `embed data` / `IndexInBounds` /
	//      `ModuleConst` / `SourceFile` / `ModuleOwner` 五項為 MUST bump，
	//      也就是說它們今天本來就進 blob —— 不鏡像即行為變更；
	//   2. 其餘四項（`Callee` / `VariadicUnion` / `GenericUnion` /
	//      `EmbedFiles`）按照 Stage 4 處置 `TypeOverride` 的先例同樣全鏡像：
	//      它們在遷移前也在 `detNodeSem` 裡，本輪是純重構，不順手改範圍。
	//      ⚠️ 特別是 `src/checker/stdast.go:156` 的 Tier-0 std blob 路徑
	//      **不跑** `monomorphizeUnions`，若 union 標記不進 blob，命中快取的
	//      std 模組會靜默丟掉單態化標記。
	NodeFacts map[uint32]*NodeFact
}

// SerializeAnns flattens prog.Sem.anns into a uint32-keyed map using the
// deterministic node-OCCURRENCE walk order (see walkProgramNodes: the index is
// the position of the occurrence in that walk, not a unique-node id). Returns
// nil when there is nothing to store.
//
// 註解**必須**跨進程保留：`#{platform=...}` 驅動 build 的平台過濾、
// `#{overflow=...}` 驅動溢位處理 codegen、`#{index-out=...}` 驅動索引守衛，
// 三者全在快取命中後仍然執行。丟失不會報錯，只會**靜默生成錯誤的程式**
// （實測前例：`nolang-overflow-ineffective` 偽陽性只在快取命中時出現）。
func SerializeAnns(prog *Program) map[uint32][]*AnnotationEntry {
	if prog == nil || prog.Sem == nil {
		return nil
	}
	anns := prog.Sem.anns
	if anns == nil || len(anns.entries) == 0 {
		return nil
	}
	m := make(map[uint32][]*AnnotationEntry)
	idx := uint32(0)
	walkProgramNodes(prog, func(n Node) {
		if ents, ok := anns.entries[n]; ok {
			m[idx] = ents
		}
		idx++
	})
	if len(m) == 0 {
		return nil
	}
	return m
}

// RestoreAnns rebuilds prog.Sem.anns from a serialized map, walking the
// (already-decoded) program in the same occurrence order used by SerializeAnns
// (see SerializeAnns for why the two walks agree).
func RestoreAnns(prog *Program, m map[uint32][]*AnnotationEntry) {
	if prog == nil || len(m) == 0 {
		return
	}
	if prog.Sem == nil {
		prog.Sem = NewSemanticContext()
	}
	if prog.Sem.anns == nil {
		prog.Sem.anns = NewAnnTable()
	}
	idx := uint32(0)
	walkProgramNodes(prog, func(n Node) {
		if ents, ok := m[idx]; ok {
			prog.Sem.anns.entries[n] = ents
		}
		idx++
	})
}

// SerializeTypeFacts flattens prog.Sem.typeFacts into a uint32-keyed map using
// the same deterministic node-OCCURRENCE walk order (see walkProgramNodes). Returns nil when there is nothing to store.
//
// 🔴 型別事實**必須**跨進程保留：`InferredType` 驅動 KLet 的 `?T` 型別與 checker
// 的四處 option 豁免；`TypeOverride` 驅動具現化／具象化後的有效型別。三者丟失
// 都不會報錯——只會靜默生成型別錯誤的 HIR。
func SerializeTypeFacts(prog *Program) map[uint32]*TypeFact {
	if prog == nil || prog.Sem == nil {
		return nil
	}
	tf := prog.Sem.typeFacts
	if tf == nil || len(tf.entries) == 0 {
		return nil
	}
	m := make(map[uint32]*TypeFact)
	idx := uint32(0)
	walkProgramNodes(prog, func(n Node) {
		if f, ok := tf.entries[n]; ok {
			m[idx] = f
		}
		idx++
	})
	if len(m) == 0 {
		return nil
	}
	return m
}

// RestoreTypeFacts rebuilds prog.Sem.typeFacts from a serialized map, walking
// the (already-decoded) program in the same occurrence order used by
// SerializeTypeFacts (see SerializeAnns for why the two walks agree).
func RestoreTypeFacts(prog *Program, m map[uint32]*TypeFact) {
	if prog == nil || len(m) == 0 {
		return
	}
	if prog.Sem == nil {
		prog.Sem = NewSemanticContext()
	}
	if prog.Sem.typeFacts == nil {
		prog.Sem.typeFacts = NewTypeFacts()
	}
	idx := uint32(0)
	walkProgramNodes(prog, func(n Node) {
		if f, ok := m[idx]; ok {
			prog.Sem.typeFacts.entries[n] = f
		}
		idx++
	})
}

// SerializeNodeFacts flattens prog.Sem.nodeFacts into a uint32-keyed map using
// the same deterministic node-OCCURRENCE walk order (see walkProgramNodes). Returns nil when there is nothing to store.
//
// 🔴 節點事實**必須**跨進程保留，理由逐類不同：
//   - `SourceFile` 丟失 → 合併進來的模組語句診斷歸因靜默回退成主檔路徑
//     （報出 `main.no:1098` 這種主檔根本沒有的行號）；
//   - `ModuleConst` / `IndexInBounds` 丟失 → 常量傳播與索引守衛豁免靜默失效；
//   - `Callee` 丟失 → checker 的「未賦值返回參數」判定把普通實參誤認成接收者；
//   - `VariadicUnion` / `GenericUnion` 丟失 → union 單態化標記消失（Tier-0 std
//     blob 路徑不重跑 monomorphizeUnions，無法自行補回）；
//   - `EmbedData` / `EmbedFiles` 丟失 → `#{embed=...}` 內容變成另一個版本的檔案。
//
// 沒有一項會報錯 —— 全是靜默生成錯誤的程式。
func SerializeNodeFacts(prog *Program) map[uint32]*NodeFact {
	if prog == nil || prog.Sem == nil {
		return nil
	}
	nf := prog.Sem.nodeFacts
	if nf == nil || len(nf.entries) == 0 {
		return nil
	}
	m := make(map[uint32]*NodeFact)
	idx := uint32(0)
	walkProgramNodes(prog, func(n Node) {
		if f, ok := nf.entries[n]; ok {
			m[idx] = f
		}
		idx++
	})
	if len(m) == 0 {
		return nil
	}
	return m
}

// RestoreNodeFacts rebuilds prog.Sem.nodeFacts from a serialized map, walking
// the (already-decoded) program in the same occurrence order used by
// SerializeNodeFacts (see SerializeAnns for why the two walks agree).
func RestoreNodeFacts(prog *Program, m map[uint32]*NodeFact) {
	if prog == nil || len(m) == 0 {
		return
	}
	if prog.Sem == nil {
		prog.Sem = NewSemanticContext()
	}
	if prog.Sem.nodeFacts == nil {
		prog.Sem.nodeFacts = NewNodeFacts()
	}
	idx := uint32(0)
	walkProgramNodes(prog, func(n Node) {
		if f, ok := m[idx]; ok {
			prog.Sem.nodeFacts.entries[n] = f
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
