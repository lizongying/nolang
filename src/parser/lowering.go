// lowering.go — lowering pass：表層 AST（SurfaceMatch）→ 核心 AST（IfExpression 鏈）。
//
// parser 在解析 match 語法（裸 match `{ cond -> body }`、matched match `x: { ... }`、
// deprecated switch/~match）時只產出表層節點 SurfaceMatch（記錄 arms 與解析期語義
// 快照），不立即 desugar。ParseProgram 結束時呼叫 lowerProgram，由 lowering pass
// 自底向上將所有 SurfaceMatch 展開為 if/elif/else 鏈（核心 AST）。
// 下游（transpiler/LSP/formatter）看到的 AST 與舊「解析期立即 desugar」實作一致。
package parser

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// ---- 表層 AST 節點 ----

// SurfaceMatch — match 語法的表層 AST 節點。
// parser 只負責收集 arms 與解析期語義快照；desugar 由 lowering pass 延後執行。
type SurfaceMatch struct {
	Token   lexer.Token // LBRACE（裸 match）或 matched match 的起始 token
	Matched Expression  // 被匹配的表達式；nil 表示裸 match（條件直接使用）
	Arms    []matchArm

	// OpeningBraceComment 保留裸 match `{` 同行註釋，lowering 時轉移到 IfExpression。
	OpeningBraceComment *CommentGroup
	// RBracePos 保留裸 match 外層 `}` 的位置，lowering 時轉移到 IfExpression.MatchEndPos。
	RBracePos lexer.Position
}

func (sm *SurfaceMatch) expressionNode()     {}
func (sm *SurfaceMatch) Pos() lexer.Position { return posFromToken(sm.Token) }
func (sm *SurfaceMatch) EndPos() lexer.Position {
	// RBracePos 記錄了外層 `}` 的位置，優先使用。
	if sm.RBracePos.Line > 0 {
		return sm.RBracePos
	}
	if n := len(sm.Arms); n > 0 && sm.Arms[n-1].body != nil {
		return sm.Arms[n-1].body.EndPos()
	}
	return posFromToken(sm.Token)
}

// newSurfaceMatch 建立表層 match 節點（不捕獲型別快照；類型推斷由 Resolver pass
// 寫入語義副表，lowering 時自 p.sem 讀取）。
func (p *Parser) newSurfaceMatch(tok lexer.Token, matched Expression, arms []matchArm) *SurfaceMatch {
	return &SurfaceMatch{Token: tok, Matched: matched, Arms: arms}
}

// ---- lowering pass 驅動 ----

// lowerProgram 對整棵 AST 執行 lowering pass：以反射遍歷所有節點，
// 將 *SurfaceMatch 就地替換為 desugar 後的 IfExpression 鏈。
// 由 ParseProgram 在解析結束、拷貝 Warnings 之前呼叫。
func (p *Parser) lowerProgram(prog *Program) {
	if prog == nil {
		return
	}
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		fmt.Fprintf(os.Stderr, "[debug-it] lowerProgram called, %d top-level statements\n", len(prog.Statements))
	}
	l := &lowerer{p: p, visited: map[uintptr]bool{}, declaredLocals: map[string]bool{}}
	l.walk(reflect.ValueOf(prog))
}

type lowerer struct {
	p           *Parser
	visited     map[uintptr]bool    // 指標去重：AST 有共享節點（如 MatchedExpr），避免重複遍歷
	curFuncName string              // current function being lowered, for function-scoped VarType lookup
	curFuncDef  *FunctionDefinition // current function definition, for result param lookup
	optTmpSeq   int                 // 單調計數器：為 compound `?=` 的 __opt_N 暫存變數產生全函式唯一名
	capTmpSeq   int                 // 單調計數器：為捕獲賦值的 __cap_N 暫存變數產生全函式唯一名
	// enclosingOvf 表示當前子樹位於一條帶 `#{overflow=...}` 行註解的陳述之內。
	// 行註解只作用「下一條陳述」及其整個子樹（含 if/match 臂體），捕獲 pass
	// 必須據此跳過 —— 否則 `#{overflow=wrap}` 底下 `{ ... -> { w = a[i] | ... } }`
	// 這種臂體內的索引/除法仍會被改寫成 option，靜默改變語意（AES key-expand）。
	enclosingOvf bool
	// declaredLocals 記錄「本函式內已經出現過的 LetStatement 目標名」，按 walk
	// 順序累積。用途有二：
	//  1. 判斷捕獲目標是否為首次宣告 —— 只有首次宣告才需要補 `a ?T = nil`
	//     預宣告；對已存在的變數再發一次預宣告會摧毀它的既有值（`a = a + b / c`
	//     的 RHS 在 ok 臂才求值，預宣告先把 a 清成 nil，RHS 就讀到 nil）。
	//  2. 語義表（VarTypes）不記錄由字面量推斷出的型別（`mi = 0.0`），單靠它
	//     判斷「是否已宣告」會漏判，故這裡自行記錄。
	declaredLocals map[string]bool
}

var surfaceMatchPtrType = reflect.TypeOf((*SurfaceMatch)(nil))
var unwrapAssignPtrType = reflect.TypeOf((*UnwrapAssignStatement)(nil))
var statementIfaceType = reflect.TypeOf((*Statement)(nil)).Elem()

// walk 遞迴遍歷任意 AST 值。替換點是「介面欄位/介面切片元素中裝的 *SurfaceMatch」——
// AST 中 Expression/Statement 均以介面持有，且所有節點實作皆為指標，
// 因此經由指標 Elem() 抵達的介面欄位一定可設置（CanSet）。
func (l *lowerer) walk(v reflect.Value) {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		ptr := v.Pointer()
		if l.visited[ptr] {
			return
		}
		l.visited[ptr] = true
		l.walk(v.Elem())
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		if v.Elem().Type() == surfaceMatchPtrType && v.CanSet() {
			sm := v.Interface().(*SurfaceMatch)
			if lowered := l.lowerSurfaceMatch(sm); lowered != nil {
				v.Set(reflect.ValueOf(lowered))
			} else {
				v.Set(reflect.Zero(v.Type()))
			}
			return
		}
		if v.Elem().Type() == unwrapAssignPtrType && v.CanSet() {
			// no fmt 等純格式化場景需要 surface AST：跳過 UnwrapAssignStatement
			// 的展開，讓 formatter 直接渲染 `?=` / `=`（保持可重解析、冪等）。
			if l.p.SkipUnwrapLowering {
				return
			}
			uas := v.Interface().(*UnwrapAssignStatement)
			lowered := l.lowerUnwrapAssign(uas)
			if lowered != nil {
				// UnwrapAssignStatement 降級為 BlockStatement（Statement）。
				// 若此節點位於 Statement 介面欄位（如 BlockStatement.Statements
				// 元素），可直接替換；若位於 Expression 介面欄位（如 ?= 被當作
				// 值使用，屬非法用法），BlockStatement 無法賦回 Expression 介面，
				// 此時發出解析錯誤並清零，避免 reflect.Set panic。
				if v.Type() == statementIfaceType {
					v.Set(reflect.ValueOf(lowered))
				} else {
					l.p.saveError(fmt.Sprintf("line %d, column %d: `?=` is a statement operator and cannot be used as an expression",
						uas.Token.Line, uas.Token.Column))
					v.Set(reflect.Zero(v.Type()))
				}
			} else {
				v.Set(reflect.Zero(v.Type()))
			}
			return
		}
		l.walk(v.Elem())
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			// 安全索引兩種 desugar（先 index-out 預設值，再 option 自動傳播）：
			//   1. `#{index-out=DEF} x = v[5]` → 改寫為 match，越界用 DEF 替代。
			//   2. option 回傳函式內的裸 `x = v[5]`（arr/vec）→ 視為 `x ?= v[5]`，
			//      越界時向上傳播錯誤。
			replaced := false
			// 行註解 `#{overflow=...}` 作用於本陳述及其整棵子樹（含 match/if
			// 臂體）。把「已顯式處理溢出」的狀態向下傳遞，使臂體內的捕獲 pass
			// 一併跳過；走完本陳述後還原。
			savedOvf := l.enclosingOvf
			if v.Index(i).CanSet() {
				stmt := v.Index(i).Interface()
				// carryFrom 保存「未展開」的外層陳述：行註解（`#{overflow=...}`）
				// 掛在最外層陳述上（side-table 條目或 OverflowMode 欄位），
				// desugar 產生取代節點後必須由它把模式帶過去，否則註解失效。
				var carryFrom Node
				if n, isNode := stmt.(Node); isNode {
					carryFrom = n
				}
				// 行註解 `#{overflow=...}` 作用於本陳述及其整棵子樹（含 match/if
				// 臂體）：在下方 walk 進入子樹前把狀態設為 true。
				if !isNil(carryFrom) && l.stmtHasOverflowAnnotation(carryFrom) {
					l.enclosingOvf = true
				}
				// 展开 ExpressionStatement → AssignExpression：索引寫入
				//（`k[i] = key[i]`、`obj.field = arr[i]`）在 AST 上為
				// ExpressionStatement 包裹 AssignExpression，必須取出內層
				// AssignExpression 才能被 maybeIndexOutAssign 辨識（其 switch
				// 直接匹配 *AssignExpression）。同時把外層 ExpressionStatement
				// 攜帶的 `#{index-out}` 註解轉掛到內層 AssignExpression，否則
				// 註解查不到、desugar 不觸發，越界索引仍被當成未處理。
				if es, ok := stmt.(*ExpressionStatement); ok {
					if ae, ok2 := es.Expression.(*AssignExpression); ok2 {
						if l.p.sem != nil {
							if anns := l.p.sem.RawAnnotationsOf(es); len(anns) > 0 {
								l.p.sem.SetRawAnnotations(ae, anns)
								// 同時填 Annotations：ResolveProgram 只對解析期直接
								// 收到 RawAnnotations 的節點（此處為外層 es）回填
								// Annotations，內層 ae 沒有；maybeIndexOutAssign
								// 讀 AnnotationsOf，必須在此一併補齊，否則註解查不到。
								l.p.sem.ensure(ae).Annotations = anns
							}
						}
						stmt = ae
					}
				}
				if !l.p.SkipSafeIndexLowering {
					if repl := l.maybeIndexOutAssign(stmt); repl != nil {
						l.carryOverflowAnnotation(carryFrom, repl)
						v.Index(i).Set(reflect.ValueOf(repl))
						replaced = true
					}
				}
				// 有號整數 `/` `%` 本身就會產生 option（除零/溢出時為 err），
				// 因此以普通 `=` 綁定其結果時，變數型別必然是 `?T`：無需用戶
				// 顯式標註，在此就地補上推斷出的註解（等同寫 `q ?int = a / b`），
				// 讓下游 checker（ovfhndld / ovf-int-default）與 MIR（typeHint →
				// 除零守衛）走已驗證的顯式路徑。
				// fmt 路徑跳過，formatter 永遠看不到合成註解。
				if !l.p.SkipUnwrapLowering {
					if ls, ok := stmt.(*LetStatement); ok {
						l.inferOptionDivMod(ls)
					}
				}
				// 捕獲賦值：`a = b + c / d` 之類「普通 `=` 綁定含可錯源的運算式」
				// 就地改寫為捕獲（a 推斷為 ?T，nil/err 存進 a，流程繼續）。
				// 必須排在 inferOptionDivMod 之後：根為 `/` `%` 的情形由它處理。
				if !l.p.SkipUnwrapLowering && !l.p.SkipSafeIndexLowering {
					if ls, ok := stmt.(*LetStatement); ok && !replaced {
						if repl := l.maybeCaptureOptionAssign(ls); repl != nil {
							l.carryOverflowAnnotation(carryFrom, repl)
							v.Index(i).Set(reflect.ValueOf(repl))
							replaced = true
						}
					}
				}
				// 按 walk 順序登記使用者寫下的宣告名（必須在捕獲 pass 之後：
				// 捕獲 pass 要判斷的是「本陳述之前」是否已宣告同名變數，才能決定
				// 要不要補 `a ?T = nil` 預宣告。對已存在的變數補預宣告會先把它的
				// 值清成 nil，`a = a + b / c` 的 RHS 在 ok 臂才求值，就讀到 nil）。
				// 合成陳述（desugar 產物）不算使用者宣告。
				if ls, ok := stmt.(*LetStatement); ok && ls.Name != nil && !ls.IsSynthetic {
					l.declaredLocals[ls.Name.Value] = true
				}
			}
			l.walk(v.Index(i))
			l.enclosingOvf = savedOvf
			_ = replaced
		}
	case reflect.Struct:
		t := v.Type()
		// Detect FunctionDefinition to track current function name for
		// function-scoped variable type lookups during match desugaring.
		var savedFuncName string
		var savedFuncDef *FunctionDefinition
		var savedDeclared map[string]bool
		savedOvf := l.enclosingOvf
		isFuncDef := false
		if v.CanAddr() {
			if fd, ok := v.Addr().Interface().(*FunctionDefinition); ok {
				savedFuncName = l.curFuncName
				savedFuncDef = l.curFuncDef
				savedDeclared = l.declaredLocals
				l.curFuncName = fd.Name
				l.curFuncDef = fd
				// 函式體是獨立作用域：不繼承外層陳述的 `#{overflow=...}` 註解，
				// 否則外層註解會靜默掩蓋巢狀函式體內真正未處理的溢出
				//（與 checker 的 ValidateUnhandledOverflow 同一規則）。
				l.enclosingOvf = false
				// 已宣告名單同樣獨立作用域：參數先入帳，函式內同名變數才不會
				// 被誤判為首次宣告。
				l.declaredLocals = map[string]bool{}
				for _, prm := range fd.Parameters {
					if prm != nil && prm.Name != "" {
						l.declaredLocals[prm.Name] = true
					}
				}
				// 預掃描區域變數容器型別（如 `av = a.to-vec()`），使其被安全索引
				// 降級辨識（isSafeIndexBase / inferIndexElemType 依賴 FuncVarType）。
				l.collectLocalTypes(fd)
				isFuncDef = true
			}
		}
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue // 非導出欄位（AST 子節點欄位均導出；matchArm 由 lowerSurfaceMatch 顯式處理）
			}
			l.walk(v.Field(i))
		}
		if isFuncDef {
			l.curFuncName = savedFuncName
			l.curFuncDef = savedFuncDef
			l.declaredLocals = savedDeclared
		}
		l.enclosingOvf = savedOvf
		// 標籤條件迴圈包裝（`#N cond: { body }`）：解析期只掛上 SurfaceMatch，
		// lowering 後補齊 Body = IfExpression.Consequence（與舊解析期行為一致）。
		if v.CanAddr() {
			if fs, ok := v.Addr().Interface().(*ForStatement); ok {
				if fs.IsCondWrapper && fs.Body == nil {
					if ife, ok := fs.Condition.(*IfExpression); ok {
						fs.Body = ife.Consequence
					}
				}
			}
		}
	}
}

// collectLocalTypes 預掃描函數體，將「未標型別、但可由 RHS 推導容器型別」的
// 區域變數（如 `av = a.to-vec()`）記入函數作用域 FuncVarTypes，使安全索引
// 降級（isSafeIndexBase / inferIndexElemType）能辨識這類 vec/slice 基底。
// 否則其型別在解析期未知 → 安全索引降級不觸發 → vec/slice 索引仍走硬
// bounds_check，越界時崩潰（違反「一定要安全」要求）。目前處理 to-vec()
// 轉換（stdsig：[n]t.to-vec -> []t），回傳 slice 型別。
func (l *lowerer) collectLocalTypes(fd *FunctionDefinition) {
	if fd == nil || fd.Body == nil {
		return
	}
	savedP := l.p.curFuncName
	savedL := l.curFuncName
	l.p.curFuncName = fd.Name
	l.curFuncName = fd.Name
	localTypes := map[string]string{}
	// 參數型別：明確標註的容器型別（arr [N]T / []T / vec[T]）同樣記入「安全索引
	// 專用」本地型別表，使 `key[i]`（key 為參數）也能被 isSafeIndexBase 辨識、
	// 走安全降級（否則降級在 ResolveProgram 之前執行，參數型別尚未進入
	// sem.FuncVarType，isSafeIndexBase 查不到 → 參數基底的越界索引無法被
	// `#{index-out}` 處理，與 checker 的判定（ResolveProgram 之後，含參數型別）
	// 不一致，導致標註失效、索引仍被報未處理）。
	for _, prm := range fd.Parameters {
		if prm.Type == nil {
			continue
		}
		nm := prm.Name
		if nm == "" {
			continue
		}
		lt := typeString(prm.Type)
		if lt != "" {
			localTypes[nm] = lt
			l.recordIndexLocalType(fd.Name, nm, lt)
		}
	}
	for _, st := range fd.Body.Statements {
		ls, ok := st.(*LetStatement)
		if !ok || ls.Name == nil {
			continue
		}
		name := ls.Name.Value
		if name == "" || ls.IsSynthetic {
			continue
		}
		if _, exists := localTypes[name]; exists {
			continue
		}
		// 1) 顯式型別標註：arr [N]T / x []T / x vec[T]
		if ls.Type != nil {
			lt := typeString(ls.Type)
			if lt != "" {
				localTypes[name] = lt
				// 寫入「安全索引專用」的解析器本地型別表（idxLocalTypes），
				// 不污染 p.sem.FuncVarTypes / VarTypes（後兩者會被 codegen
				// 讀取，寫入 `a`/`av` 等常見名會竄改 std 函式如 str.to-i8 的
				// 區域變數型別，造成跨函數型別污染崩潰）。
				l.recordIndexLocalType(fd.Name, name, lt)
			}
			continue
		}
		// 2) RHS 為 to-vec()：接收者須為已知容器，元素型別取其元素。
		if elem := l.toVecElemType(ls.Value, localTypes); elem != "" {
			lt := "[]" + elem // to-vec 回傳 slice
			localTypes[name] = lt
			l.recordIndexLocalType(fd.Name, name, lt)
		}
	}
	l.p.curFuncName = savedP
	l.curFuncName = savedL
}

