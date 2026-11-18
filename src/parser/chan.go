package parser

import (
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/ty"
)

// chan.go — 通道語法的解析入口。
//
// 三個語法共用 `<-`：
//
//	ch <- v         發送（陳述）
//	v <- ch: { ... } 接收迴圈（每輪綁定三態 ?T；nil=已關閉、err=打開但空）
//	<- ch: { ... }   接收迴圈，丟棄迴圈變數（body 直接是 match 臂）
//
// 發送與接收的區分靠**左側宣告型別**：左側是 chan[T] 才是發送，其餘維持
// for-range 原義。型別不可知時退回語法判據（見 chanSendAhead）。

// synthesizeChanInit 為裸通道宣告補上初始值。
//
//	ch chan[i64]              →  ch chan[i64] = with-cap(0)
//	ch chan[i64] = with-cap(8)   原樣保留
//
// 零值的通道句柄是 null，`ch <- v` 在運行期會 panic；而 `chan` 沒有可供
// 「隱式零值」使用的字面量（null 不是 Nolang 的值），所以分配必須在宣告處
// 發生。容量 0 不額外分配緩衝，與 send 時按需增長（首次 4）配合。
func (p *Parser) synthesizeChanInit(stmt *LetStatement, tok lexer.Token) {
	if stmt.Value != nil {
		return
	}
	if _, isChan := stmt.Type.(*ChanType); !isChan {
		return
	}
	stmt.Value = &CallExpression{
		Token:    tok,
		Function: &Identifier{Token: tok, Value: "with-cap"},
		Arguments: []Expression{
			&IntegerLiteral{Token: tok, Value: 0, Raw: "0"},
		},
	}
	// 記下「這份初始值是合成的」：fmt 只印型別標註，不印 `= with-cap(0)`。
	p.fmtTrivia.SetRTFlag(stmt, RTChanDecl)
}

// chanVarType 回報變數宣告型別是否為 chan[T]，是則給出其結構型別。
// 先查函式作用域（區域變數/參數），再回退模組層。
func (p *Parser) chanVarType(name string) (*ty.Type, bool) {
	if name == "" || p.sem == nil {
		return nil, false
	}
	if t, ok := p.sem.FuncVarTypeRef(p.curFuncName, name); ok && t != nil && t.Kind == ty.Chan {
		return t, true
	}
	if t, ok := p.sem.VarTypes[name]; ok && t != nil && t.Kind == ty.Chan {
		return t, true
	}
	return nil, false
}

// parseChanSendStatement 解析 `ch <- value`。
// 前置條件：currentToken 是通道名 IDENT，peekToken 是 ARROW。
func (p *Parser) parseChanSendStatement() Statement {
	nameTok := p.currentToken
	chanExpr := &Identifier{Token: nameTok, Value: nameTok.Literal}
	p.nextToken() // IDENT -> ARROW
	arrowTok := p.currentToken
	p.nextToken() // skip ARROW
	value := p.parseExpression(LOWEST)
	if value == nil {
		return nil
	}
	if !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
		p.skipToStatementEnd()
	}
	return &ChanSendStatement{Token: arrowTok, Chan: chanExpr, Value: value}
}

// chanRecvVarName 是 `<- ch: { arms }` 脫糖時合成的迴圈變數名。
// 以 `__` 前綴與使用者識別字隔離（與 async 組的 `__ag0` 同慣例）。
const chanRecvVarName = "__chan_in"

