package parser

// 陳述層級塊 match `{ body }: { arms }` 的展開器。
//
// match 主語有三種形態：值 `x: {} `、表達式 `x + 1: {} `、塊 `{ … }: {}`。
// 第三種是陳述層級語法：主體是一段陳述，捕捉其中 `?=` 的上拋，讓「沒有 ?T
// 結果參數」的函式也能就地處理錯誤：
//
//	{
//	    state[5] ?= state[9]
//	}: {
//	    err ->
//	    -> _
//	}
//
// 展開後完全由既有核心 AST 組成（不新增任何 HIR/MIR 節點）：
//
//	let __bm_N      ?i64 = nil   // 起始 nil；body 走完仍是 nil
//	let __bm_N_halt i64  = 0    // 任一次 `?=` 傳播（nil 或 err）就置 1
//	<CPS(body, __bm_N)>
//	__bm_N: { <使用者 arms> }    // 只可能是 nil / err，ok 不可達
//
// halt 旗標不可省：`?=` 的 nil 傳播會把 subject 寫成 nil，與「主體順利走完」
// 的初值相同，只靠 `subject == nil` 守衛無法停住後續陳述。旗標把「是否已傳播」
// 與「傳播了哪個變體」分開，兩者各自守自己的語意。
//
// 最近的塊 match 捕捉：expandBlockMatch 對 body 裡的巢狀
// BlockMatchStatement 完全不遞迴（留給 H0 / walker 各自展開自己的 Subject），
// FunctionDefinition 同理是邊界；FunctionLiteral 則照現有 scope 規則遞迴。
//
// ⚠️ 展開必須是 copy-on-write：`no fmt` 與 checker 的 BuildTHIRView 共用同一份
// 糖 *Program，H0 展開時若原地改寫 body.Statements，formatter 就會把合成的
// `__bm_N: { … }` 當成使用者寫的陳述印出來（且重跑會不斷疊加）。因此這裡只
// 複製「通往 `?=` 的容器鏈」，葉子陳述指標保持共用 —— node-keyed 副表
// （TypeFacts.InferredType 等以 LetStatement 為鍵）不會失效。
//
// Subject 在 parse time 就寫進 AST（gob-safe），所以 H0、js/wasm、checker
// 三條路徑看到同名變數，HIR-equivalence seam 才能逐位元一致。

import (
	"fmt"
	"reflect"

	"github.com/lizongying/nolang/lexer"
)

var (
	blockMatchPtrType = reflect.TypeOf((*BlockMatchStatement)(nil))
	bmStmtSliceType   = reflect.TypeOf([]Statement(nil))
	bmStmtIfaceType   = reflect.TypeOf((*Statement)(nil)).Elem()
	bmUnwrapPtrType   = reflect.TypeOf((*UnwrapAssignStatement)(nil))
)

// blockMatchHaltName 是「已經傳播過」的 i64 旗標名，由 subject 派生，天然唯一。
func blockMatchHaltName(subject string) string { return subject + "_halt" }

// blockMatchTail 是 whole-option propagation 之後的控制流尾巴：函式層級 `?=`
// 用 bare return 上拋；塊 match 下不 return，改設 halt 旗標，由
// `if __bm_N_halt == 0` 守衛停住後續陳述。
func blockMatchTail(subject string, tok lexer.Token) []Statement {
	if subject != "" {
		return []Statement{&ExpressionStatement{
			Token: tok,
			Expression: &AssignExpression{
				Token: tok,
				Left:  &Identifier{Token: tok, Value: blockMatchHaltName(subject)},
				Value: &IntegerLiteral{Token: tok, Value: 1},
			},
		}}
	}
	return []Statement{&ReturnStatement{Token: tok}}
}

// blockMatchGuard 產出 `__bm_N_halt == 0`。
func blockMatchGuard(tok lexer.Token, subject string) Expression {
	return &InfixExpression{
		Token:    tok,
		Left:     &Identifier{Token: tok, Value: blockMatchHaltName(subject)},
		Operator: "==",
		Right:    &IntegerLiteral{Token: tok, Value: 0},
	}
}

// blockMatchNilGuard 保留舊名給 lowering.go 的 guardOptionOperands 路徑使用。
func blockMatchNilGuard(tok lexer.Token, subject string) Expression {
	return blockMatchGuard(tok, subject)
}

type bmExpander struct {
	p          *Parser
	tok        lexer.Token
	subject    string
	resultName string
	seq        int // 這個塊 match 內每個 `?=` 的唯一暫存名序號
}