// toVecElemType 從 `a.to-vec()` / `a.to-vec` 這類 RHS 推導容器元素型別。
// to-vec 在固定陣列 [n]t 上回傳 []t（stdsig_gen.go），故元素取接收者陣列/
// 切片/vec 的元素型別。
func (l *lowerer) toVecElemType(val Expression, localTypes map[string]string) string {
	var recvName string
	switch v := val.(type) {
	case *CallExpression:
		de, ok := v.Function.(*DotExpression)
		if !ok || de.Property != "to-vec" {
			return ""
		}
		recvName = dotReceiverName(de.Receiver)
	case *DotExpression:
		if v.Property != "to-vec" {
			return ""
		}
		recvName = dotReceiverName(v.Receiver)
	default:
		return ""
	}
	if recvName == "" {
		return ""
	}
	recvType := ""
	if t, ok := localTypes[recvName]; ok {
		recvType = t
	} else if t, ok := l.p.sem.FuncVarType(l.curFuncName, recvName); ok {
		recvType = t
	} else if t, ok := l.p.sem.VarTypes[recvName]; ok {
		recvType = t
	}
	return containerElemType(strings.TrimPrefix(recvType, "?"))
}

// dotReceiverName 返回 `a` / `a.field` 這類接收者表達式中最左識別符名稱。
func dotReceiverName(recv Expression) string {
	switch r := recv.(type) {
	case *Identifier:
		return r.Value
	case *DotExpression:
		return dotReceiverName(r.Receiver)
	}
	return ""
}

// recordIndexLocalType 將區域變數容器型別寫入「安全索引專用」的解析器本地
// 型別表 idxLocalTypes（按函數作用域）。此表僅供 isSafeIndexBase /
// inferIndexElemType 查詢，不寫入 sem.FuncVarTypes / VarTypes，故不會影響
// codegen 對 std 函式區域變數的型別推斷（避免跨函數型別污染崩潰）。
func (l *lowerer) recordIndexLocalType(funcName, name, lt string) {
	if l.p.idxLocalTypes == nil {
		l.p.idxLocalTypes = make(map[string]map[string]string)
	}
	if l.p.idxLocalTypes[funcName] == nil {
		l.p.idxLocalTypes[funcName] = make(map[string]string)
	}
	l.p.idxLocalTypes[funcName][name] = lt
}

// inferOptionDivMod — 有號整數 `/` `%` 本身產生 option（除零/溢出為 err），
// 故普通 `=` 綁定其結果時變數型別必然是 `?T`，無需用戶顯式標註：在
// lowering 階段就地補上推斷出的 `?T` 型別註解（合成 NullableType）並註冊
// 變數型別，使 checker（ovfhndld declaredOption 豁免、ovf-int-default lint）
// 與 MIR（KLet typeHint → 除零守衛）完全復用顯式路徑 `q ?int = a / b`。
// 推斷不出（運算元型別未知/非有號整數/已標註/帶 overflow 註解）時保守
// 不改寫，維持原有行為。
func (l *lowerer) inferOptionDivMod(s *LetStatement) {
	if s == nil || s.IsSynthetic || s.Name == nil || s.Value == nil {
		return
	}
	// 顯式標註（含 ?T）一律不碰：用戶意圖優先，也保證冪等（重解析不重複改寫）。
	if s.Type != nil {
		return
	}
	inf, ok := unwrapLowerExpr(s.Value).(*InfixExpression)
	if !ok || (inf.Operator != "/" && inf.Operator != "%") {
		return
	}
	// `#{overflow=...}` 行註解表示用戶已顯式處理溢出語義（wrap 等），不改寫。
	if s.OverflowMode != "" {
		return
	}
	if l.p.sem != nil {
		for _, e := range l.p.sem.RawAnnotationsOf(s) {
			if e != nil && e.Key == "overflow" {
				return
			}
		}
	}
	lt, okLT := l.signedIntOperandType(inf.Left)
	rt, okRT := l.signedIntOperandType(inf.Right)
	if !okLT || !okRT {
		return
	}
	base := lt
	if len(rt) > len(base) {
		base = rt
	}
	// 合成與顯式寫法同形的註解 `?base`（如 ?int）並註冊型別。
	nullTok := s.Token
	innerTok := s.Token
	innerTok.Literal = base
	s.Type = &NullableType{
		Token:      nullTok,
		Type:       &NamedType{Token: innerTok, Value: base},
		IsInferred: true,
	}
	l.p.setVarType(s.Name.Value, "?"+base)
}

// synthesizeNullableType 就地為 s 補上推斷出的 `?inner` 註解（合成 NullableType）
// 並註冊變數型別，使下游 checker（ovfhndld/idxhndld 的 declaredOption 豁免）與
// MIR（KLet typeHint）完全復用「用戶顯式寫 ?T」的已驗證路徑。
func (l *lowerer) synthesizeNullableType(s *LetStatement, inner string) {
	if s == nil || s.Name == nil || inner == "" {
		return
	}
	tok := s.Token
	innerTok := tok
	innerTok.Literal = inner
	s.Type = &NullableType{
		Token:      tok,
		Type:       &NamedType{Token: innerTok, Value: inner},
		IsInferred: true,
	}
	l.p.setVarType(s.Name.Value, "?"+inner)
}

// captureNode 描述一個「可錯源」子表達式：它在運行期可能產生 nil/err。
type captureNode struct {
	expr  Expression // 原始子表達式，會被綁定到 __cap_N
	inner string     // option 內部型別 T
	name  string     // 綁定用的臨時變數名 __cap_N
}

// captureInfixInner 回報 `a / b` / `a % b` 這類自身即為可錯源的 Infix 的內部型別
// （提升後的有號整型）。非 `/` `%`、或運算元型別不可判定時回傳 ""。
// 純算術（+ - * 取負 shl）本身不是可錯源 —— 否則所有算術都會變成 option。
func (l *lowerer) captureInfixInner(inf *InfixExpression) string {
	if inf == nil || (inf.Operator != "/" && inf.Operator != "%") {
		return ""
	}
	lt, okLT := l.signedIntOperandType(inf.Left)
	rt, okRT := l.signedIntOperandType(inf.Right)
	if !okLT || !okRT {
		return ""
	}
	if len(rt) > len(lt) {
		return rt
	}
	return lt
}

// callOptionInner 回報回傳 option 的呼叫的內部型別；非 option 回傳 ""。
func (l *lowerer) callOptionInner(c *CallExpression) string {
	if c == nil || l.p == nil {
		return ""
	}
	inferred := l.p.inferTypeFromCallExpr(c)
	if inferred != "" && strings.HasPrefix(inferred, "?") {
		return inferred[1:]
	}
	return ""
}

// enclosingFuncHasOptResult 回報當前所在函式是否具有 option 結果參數（`?T`）。
//
// 安全索引的捕獲以此為閘門，鏡像被取代的 maybeAutoPropagateIndex 的既有範圍：
// 那條路徑只在「有 option 結果參數的函式內」把裸 `x = v[i]` 改寫為 `x ?= v[i]`，
// 於是 `x = v[i]` 變成 option 的語義也只在同一範圍內成立。
//
// 範圍若放大到所有函式與指令稿，`x = v[i]` 會把純量變數靜默變成 option，凡是
// 之後以普通值使用它的地方（呼叫引數、結構體欄位賦值…）都會踩到 option→純量
// 尚未支援的 codegen 路徑（tests/x25519-vec.no、tests/diff-debug.no 即為此類）。
// 頂層指令稿（curFuncDef == nil）一律不啟用，與舊行為一致。
func (l *lowerer) enclosingFuncHasOptResult() bool {
	if l.curFuncDef == nil {
		return false
	}
	for _, res := range l.curFuncDef.Results {
		if res != nil && res.Type != nil && strings.HasPrefix(typeString(res.Type), "?") {
			return true
		}
	}
	return false
}

// addCaptureNode 把 e 登記為捕獲節點（分配 __cap_N、註冊型別），回傳替代 e 的識別符。
func (l *lowerer) addCaptureNode(e Expression, inner string, out *[]captureNode, tok lexer.Token) Expression {
	l.capTmpSeq++
	name := fmt.Sprintf("__cap_%d", l.capTmpSeq)
	l.p.setVarType(name, "?"+inner)
	*out = append(*out, captureNode{expr: e, inner: inner, name: name})
	return &Identifier{Token: tok, Value: name}
}

// collectCaptureNodes 按求值順序收集可錯源節點，並就地把它們替換成對應的
// __cap_N 識別符，回傳改寫後的表達式。
//
// inArithOperand 表示 e 目前位於「算術運算元」位置：只有在此位置才直接捕獲
// option 變數與回傳 option 的呼叫（作為呼叫引數的 option 交給被呼叫方，與
// guardInCallArg 的既有註釋一致）。
func (l *lowerer) collectCaptureNodes(e Expression, out *[]captureNode, tok lexer.Token, inArithOperand bool) Expression {
	switch v := e.(type) {
	case *GroupedExpression:
		if v.Expression != nil {
			v.Expression = l.collectCaptureNodes(v.Expression, out, tok, inArithOperand)
		}
		return v
	case *InfixExpression:
		// `/` `%` 自身即為可錯源，且只取最外層：巢狀 `(b / c) / d` 只捕獲外層，
		// 內部行為與 `?=` 現狀完全一致。
		if inner := l.captureInfixInner(v); inner != "" {
			return l.addCaptureNode(v, inner, out, tok)
		}
		v.Left = l.collectCaptureNodes(v.Left, out, tok, true)
		v.Right = l.collectCaptureNodes(v.Right, out, tok, true)
		return v
	case *IndexExpression:
		if v.Left != nil {
			v.Left = l.collectCaptureNodes(v.Left, out, tok, false)
		}
		if v.Index != nil {
			v.Index = l.collectCaptureNodes(v.Index, out, tok, false)
		}
		if l.p.isSafeIndexBase(v) && l.enclosingFuncHasOptResult() {
			if elem := l.p.inferIndexElemType(v); elem != "" {
				return l.addCaptureNode(v, elem, out, tok)
			}
		}
		return v
	case *CallExpression:
		for i := range v.Arguments {
			v.Arguments[i] = l.collectCaptureNodes(v.Arguments[i], out, tok, false)
		}
		if inArithOperand {
			if inner := l.callOptionInner(v); inner != "" {
				return l.addCaptureNode(v, inner, out, tok)
			}
		}
		return v
	case *Identifier:
		if inArithOperand && v.Value != "self" {
			if inner := l.optionInnerOfVar(v.Value); inner != "" {
				return l.addCaptureNode(v, inner, out, tok)
			}
		}
		return v
	}
	return e
}

// maybeCaptureOptionAssign 偵測「普通 `=` 綁定含可錯源的運算式」並就地改寫為捕獲：
//
//	a = b + c / d
//	→ __cap_1 ?i64 = c / d
//	  a ?i64 = nil
//	  match __cap_1 { nil -> a = __cap_1
//	                  err -> a = __cap_1
//	                  ok  -> a = b + __cap_1 }
//
// 錯誤（nil/err）就地存進 a，流程繼續；之後用 `a: { ok -> ... }` 消費。
// 多個可錯源時綁定放進上一層的 ok 臂，保持由左至右的求值順序。
//
// 保守跳過的情形（維持今日行為，交由 checker 或既有路徑處理）：
//   - 已帶 `#{overflow=...}` / `#{index-out=...}` 註解或 OverflowMode
//   - 顯式標註非 option 型別、或變數已確定為非 option（無法把 option 存進 plain）
//   - 根為 `/` `%`（inferOptionDivMod 已處理）、根為 option 變數（賦值即拷貝）
//   - 根為安全索引（僅合成 ?elem 註解，無結構 desugar）
//   - 根為呼叫但回傳型別未知、或回傳本身已是 option（整體拷貝語意）
//   - 根為其他型別（字面量、欄位存取…）語意未定
//   - 掃描後沒有任何可錯源節點（`len(nodes) == 0`）
func (l *lowerer) maybeCaptureOptionAssign(stmt interface{}) Statement {
	ls, ok := stmt.(*LetStatement)
	if !ok || ls == nil || ls.IsSynthetic || ls.Name == nil || ls.Value == nil {
		return nil
	}
	if ls.OverflowMode != "" {
		return nil
	}
	// 外層陳述帶 `#{overflow=...}` 行註解 → 整棵子樹（含 match/if 臂體）已由用戶
	// 顯式處理溢出語義，不改寫（鏡像 checker 的 effOverflow 傳遞規則）。
	if l.enclosingOvf {
		return nil
	}
	if l.p.sem != nil {
		for _, e := range l.p.sem.RawAnnotationsOf(ls) {
			if e != nil && (e.Key == "overflow" || e.Key == "index-out") {
				return nil
			}
		}
	}
	// 顯式標註非 option 型別 → 用戶強制 plain，交 checker 報錯。
	//
	// 例外：parse 期替 `v = arr[i]` 推斷出的元素型別（IsInferred，見
	// parseLetStatement 的 `case *IndexExpression`）不是用戶意圖，捕獲 pass
	// 有權把它改寫成 `?elem`。少了這個例外，`v = arr[i]` 在 lowering 前就被
	// 標成 plain i64，根索引的捕獲分支永遠走不到（越界錯誤只能靠 checker 報）。
	if ls.Type != nil && !strings.HasPrefix(typeString(ls.Type), "?") && !typeIsInferred(ls.Type) {
		return nil
	}
	// 變數已確定為非 option → 交 checker 報錯（訊息建議 ?= / ?T / match）。
	//
	// 僅在陳述本身沒有型別節點時才據此拒絕：有型別節點代表這是（重新）宣告，
	// sem 中的型別正是本陳述剛登記的（如上面的 parse 期索引推斷），用它來否決
	// 自己是循環論證。真正「先宣告 plain、再賦值」的情形（`v i64 = 0` 之後的
	// `v = arr[i]`）其 ls.Type 為 nil，仍會被這一關擋下。
	//
	// 判定必須用 localDefinitelyNonOption（函式作用域）而非 isDefinitelyNonOptionVar
	// —— 後者經 FuncVarType 回退到全域 VarTypes，會被其他函式的同名參數污染。
	if !l.captureTargetAllowsOption(ls) {
		return nil
	}
	root := unwrapLowerExpr(ls.Value)
	// 根為 `/` `%`：inferOptionDivMod 已處理（且它會自行合成 ?T）。
	if inf, isInf := root.(*InfixExpression); isInf && (inf.Operator == "/" || inf.Operator == "%") {
		return nil
	}
	// 根為 option 變數：賦值本身即整體拷貝，現有行為已正確。
	if id, isID := root.(*Identifier); isID && l.optionInnerOfVar(id.Value) != "" {
		return nil
	}
	// 根為安全索引（無內部可錯子表達式）：只合成 ?elem，越界 → nil 即捕獲。
	if idx, isIdx := root.(*IndexExpression); isIdx && l.p.isSafeIndexBase(idx) {
		if !l.enclosingFuncHasOptResult() {
			return nil
		}
		if elem := l.p.inferIndexElemType(idx); elem != "" {
			l.synthesizeNullableType(ls, elem)
		}
		return nil
	}
	// 根型別決定 option 內部型別 T（方案 §1「T 推斷」）：
	//   - 算術 Infix（`+ - * <<` …）：T = i64 —— option 內部資料一律 i64，
	//     見 lowerUnwrapAssign 對 ?T 內部型別的說明。
	//   - 呼叫：T = 回傳型別去 `?`。`a = g(b / c)` 的捕獲點在引數內，結果型別
	//     仍是 g 的回傳型別；回傳本身已是 option（`?T`）時整個呼叫即為 option，
	//     屬既有「賦值即整體拷貝」語意，不由此 pass 處理（保守跳過）。
	//   - 其餘（字面量、欄位存取…）語意未定，保守不改寫。
	var inner string
	switch r := root.(type) {
	case *InfixExpression:
		inner = "i64"
	case *CallExpression:
		rt := l.p.returnTypeFromCallExpr(r)
		if rt == "" || strings.HasPrefix(rt, "?") {
			return nil
		}
		inner = rt
	default:
		return nil
	}
	var nodes []captureNode
	rewritten := l.collectCaptureNodes(ls.Value, &nodes, ls.Token, false)
	if len(nodes) == 0 {
		return nil
	}
	// 顯式標註 ?T 時補內部守衛（推斷內部型別以顯式為準）。
	if ls.Type != nil {
		if explicit := strings.TrimPrefix(typeString(ls.Type), "?"); explicit != "" {
			inner = explicit
		}
	}
	// 目標是否在原始碼中已宣告（供下面的預宣告判斷）。必須在
	// synthesizeNullableType 之前取樣：它會把 `a` 註冊進 VarTypes，之後再問
	// 「VarTypes 是否為空」永遠是 false，預宣告就成了死碼。
	//
	// 判定用 declaredLocals（walk 順序的宣告名單）+ 函式作用域的型別查詢，
	// 不能直接讀全域 VarTypes：
	//   - 全域表不記錄由字面量推斷出的型別（`mi = 0.0` 之後 VarTypes["mi"] 仍空），
	//     漏判會對已存在的變數再發一次 `mi ?f64 = nil` 預宣告，清掉它的既有值；
	//   - 全域表會被其他函式的同名參數污染（f 的 `c i64` 讓 h 的區域 `c` 被當成
	//     已宣告），漏掉 h 需要的預宣告。
	wasDeclared := l.declaredLocals[ls.Name.Value]
	if !wasDeclared {
		if _, ok := l.localDeclaredType(ls.Name.Value); ok {
			wasDeclared = true
		}
	}
	l.synthesizeNullableType(ls, inner)

	tok := ls.Token
	// 由最內層節點往外建巢狀 match；最內層 body 即最終賦值。
	body := Statement(&LetStatement{
		Token:       tok,
		Name:        ls.Name,
		Value:       rewritten,
		IsSynthetic: true,
	})
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		capIdent := &Identifier{Token: tok, Value: n.name}
		// nil/err 臂：把整個 option 結構拷貝進目標（IsPropagation 鏡像 ?= 的
		// 傳播賦值，避免 codegen 對 arm 內哨兵 it 綁定的處理把這次 store 吞掉）。
		copyArm := func() *BlockStatement {
			return &BlockStatement{Token: tok, Statements: []Statement{
				&LetStatement{Token: tok, Name: ls.Name, Value: capIdent, IsSynthetic: true, IsPropagation: true},
			}}
		}
		arms := []matchArm{
			{condition: &Identifier{Token: tok, Value: "nil"}, body: copyArm(), isBlockBody: true, pos: posFromToken(tok), skipItBinding: true},
			{condition: &Identifier{Token: tok, Value: "err"}, body: copyArm(), isBlockBody: true, pos: posFromToken(tok), skipItBinding: true},
			{condition: &Identifier{Token: tok, Value: "ok"}, body: &BlockStatement{Token: tok, Statements: []Statement{body}}, isBlockBody: true, pos: posFromToken(tok), skipItBinding: true},
		}
		sm := &SurfaceMatch{Token: tok, Matched: capIdent, Arms: arms}
		savedFuncName := l.p.curFuncName
		l.p.curFuncName = l.curFuncName
		lowered := l.p.buildMatchDesugar(sm)
		l.p.curFuncName = savedFuncName
		if lowered == nil {
			return nil
		}
		bind := &LetStatement{
			Token:       tok,
			Name:        &Identifier{Token: tok, Value: n.name},
			IsSynthetic: true,
			Type:        &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: n.inner}},
			Value:       n.expr,
		}
		body = &BlockStatement{Token: tok, Statements: []Statement{
			bind,
			&ExpressionStatement{Token: tok, Expression: lowered},
		}}
	}
	var stmts []Statement
	// 目標尚未宣告時預先 `a ?T = nil` 宣告：所有臂都會寫入 a，nil 槽無堆指標，
	// 後續覆寫安全。已宣告為 ?T 時只需臂內重賦值。
	if !wasDeclared {
		stmts = append(stmts, &LetStatement{
			Token:       tok,
			Name:        ls.Name,
			IsSynthetic: true,
			Type:        &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: inner}},
			Value:       &NilLiteral{Token: tok},
		})
	}
	stmts = append(stmts, body)
	return &BlockStatement{Token: tok, Statements: stmts, CommentedNode: ls.CommentedNode}
}

