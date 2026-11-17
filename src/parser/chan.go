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
	match := p.parseMatchExprFrom(subj)
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
	return &ForStatement{Token: arrowTok, IterRange: iter, Body: body}
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

// detectChanLoopForm 在 body 解析完成後定形通道接收迴圈：
//
//	<- ch: { ok -> ... }      三態 ?T（body 自己 match 迴圈變數）
//	i <- ch: { co bar(i) }    元素 T（body 不 match，只取值）
//
// 判據是語法的，不是型別的：body 的第一條陳述若正好是對迴圈變數的 match
// （`v: { ... }`），作者顯然要自己分辨 ok/nil/err，變數就該是三態 ?T；否則
// 他只想要值，變數就是 T，而 nil/err 等同「沒有下一個」而結束迴圈。這條規則
// 讓兩種形態在原始碼裡一眼可辨，不需要型別推斷或額外標註。
func (p *Parser) detectChanLoopForm(stmt *ForStatement) {
	if stmt == nil || stmt.IterRange == nil || !stmt.IterRange.IsChan {
		return
	}
	ir := stmt.IterRange
	ir.ChanValue = !bodyStartsWithMatchOn(stmt.Body, ir.Variable)
}

// bodyStartsWithMatchOn 回報區塊的第一條陳述是否為 `name: { ... }`。
func bodyStartsWithMatchOn(b *BlockStatement, name string) bool {
	if b == nil || name == "" || len(b.Statements) == 0 {
		return false
	}
	es, ok := b.Statements[0].(*ExpressionStatement)
	if !ok {
		return false
	}
	subj := matchSubjectOf(es.Expression)
	if subj == nil {
		return false
	}
	id, ok := subj.(*Identifier)
	return ok && id.Value == name
}

// matchSubjectOf 取出 match 節點的被匹配表達式；非 match 回 nil。
// 表層 match 走 SurfaceMatch；已經降糖過的形式（裸 match `{ ... }`）沒有
// 被匹配者，同樣回 nil。
func matchSubjectOf(e Expression) Expression {
	if sm, ok := e.(*SurfaceMatch); ok {
		return sm.Matched
	}
	return nil
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