// expandBlockMatch 把 bm 展開成一只全新的 *BlockStatement。
//
// 回傳的是**新節點**：bm.Body 與其陳述完全不被改寫，所以同一份糖 AST 可以
// 先被 BuildTHIRView 展開、再被 no fmt 原樣印出。多次呼叫結果等價（名稱由
// parse-time Subject + 區內序號決定），H0 與 js/wasm 各自展開也不會互相干擾。
func expandBlockMatch(p *Parser, bm *BlockMatchStatement, resultName string) Statement {
	if bm == nil || bm.Body == nil {
		return nil
	}
	subject := bm.Subject
	if subject == "" {
		subject = "__bm_0"
	}
	tok := bm.Body.Token
	// H0 的合成節點不經 checker，顯式型別是必要的；同時註冊型別，
	// 讓 desugarSurfaceMatch / emitSubjectMatchHIR 能把 arms 判成 option match。
	p.setVarType(subject, "?i64")

	e := &bmExpander{p: p, tok: tok, subject: subject, resultName: resultName}
	decl := &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: subject},
		Type:        &NullableType{Token: tok, Type: &NamedType{Token: tok, Value: "i64"}, IsInferred: true},
		Value:       &NilLiteral{Token: tok},
		IsSynthetic: true,
	}
	halt := &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: blockMatchHaltName(subject)},
		Type:        &NamedType{Token: tok, Value: "i64"},
		Value:       &IntegerLiteral{Token: tok, Value: 0},
		IsSynthetic: true,
	}
	body := e.rewriteList(bm.Body.Statements, subject)

	all := make([]Statement, 0, len(body)+3)
	all = append(all, decl, halt)
	all = append(all, body...)
	if bm.Match != nil {
		// 糖 AST 的 Match.Matched 必須保持 nil（no fmt 才會印 bare `{`），
		// 這裡只用 shallow copy 掛上 subject，不改原節點、不改 arms。
		cp := *bm.Match
		cp.Matched = &Identifier{Token: cp.Token, Value: subject}
		cp.PreChecked = false
		all = append(all, &ExpressionStatement{Token: cp.Token, Expression: &cp})
	}
	out := &BlockStatement{
		Token:               bm.Body.Token,
		RBrace:              bm.Body.RBrace,
		Statements:          all,
		IsInline:            bm.Body.IsInline,
		IsWrapper:           bm.Body.IsWrapper,
		CommentedNode:       bm.Body.CommentedNode,
		TrailingComments:    bm.Body.TrailingComments,
		ClosingBraceComment: bm.Body.ClosingBraceComment,
		BetweenComments:     bm.Body.BetweenComments,
	}
	return out
}

// rewriteList 是 CPS：遇到第一個「可能寫入 subject」的陳述後，把其餘陳述
// 整包關進 `if __bm_N_halt == 0`，並遞迴處理其餘陳述。整包（而非逐條）關進去
// 是為了不切斷宣告的可見性。
func (e *bmExpander) rewriteList(stmts []Statement, subject string) []Statement {
	out := make([]Statement, 0, len(stmts)+2)
	for i, s := range stmts {
		if isNil(s) {
			out = append(out, s)
			continue
		}
		if uas, ok := s.(*UnwrapAssignStatement); ok {
			out = append(out, e.expandUnwrap(uas, subject))
			if rest := stmts[i+1:]; len(rest) > 0 {
				out = append(out, e.guardStmt(subject, e.rewriteList(rest, subject)))
			}
			return out
		}
		if e.containsUnwrap(reflect.ValueOf(s)) {
			ns := e.rewriteValue(reflect.ValueOf(s), map[uintptr]reflect.Value{}).Interface().(Statement)
			if fs, ok := ns.(*ForStatement); ok && !fs.IsCondWrapper {
				e.wrapLoopBody(fs, subject)
			}
			out = append(out, ns)
			if rest := stmts[i+1:]; len(rest) > 0 {
				out = append(out, e.guardStmt(subject, e.rewriteList(rest, subject)))
			}
			return out
		}
		out = append(out, s)
	}
	return out
}

// guardStmt 把 rest 關進 `if __bm_N_halt == 0 { ... }`。
func (e *bmExpander) guardStmt(subject string, rest []Statement) Statement {
	if len(rest) == 0 {
		return &EmptyStatement{Token: e.tok}
	}
	return &ExpressionStatement{
		Token: e.tok,
		Expression: &IfExpression{
			Token:       e.tok,
			Condition:   blockMatchGuard(e.tok, subject),
			Consequence: &BlockStatement{Token: e.tok, Statements: rest},
		},
	}
}

// wrapLoopBody 讓迴圈主體的每一次 iteration 都先檢查 halt 旗標：
// 一次 `?=` 失敗後，剩餘 iteration 全部空轉，不再執行任何使用者陳述。
func (e *bmExpander) wrapLoopBody(fs *ForStatement, subject string) {
	if fs == nil || fs.Body == nil {
		return
	}
	inner := e.guardStmt(subject, []Statement{fs.Body})
	fs.Body = &BlockStatement{
		Token:      fs.Body.Token,
		RBrace:     fs.Body.RBrace,
		Statements: []Statement{inner},
	}
}