// signedIntOperandType 回傳運算元的有號整數型別名（int/i64/...）。僅在型別
//
//	statically 可知且有號時回傳 ok：整數字面量 → i64；識別符 → 語義表查詢
//
// （self 用方法 receiver 型別）；括號展開。型別未知或非整數回 ok=false，
// 保守跳過推斷（i128 除外：codegen 對其退化為 wrap，不產生 option）。
func (l *lowerer) signedIntOperandType(e Expression) (string, bool) {
	switch x := unwrapLowerExpr(e).(type) {
	case *IntegerLiteral:
		return "i64", true
	case *Identifier:
		name := x.Value
		if name == "self" || name == "." {
			return l.lowerSelfType()
		}
		typ := ""
		if l.p.sem != nil {
			if t, has := l.p.sem.FuncVarType(l.curFuncName, name); has {
				typ = t
			}
		}
		return lowerSignedIntName(typ)
	default:
		return "", false
	}
}

// lowerSelfType 回傳當前方法 receiver（self）的有號整數型別；非方法或
// 非整數接收者回 ok=false。
func (l *lowerer) lowerSelfType() (string, bool) {
	fd := l.curFuncDef
	if fd == nil || !fd.IsMethodDef || len(fd.Results) == 0 {
		return "", false
	}
	if r := fd.Results[0]; r != nil && r.Name == "self" && r.Type != nil {
		return lowerSignedIntName(typeString(r.Type))
	}
	return "", false
}

// unwrapLowerExpr 剝除括號節點，回傳內層表達式。
func unwrapLowerExpr(e Expression) Expression {
	for {
		g, ok := e.(*GroupedExpression)
		if !ok || g.Expression == nil {
			return e
		}
		e = g.Expression
	}
}

// lowerSignedIntName 判定型別字串是否為可產生 option 的有號整數型別：
// 接受 int/i8/i16/i32/i64（含 number.int 等限定名與 *int 指標形式），
// 拒絕無號（u8..u128/byte）、i128（codegen 退化 wrap）、f32/f64、str 等
// 已宣告型別、以及已為 option（?T）的型別。
func lowerSignedIntName(typ string) (string, bool) {
	typ = strings.TrimSpace(typ)
	if typ == "" || strings.HasPrefix(typ, "?") {
		return "", false
	}
	for strings.HasPrefix(typ, "*") {
		typ = strings.TrimSpace(typ[1:])
	}
	if i := strings.LastIndex(typ, "."); i >= 0 {
		typ = typ[i+1:]
	}
	switch typ {
	case "int", "i8", "i16", "i32", "i64":
		return typ, true
	}
	return "", false
}

// maybeIndexOutAssign 偵測帶 `#{index-out=DEF}` 註解的安全索引賦值
// （`x = v[5]`，v 為 arr/vec/slice，DEF 為字面量預設值），將其就地改寫為
// match：越界時用 DEF 替代，否則取出元素。返回 *BlockStatement；非候選則回傳 nil。
//
// 這條路徑與 `x ?= v[5]` 共用安全索引 codegen（`__tmp = v[5]` 產生 %option，
// 越界回傳 none），差別只在 none arm 的行為：?= 向上傳播錯誤，index-out 用 DEF 替代。
func (l *lowerer) maybeIndexOutAssign(stmt interface{}) Statement {
	// 確保後續 inferIndexElemType / isSafeIndexBase 的函數作用域型別查詢正確
	// （desugar 區段會自行 save/restore，此處設定對早退路徑無副作用）。
	l.p.curFuncName = l.curFuncName
	var value Expression
	var name *Identifier
	var leftExpr Expression // 非識別符 LHS（如 `out[i] = .[i]` 的索引寫入 LHS）
	var tok lexer.Token
	switch s := stmt.(type) {
	case *LetStatement:
		if s.IsSynthetic {
			return nil
		}
		value = s.Value
		if s.Name != nil {
			name = s.Name
			tok = s.Token
		}
	case *AssignExpression:
		value = s.Value
		if id, ok := s.Left.(*Identifier); ok {
			name = id
			tok = s.Token
		} else {
			// 索引寫入 LHS（arr[i] = x[i] / obj.field = x[i]）：保留 LHS 表達式，
			// 在兩個 match arm 中對其賦值（ok arm 寫解箱元素，none arm 寫 DEF）。
			leftExpr = s.Left
			tok = s.Token
		}
	default:
		return nil
	}
	if (name == nil && leftExpr == nil) || value == nil {
		return nil
	}
	idx, ok := value.(*IndexExpression)
	if !ok {
		return nil
	}
	if !l.p.isSafeIndexBase(idx) {
		return nil
	}
	// 必須帶 #{index-out=...} 註解。
	if l.p.sem == nil {
		return nil
	}
	var defVal AnnotationValue
	found := false
	// 使用 RawAnnotationsOf（desugar walk 在將註解從外層 ExpressionStatement
	// 轉移到內層 AssignExpression 後寫入的即時 side-table），而非 AnnotationsOf
	// （後者讀取 ResolveProgram 階段填入的 ns.Annotations，對 index-write LHS
	// 這類「註解掛在外層、desugar 時才轉移進內層」的語句會查不到而誤判為未處理）。
	for _, e := range l.p.sem.RawAnnotationsOf(stmt.(Node)) {
		if e != nil && e.Key == "index-out" {
			defVal = e.Value
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	// 推斷元素型別；推斷不出則無法產生安全的 none 預設值，報錯。
	elem := l.p.inferIndexElemType(idx)
	if elem == "" {
		l.p.saveError(fmt.Sprintf("line %d, column %d: cannot infer element type for safe index with #{index-out}; annotate the container or use an explicit type", tok.Line, tok.Column))
		return nil
	}
	// 建構 __idx_out_N = v[i]（?elem 安全索引）。
	tmpName := fmt.Sprintf("__idx_out_%d_%d", tok.Line, tok.Column)
	tmpIdent := &Identifier{Token: tok, Value: tmpName}
	l.p.setVarType(tmpName, "?"+elem)
	tmpAssign := &LetStatement{
		Token:       tok,
		Name:        tmpIdent,
		IsSynthetic: true, // 防止 walk 時被後續 capture / index-out pass 二次改寫
		Type:        &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: elem}},
		Value:       idx,
	}
	// 建構預設值字面量（依元素型別解釋 DEF）。
	defLit, errMsg := defaultLiteralFor(tok, elem, defVal)
	if errMsg != "" {
		l.p.saveError(fmt.Sprintf("line %d, column %d: %s", tok.Line, tok.Column, errMsg))
		return nil
	}
	if defLit == nil {
		defLit = &IntegerLiteral{Token: tok, Value: 0}
	}
	// arms: ok -> lhs = it（解箱元素寫入 lhs）；-> 預設值（越界/錯誤時用 DEF）。
	// 識別符 LHS（x = ...）走 LetStatement；索引/點 LHS（arr[i] = ... / obj.field = ...）
	// 走 AssignExpression，在兩個 arm 中對同一 LHS 賦值（ok arm 寫解箱元素，none
	// arm 寫 DEF）。none arm 從不使用 `it`，skipItBinding 避免其 %str-long 型別的
	// `it` 綁定覆寫 ok arm 的元素型別 `it`。
	assignToLHS := func(val Expression) Statement {
		if name != nil {
			return &LetStatement{Token: tok, Name: name, Value: val}
		}
		return &ExpressionStatement{Token: tok, Expression: &AssignExpression{Token: tok, Left: leftExpr, Value: val}}
	}
	okBody := &BlockStatement{Token: tok, Statements: []Statement{
		assignToLHS(&Identifier{Token: tok, Value: "it"}),
	}}
	defAssign := assignToLHS(defLit)
	arms := []matchArm{
		{condition: &Identifier{Token: tok, Value: "ok"}, body: okBody, isBlockBody: true, pos: posFromToken(tok)},
		{isWildcard: true, body: &BlockStatement{Token: tok, Statements: []Statement{defAssign}}, isBlockBody: true, skipItBinding: true, pos: posFromToken(tok)},
	}
	sm := &SurfaceMatch{Token: tok, Matched: tmpIdent, Arms: arms}
	savedFuncName := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	lowered := l.p.buildMatchDesugar(sm)
	l.p.curFuncName = savedFuncName
	if lowered == nil {
		return nil
	}
	// 識別符 LHS 且尚未宣告時預先 `let x = DEF` 宣告（所有分支皆會賦值，故安全）；
	// 索引/點 LHS 是既有 lvalue，無需預先宣告。
	var stmts []Statement
	if name != nil && (l.p.sem == nil || l.p.sem.VarTypes[name.Value] == "") {
		preDecl := &LetStatement{Token: tok, Name: name, Value: defLit}
		stmts = []Statement{preDecl, tmpAssign, &ExpressionStatement{Token: tok, Expression: lowered}}
	} else {
		stmts = []Statement{tmpAssign, &ExpressionStatement{Token: tok, Expression: lowered}}
	}
	return &BlockStatement{Token: tok, Statements: stmts}
}

// carryOverflowAnnotation 把被安全索引 desugar 取代的原陳述之 `#{overflow = ...}`
// 條目轉掛到取代節點上。
//
// 安全索引 desugar 會以全新的 match/Block 取代原陳述（`arr[idx] = v` →
// `__tmp = arr[idx]` + match），而 `#{overflow = wrap}` 這類**行註解**是綁在原陳述
// 節點上的。若不轉移，取代節點就變成「未標註」：
//   - lint（checker.ValidateIntOverflow，跑在 lowering 之後的程式上）會把
//     索引算式 `arr[base + i]` 裡真正的整數運算當成未處理而繼續報告
//     （ovf-int-default），即使原始碼上方明明寫了 `#{overflow=wrap}`；
//   - 型別校驗（checker.overflowModeFromSem）也讀不到模式。
//
// 這正是「加了註解卻仍被誤報」的根因，故在此把 overflow 條目一併帶到取代節點
// （只帶 overflow，不帶 index-out：index-out 的語意已由 desugar 本身實現）。
//
// 註解來源有兩個：側表條目（Ident 開頭的陳述由 parser 直接附加），以及
// OverflowMode 欄位（`.field[i] = v` / `return ...` 這類無法直接附加的陳述，
// 行注解只寫欄位而不進側表，以免 formatter 把同一行註解印兩次）。
func (l *lowerer) carryOverflowAnnotation(from Node, to Statement) {
	if l.p.sem == nil || to == nil || isNil(from) {
		return
	}
	entries := l.p.overflowEntries(l.p.sem.RawAnnotationsOf(from))
	if len(entries) == 0 {
		entries = l.p.overflowEntries(l.p.sem.AnnotationsOf(from))
	}
	if len(entries) == 0 {
		if mode := overflowModeFieldOf(from); mode != "" {
			entries = []*AnnotationEntry{{
				Key:   "overflow",
				Value: &AnnotationIdentValue{Value: mode},
			}}
		}
	}
	if len(entries) == 0 {
		return
	}
	l.p.sem.SetRawAnnotations(to, entries)
	l.p.sem.ensure(to).Annotations = entries
}

// stmtHasOverflowAnnotation 回報陳述是否帶有 `#{overflow=...}` 行註解（側表條目
// 或 OverflowMode 欄位）。捕獲 pass 用它判斷「本陳述／外層陳述已由用戶顯式處理
// 溢出語義」而整棵子樹跳過改寫。
func (l *lowerer) stmtHasOverflowAnnotation(n Node) bool {
	if isNil(n) || l.p.sem == nil {
		return false
	}
	if len(l.p.overflowEntries(l.p.sem.RawAnnotationsOf(n))) > 0 {
		return true
	}
	if len(l.p.overflowEntries(l.p.sem.AnnotationsOf(n))) > 0 {
		return true
	}
	return overflowModeFieldOf(n) != ""
}

// overflowModeFieldOf 讀取陳述節點的 OverflowMode 欄位（行注解在無法直接附加
// 註解的陳述上只寫此欄位）。欄位集合與 setStmtOverflowMode 保持一致。
func overflowModeFieldOf(n Node) string {
	switch s := n.(type) {
	case *ForStatement:
		return s.OverflowMode
	case *ExpressionStatement:
		return s.OverflowMode
	case *LetStatement:
		return s.OverflowMode
	case *ReturnStatement:
		return s.OverflowMode
	case *MultiAssignStatement:
		return s.OverflowMode
	}
	return ""
}