// parseChanRecvSugar 解析 `<- ch: { arms }` 並就地脫糖成
//
//	__chan_in <- ch: {
//	    __chan_in: { arms }
//	}
//
// 前置條件：currentToken 是 ARROW，且語句以此開頭。
func (p *Parser) parseChanRecvSugar() Statement {
	arrowTok := p.currentToken // <-
	p.nextToken()              // skip <-
	chanExpr := p.parseChanRecvSource()
	if chanExpr == nil {
		return nil
	}
	if p.currentToken.Type != lexer.COLON {
		p.saveErrorf("chnrcvcl", "line %d, column %d: expected ':' after the channel in '<- ch: { ... }', got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		return nil
	}
	p.nextToken() // COLON -> LBRACE
	subj := &Identifier{Token: arrowTok, Value: chanRecvVarName}
	// 合成的 match 是**迴圈體**，永遠不是值：臂以賦值結尾（`ok -> r = it`）是常態。
	// 外層若在值語境（典型 `x = v: { 1 -> { … <- c: { ok -> r = it } … } }`），
	// CTX_EXPR 會一路帶著，validateMatchArmReturns 於是要求每臂以表達式結尾，
	// 誤報 9q9raufb 在 `<- c: {` 這一行上（臂體裡的語句其實沒錯）。
	savedCtx := p.ctx
	p.ctx = p.ctx.filterOut(CTX_EXPR)
	match := p.parseMatchExprFrom(subj)
	p.ctx = savedCtx
	if match == nil {
		return nil
	}
	if !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
		p.skipToStatementEnd()
	}
	body := &BlockStatement{
		Token: arrowTok,
		Statements: []Statement{
			&ExpressionStatement{Token: arrowTok, Expression: match},
		},
	}
	iter := &IterationExpr{
		Token:     arrowTok,
		Variable:  chanRecvVarName,
		RangeExpr: chanExpr,
		IsChan:    true,
	}
	forStmt := &ForStatement{Token: arrowTok, IterRange: iter, Body: body}
	// fmt 據此印回 `<- ch: { ... }`（不印合成的迴圈變數與內層 match）。
	p.fmtTrivia.SetRTFlag(forStmt, RTChanRecv)
	return forStmt
}