// expandUnwrap 為單一 `?=` 建一隻新的 lowerer（與 H0 的
// lowerUnwrapAssignToHIR 同型），把 blockMatchSubject 設成這個塊 match 的
// subject。如此 resultName 探測、D9 分支、guardOptionOperands、型別推斷等
// 全部沿用既有實作，只是失敗臂改成「寫 subject + 設 halt、不 return」。
func (e *bmExpander) expandUnwrap(uas *UnwrapAssignStatement, subject string) Statement {
	e.seq++
	tok := uas.Token
	l := &lowerer{
		p:                  e.p,
		visited:            map[uintptr]bool{},
		declaredLocals:     map[string]bool{},
		curFuncName:        e.p.curFuncName,
		resultNameOverride: e.resultName,
		blockMatchSubject:  subject,
		unwrapTmpOverride:  fmt.Sprintf("%s_u%d", subject, e.seq),
	}
	stmt := l.lowerUnwrapAssign(uas)
	if stmt == nil {
		return &EmptyStatement{Token: tok}
	}
	return stmt
}

// containsUnwrap 判斷某陳述的子樹裡是否有「會被本塊 match 捕捉」的 `?=`。
// FunctionDefinition 與巢狀 BlockMatchStatement 是邊界（後者自己捕捉），
// 其餘（裸塊 / if / 迴圈 / match arms / FunctionLiteral）一律遞迴。
func (e *bmExpander) containsUnwrap(v reflect.Value) bool {
	return e.scanUnwrap(v, map[uintptr]bool{})
}

func (e *bmExpander) scanUnwrap(v reflect.Value, seen map[uintptr]bool) bool {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return false
		}
		return e.scanUnwrap(v.Elem(), seen)
	case reflect.Ptr:
		if v.IsNil() {
			return false
		}
		switch v.Interface().(type) {
		case *UnwrapAssignStatement:
			return true
		case *FunctionDefinition, *BlockMatchStatement,
			*SemanticContext, *CommentGroup, *Comment, *AnnotationEntry:
			return false
		}
		if seen[v.Pointer()] {
			return false
		}
		seen[v.Pointer()] = true
		return e.scanUnwrap(v.Elem(), seen)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if e.scanUnwrap(v.Index(i), seen) {
				return true
			}
		}
	case reflect.Struct:
		for _, i := range exportedFieldIndices(v.Type()) {
			if e.scanUnwrap(v.Field(i), seen) {
				return true
			}
		}
	}
	return false
}

// rewriteValue 是 copy-on-write 的樹改寫：只複製「通往 `?=`」的容器節點，
// 其餘子樹原樣共用指標。回傳值可直接 Set 回父欄位；memo 保留共享子樹的
// 指標同一性（同一節點被兩個欄位引用時不會複製成兩份）。
func (e *bmExpander) rewriteValue(v reflect.Value, memo map[uintptr]reflect.Value) reflect.Value {
	if !v.IsValid() || !e.containsUnwrap(v) {
		return v
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		elem := v.Elem()
		if elem.Kind() == reflect.Ptr && elem.Type() == bmUnwrapPtrType {
			expanded := e.expandUnwrap(elem.Interface().(*UnwrapAssignStatement), e.subject)
			if !reflect.TypeOf(expanded).Implements(v.Type()) {
				return v
			}
			out := reflect.New(v.Type()).Elem()
			out.Set(reflect.ValueOf(expanded))
			return out
		}
		inner := e.rewriteValue(elem, memo)
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out
	case reflect.Ptr:
		if v.IsNil() {
			return v
		}
		switch v.Interface().(type) {
		case *FunctionDefinition, *BlockMatchStatement,
			*SemanticContext, *CommentGroup, *Comment, *AnnotationEntry:
			return v
		}
		if cp, ok := memo[v.Pointer()]; ok {
			return cp
		}
		np := reflect.New(v.Type().Elem())
		memo[v.Pointer()] = np
		np.Elem().Set(v.Elem()) // 未匯出欄位原樣帶過（與 clone.go 同口徑）
		for _, i := range exportedFieldIndices(v.Elem().Type()) {
			f := v.Elem().Field(i)
			if !e.containsUnwrap(f) {
				continue
			}
			np.Elem().Field(i).Set(e.rewriteValue(f, memo))
		}
		return np
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		if v.Type() == bmStmtSliceType {
			return reflect.ValueOf(e.rewriteList(v.Interface().([]Statement), e.subject))
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(e.rewriteValue(v.Index(i), memo))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := 0; i < v.Len(); i++ {
			if e.containsUnwrap(v.Index(i)) {
				out.Index(i).Set(e.rewriteValue(v.Index(i), memo))
			}
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for _, i := range exportedFieldIndices(v.Type()) {
			f := v.Field(i)
			if e.containsUnwrap(f) {
				out.Field(i).Set(e.rewriteValue(f, memo))
			}
		}
		return out
	}
	return v
}