// defaultLiteralFor 依元素型別 elem 解釋 #{index-out} 的預設註解值 defVal，
// 產生對應的 AST 字面量。回傳 (字面量, 錯誤訊息)；錯誤訊息非空表示無法轉換。
func defaultLiteralFor(tok lexer.Token, elem string, defVal AnnotationValue) (Expression, string) {
	switch elem {
	case "f64", "f32":
		switch val := defVal.(type) {
		case *AnnotationIntValue:
			return &FloatLiteral{Token: tok, Value: float64(val.Value), Raw: strconv.FormatInt(val.Value, 10)}, ""
		case *AnnotationIdentValue:
			f, err := strconv.ParseFloat(val.Value, 64)
			if err != nil {
				return nil, fmt.Sprintf("invalid float default %q for element type %s", val.Value, elem)
			}
			return &FloatLiteral{Token: tok, Value: f, Raw: val.Value}, ""
		case *AnnotationStringValue:
			f, err := strconv.ParseFloat(val.Value, 64)
			if err != nil {
				return nil, fmt.Sprintf("invalid float default %q for element type %s", val.Value, elem)
			}
			return &FloatLiteral{Token: tok, Value: f, Raw: val.Value}, ""
		default:
			return nil, fmt.Sprintf("#{index-out} default for %s must be a float literal (e.g. 0.0)", elem)
		}
	case "bool":
		switch val := defVal.(type) {
		case *AnnotationBoolValue:
			return &BooleanLiteral{Token: tok, Value: val.Value}, ""
		case *AnnotationIntValue:
			return &BooleanLiteral{Token: tok, Value: val.Value != 0}, ""
		}
		return nil, "#{index-out} default for bool must be true or false"
	case "str", "txt":
		if sv, ok := defVal.(*AnnotationStringValue); ok {
			return &StringLiteral{Token: tok, Value: sv.Value, Raw: "'" + sv.Value + "'"}, ""
		}
		if iv, ok := defVal.(*AnnotationIntValue); ok && iv.Value == 0 {
			return &StringLiteral{Token: tok, Value: "", Raw: "''"}, ""
		}
		return nil, "#{index-out} default for str must be a string literal (e.g. '')"
	default:
		// 整數類元素：i8..i128, u8..u128, byte, char。
		switch val := defVal.(type) {
		case *AnnotationIntValue:
			return &IntegerLiteral{Token: tok, Value: val.Value, Raw: strconv.FormatInt(val.Value, 10)}, ""
		case *AnnotationStringValue:
			// 字元預設：取首個 rune 的碼點。
			r := []rune(val.Value)
			if len(r) == 0 {
				return &IntegerLiteral{Token: tok, Value: 0, Raw: "0"}, ""
			}
			return &IntegerLiteral{Token: tok, Value: int64(r[0]), Raw: val.Value}, ""
		default:
			return nil, fmt.Sprintf("#{index-out} default for %s must be an integer or char literal", elem)
		}
	}
}

// preRegisterEnumArmBindings 在展開 match 之前，先依「被匹配變數的靜態型別」把
// 帶載荷變體析構臂（`some(v) ->` / `rect(w, h) ->`）的綁定名與其欄位型別登記進
// 函式作用域符號表。lowerSurfaceMatch 是先自底向上 walk arm 體（其中可能含有以該
// 綁定變數為主體的嵌套 match），之後才 buildMatchDesugar；若不預先登記，內層 match
// 在解析期查不到綁定變數的型別，就會把它自己的變體臂 `a(v)` 當成普通呼叫，導致
// `v` 未定義。此處登記後，內層 match 便能正確解析自身變體並繼續綁定載荷。
func (l *lowerer) preRegisterEnumArmBindings(sm *SurfaceMatch) {
	if sm == nil || len(sm.Arms) == 0 {
		return
	}
	ident, ok := sm.Matched.(*Identifier)
	if !ok {
		return
	}
	p := l.p
	mt, ok := p.sem.FuncVarType(l.curFuncName, ident.Value)
	if !ok || mt == "" {
		return
	}
	if _, isEnum := p.sem.EnumVariantsOf(mt); !isEnum {
		return
	}
	saved := p.curFuncName
	p.curFuncName = l.curFuncName
	defer func() { p.curFuncName = saved }()
	for i := range sm.Arms {
		a := &sm.Arms[i]
		call, ok := a.condition.(*CallExpression)
		if !ok || len(call.Arguments) == 0 {
			continue
		}
		fn, ok := call.Function.(*Identifier)
		if !ok {
			continue
		}
		names := make([]string, 0, len(call.Arguments))
		allBare := true
		for _, arg := range call.Arguments {
			id, isID := arg.(*Identifier)
			if !isID {
				allBare = false
				break
			}
			names = append(names, id.Value)
		}
		if !allBare {
			continue
		}
		fts := p.enumVariantFieldTypes(mt, fn.Value)
		if len(fts) == 0 || len(fts) != len(names) {
			continue
		}
		if p.sem.DeclaredVars == nil {
			p.sem.DeclaredVars = make(map[string]bool)
		}
		for k, nm := range names {
			p.sem.DeclaredVars[nm] = true
			if fts[k] != "" {
				p.sem.SetFuncVarType(l.curFuncName, nm, fts[k])
				p.setVarType(nm, fts[k])
			}
		}
	}
}

// matchArmCovers reports which of the option's three cases (nil / err / ok) an
// arm claims. A combined arm (`nil || err ->`) claims both.
func matchArmCovers(a *matchArm) (nilOK, errOK, okOK bool) {
	for _, pat := range a.multiOptionPatterns {
		switch pat {
		case "nil":
			nilOK = true
		case "err":
			errOK = true
		case "ok":
			okOK = true
		}
	}
	switch c := a.condition.(type) {
	case *NilLiteral:
		nilOK = true
	case *Identifier:
		switch c.Value {
		case "nil":
			nilOK = true
		case "err":
			errOK = true
		case "ok":
			okOK = true
		}
	}
	// `ok ->` is the dotVal arm; `ok(cond) ->` carries a raw boolean condition.
	if a.isDotVal || a.isRawCond {
		okOK = true
	}
	return nilOK, errOK, okOK
}

// rejectItInNonOkArm rejects `it` inside a catch-all `->` arm that is not
// provably the ok arm.
//
// `it` is meaningful only where the matched option is known to hold a value:
// an explicit `ok ->` arm, or a catch-all `->` that runs only after BOTH `nil`
// and `err` have been claimed by earlier arms. A bare `->` with no such arms
// still receives nil/err, so `it` there is uninitialized payload — the
// compiler must say so instead of silently emitting a deref.
func (l *lowerer) rejectItInNonOkArm(sm *SurfaceMatch) {
	if sm == nil {
		return
	}
	// 只對 option 型別的 match 生效。enum / 純值 match 的 catch-all 臂裡 `it`
	// 就是被匹配的值本身（不存在 nil / err 的可能），不能報錯。
	if ident, ok := sm.Matched.(*Identifier); ok {
		if t, ok := l.p.sem.FuncVarType(l.curFuncName, ident.Value); ok {
			if _, isEnum := l.p.sem.EnumVariantsOf(t); isEnum {
				return
			}
			if !strings.HasPrefix(t, "?") && !strings.Contains(t, "|") {
				return
			}
		}
	} else {
		// 非識別符主體：無從判定是否為 option，不報。裸 match（`{ cond -> ... }`）
		// 沒有主體，`it` 指的是外層 match 綁的那個值，也不該由這裡判定。
		return
	}
	var nilCovered, errCovered, okCovered bool
	for i := range sm.Arms {
		n, e, o := matchArmCovers(&sm.Arms[i])
		nilCovered = nilCovered || n
		errCovered = errCovered || e
		okCovered = okCovered || o
	}
	// The catch-all arm gets every case the explicit arms did NOT claim. `it`
	// has a single well-defined meaning only when exactly one case is left:
	//   nil+err claimed -> `->` is ok  (it = the value)
	//   ok+nil  claimed -> `->` is err (it = the error message)
	//   ok+err  claimed -> `->` is nil (it = nil)
	// Anything else means `it` may be any of several unrelated things.
	remaining := 0
	if !nilCovered {
		remaining++
	}
	if !errCovered {
		remaining++
	}
	if !okCovered {
		remaining++
	}
	if remaining <= 1 {
		return
	}
	for i := range sm.Arms {
		a := &sm.Arms[i]
		// Only the catch-all `->` arm is ambiguous. `ok ->` (isDotVal) and
		// explicit pattern arms are fine.
		if !a.isWildcard || a.isDotVal || a.skipItBinding {
			continue
		}
		if !armUsesIt(a.body) {
			continue
		}
		line, col := a.pos.Line, a.pos.Column
		if line == 0 && a.body != nil && len(a.body.Statements) > 0 {
			line = a.body.Statements[0].Pos().Line
			col = a.body.Statements[0].Pos().Column
		}
		var missing []string
		if !nilCovered {
			missing = append(missing, "nil")
		}
		if !errCovered {
			missing = append(missing, "err")
		}
		if !okCovered {
			missing = append(missing, "ok")
		}
		l.p.saveError(fmt.Sprintf("line %d, column %d: `it` is not valid in this arm: the catch-all `->` arm may receive %s; claim them with explicit arms (e.g. `nil ->` / `err ->`) or use `ok ->` before reading `it`",
			line, col, strings.Join(missing, " / ")))
	}
}

// armUsesIt reports whether an arm body references the implicit `it` binding.
// Nested matches are NOT scanned except for their subject: their arms bind
// their own `it`, which belongs to the inner match, not this one.
func armUsesIt(body *BlockStatement) bool {
	if body == nil {
		return false
	}
	seen := make(map[uintptr]bool)
	var scan func(v reflect.Value) bool
	scan = func(v reflect.Value) bool {
		switch v.Kind() {
		case reflect.Ptr:
			if v.IsNil() {
				return false
			}
			p := v.Pointer()
			if seen[p] {
				return false
			}
			seen[p] = true
			switch n := v.Interface().(type) {
			case *Identifier:
				return n.Value == "it"
			case *SurfaceMatch:
				return n.Matched != nil && scan(reflect.ValueOf(n.Matched))
			}
			return scan(v.Elem())
		case reflect.Interface:
			if v.IsNil() {
				return false
			}
			if v.Elem().Type() == surfaceMatchPtrType {
				sm, ok := v.Elem().Interface().(*SurfaceMatch)
				if ok {
					return sm.Matched != nil && scan(reflect.ValueOf(sm.Matched))
				}
			}
			return scan(v.Elem())
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if scan(v.Index(i)) {
					return true
				}
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if scan(v.Field(i)) {
					return true
				}
			}
		}
		return false
	}
	return scan(reflect.ValueOf(body))
}

// lowerSurfaceMatch 將單個表層 match 節點展開為核心 AST。
// 先自底向上處理 matched 與各 arm 內部（嵌套 match），再建 if 鏈。
func (l *lowerer) lowerSurfaceMatch(sm *SurfaceMatch) Expression {
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		if ident, ok := sm.Matched.(*Identifier); ok {
			fmt.Fprintf(os.Stderr, "[debug-it] lowerSurfaceMatch: matched=%q curFuncName=%q\n", ident.Value, l.curFuncName)
		} else if sm.Matched == nil {
			fmt.Fprintf(os.Stderr, "[debug-it] lowerSurfaceMatch: matched=nil (bare match) curFuncName=%q\n", l.curFuncName)
		} else {
			fmt.Fprintf(os.Stderr, "[debug-it] lowerSurfaceMatch: matched=%T curFuncName=%q\n", sm.Matched, l.curFuncName)
		}
	}
	// 先把析構綁定名/型別登記進符號表，內層 match 才能解析其主體型別。
	l.preRegisterEnumArmBindings(sm)
	// `it` 只在「可證明是 ok」的臂裡有效：catch-all `->` 臂若前面沒有把 nil 與
	// err 都處理掉，走到這裡時值仍可能是 nil 或 err，讀 `it` 沒有意義（且會
	// 讀到未初始化的載荷）。此時若臂體用到 `it`，直接報錯——必須改用 `ok ->`
	// 或補上 `nil ->` / `err ->`。檢查必須在 walk 之前做：walk 會把內層 match
	// 展開成 if/else 並插入它自己的合成 `let it`，那之後就分不清 `it` 屬於哪層。
	l.rejectItInNonOkArm(sm)
	l.walk(reflect.ValueOf(&sm.Matched))
	for i := range sm.Arms {
		a := &sm.Arms[i]
		l.walk(reflect.ValueOf(&a.condition))
		for j := range a.multiValuePatterns {
			l.walk(reflect.ValueOf(&a.multiValuePatterns[j]))
		}
		if a.body != nil {
			l.walk(reflect.ValueOf(a.body))
		}
	}

	if sm.Matched == nil {
		// Sync curFuncName to parser for function-scoped VarType lookups.
		savedFuncName := l.p.curFuncName
		l.p.curFuncName = l.curFuncName
		result := l.p.buildBareMatchDesugar(sm.Token, sm.Arms)
		l.p.curFuncName = savedFuncName
		if ifExpr, ok := result.(*IfExpression); ok && ifExpr != nil {
			l.p.sem.SetOpeningBraceComment(ifExpr, sm.OpeningBraceComment)
			// 轉移外層 `}` 位置到 IfExpression，讓 EndPos() 能準確返回 `}` 行號。
			ifExpr.MatchEndPos = sm.RBracePos
		}
		return result
	}
	// Sync curFuncName to parser for function-scoped VarType lookups.
	savedFuncName := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	result := l.p.buildMatchDesugar(sm)
	l.p.curFuncName = savedFuncName
	if ifExpr, ok := result.(*IfExpression); ok && ifExpr != nil {
		ifExpr.MatchEndPos = sm.RBracePos
	}
	return result
}

// ---- desugar 構建器（原 desugar.go，現屬 lowering 層） ----

// buildBareMatchDesugar 建立 if/elif/else 鏈（無 matched expression，條件直接使用）
func (p *Parser) buildBareMatchDesugar(tok lexer.Token, arms []matchArm) Expression {
	if len(arms) == 0 {
		return nil
	}

	var ifExpr *IfExpression
	var defaultBody *BlockStatement // 最內層非 dotVal wildcard body
	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]
		if arm.isWildcard {
			if ifExpr == nil {
				if defaultBody != nil && arm.isDotVal {
					// dotVal arm (ok->) after a regular -> wildcard (else):
					// treat as conditional arm: if ok { body } else { defaultBody }
					newIf := &IfExpression{
						Token:           tok,
						Condition:       &Identifier{Token: tok, Value: "ok"},
						Consequence:     arm.body,
						Alternative:     defaultBody,
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
						DotValBody:      arm.body,
					}
					p.sem.SetRTFlag(newIf, RTBareMatch|RTMatchWildcard)
					ifExpr = newIf
				} else if arm.isDotVal {
					// dotVal arm without prior default — treat as standalone ok condition
					newIf := &IfExpression{
						Token:           tok,
						Condition:       &Identifier{Token: tok, Value: "ok"},
						Consequence:     arm.body,
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
						DotValBody:      arm.body,
					}
					p.sem.SetRTFlag(newIf, RTBareMatch|RTMatchWildcard)
					ifExpr = newIf
				} else {
					// Regular wildcard (->): store as defaultBody for next arm's else
					ifExpr = &IfExpression{
						Token:       tok,
						Condition:   &IntegerLiteral{Token: tok, Value: 1},
						Consequence: arm.body,
					}
					p.sem.SetRTFlag(ifExpr, RTBareMatch|RTMatchWildcard)
				}
			} else {
				if arm.isDotVal {
					// dotVal arm (ok->) when other arms already processed:
					// wrap as outer condition
					newIf := &IfExpression{
						Token:       tok,
						Condition:   &Identifier{Token: tok, Value: "ok"},
						Consequence: arm.body,
						Alternative: &BlockStatement{
							Token:      tok,
							Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
						},
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
						DotValBody:      arm.body,
					}
					p.sem.SetRTFlag(newIf, RTBareMatch|RTMatchWildcard)
					ifExpr = newIf
				} else {
					if ifExpr.Alternative == nil {
						ifExpr.Alternative = arm.body
					}
				}
			}
			if !arm.isDotVal && defaultBody == nil {
				defaultBody = arm.body
			}
		} else {
			var equalityPattern Expression
			var rangePattern *RangeExpression
			if rng, isRange := arm.condition.(*RangeExpression); isRange {
				rangePattern = rng
			} else {
				equalityPattern = arm.condition
			}
			newIf := &IfExpression{
				Token:           tok,
				Condition:       arm.condition,
				Consequence:     arm.body,
				Alternative:     nil,
				EqualityPattern: equalityPattern,
				RangePattern:    rangePattern,
			}
			p.sem.SetRTFlag(newIf, RTBareMatch)
			if ifExpr != nil {
				newIf.Alternative = &BlockStatement{
					Token:      tok,
					Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
				}
			}
			ifExpr = newIf
		}
	}

	return ifExpr
}