// parseChanRecvSource 解析 `<-` 之後、`:` 之前的接收源。
//
// 刻意不用通用的 parseExpression：`<- ch: { ... }` 的 `ch` 後面緊跟 `:`，
// 而 `ch: { arms }` 正是 match 的拼法，通用解析會把整段 match 當成接收源吃掉，
// 於是 `:` 永遠等不到（症狀：「expected ':' after the channel in '<- ch: {...}'，
// got NEWLINE」）。這裡只認識別字（含 `a.b` 點號鏈）——通道一定是個名字，
// 不是需要完整優先級解析的表達式。
func (p *Parser) parseChanRecvSource() Expression {
	if p.currentToken.Type != lexer.IDENT {
		p.saveErrorf("chnrcvex", "line %d, column %d: expected the channel name after '<-', got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		return nil
	}
	var expr Expression = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
	p.nextToken()
	for p.currentToken.Type == lexer.DOT {
		p.nextToken() // skip .
		if p.currentToken.Type != lexer.IDENT {
			p.saveErrorf("chnrcvdt", "line %d, column %d: expected a field name after '.', got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			return nil
		}
		expr = &DotExpression{Token: p.currentToken, Receiver: expr, Property: p.currentToken.Literal}
		p.nextToken()
	}
	return expr
}

// parseChanLoopBody 解析通道接收迴圈 `v <- ch: { ... }` 的 body，並定形三態 /
// 元素兩種形態。
//
// 判據必須在**解析 body 之前**用 token 取得，不能等 body 解析完再看 AST：
// 區塊裡第一條陳述如果是 match（顯式主語 `v: { ... }` 或裸 `{ ok -> ... }`），
// parseBlockStatement 會立刻把它降糖成 IfExpression 鏈，SurfaceMatch 已經不在，
// 事後再也認不出原意（歷史症狀：`i <- ch:` 永遠被當成元素形態）。
//
//	{ co bar(i) }           元素 T：body 是普通陳述
//	{ print(i) }            元素 T
//	{ i: { ok -> ... } }    三態 ?T：body 自己 match 迴圈變數
//	{ ok -> ... nil -> ... } 三態 ?T：省掉 `v:` 的簡寫
//
// 最後一種與 `<- ch: { ... }` 同義，主語就是迴圈變數——這裡直接把 body 當成
// 「對 `v` 的 match」解析（等同 parseChanRecvSugar 對無變數形式做的事）。少了
// 這一步，裸 match 沒有被匹配者：臂上的 ok/nil/err 退化成獨立 guard（每輪全跑），
// `it` 綁到空值，`co f(it)` 的實參被降成 void。
//
// 呼叫時 currentToken 必須在 body 的 `{` 上。
func (p *Parser) parseChanLoopBody(stmt *ForStatement) *BlockStatement {
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	ir := stmt.IterRange
	if p.currentToken.Type != lexer.LBRACE {
		return p.parseLoopBody()
	}
	subjectForm, bareArms := p.chanLoopBodyAhead(ir.Variable)
	if !subjectForm {
		ir.ChanValue = true
		return p.parseLoopBody()
	}
	ir.ChanValue = false
	if !bareArms {
		// 顯式主語：body 自己就是 match，照常解析。
		return p.parseLoopBody()
	}
	tok := p.currentToken // {
	p.nextToken()         // skip {
	subj := &Identifier{Token: tok, Value: ir.Variable}
	// 與 parseChanRecvSugar 同一個理由：合成的 match 是迴圈體，不是值。
	savedCtx := p.ctx
	p.ctx = p.ctx.filterOut(CTX_EXPR)
	match := p.parseMatchExprFrom(subj)
	p.ctx = savedCtx
	body := &BlockStatement{Token: tok}
	if match != nil {
		body.Statements = []Statement{&ExpressionStatement{Token: tok, Expression: match}}
	}
	// 這裡的 `}` 由 parseMatchExprFrom 吃掉；與 parseLoopBody 的約定一致
	//（後者呼叫 parseBlockStatement 後自己 nextToken 跳過 `}`），呼叫端看到的
	// currentToken 都是 body 之後的第一個 token。
	return body
}

// chanLoopBodyAhead 在不移動解析位置的前提下，回報 body 開頭是否為三態寫法，
// 以及它是否為「只有臂、沒有主語」的簡寫形式。currentToken 必須在 `{` 上。
func (p *Parser) chanLoopBodyAhead(varName string) (subjectForm, bareArms bool) {
	if p.currentToken.Type != lexer.LBRACE || varName == "" {
		return false, false
	}
	i := 0
	t1 := p.peekAhead(i)
	for t1.Type == lexer.NEWLINE {
		i++
		t1 = p.peekAhead(i)
	}
	t2 := p.peekAhead(i + 1)
	t3 := p.peekAhead(i + 2)
	// `v: { ... }`——顯式主語。
	if t1.Type == lexer.IDENT && t1.Literal == varName &&
		t2.Type == lexer.COLON && t3.Type == lexer.LBRACE {
		return true, false
	}
	// `ok -> ...` / `nil -> ...` / `err -> ...`——省略主語的三態簡寫。
	// 只認這三個臂名：`{ i > 5 -> ... }` 是元素形態下合法的獨立 guard，
	// 不能被誤判成三態。臂的箭頭是 RARROW（`->`），與迴圈的 ARROW（`<-`）不同。
	if t2.Type == lexer.RARROW || t2.Type == lexer.ARROW {
		switch t1.Literal {
		case "ok", "nil", "err":
			return true, true
		}
	}
	return false, false
}

// peekAhead 取 currentToken 之後第 k 個 token（k=0 是 peekToken），
// 與 p.look 的 0 基約定對齊（look(0) 是 currentToken 之後的第 2 個）。
func (p *Parser) peekAhead(k int) lexer.Token {
	if k <= 0 {
		return p.peekToken
	}
	return p.look(k - 1)
}

// chanIterIsChan 判斷 `v <- X:` 的 X 是否為通道，是則把迭代標記為通道接收。
// 只認識別字形式的接收源（`v <- ch`），與 for-range 的既有分派保持一致。
func (p *Parser) chanIterIsChan(ir *IterationExpr) bool {
	if ir == nil {
		return false
	}
	id, ok := ir.RangeExpr.(*Identifier)
	if !ok {
		return false
	}
	_, isChan := p.chanVarType(id.Value)
	return isChan
}

// chanElemTypeString 由 chan[T] 的原始拼寫取出元素型別拼寫（`chan[i64]` → `i64`）。
// 用於把接收迴圈的變數型別登記成 `?T`（三態）。
func chanElemTypeString(chanType string) string {
	if !strings.HasPrefix(chanType, "chan[") || !strings.HasSuffix(chanType, "]") {
		return ""
	}
	return chanType[len("chan[") : len(chanType)-1]
}
