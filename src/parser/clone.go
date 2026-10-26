// S5 stage 6（2026-10-21）：裝配工作副本——ParseProgram 輸出不可變。
//
// §1.4 嚴格範圍要求「ParseProgram 返回之後任何階段不得寫入 AST 節點欄位」。
// 裝配期的改名 / 模組前綴 / 呼叫解析 / 常量傳播等 pass（§13.5.2 的15 個寫點）
// 全是「連結期結構改寫」，其讀點面（呼叫/型別/名字消費者）數以百計，無法逐族
// 邊 side-table。Stage 6 改為**在裝配入口取工作副本**：CloneProgram 深拷貝
// 整棵樹並把 Sem 的 node-keyed 條目重映射到副本節點，之後所有裝配寫入都落在
// 副本上——ParseProgram 的輸出（含各模組 / embed / AST 依賴的解析產物）自
// 此永久不可變，AST 寫入門禁（TestASTWriteInventoryPinned）釘住零寫入。
//
// 拷貝語義與 astcache 的 occurrence walk / gob 口徑一致（見 walkValue）：
//   - 只深拷貝可匯出欄位；未匯出欄位以淺拷貝原樣帶過（gob 同樣不編碼它們，
//     快取命中路徑早就是這個語義）；
//   - 指標以 (型別, 位址) memo 化——共享子樹拷成同一個副本，指標鍵同一性
//     （pointer-keyed dedup / mainStmtSet 一類）在副本側完全保留；
//   - 實作 Node 的指標同時記入 nodes 對映，供 SemanticContext 的四張
//     node-keyed side-table（anns / typeFacts / nodeFacts / captureFacts）換鍵。
package parser

import "reflect"

// CloneProgram 回傳 prog 的深拷貝工作副本：Statements / FreeComments /
// TrailingComments / BuiltinFuncNames 全部深拷貝，Sem 經 nodes 對映把
// node-keyed 條目換鍵到副本節點後複製（名稱級映射原樣帶過）。
// prog 本身及其可達節點不被觸碰。
func CloneProgram(prog *Program) *Program {
	if prog == nil {
		return nil
	}
	c := &cloner{
		memo:  make(map[memoKey]reflect.Value),
		nodes: make(map[Node]Node),
	}
	out := &Program{}
	if prog.Statements != nil {
		out.Statements = make([]Statement, len(prog.Statements))
		for i, s := range prog.Statements {
			out.Statements[i] = c.cloneValue(reflect.ValueOf(s)).Interface().(Statement)
		}
	}
	if prog.FreeComments != nil {
		out.FreeComments = make([]*CommentGroup, len(prog.FreeComments))
		for i, cg := range prog.FreeComments {
			if cg == nil {
				continue
			}
			out.FreeComments[i] = c.cloneValue(reflect.ValueOf(cg)).Interface().(*CommentGroup)
		}
	}
	if prog.TrailingComments != nil {
		out.TrailingComments = c.cloneValue(reflect.ValueOf(prog.TrailingComments)).Interface().(*CommentGroup)
	}
	if prog.Warnings != nil {
		out.Warnings = append([]string(nil), prog.Warnings...)
	}
	if prog.Errors != nil {
		out.Errors = append([]string(nil), prog.Errors...)
	}
	if prog.BuiltinFuncNames != nil {
		out.BuiltinFuncNames = make(map[string]bool, len(prog.BuiltinFuncNames))
		for k, v := range prog.BuiltinFuncNames {
			out.BuiltinFuncNames[k] = v
		}
	}
	out.Sem = c.remapSem(prog.Sem)
	// 排版 trivia 表：與三張語義表同款換鍵（鍵指向副本節點）。該表是 formatter
	// 專用、且刻意不進 blob，但 clone 若漏拷就等於「深拷貝默默丟資料」，且
	// 裝配期工作副本正是靠 CloneProgram 取得的——故必須一併複製。
	out.FmtTrivia = prog.FmtTrivia.Clone(func(n Node) Node {
		if nn, ok := c.nodes[n]; ok {
			return nn
		}
		return n
	})
	return out
}

type memoKey struct {
	typ reflect.Type
	ptr uintptr
}

type cloner struct {
	memo  map[memoKey]reflect.Value
	nodes map[Node]Node
}

// cloneValue 遞迴深拷貝 v。指標共享（含環）經 memo 保持；實作 Node 的指標
// 同步記入 c.nodes（Sem 換鍵用）。
func (c *cloner) cloneValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Invalid:
		return v
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(c.cloneValue(v.Elem()))
		return out
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		k := memoKey{v.Type(), v.Pointer()}
		if nv, ok := c.memo[k]; ok {
			return nv
		}
		nv := reflect.New(v.Type().Elem())
		c.memo[k] = nv
		if v.CanInterface() {
			if n, ok := v.Interface().(Node); ok {
				if nn, ok2 := nv.Interface().(Node); ok2 {
					c.nodes[n] = nn
				}
			}
		}
		nv.Elem().Set(c.cloneValue(v.Elem()))
		return nv
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		n := v.Len()
		out := reflect.MakeSlice(v.Type(), n, n)
		for i := 0; i < n; i++ {
			out.Index(i).Set(c.cloneValue(v.Index(i)))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(c.cloneValue(v.Index(i)))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		// 先整體淺拷貝（未匯出欄位原樣帶過——gob 口徑下它們本就不進快取），
		// 再把可匯出欄位換成深拷貝結果。
		out.Set(v)
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if !f.CanInterface() {
				continue
			}
			out.Field(i).Set(c.cloneValue(f))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(c.cloneValue(iter.Key()), c.cloneValue(iter.Value()))
		}
		return out
	default:
		// 基本型別（int / string / bool / ...）：值拷貝。
		return v
	}
}

// remapSem 複製 src 語義副表：node-keyed 條目換鍵到副本節點（未在對映中的
// 鍵原樣保留——它們指向未拷貝的孤立節點，不會被副本管線寫入）；名稱級映射
// 經 Merge 原樣匯入（語義與模組合併時的 Merge 完全一致）。
func (c *cloner) remapSem(src *SemanticContext) *SemanticContext {
	out := NewSemanticContext()
	if src == nil {
		return out
	}
	out.Merge(src) // 名稱級映射 + anns + typeFacts + nodeFacts + captureFacts（暫為舊鍵）
	// 註解不在此列：它們住在 AST 節點上（`AnnotatedNode.Annotations`），
	// 已由上面的 AST 深拷貝隨節點一起複製，無需換鍵。
	// 型別事實表／節點事實表／捕獲事實表換鍵到副本節點（三者同口徑）。
	// 三表已由 Merge 帶過來，這裡只需重映射鍵。
	if out.typeFacts != nil && len(out.typeFacts.entries) > 0 {
		out.typeFacts = out.typeFacts.Clone(func(n Node) Node {
			if nn, ok := c.nodes[n]; ok {
				return nn
			}
			return n
		})
	}
	if out.nodeFacts != nil && len(out.nodeFacts.entries) > 0 {
		out.nodeFacts = out.nodeFacts.Clone(func(n Node) Node {
			if nn, ok := c.nodes[n]; ok {
				return nn
			}
			return n
		})
	}
	if out.captureFacts != nil && len(out.captureFacts.entries) > 0 {
		out.captureFacts = out.captureFacts.Clone(func(n Node) Node {
			if nn, ok := c.nodes[n]; ok {
				return nn
			}
			return n
		})
	}
	return out
}