// buildMatchDesugar 建立 if/elif/else 鏈
//
// 對 option match（含 err/nil arm），直接使用 `matched == err` / `matched == nil`
// 比較，由 transpiler 的 generateInfixI1 識別 %option 變數並生成 tag 比較的 LLVM IR。
// wildcard arm（含 ok/val/->）作為 else 分支。
//
// 型別資訊（matched 變數型別、枚舉變體）一律取自 sm 的解析期快照，
// 不讀 parser 的活動符號表（lowering 延後執行時符號表已是整檔終態）。
func (p *Parser) buildMatchDesugar(sm *SurfaceMatch) Expression {
	tok, matched, arms := sm.Token, sm.Matched, sm.Arms
	if len(arms) == 0 {
		return nil
	}

	// 索引表達式主體（如 `parts[i]`）的安全索引回傳 Option（?elem）。當 match
	// 是 option match（arm 含 ok/nil/err 或 `ok ->`）時，把主體歸一化為合成的
	// ?elem 區域變數，複用識別符 option-match desugar 路徑：
	//
	//   `parts[i]: { ok -> ... -> ... }`  →
	//   `if 1 { __match_subj_L_C ?elem = parts[i]; __match_subj_L_C: { ok -> ... -> ... } }`
	//
	// 這樣 `it` 正確綁定 payload、條件走 tag 比較，而非退化成
	// `streq(parts[i], '')` 字串比較（那會讓 `s = it` 變成 void，codegen 後
	// 報 "receiver has no slot"）。歸一化只在 option match 上觸發：值 match
	// （如 `arr[i]: { 0 -> }`）保持原本的裸元素比對，不受影響。
	origMatched := matched
	subjName := ""
	subjElem := ""
	if idx, ok := matched.(*IndexExpression); ok && p.isSafeIndexBase(idx) {
		isOptMatch := false
		for _, a := range arms {
			if a.isDotVal {
				isOptMatch = true
				break
			}
			if ident, ok := a.condition.(*Identifier); ok {
				if ident.Value == "ok" || ident.Value == "nil" || ident.Value == "err" {
					isOptMatch = true
					break
				}
			} else if _, ok := a.condition.(*NilLiteral); ok {
				isOptMatch = true
				break
			}
		}
		if isOptMatch {
			if elem := p.inferIndexElemType(idx); elem != "" {
				subjElem = elem
				subjName = fmt.Sprintf("__match_subj_%d_%d", tok.Line, tok.Column)
				subjType := "?" + elem
				if strings.HasPrefix(elem, "?") {
					subjType = elem
				}
				p.sem.SetFuncVarType(p.curFuncName, subjName, subjType)
				p.setVarType(subjName, subjType)
				matched = &Identifier{Token: tok, Value: subjName}
			}
		}
	}

	// 類型推斷結果取自語義副表（Resolver pass 寫入 p.sem），不再依賴解析期快照。
	// Use function-scoped lookup (curFuncName) to avoid cross-function type
	// pollution when same-named locals exist in different functions.
	matchedVarType := ""
	isEnumType := false
	var enumVariants []string
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := p.sem.FuncVarType(p.curFuncName, ident.Value); ok {
			matchedVarType = t
			if vs, ok := p.sem.EnumVariantsOf(t); ok {
				isEnumType = true
				enumVariants = vs
			}
			if os.Getenv("NOLANG_DEBUG_IT") != "" {
				fmt.Fprintf(os.Stderr, "[debug-it] buildMatchDesugar: matched=%q matchedVarType=%q\n", ident.Value, matchedVarType)
			}
		} else {
			if os.Getenv("NOLANG_DEBUG_IT") != "" {
				fmt.Fprintf(os.Stderr, "[debug-it] buildMatchDesugar: matched=%q FuncVarType NOT FOUND curFunc=%q\n", ident.Value, p.curFuncName)
				// Dump FuncVarTypes for curFuncName to see what's registered
				if vars, ok := p.sem.FuncVarTypes[p.curFuncName]; ok {
					for k, v := range vars {
						fmt.Fprintf(os.Stderr, "[debug-it]   FuncVarTypes[%q][%q]=%q\n", p.curFuncName, k, v)
					}
				} else {
					fmt.Fprintf(os.Stderr, "[debug-it]   FuncVarTypes[%q] map not found\n", p.curFuncName)
				}
			}
		}
	}

	// Determine element type from option type for per-arm `it` type inference.
	// For ?i64, elemType = "i64"; for result unions (i64 | err), extract the payload type.
	elemType := ""
	if matchedVarType != "" {
		if strings.HasPrefix(matchedVarType, "?") {
			elemType = strings.TrimPrefix(matchedVarType, "?")
		} else if strings.Contains(matchedVarType, "|") {
			// 聯合型別（如 i64 | err）：提取 payload 型別（i64）
			elemType = unionElemType(matchedVarType)
		} else if isEnumType {
			// For enum match, set elemType to trigger per-arm it binding path
			elemType = matchedVarType
		}
	}

	// Shared it binding (used for hasRawCond or fallback)
	itStmt := p.buildItBinding(tok, matched)

	// For hasRawCond, set the element type on the shared binding for LSP inference
	if itStmt != nil && elemType != "" {
		itStmt.Type = &NamedType{Value: elemType}
	}

	// Check if any arm uses ok(cond) — if so, `it` must be bound BEFORE the if-chain
	// (not inside arm bodies) because the condition references `it`.
	hasRawCond := false
	for _, arm := range arms {
		if arm.isRawCond {
			hasRawCond = true
			break
		}
	}

	// Build from last to first (inside-out)
	var ifExpr *IfExpression
	var defaultBody *BlockStatement       // 最內層 wildcard body（直接作為 else，避免 if 1 {} 包裝）
	var defaultDotValBody *BlockStatement // track val branch body separately

	// Check which variants are explicitly listed (for computing else arm complement)
	hasExplicitOk, hasExplicitErr, hasExplicitNil := false, false, false
	// For enum types, track which enum variant identifiers are listed
	enumListedVariants := make(map[string]bool)
	matchedIsEnum := isEnumType

	// 標籤列舉析構綁定泛化：`some(v) -> ...`（不限於 ok）。
	// 解析期無法判定（尚不知被匹配變數的靜態型別），故在此依 matchedVarType 決定：
	// 僅當 X 是本枚舉的「帶載荷變體」且括號內為單一裸識別符時，才視為析構綁定——
	// 把 arm.condition 規範化為 Identifier{X}，載荷綁定到 v。其餘 IDENT(expr) 形態
	// （如 ok(it > 127) 這類條件臂）保持原樣，不受影響。
	if matchedIsEnum {
		for i := range arms {
			a := &arms[i]
			if a.bindingName != "" || len(a.bindingNames) > 0 {
				continue
			}
			call, ok := a.condition.(*CallExpression)
			if !ok || len(call.Arguments) == 0 {
				continue
			}
			fn, ok := call.Function.(*Identifier)
			if !ok {
				continue
			}
			// 括號內必須全為裸識別符（綁定名）。
			names := make([]string, 0, len(call.Arguments))
			allBare := true
			for _, arg := range call.Arguments {
				id, isId := arg.(*Identifier)
				if !isId {
					allBare = false
					break
				}
				names = append(names, id.Value)
			}
			if !allBare {
				continue
			}
			fieldTypes := p.enumVariantFieldTypes(matchedVarType, fn.Value)
			if len(fieldTypes) == 0 || len(fieldTypes) != len(names) {
				continue
			}
			if len(names) == 1 {
				a.bindingName = names[0]
			} else {
				a.bindingNames = names
				a.bindingFields = fieldTypes
			}
			a.bindingVariant = fn.Value
			a.condition = &Identifier{Token: fn.Token, Value: fn.Value}
		}
	}

	for _, a := range arms {
		if len(a.multiOptionPatterns) > 0 {
			// Combined option patterns: mark all as explicit
			for _, pat := range a.multiOptionPatterns {
				if pat == "err" {
					hasExplicitErr = true
				} else if pat == "nil" {
					hasExplicitNil = true
				} else if pat == "ok" {
					hasExplicitOk = true
				}
			}
		} else if a.isDotVal {
			hasExplicitOk = true
		} else if a.condition != nil {
			if ident, ok := a.condition.(*Identifier); ok {
				if ident.Value == "err" {
					hasExplicitErr = true
				} else if ident.Value == "nil" {
					hasExplicitNil = true
				} else if ident.Value == "ok" {
					hasExplicitOk = true
				} else if matchedIsEnum {
					enumListedVariants[ident.Value] = true
				}
			} else if _, ok := a.condition.(*NilLiteral); ok {
				hasExplicitNil = true
			}
		}
	}

	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]

		// Create per-arm `it` binding with correct unwrapped type for LSP inference.
		// For ?i64: err arm → it: err, nil arm → it: nil, ok arm → it: i64
		// The per-arm path runs even when elemType == "" (matched option type
		// unknown at parse time, e.g. a method-call result resolved later): in
		// that case buildItBindingForArm returns nil, and we only fall back to the
		// shared binding for the ok/else arm (safe — the option is non-nil there,
		// so the struct-deref codegen is guarded by the live variant). Sentinel
		// (nil/err) arms must NOT receive any `it` binding in that case, since the
		// shared Type=nil binding would emit an unconditional deref of the option's
		// data field, which is 0 (null) when the option is nil/err → segfault.
		if itStmt != nil && !hasRawCond {
			var armType string
			// 標籤列舉帶載荷的變體臂：armType/綁定型別取「載荷型別」而非變體名，
			// 使 `some(v) ->` 的 v 與隱含 `it` 得到正確型別（如 i64/f64/str）。
			armBindingType := ""
			skipItBinding := false
			if arm.skipItBinding {
				// Generated sentinel arms (e.g. ?= / #{index-out} err/nil arms)
				// never reference `it`; opt out so a sentinel arm's %str-long/nil
				// `it` type can't clobber the ok arm's element-type `it`.
				skipItBinding = true
			}
			if len(arm.multiOptionPatterns) > 0 {
				// Combined option patterns: compute armType from pattern set
				hasOk, hasErr, hasNil := false, false, false
				for _, pat := range arm.multiOptionPatterns {
					switch pat {
					case "ok":
						hasOk = true
					case "err":
						hasErr = true
					case "nil":
						hasNil = true
					}
				}
				if hasOk && hasErr && hasNil {
					armType = "ok_err_nil"
				} else if hasOk && hasErr {
					armType = "ok_err"
				} else if hasOk && hasNil {
					armType = "ok_nil"
				} else if hasErr && hasNil {
					armType = "else" // err | nil
				} else if hasOk {
					armType = "ok"
				} else if hasErr {
					armType = "err"
				} else if hasNil {
					armType = "nil"
				}
			} else if arm.isWildcard {
				if arm.isDotVal {
					armType = "ok" // ok-> is explicit ok case
					// 標籤列舉：`ok(v) ->` 這類走 dotVal 路徑的析構綁定，其綁定型別
					// 同樣取載荷型別（由 bindingVariant 在本枚舉內解析）。
					if matchedIsEnum && arm.bindingVariant != "" {
						if pt := p.enumVariantPayload(matchedVarType, arm.bindingVariant); pt != "" {
							armType = pt
							armBindingType = pt
						}
					}
				} else {
					// Compute complement: which variants remain for -> else arm
					if matchedIsEnum {
						// For enum types, compute complement dynamically
						var remaining []string
						for _, v := range enumVariants {
							if !enumListedVariants[v] {
								remaining = append(remaining, v)
							}
						}
						if len(remaining) == 0 {
							// All enum variants listed — the else arm is dead code
							skipItBinding = true
							pos := arm.pos
							if pos.Line == 0 {
								pos = arm.body.Pos()
							}
							p.saveWarning(fmt.Sprintf("line %d, column %d: '->' arm is unreachable: all enum variants have been listed",
								pos.Line, pos.Column))
						} else {
							armType = strings.Join(remaining, " | ")
						}
					} else {
						// Three variants: ok(elemType), err, nil
						okListed, errListed, nilListed := hasExplicitOk, hasExplicitErr, hasExplicitNil
						if okListed && errListed && nilListed {
							// All three option variants listed — the else arm is dead code
							skipItBinding = true
							pos := arm.pos
							if pos.Line == 0 {
								pos = arm.body.Pos()
							}
							p.saveWarning(fmt.Sprintf("line %d, column %d: '->' arm is unreachable: all option variants (ok, err, nil) have been listed",
								pos.Line, pos.Column))
						} else if okListed && !errListed && nilListed {
							armType = "err" // only err remains
						} else if okListed && errListed && !nilListed {
							armType = "nil" // only nil remains
						} else if okListed && !errListed && !nilListed {
							armType = "else" // err | nil
						} else if !okListed && errListed && nilListed {
							armType = "ok" // only i64 remains
						} else if !okListed && !errListed && nilListed {
							armType = "ok_err" // i64 | err
						} else if !okListed && errListed && !nilListed {
							armType = "ok_nil" // i64 | nil
						} else if !okListed && !errListed && !nilListed {
							armType = "ok_err_nil" // i64 | err | nil
						}
					}
				}
			} else if ident, ok := arm.condition.(*Identifier); ok {
				if ident.Value == "err" || ident.Value == "nil" {
					armType = ident.Value
				} else if ident.Value == "ok" {
					armType = "ok"
				} else if matchedIsEnum {
					armType = ident.Value // Use variant name as arm type for it binding
					// 帶載荷變體（some(v) ->）：綁定型別 = 載荷型別。
					// 由被匹配變數的靜態型別（matchedVarType）在解析期補全查得。
					if pt := p.enumVariantPayload(matchedVarType, ident.Value); pt != "" {
						armType = pt
						armBindingType = pt
					}
				}
			} else if _, ok := arm.condition.(*NilLiteral); ok {
				armType = "nil"
			} else if arm.isDotVal {
				// ok-> arm (dotVal wildcard): it should be the unwrapped elemType
				armType = "ok"
			}
			if armType != "" {
				// 析構綁定 `ok(v) -> ...`：把載荷綁到 v 而非隱含的 `it`。
				// 嵌套 match 時這能保住外層的 `it`（否則內層會把它覆蓋掉）。
				bindName := arm.bindingName
				if bindName == "" {
					bindName = "it"
				}
				// Use per-arm position so walker/index can distinguish synthetic bindings
				var armTok lexer.Token
				if arm.condition != nil {
					pos := arm.condition.Pos()
					armTok = lexer.Token{Type: lexer.IDENT, Literal: bindName, Line: pos.Line, Column: pos.Column}
				} else if len(arm.body.Statements) > 0 {
					pos := arm.body.Statements[0].Pos()
					armTok = lexer.Token{Type: lexer.IDENT, Literal: bindName, Line: pos.Line, Column: pos.Column}
				} else {
					armTok = tok
				}
				if arm.bindingName != "" {
					// 讓 checker / codegen 知道 v 的型別（等同 `it` 在 ok 臂的 elemType）。
					// DeclaredVars 必須註冊，否則 ValidateUndefinedVars 會報
					// "'v' is not defined"（`it` 是 checker 裡寫死的關鍵字，自訂名沒有這待遇）。
					if p.sem.DeclaredVars == nil {
						p.sem.DeclaredVars = make(map[string]bool)
					}
					p.sem.DeclaredVars[arm.bindingName] = true
					bt := elemType
					if armBindingType != "" {
						bt = armBindingType
					}
					if bt != "" {
						p.sem.SetFuncVarType(p.curFuncName, arm.bindingName, bt)
						p.setVarType(arm.bindingName, bt)
					}
				}
				if armIt := p.buildItBindingForArm(armTok, matched, armType, elemType, matchedVarType, isEnumType, bindName); armIt != nil {
					// Set the synthetic end position to cover the arm body
					bodyEnd := arm.body.EndPos()
					if bodyEnd.Line == 0 && bodyEnd.Column == 0 && len(arm.body.Statements) > 0 {
						bodyEnd = arm.body.Statements[len(arm.body.Statements)-1].EndPos()
					}
					armIt.SyntheticEnd = bodyEnd
					// 多欄位析構綁定（`rect(w, h) -> ...`）：按位置把 it.f0 / it.f1
					// 綁到各欄位名。逆序 prepend，使最終語句順序為 `it; w; h; body`。
					if len(arm.bindingNames) > 0 {
						for j := len(arm.bindingNames) - 1; j >= 0; j-- {
							fname := arm.bindingNames[j]
							ftype := ""
							if j < len(arm.bindingFields) {
								ftype = arm.bindingFields[j]
							}
							if p.sem.DeclaredVars == nil {
								p.sem.DeclaredVars = make(map[string]bool)
							}
							p.sem.DeclaredVars[fname] = true
							if ftype != "" {
								p.sem.SetFuncVarType(p.curFuncName, fname, ftype)
								p.setVarType(fname, ftype)
							}
							extract := &LetStatement{
								Token: tok,
								Name:  &Identifier{Token: tok, Value: fname},
								Value: &DotExpression{
									Token:    tok,
									Receiver: &Identifier{Token: tok, Value: bindName},
									Property: fmt.Sprintf("f%d", j),
								},
								IsSynthetic: true,
							}
							arm.body = p.prependStmt(arm.body, extract)
						}
					}
					arm.body = p.prependStmt(arm.body, armIt)
				} else if !skipItBinding && !isSentinelArmType(armType) {
					// ok/else arm whose matched type is unknown at parse time:
					// fall back to the shared `it` binding (best-effort). Safe,
					// because `it` is only meaningful in the ok arm where the
					// option is non-nil, so the struct-deref is guarded by the
					// live variant. Sentinel (nil/err) arms must NOT receive any
					// `it` binding here — doing so would emit an unconditional
					// deref of the option's data field (null when nil/err).
					arm.body = p.prependStmt(arm.body, p.namedItBinding(itStmt, bindName))
				}
				// Sentinel arms with unknown matched type: intentionally bind nothing.
			} else if !skipItBinding && !arm.isWildcard {
				// Non-wildcard arm: safe to bind shared `it`.
				// Wildcard arms (->) with unknown matched type must NOT receive
				// shared `it` binding: when the matched var is an option and the
				// arm is the else/catch-all, the option may be nil (data=0),
				// and the shared binding unconditionally dereferences the data
				// field, causing null pointer dereference / segfault.
				arm.body = p.prependStmt(arm.body, itStmt)
			}
		}

		if arm.isWildcard {
			if ifExpr == nil {
				if defaultBody != nil && arm.isDotVal {
					// dotVal arm (ok->) after a regular -> wildcard (else):
					// treat as conditional arm: if matched == ok { body } else { defaultBody }
					cond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					}
					newIf := &IfExpression{
						Token:           tok,
						Condition:       cond,
						Consequence:     arm.body,
						Alternative:     defaultBody,
						MatchedExpr:     matched,
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
						DotValBody:      arm.body,
					}
					p.sem.SetRTFlag(newIf, RTBareMatch|RTMatchWildcard)
					ifExpr = newIf
				} else {
					// 最內層 wildcard：儲存 body 作為下一個條件 arm 的 else
					defaultBody = arm.body
					if arm.isDotVal {
						defaultDotValBody = arm.body
					}
				}
			} else {
				if arm.isDotVal {
					// ok-> when other arms already processed (e.g., {ok ->, nil ->, ->}):
					// wrap as outer condition instead of overwriting the alternative
					cond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					}
					newIf := &IfExpression{
						Token:       tok,
						Condition:   cond,
						Consequence: arm.body,
						Alternative: &BlockStatement{
							Token:      tok,
							Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
						},
						MatchedExpr:     matched,
						EqualityPattern: &Identifier{Token: tok, Value: "ok"},
						DotValBody:      arm.body,
					}
					p.sem.SetRTFlag(newIf, RTBareMatch|RTMatchWildcard)
					ifExpr = newIf
				} else {
					ifExpr.Alternative = arm.body
				}
			}
		} else {
			// 構造 match 條件
			var cond Expression
			var rangePattern *RangeExpression
			var equalityPattern Expression
			var optionPatterns []string
			var valuePatterns []Expression
			var rawCond Expression
			if len(arm.multiOptionPatterns) > 0 {
				// Combined option patterns: (matched == p1) || (matched == p2) || ...
				optionPatterns = arm.multiOptionPatterns
				for i, pat := range arm.multiOptionPatterns {
					patCond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: pat},
					}
					if i == 0 {
						cond = patCond
					} else {
						cond = &InfixExpression{
							Token:    tok,
							Left:     cond,
							Operator: "||",
							Right:    patCond,
						}
					}
				}
			} else if len(arm.multiValuePatterns) > 0 {
				// Combined value patterns: 1 || 3 || 5 → (matched == 1) || (matched == 3) || (matched == 5)
				valuePatterns = arm.multiValuePatterns
				for i, pat := range arm.multiValuePatterns {
					patCond := &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    pat,
					}
					if i == 0 {
						cond = patCond
					} else {
						cond = &InfixExpression{
							Token:    tok,
							Left:     cond,
							Operator: "||",
							Right:    patCond,
						}
					}
				}
			} else if arm.isRawCond {
				// ok(cond) arm: condition is (matched == ok) && cond
				rawCond = arm.condition
				cond = &InfixExpression{
					Token: tok,
					Left: &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					},
					Operator: "&&",
					Right:    arm.condition,
				}
			} else if rng, isRange := arm.condition.(*RangeExpression); isRange {
				// Range condition: [a..b] → matched >= a && matched <= b
				rangePattern = rng
				cond = p.desugarRangeCondition(tok, matched, rng)
			} else {
				// matched == condition
				// 對 option 變數，condition 為 err/nil 時由 transpiler 生成 tag 比較
				equalityPattern = arm.condition
				cond = &InfixExpression{
					Token:    tok,
					Left:     matched,
					Operator: "==",
					Right:    arm.condition,
				}
			}
			newIf := &IfExpression{
				Token:           tok,
				Condition:       cond,
				Consequence:     arm.body,
				Alternative:     nil,
				MatchedExpr:     matched,
				RangePattern:    rangePattern,
				EqualityPattern: equalityPattern,
				OptionPatterns:  optionPatterns,
				ValuePatterns:   valuePatterns,
				RawCond:         rawCond,
			}
			p.sem.SetRTFlag(newIf, RTBareMatch)
			if ifExpr != nil {
				newIf.Alternative = &BlockStatement{
					Token:      tok,
					Statements: []Statement{&ExpressionStatement{Token: tok, Expression: ifExpr}},
				}
			} else if defaultBody != nil {
				// 直接使用 wildcard body 作為 else，避免 if 1 {} 包裝
				newIf.Alternative = defaultBody
				if defaultDotValBody == defaultBody {
					newIf.DotValBody = defaultBody
				}
			}
			ifExpr = newIf
		}
	}

	// 若所有 arm 都是 wildcard，或只有 wildcard 而無條件 arm
	if ifExpr == nil {
		if defaultBody != nil {
			// 唯一 arm 是 wildcard。
			// 對 ok-> (dotVal) arm，生成 `matched == ok` 條件而非硬編碼 true。
			// 否則當 option 為 nil 時仍進入 ok 分支，解引用空指針導致段錯誤。
			// ok-> 語法本身就表示匹配 option 的 ok 變體，無需依賴類型推斷。
			if defaultDotValBody == defaultBody {
				ifExpr = &IfExpression{
					Token: tok,
					Condition: &InfixExpression{
						Token:    tok,
						Left:     matched,
						Operator: "==",
						Right:    &Identifier{Token: tok, Value: "ok"},
					},
					Consequence:     defaultBody,
					MatchedExpr:     matched,
					EqualityPattern: &Identifier{Token: tok, Value: "ok"},
					DotValBody:      defaultBody,
				}
				p.sem.SetRTFlag(ifExpr, RTBareMatch|RTMatchWildcard)
			} else {
				// 非 dotVal wildcard：用 if 1 {} 包裝（無法避免）
				ifExpr = &IfExpression{
					Token:       tok,
					Condition:   &IntegerLiteral{Token: tok, Value: 1},
					Consequence: defaultBody,
					MatchedExpr: matched,
				}
				p.sem.SetRTFlag(ifExpr, RTBareMatch|RTMatchWildcard)
			}
		} else {
			return nil
		}
	}

	// When ok(cond) arms exist, wrap the if-chain in `if 1 { it = matched; <if-chain> }`
	// so that `it` is bound before the condition is evaluated.
	if hasRawCond && itStmt != nil {
		ifExpr = &IfExpression{
			Token:     tok,
			Condition: &IntegerLiteral{Token: tok, Value: 1},
			Consequence: &BlockStatement{
				Token: tok,
				Statements: []Statement{
					itStmt,
					&ExpressionStatement{Token: tok, Expression: ifExpr},
				},
			},
			MatchedExpr: matched,
		}
		p.sem.SetRTFlag(ifExpr, RTBareMatch|RTMatchWrapper)
	}

	// 索引主體歸一化：把 `let __match_subj ?elem = <orig index>` 包在 `if 1 { ... }`
	// 外層，使 match 主體（__match_subj）成為安全索引產生的 %option 變數，
	// 後續 arm 的 `it` 綁定與 `== ok/nil/err` 條件都走識別符 option 路徑。
	if subjName != "" && subjElem != "" && ifExpr != nil {
		subjAssign := &LetStatement{
			Token:       tok,
			Name:        &Identifier{Token: tok, Value: subjName},
			IsSynthetic: true,
			Type:        &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: subjElem}},
			Value:       origMatched,
		}
		ifExpr = &IfExpression{
			Token:     tok,
			Condition: &IntegerLiteral{Token: tok, Value: 1},
			Consequence: &BlockStatement{
				Token: tok,
				Statements: []Statement{
					subjAssign,
					&ExpressionStatement{Token: tok, Expression: ifExpr},
				},
			},
			MatchedExpr: &Identifier{Token: tok, Value: subjName},
		}
		p.sem.SetRTFlag(ifExpr, RTBareMatch|RTMatchWrapper)
	}

	return ifExpr
}

// desugarRangeCondition 將 match arm 中的 RangeExpression 條件 desugar 為布爾表達式。
// 有界（4 種）：
//
//	[a..b] → matched >= a && matched <= b
//	[a..b) → matched >= a && matched < b
//	(a..b] → matched > a && matched <= b
//	(a..b) → matched > a && matched < b
//
// 無上限（4 種，End=nil）：
//
//	[a..)  → matched >= a
//	[a..]  → matched >= a
//	(a..)  → matched > a
//	(a..]  → matched > a
//
// 無下限（4 種，Start=nil）：
//
//	[..b]  → matched <= b
//	[..b)  → matched < b
//	(..b]  → matched <= b
//	(..b)  → matched < b
//
// 完全無界（4 種，Start=nil 且 End=nil）：
//
//	[..]   → true (匹配所有值)
//	[..)   → true
//	(..]   → true
//	(..)   → true
func (p *Parser) desugarRangeCondition(tok lexer.Token, matched Expression, rng *RangeExpression) Expression {
	var leftOp string
	if rng.LeftInc {
		leftOp = ">="
	} else {
		leftOp = ">"
	}
	var rightOp string
	if rng.RightInc {
		rightOp = "<="
	} else {
		rightOp = "<"
	}

	if rng.Start != nil && rng.End != nil {
		leftCond := &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: leftOp,
			Right:    rng.Start,
		}
		rightCond := &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: rightOp,
			Right:    rng.End,
		}
		return &InfixExpression{
			Token:    tok,
			Left:     leftCond,
			Operator: "&&",
			Right:    rightCond,
		}
	}
	if rng.Start != nil {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: leftOp,
			Right:    rng.Start,
		}
	}
	if rng.End != nil {
		return &InfixExpression{
			Token:    tok,
			Left:     matched,
			Operator: rightOp,
			Right:    rng.End,
		}
	}
	// 完全無界 [..], [..), (..], (..) — 永真，匹配所有值
	return &IntegerLiteral{Token: tok, Value: 1}
}

// buildItBinding creates `it = matched` LetStatement when matched is an Identifier.
// Returns nil if matched is not an Identifier.
//
// When the variable has a known parse-time type (in varDeclTypes), the binding
// is created normally. When the type is unknown at parse time (e.g. the variable
// was assigned from a generic method call whose return type is only resolved
// after monomorphization, like v = m.get('a') where get returns ?v), a fallback
// binding with Type = nil is created. The codegen determines the type from
// g.varTypes at generation time.
func (p *Parser) buildItBinding(tok lexer.Token, matched Expression) *LetStatement {
	// 識別符與索引表達式（如 `parts[i]`）都可作為 option-match 主體：索引主體
	// 回傳的選項 rvalue 需綁定到 `it`，後續臂體 `s = it` 才能取到 payload。
	switch matched.(type) {
	case *Identifier, *IndexExpression:
	default:
		return nil
	}
	// Create the binding regardless of whether the type is known at parse time.
	// When varDeclTypes doesn't have the variable, Type is left nil so codegen
	// can infer it from g.varTypes (e.g. %option for option-returning calls).
	return &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: "it"},
		Value:       matched,
		IsSynthetic: true,
	}
}

// unionElemType 從聯合型別字串中提取 payload（非 err/nil）型別。
// "?i64" → "i64"；"i64 | err" → "i64"；"i64 | err | nil" → "i64"。
// 若無法唯一確定（如多個非變體型別）則回傳空字串。
func unionElemType(t string) string {
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, "?") {
		return strings.TrimPrefix(t, "?")
	}
	parts := strings.Split(t, "|")
	var elem []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "err" || p == "nil" {
			continue
		}
		elem = append(elem, p)
	}
	if len(elem) == 1 {
		return elem[0]
	}
	return ""
}

// enumVariantPayload 查詢枚舉變體的載荷型別，回傳 "" 表示單元變體或不存在。
// 同時嘗試原樣鍵與去模組前綴的裸名鍵（解析期以裸名登記，模組前綴化後可能不一致）。
func (p *Parser) enumVariantPayload(enumType, variant string) string {
	if p.sem == nil || enumType == "" {
		return ""
	}
	if pt, ok := p.sem.EnumVariantPayloadOf(enumType, variant); ok {
		return pt
	}
	if idx := strings.LastIndex(enumType, "."); idx > 0 {
		if pt, ok := p.sem.EnumVariantPayloadOf(enumType[idx+1:], variant); ok {
			return pt
		}
	}
	return ""
}

// enumVariantFieldTypes 查詢枚舉變體的載荷欄位型別列表（單元變體回傳 nil）。
// 同時嘗試原樣鍵與去模組前綴的裸名鍵。
func (p *Parser) enumVariantFieldTypes(enumType, variant string) []string {
	if p.sem == nil || enumType == "" {
		return nil
	}
	if fs := p.sem.EnumVariantFieldsOf(enumType, variant); len(fs) > 0 {
		return fs
	}
	if idx := strings.LastIndex(enumType, "."); idx > 0 {
		if fs := p.sem.EnumVariantFieldsOf(enumType[idx+1:], variant); len(fs) > 0 {
			return fs
		}
	}
	return nil
}

// isSentinelArmType reports whether armType denotes only the nil/err variants
// (no payload), e.g. "nil", "err", or "err | nil". For these arms `it` is a
// placeholder and must never trigger the struct-deref codegen path
// (stmt.go:6339), which would load the option's data field unconditionally —
// including when the option is nil/err (data == 0 → null deref → segfault).
func isSentinelArmType(armType string) bool {
	if armType == "" {
		return false
	}
	// "else" denotes the complement of ok (i.e., err | nil) — both are
	// sentinels with no payload, so it must be treated as sentinel.
	if strings.TrimSpace(armType) == "else" {
		return true
	}
	for _, part := range strings.Split(armType, "|") {
		part = strings.TrimSpace(part)
		if part == "err" || part == "nil" || part == "" {
			continue
		}
		return false
	}
	return true
}

// buildItBindingForArm creates `it = matched` LetStatement with the correct type
// for the specific match arm. For option types (e.g., ?i64):
//
//	err arm -> it: err (variant type)
//	nil arm -> it: nil (variant type)
//	ok arm  -> it: elemType (e.g., i64 for ?i64)
//
// 型別資訊取自 sm 的解析期快照（MatchedVarType/IsEnumType）。
func (p *Parser) buildItBindingForArm(tok lexer.Token, matched Expression, armType string, elemType string, matchedVarType string, isEnumType bool, name string) *LetStatement {
	if name == "" {
		name = "it"
	}
	_, ok := matched.(*Identifier)
	if !ok {
		return nil
	}
	t := matchedVarType

	// When matchedVarType is unknown at parse time (e.g., variable assigned from
	// a method call whose return type is resolved later), we still need to create
	// `it` bindings for err and nil arms so the codegen can unwrap the error
	// message at runtime. Without this, the err arm body (e.g. `content = err(it)`)
	// would reference `it` from the ok arm's collectVarDecls, causing a type
	// mismatch (e.g., %vec instead of %str-long).
	// For ok arms (armType "ok", "else", "ok_err", etc.) with unknown type,
	// return nil so the caller falls back to the shared `it` binding (safe,
	// because the option is non-nil in ok arms, so the struct-deref is guarded
	// by the live variant).
	if t == "" {
		switch armType {
		case "err":
			return &LetStatement{
				Token:       tok,
				Name:        &Identifier{Token: tok, Value: name},
				Value:       matched,
				Type:        &NamedType{Value: "err"},
				IsSynthetic: true,
			}
		case "nil":
			return &LetStatement{
				Token:       tok,
				Name:        &Identifier{Token: tok, Value: name},
				Value:       matched,
				Type:        &NamedType{Value: "nil"},
				IsSynthetic: true,
			}
		default:
			return nil
		}
	}

	var typeStr string
	if strings.HasPrefix(t, "?") && elemType != "" {
		// Option type: map armType to specific type string
		switch armType {
		case "err":
			typeStr = "err"
		case "nil":
			typeStr = "nil"
		case "ok":
			typeStr = elemType
		case "else":
			typeStr = "err | nil"
		case "ok_err", "ok_nil", "ok_err_nil":
			// For codegen, `it` in ok-containing arms is the payload type.
			// The union type (e.g. "file | nil") is only meaningful for the
			// type checker; using it in codegen causes method dispatch to
			// generate invalid function names with `|` characters.
			typeStr = elemType
		default:
			return nil
		}
	} else if !isEnumType && elemType != "" && (strings.Contains(t, "err") || strings.Contains(t, "nil")) {
		// 結果聯合型別（如 i64 | err / i64 | err | nil）：按 armType 收窄 it。
		// 與 ?X 選項型別類似，但變體為 err/nil（而非 ok/err/nil）。
		var remaining []string
		for _, v := range []string{"err", "nil"} {
			if strings.Contains(t, v) {
				remaining = append(remaining, v)
			}
		}
		remStr := strings.Join(remaining, " | ")
		switch armType {
		case "err":
			typeStr = "err"
		case "nil":
			typeStr = "nil"
		case "ok":
			typeStr = elemType
		case "else":
			typeStr = remStr
		case "ok_err", "ok_nil", "ok_err_nil":
			// For codegen, `it` in ok-containing arms is the payload type.
			typeStr = elemType
		default:
			return nil
		}
	} else if isEnumType {
		// Enum type: armType is the variant name or union expression (e.g., "status1" or "status2 | status3")
		typeStr = armType
	} else {
		return nil
	}

	return &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: name},
		Value:       matched,
		Type:        &NamedType{Value: typeStr},
		IsSynthetic: true,
	}
}

// namedItBinding 把共享的 `it = matched` 綁定改名為 `name = matched`
// （析構綁定 `ok(v) -> ...` 用）。name 為空或 "it" 時直接回傳原節點。
func (p *Parser) namedItBinding(it *LetStatement, name string) *LetStatement {
	if it == nil || name == "" || name == "it" {
		return it
	}
	nb := *it
	nb.Name = &Identifier{Token: it.Token, Value: name}
	return &nb
}

// prependStmt prepends a statement to a BlockStatement, returning a new BlockStatement.
func (p *Parser) prependStmt(body *BlockStatement, stmt Statement) *BlockStatement {
	if body == nil {
		return &BlockStatement{Statements: []Statement{stmt}}
	}
	stmts := make([]Statement, 0, len(body.Statements)+1)
	stmts = append(stmts, stmt)
	stmts = append(stmts, body.Statements...)
	nb := &BlockStatement{Token: body.Token, Statements: stmts, IsInline: body.IsInline}
	// Preserve the original body block's comments when the desugar injects an
	// `it` binding. The arm-body block is REPLACED by nb, so without this the
	// comments that live inside the block (trailing comments before `}`, the
	// closing-brace comment and the `{`-line comment) are silently dropped.
	// Symptom: option-match arms such as `ok -> { s ; tail }` lost `; tail` on
	// format (notools/nonpm tests). Bare-match arms never call prependStmt,
	// which is why only `v: { ok -> { ... } }` regressed.
	nb.TrailingComments = body.TrailingComments
	nb.ClosingBraceComment = body.ClosingBraceComment
	nb.RBrace = body.RBrace
	if obc := p.sem.OpeningBraceCommentOf(body); obc != nil {
		p.sem.SetOpeningBraceComment(nb, obc)
	}
	// 保留原 body 區塊的 #{overflow = ...} 等註解：裸配對臂的溢出模式掛在
	// bodyBlock 上，desugar 插入 `it` 綁定時若丟棄註解，generateIfExpression
	// 便無法從 Consequence/Alternative 區塊讀到模式，導致臂體條件內的整數
	// 運算（如 `n >= 0 - 128` 的 `0 - 128`）退回預設 option 模式。
	if an := p.sem.AnnotationsOf(body); len(an) > 0 {
		p.sem.SetRawAnnotations(nb, an)
	}
	return nb
}

// lowerUnwrapAssign desugars `v ?= expr` into a match/if chain:
//
//	__unwrap_N = expr
//	__unwrap_N: {
//	    nil || err -> {
//	        result = __unwrap_N
//	        return
//	    }
//	    -> v = it
//	}
//
// The result param name is found from the current function definition's Results.
// If no option-typed result param is found, the desugar produces a parse error
// (the checker will report it).
func (l *lowerer) lowerUnwrapAssign(uas *UnwrapAssignStatement) Statement {
	tok := uas.Token
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		nm := ""
		if uas.Name != nil {
			nm = uas.Name.Value
		}
		fmt.Fprintf(os.Stderr, "[debug-it] lowerUnwrapAssign: name=%q line=%d curFunc=%q\n", nm, tok.Line, l.curFuncName)
	}

	// First, walk into the Value expression to lower any nested SurfaceMatch.
	l.walk(reflect.ValueOf(&uas.Value))

	// Find the option-typed result param name from the current function def.
	resultName := ""
	if l.curFuncDef != nil {
		for _, res := range l.curFuncDef.Results {
			if res.Type != nil {
				ts := typeString(res.Type)
				if strings.HasPrefix(ts, "?") {
					resultName = res.Name
					break
				}
			}
		}
	}

	// --- 複合 / 非 option ?= 檢查與 option 運算元守衛注入 ---
	// 解析期能確定的「明確非 option」右側（字面量、已知非 option 區域變數）直接使用
	// ?= 無意義，報錯提示改用 `=`。其餘（呼叫回傳型別、未知型別變數）在解析期尚不可判
	// 定，交由檢查器處理，避免誤報（見 v ?= bar() 之類的 option 回傳呼叫）。
	// 若右側含 option 運算元（如 a ?= b + c + d、a ?= f(b + c)），為每個 option 運算元
	// 綁定臨時變數並注入傳播守衛（nil/err → result = __opt_N; return）。
	var guards []Statement
	if resultName != "" {
		if l.rhsDefinitelyNonOption(uas.Value) {
			l.p.saveError(fmt.Sprintf("line %d, column %d: `?=` RHS `%s` is not an option — use `=` instead",
				tok.Line, tok.Column, uas.String()))
			return nil
		}
		l.guardOptionOperands(uas.Value, &guards, resultName, tok)
	}

	// Create a unique temporary variable name for the option value.
	tmpName := fmt.Sprintf("__unwrap_%d", tok.Line)
	tmpIdent := &Identifier{Token: tok, Value: tmpName}

	// Infer the type of the temp variable from the RHS expression.
	// This allows buildMatchDesugar to resolve elemType for `it` bindings.
	savedFuncName2 := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	if call, ok := uas.Value.(*CallExpression); ok {
		if inferred := l.p.inferTypeFromCallExpr(call); inferred != "" {
			l.p.setVarType(tmpName, inferred)
		}
	}
	l.p.curFuncName = savedFuncName2

	// 對於算術 RHS（如 `a ?= b - c`），?= 需要 option 接收端才能傳播溢出錯誤。
	// 將暫存變數標註為 ?i64：overflow 預設回傳的 option 內部資料一律為 i64
	//（窄型別已 zext / 符號擴展至 i64），故 option 結構 %option 與內部型別無關。
	// 必須在 LetStatement 上顯式掛 NullableType 節點，否則 checker 會從 RHS
	//（未標註減法預設 wrap）推斷成純 i64，導致後續 `r = __unwrap_N` 報
	// i64 -> ?i64 型別錯誤。
	var tmpType Type
	var elemType string // 安全索引的元素型別（v[i] 的 i）；用於標註 LHS 區域變數型別
	switch v := uas.Value.(type) {
	case *InfixExpression:
		l.p.setVarType(tmpName, "?i64")
		tmpType = &NullableType{
			Token: tok,
			Type:  &NamedType{Token: tok, Value: "i64"},
		}
		// 解箱後內部值型別：overflow 預設 option 內部資料一律為 i64
		//（窄型別已 zext / 符號擴展至 i64），故解箱值恆為 i64。標註 LHS
		// 區域變數型別，避免 codegen 退回被全域 VarTypes 同名污染（std 常見
		// a:str → %str-long）的型別 → `%a.val defined with type '%str-long'
		// but expected 'i64'（見 q_probe_c / q_probe_ab A）。
		elemType = "i64"
		if uas.Name != nil && uas.Name.Value != resultName {
			l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, "i64")
		}
	case *CallExpression:
		// 函式呼叫 RHS（如 a ?= f(n)）：從呼叫回傳型別推導解箱後內部值型別，
		// 標註 LHS 區域變數，避免 codegen 退回被全域 VarTypes 同名污染（std
		// 常見 a:str → %str-long）的型別 → `%a.val defined with type '%str-long'
		// but expected 'i64'（見 q_probe_ab A）。LHS 為結果參數時不標註（結果
		// 參數本身已是 ?T，走既有 wrap 路徑，保持 res ?= f(21) 既有行為）。
		if inferred := l.p.inferTypeFromCallExpr(v); inferred != "" {
			inner := strings.TrimPrefix(inferred, "?")
			elemType = inner
			l.p.setVarType(tmpName, inferred)
			if uas.Name != nil && uas.Name.Value != resultName {
				l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, inner)
			}
		}
	case *IndexExpression:
		// 安全索引：`v[i]` 越界回傳 None；?= 解箱並向上傳播錯誤。
		// 僅支援直接 arr/vec/slice 變數（識別符基底）；struct 欄位索引
		// （receiver.field[i]）安全索引 codegen 尚未支援，直接報錯。
		if _, ok := v.Left.(*Identifier); !ok {
			l.p.saveError(fmt.Sprintf("line %d, column %d: safe-index `?=` only supports direct array/vec variables, not struct fields (receiver.field[i])", tok.Line, tok.Column))
			return nil
		}
		// 推斷元素型別以便讓 match desugar 的 it 綁定拿到正確型別。
		if elem := l.p.inferIndexElemType(v); elem != "" {
			elemType = elem
			l.p.setVarType(tmpName, "?"+elem)
			tmpType = &NullableType{
				Token: tok,
				Type:  &NamedType{Token: tok, Value: elem},
			}
			// 記錄 LHS 區域變數（如未標型別的 x）的元素型別到「函數作用域」
			// FuncVarTypes，避免 codegen 退回被全域 VarTypes 同名污染
			// （std 常見 x:str → %str-long）而把 x 誤當字串，導致
			// `%x.val defined with type '%str-long' but expected 'i64'`
			// （見 safe-index-utl.no 的 safe-utl）。寫入當前函數作用域
			// （非全域）確保不跨函數污染 std。
			if uas.Name != nil {
				l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, elem)
			}
		}
	}

	// 後備推斷：若 elemType/tmpType 仍未設定（例如 RHS 是直接識別符 `inner ?= res`，
	// 而非 Infix/Index 表達式），從 RHS 變數的選項型別推導元素型別。
	// 否則 codegen 會退回被全域 VarTypes 同名污染（std 常見 inner/x/v:str → %str-long）
	// 的型別，導致 inner 被誤配為 %str-long、且 __unwrap_N 被誤配為 i64（8 位元組）
	// 卻以 %option（16 位元組）存取 → 記憶體損壞與崩潰（見 test-iso-q.no）。
	if elemType == "" {
		if ident, ok := uas.Value.(*Identifier); ok {
			t := ""
			if tt, ok2 := l.p.sem.FuncVarType(l.curFuncName, ident.Value); ok2 {
				t = tt
			} else if tt, ok2 := l.p.sem.VarTypes[ident.Value]; ok2 {
				t = tt
			}
			if strings.HasPrefix(t, "?") {
				elemType = strings.TrimPrefix(t, "?")
				l.p.setVarType(tmpName, "?"+elemType)
				tmpType = &NullableType{
					Token: tok,
					Type:  &NamedType{Token: tok, Value: elemType},
				}
				if uas.Name != nil {
					l.p.sem.SetFuncVarType(l.curFuncName, uas.Name.Value, elemType)
				}
			}
		}
	}

	// Build the temporary variable assignment: __unwrap_N = expr
	tmpAssign := &LetStatement{
		Token: tok,
		Name:  tmpIdent,
		Type:  tmpType,
		Value: uas.Value,
	}

	// Build match arms.
	var arms []matchArm

	var preStmts []Statement
	// 傳播賦值目標是否「就是」option 結果參數本身（`r ?= ...`）。
	// 僅此情形才能把 `r = __unwrap_N` 提前到 match 之前：此時 r 與 __unwrap_N
	// 型別一致（同為 ?T），整體 option 結構淺拷貝即語意正確。
	// 若目標是其他局部變數（如 `size ?= fstat-size(.fd)`，size:i64 而結果參數
	// content:?[]byte），提前拷貝會把 ?i64 的整個 option 結構（ok 時 data=原始
	// i64）塞進 ?[]byte 的結果槽 —— 之後 codegen 對 content 的 freeOldHeapValue
	// /clone 會把該 i64 當 box 指標解引用 → segfault（見 fs.no file.read-bytes）。
	// 此時改為在 nil/err arm 內才拷貝（僅傳播 err/nil 結果），ok 路徑不污染
	// 結果槽，由用戶程式碼自行賦值。
	targetIsResult := uas.Name != nil && uas.Name.Value == resultName
	if resultName != "" && targetIsResult {
		// 將 `result = __unwrap_N` 提前到 match 之前：三個 arm 都不再寫回 r，
		// 因為 match 前的這次賦值已把 opt 結果（ok/err/nil）寫入 r，arm 內
		// 只需 return（nil/err）或提取內部值（ok 臂的 v = it）。
		// 關鍵：若把 `r = __unwrap_N` 放在 err/nil arm 內，該賦值會因 arm 內的
		// sentinel `it` 綁定而未被 codegen 發出（codegen 僅對 ok 臂發出 store
		// %option 到 %r），導致 r 停留初始 nil、呼叫方匹配到 nil 而非 err。
		// 提前可徹底規避此 codegen 坑。
		preStmts = append(preStmts, &LetStatement{
			Token: tok,
			Name:  &Identifier{Token: tok, Value: resultName},
			Value: tmpIdent,
		})
		// nil -> return；err -> return（r 已在 match 前寫入 __unwrap_N）。
		// 分別展開為兩個獨立 arm（而非 multiOptionPatterns 的
		// `matched == nil || matched == err` 組合條件），因為組合 OR 條件會
		// 觸發 option 變數的「值比較」代碼路徑（對 err/nil 關鍵字做 load，
		// 產生未定義的 %err / 錯誤的 %nil 比較）；獨立 arm 走 tag 比較路徑。
		arms = append(arms, matchArm{
			condition:     &Identifier{Token: tok, Value: "nil"},
			body:          &BlockStatement{Token: tok, Statements: []Statement{&ReturnStatement{Token: tok}}},
			isBlockBody:   true,
			pos:           posFromToken(tok),
			skipItBinding: true,
		})
		arms = append(arms, matchArm{
			condition:     &Identifier{Token: tok, Value: "err"},
			body:          &BlockStatement{Token: tok, Statements: []Statement{&ReturnStatement{Token: tok}}},
			isBlockBody:   true,
			pos:           posFromToken(tok),
			skipItBinding: true,
		})
	} else if resultName != "" {
		// 非結果參數目標（`v ?= ...` 且 v != 結果參數）：把 option 結構的傳播
		// 拷貝放進 nil/err arm 內，ok 路徑完全不動結果參數。
		propagate := func() *BlockStatement {
			return &BlockStatement{Token: tok, Statements: []Statement{
				&LetStatement{
					Token:         tok,
					Name:          &Identifier{Token: tok, Value: resultName},
					Value:         tmpIdent,
					IsPropagation: true,
				},
				&ReturnStatement{Token: tok},
			}}
		}
		arms = append(arms, matchArm{
			condition:     &Identifier{Token: tok, Value: "nil"},
			body:          propagate(),
			isBlockBody:   true,
			pos:           posFromToken(tok),
			skipItBinding: true,
		})
		arms = append(arms, matchArm{
			condition:     &Identifier{Token: tok, Value: "err"},
			body:          propagate(),
			isBlockBody:   true,
			pos:           posFromToken(tok),
			skipItBinding: true,
		})
	} else {
		// No option result param — emit a parse error placeholder body.
		l.p.saveError(fmt.Sprintf("line %d, column %d: `?=` can only be used inside a function with an option-typed result param", tok.Line, tok.Column))
		arms = append(arms, matchArm{
			condition:     &Identifier{Token: tok, Value: "nil"},
			body:          &BlockStatement{Token: tok, Statements: []Statement{&ReturnStatement{Token: tok}}},
			isBlockBody:   true,
			pos:           posFromToken(tok),
			skipItBinding: true,
		})
		arms = append(arms, matchArm{
			condition:     &Identifier{Token: tok, Value: "err"},
			body:          &BlockStatement{Token: tok, Statements: []Statement{&ReturnStatement{Token: tok}}},
			isBlockBody:   true,
			pos:           posFromToken(tok),
			skipItBinding: true,
		})
	}

	// -> { v = it }（wildcard / ok 臂：r 已在 match 前寫入 __unwrap_N，
	// 此處僅把內部值解箱給 v）。欄位/索引目標改用 AssignExpression（target = it）。
	var okArm Statement
	if uas.Target != nil {
		okArm = &ExpressionStatement{
			Token: tok,
			Expression: &AssignExpression{
				Token: tok,
				Left:  uas.Target,
				Value: &Identifier{Token: tok, Value: "it"},
			},
		}
	} else {
		la := &LetStatement{
			Token: tok,
			Name:  uas.Name,
			Value: &Identifier{Token: tok, Value: "it"},
		}
		// 標註 LHS 區域變數的元素型別（安全索引 v[i] 的解箱值），使 generateLet
		// 以正確型別配置 x 的 alloca，而非退回被全域 VarTypes 同名污染（std 常見
		// x:str → %str-long）的型別——否則 `%x.val defined with type '%str-long'
		// but expected 'i64'。節點級標註，零跨函數污染風險。
		if elemType != "" {
			la.Type = &NamedType{Token: tok, Value: elemType}
		}
		okArm = la
	}
	okStmts := []Statement{okArm}
	if resultName == "" {
		// 無 result param（理論上不會發生，lowerUnwrapAssign 已報錯）：
		// 僅解箱給 v。
		okStmts = []Statement{
			&LetStatement{
				Token: tok,
				Name:  uas.Name,
				Value: &Identifier{Token: tok, Value: "it"},
			},
		}
	}
	okBody := &BlockStatement{
		Token:      tok,
		Statements: okStmts,
	}
	arms = append(arms, matchArm{
		isWildcard:  true,
		body:        okBody,
		isBlockBody: true,
		pos:         posFromToken(tok),
	})

	// Build SurfaceMatch and desugar it.
	sm := &SurfaceMatch{
		Token:   tok,
		Matched: tmpIdent,
		Arms:    arms,
	}

	// Set the var type for the temp variable so buildMatchDesugar can infer elemType.
	// Try to infer from the Value expression (e.g. function call returning ?T).
	savedFuncName := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	lowered := l.p.buildMatchDesugar(sm)
	l.p.curFuncName = savedFuncName

	if lowered == nil {
		return nil
	}

	// Wrap in a block: { [__opt_N = operand; match {nil/err -> propagate}]; __unwrap_N = expr; [result = __unwrap_N]; <lowered match> }
	// guards（option 運算元守衛）必須在 __unwrap_N 指派與 match 之前，確保任一
	// option 運算元為 nil/err 時先上拋；preStmts（result = __unwrap_N）亦須在
	// match 之前，確保 r 在 arm 內的 return 前已被賦值為 opt 結果（ok/err/nil）。
	stmts := make([]Statement, 0, len(guards)+len(preStmts)+2)
	stmts = append(stmts, guards...)
	stmts = append(stmts, tmpAssign)
	for _, s := range preStmts {
		stmts = append(stmts, s)
	}
	stmts = append(stmts, &ExpressionStatement{Token: tok, Expression: lowered})
	return &BlockStatement{
		Token:      tok,
		Statements: stmts,
	}
}

// optionInnerOfVar 回傳變數 name 的 option 元素型別（? 後的 T），非 option 回傳 ""。
// 優先查函數作用域（避免跨函數同名區域變數污染），回退查全域 VarTypes。
func (l *lowerer) optionInnerOfVar(name string) string {
	if t, ok := l.p.sem.FuncVarType(l.curFuncName, name); ok && strings.HasPrefix(t, "?") {
		return t[1:]
	}
	if t, ok := l.p.sem.VarTypes[name]; ok && strings.HasPrefix(t, "?") {
		return t[1:]
	}
	return ""
}

// exprOptionInnerType 若表達式 e 為 option 值（option 變數、option 回傳呼叫、
// 安全索引），回傳其元素型別（?T 的 T）；否則回傳 ""。
// 算術 Infix 一律視為 option 值（溢位回傳 ?i64），以相容既有溢位傳播語意。
func (l *lowerer) exprOptionInnerType(e Expression) string {
	switch v := e.(type) {
	case *Identifier:
		return l.optionInnerOfVar(v.Value)
	case *CallExpression:
		if inferred := l.p.inferTypeFromCallExpr(v); inferred != "" && strings.HasPrefix(inferred, "?") {
			return inferred[1:]
		}
		return ""
	case *IndexExpression:
		if elem := l.p.inferIndexElemType(v); elem != "" {
			return elem
		}
		return ""
	case *InfixExpression:
		return "i64" // 算術溢位 → ?i64
	}
	return ""
}

// isDefinitelyNonOptionVar 回報變數 name 是否「確定為非 option」：僅當其型別在
// 解析期已知且不以 ? 開頭時成立。型別未知（未宣告 / 尚未推斷）或非 option 回傳
// 的呼叫則回傳 false —— 解析期無法判定，交由檢查器處理，避免誤報。
func (l *lowerer) isDefinitelyNonOptionVar(name string) bool {
	if t, ok := l.p.sem.FuncVarType(l.curFuncName, name); ok {
		return !strings.HasPrefix(t, "?")
	}
	if t, ok := l.p.sem.VarTypes[name]; ok {
		return !strings.HasPrefix(t, "?")
	}
	return false
}

// localDeclaredType 回報 name 在「當前函式作用域」已知的型別：優先函式參數
// （含方法 self），其次 FuncVarTypes 的本函式條目；頂層（curFuncName == ""）才查
// 全域 VarTypes。
//
// 刻意**不做** FuncVarType 的全域回退：全域 VarTypes 會被其他函式的同名參數／
// 區域變數覆寫（std 常見 `c i64` 參數），回退會讓本函式的判斷被別人的型別否決。
// 實測：`f = (b i64, c i64, d i64) { a = b + c / d }` 之後，
// `h = (m i64) { c = y + 1 }` 的捕獲被靜默跳過（`FuncVarType("h","c")` 回退到 f
// 的參數型別 i64），連帶使 `c` 不再是 option、溢出檢查消失、後續 match 走錯臂。
func (l *lowerer) localDeclaredType(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if l.curFuncDef != nil {
		for _, prm := range l.curFuncDef.Parameters {
			if prm != nil && prm.Name == name && prm.Type != nil {
				return typeString(prm.Type), true
			}
		}
	}
	if l.curFuncName != "" {
		if l.p.sem != nil {
			if vars, ok := l.p.sem.FuncVarTypes[l.curFuncName]; ok {
				if t, ok := vars[name]; ok {
					return t, true
				}
			}
		}
		return "", false
	}
	if l.p.sem != nil {
		if t, ok := l.p.sem.VarTypes[name]; ok {
			return t, true
		}
	}
	return "", false
}

// localDefinitelyNonOption 回報 name 在當前作用域是否「已宣告且非 option」。
func (l *lowerer) localDefinitelyNonOption(name string) bool {
	t, ok := l.localDeclaredType(name)
	return ok && !strings.HasPrefix(t, "?")
}

// captureTargetAllowsOption 回報捕獲 pass 是否可以把 s 的目標改寫成 option 變數。
//
// 規則是「已存在的變數不改變其型別」：
//   - 目標在本函式內已出現過（declaredLocals，含參數）→ 只有當它本來就是 option
//     才允許；否則改寫會讓它之前的所有使用（普通值語境）突然拿到 option，
//     且無法把 option 存進已宣告的純量變數（方案 §1：「變數已宣告為非 option
//     型別 → 不碰，交 checker 報」）。
//   - 目標是首次宣告 → 只要型別表沒有明確的非 option 型別（如函式參數）就允許。
//
// 判定刻意不直接讀全域 VarTypes：它會被其他函式的同名參數污染（f 的 `c i64`
// 讓 h 的區域 `c` 被當成已宣告），且方法體在解析期的 curFuncName 與 lowering 期
// 不一致，`v i64 = 0` 的型別可能只落在全域表 —— 兩種情況都會讓判定失準。
func (l *lowerer) captureTargetAllowsOption(s *LetStatement) bool {
	if s == nil || s.Name == nil {
		return false
	}
	name := s.Name.Value
	if l.declaredLocals[name] {
		t, ok := l.localDeclaredType(name)
		return ok && strings.HasPrefix(t, "?")
	}
	if s.Type == nil && l.localDefinitelyNonOption(name) {
		return false
	}
	return true
}

// rhsDefinitelyNonOption 判斷 ?= 右側是否「明確非 option」且解析期可判定：
// 字面量、或已知非 option 的區域變數。此類右側使用 ?= 無意義，應報錯提示改用 `=`。
// 算術（可溢位）、安全索引、option 變數/呼叫、以及型別未知者皆回傳 false，
// 交由後續（檢查器或既有 desugar）處理。
func (l *lowerer) rhsDefinitelyNonOption(e Expression) bool {
	switch v := e.(type) {
	case *IntegerLiteral, *FloatLiteral, *StringLiteral, *BooleanLiteral:
		return true
	case *Identifier:
		return l.isDefinitelyNonOptionVar(v.Value)
	case *IndexExpression:
		return false // 安全索引回傳 option
	case *InfixExpression:
		// 若整個算術運算「明確為 plain」（所有葉節點皆為字面量或已知非 option
		// 區域變數、且不含任何呼叫），則結果必為非 option（如 10+20、x+1 且
		// x 為 i64），使用 ?= 無意義，應報錯提示改用 `=`。只要任一運算元是 option
		// 變數、未知型別變數或呼叫（可能回傳 option），即視為「可能 option」，
		// 算術 ?= 的右側（如 `a ?= b - c`）：?= 要求以 option（溢出）模式求值 RHS，
		// 溢出時向上傳播錯誤；故即使運算元皆為已知非 option 變數（b/c 為 i64），也算
		// 「可能為 option」（運算本身可能溢出 → 回傳 %option），不應在此報錯。
		// 僅「純字面」算術（如 `v ?= 10 + 20`，編譯期常量永遠不溢出、?= 無意義）
		// 才保留報錯提示改用 `=`。
		if l.infixAllLiterals(v) {
			return true
		}
		return false
	case *CallExpression:
		return false // 回傳型別解析期不可判定，交由檢查器
	}
	return false
}

// infixDefinitelyPlain 遞迴判斷算術運算是否「明確為非 option」：僅由字面量與
// 已知非 option 區域變數組成、且不含任何呼叫 / option 運算元時成立。只要出現
// 未知型別變數、option 變數或函式呼叫（可能回傳 option），即回傳 false。
func (l *lowerer) infixDefinitelyPlain(e Expression) bool {
	switch v := e.(type) {
	case *IntegerLiteral, *FloatLiteral, *StringLiteral, *BooleanLiteral:
		return true
	case *Identifier:
		return l.isDefinitelyNonOptionVar(v.Value)
	case *InfixExpression:
		return l.infixDefinitelyPlain(v.Left) && l.infixDefinitelyPlain(v.Right)
	case *CallExpression:
		return false // 呼叫可能回傳 option
	}
	return false
}

// infixAllLiterals 遞迴判斷算術運算是否「全為字面量」（不含任何識別符或呼叫）。
// 純字面算術（如 10 + 20、1 * 3 - 2）為編譯期常量，永不溢出，故 `?=` 對其無意義；
// 一旦出現任何變數或呼叫（運算可能溢出），即應放行 ?= 以啟用溢出傳播。
func (l *lowerer) infixAllLiterals(e Expression) bool {
	switch v := e.(type) {
	case *IntegerLiteral, *FloatLiteral, *StringLiteral, *BooleanLiteral:
		return true
	case *InfixExpression:
		return l.infixAllLiterals(v.Left) && l.infixAllLiterals(v.Right)
	}
	return false
}

// guardOptionOperands 遍歷 ?= 右側表達式，為其中的 option 運算元注入傳播守衛。
// 僅在「算術運算元」位置直接解包 option 運算元；呼叫引數位置則只遞迴下探
// （如 f(b+c) 的 b+c），不直接解包作為引數的 option 變數（交給被呼叫方或 ?= 豁免）。
// 守衛語句會被附加到 *guards，並就地重寫 uas.Value 中的 option 運算元為臨時變數。
func (l *lowerer) guardOptionOperands(e Expression, guards *[]Statement, resultName string, tok lexer.Token) {
	switch v := e.(type) {
	case *InfixExpression:
		v.Left = l.guardInArithmetic(v.Left, guards, resultName, tok)
		v.Right = l.guardInArithmetic(v.Right, guards, resultName, tok)
	case *CallExpression:
		for i := range v.Arguments {
			v.Arguments[i] = l.guardInCallArg(v.Arguments[i], guards, resultName, tok)
		}
	}
}

func (l *lowerer) guardInArithmetic(e Expression, guards *[]Statement, resultName string, tok lexer.Token) Expression {
	if inner := l.exprOptionInnerType(e); inner != "" {
		return l.bindAndGuard(e, inner, guards, resultName, tok)
	}
	switch v := e.(type) {
	case *InfixExpression:
		v.Left = l.guardInArithmetic(v.Left, guards, resultName, tok)
		v.Right = l.guardInArithmetic(v.Right, guards, resultName, tok)
		return v
	case *CallExpression:
		for i := range v.Arguments {
			v.Arguments[i] = l.guardInCallArg(v.Arguments[i], guards, resultName, tok)
		}
		return v
	}
	return e
}

func (l *lowerer) guardInCallArg(e Expression, guards *[]Statement, resultName string, tok lexer.Token) Expression {
	switch v := e.(type) {
	case *InfixExpression:
		v.Left = l.guardInArithmetic(v.Left, guards, resultName, tok)
		v.Right = l.guardInArithmetic(v.Right, guards, resultName, tok)
		return v
	case *CallExpression:
		for i := range v.Arguments {
			v.Arguments[i] = l.guardInCallArg(v.Arguments[i], guards, resultName, tok)
		}
		return v
	}
	return e
}

// bindAndGuard 將 option 運算元 e 綁定到新臨時變數 __opt_N，並注入傳播守衛
// （nil/err → result = __opt_N; return），回傳替代原運算元的臨時變數識別符。
func (l *lowerer) bindAndGuard(e Expression, inner string, guards *[]Statement, resultName string, tok lexer.Token) Expression {
	l.optTmpSeq++
	name := fmt.Sprintf("__opt_%d", l.optTmpSeq)
	tmpIdent := &Identifier{Token: tok, Value: name}
	l.p.setVarType(name, "?"+inner)
	// __opt_N = e
	*guards = append(*guards, &LetStatement{
		Token: tok,
		Name:  tmpIdent,
		Type:  &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: inner}},
		Value: e,
	})
	// match __opt_N { nil/err -> result = __opt_N; return }
	*guards = append(*guards, l.makePropagateGuard(name, resultName, tok))
	return tmpIdent
}

// makePropagateGuard 產生 `match __opt_N { nil/err -> result = __opt_N; return }`
// 的 Statement（ok 情況落空、繼續執行後續語句）。
func (l *lowerer) makePropagateGuard(optName, resultName string, tok lexer.Token) Statement {
	tmpIdent := &Identifier{Token: tok, Value: optName}
	mkBody := func() *BlockStatement {
		assignRes := &LetStatement{
			Token: tok,
			Name:  &Identifier{Token: tok, Value: resultName},
			Value: tmpIdent,
		}
		return &BlockStatement{Token: tok, Statements: []Statement{assignRes, &ReturnStatement{Token: tok}}}
	}
	nilArm := matchArm{
		condition:     &Identifier{Token: tok, Value: "nil"},
		body:          mkBody(),
		isBlockBody:   true,
		pos:           posFromToken(tok),
		skipItBinding: true,
	}
	errArm := matchArm{
		condition:     &Identifier{Token: tok, Value: "err"},
		body:          mkBody(),
		isBlockBody:   true,
		pos:           posFromToken(tok),
		skipItBinding: true,
	}
	sm := &SurfaceMatch{Token: tok, Matched: tmpIdent, Arms: []matchArm{nilArm, errArm}}
	saved := l.p.curFuncName
	l.p.curFuncName = l.curFuncName
	desugared := l.p.buildMatchDesugar(sm)
	l.p.curFuncName = saved
	if desugared == nil {
		return &BlockStatement{Token: tok, Statements: mkBody().Statements}
	}
	return &ExpressionStatement{Token: tok, Expression: desugared}
}
