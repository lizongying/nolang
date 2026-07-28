// annotation.go — @ 注解语句、注解体/值/数组/区间解析与挂载。
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// parseAnnotationStatement 解析 #{...} 註解語句。
//
//	#{c}
//	_name = (params) (results)
//
// 或
//
//	#{derive=[Serialize, Deserialize], range=[0..256), max=100, debug}
//
// 當註解包含 FFI 語言鍵（c、cpp、rust 等）且後續為函式宣告時，
// 轉換為 ExternStatement。
//
// 對於非 FFI 註解，若後續為宣告（let、struct definition、function definition），
// 註解條目會附加到該宣告上；否則作為獨立 AnnotationStatement 保留。
func (p *Parser) parseAnnotationStatement() Statement {
	// currentToken 為 HASH_LBRACE (#{)
	annotToken := p.currentToken
	p.nextToken() // skip #{

	entries := p.parseAnnotationBody()

	// 預期 '}'
	if p.currentToken.Type != lexer.RBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected '}' to close annotation, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip }

	annotStmt := &AnnotationStatement{
		Token:   annotToken,
		Entries: entries,
	}

	// 檢查是否為 FFI 註解
	ffiLang := annotStmt.GetFFILang()
	if ffiLang != "" {
		// 跳過 NEWLINE，檢查後續是否為函式宣告
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		// 檢查是否為 IDENT = ( ... 格式的 FFI 宣告
		if p.currentToken.Type == lexer.IDENT {
			// 收集非 FFI 語言鍵的額外註解
			var extraAnnots []*AnnotationEntry
			for _, e := range entries {
				if e.Key != ffiLang {
					extraAnnots = append(extraAnnots, e)
				}
			}

			return p.parseAnnotationFFIDeclaration(annotToken, ffiLang, extraAnnots)
		}
	}

	// 非 FFI 註解：嘗試附加到後續宣告
	// 收集連續的註解條目（以空行分隔的多個 #{...}），合併後附加到後續宣告
	for {
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type != lexer.HASH_LBRACE {
			break
		}
		// 解析下一個註解並合併條目
		p.nextToken() // skip #{
		moreEntries := p.parseAnnotationBody()
		if p.currentToken.Type != lexer.RBRACE {
			msg := fmt.Sprintf("line %d, column %d: expected '}' to close annotation, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			break
		}
		p.nextToken() // skip }
		entries = append(entries, moreEntries...)
		annotStmt.Entries = entries
	}

	// 若後續為 IDENT 開頭的宣告，附加註解
	if p.currentToken.Type == lexer.IDENT {
		// 暫存註解條目，解析下一個語句後附加
		p.pendingAnnotations = entries
		stmt := p.parseStatement()
		if stmt != nil {
			p.attachAnnotations(stmt, entries)
			p.pendingAnnotations = nil
			return stmt
		}
		p.pendingAnnotations = nil
	}

	p.skipToStatementEnd()
	return annotStmt
}

// attachAnnotations 將解析期收集的 #{...} 註解條目記錄到語義副表（side-table）。
// 不再掛載到 AST 節點上；平台鍵/泛型參數/embed 由獨立 Resolver pass
// （ResolveProgram）收尾計算並存入 side-table。
func (p *Parser) attachAnnotations(stmt Statement, entries []*AnnotationEntry) {
	p.sem.SetRawAnnotations(stmt, entries)
}

// extractGenericParams 從註解條目中找出 generic 鍵的陣列值，提取型別參數名稱列表。
// 例如 #{generic=[k,v]} 會回傳 ["k", "v"]；若無 generic 鍵則回傳 nil。
func extractGenericParams(entries []*AnnotationEntry) []string {
	for _, e := range entries {
		if e.Key != "generic" {
			continue
		}
		arr, ok := e.Value.(*AnnotationArrayValue)
		if !ok {
			continue
		}
		var params []string
		for _, el := range arr.Elements {
			if ident, ok := el.(*AnnotationIdentValue); ok {
				params = append(params, ident.Value)
			}
		}
		return params
	}
	return nil
}

// parseAnnotationFFIDeclaration 從 #{c} 註解建立 ExternStatement。
// 與 parseFFIDeclaration 類似，但使用來自註解的語言名稱和額外註解。
func (p *Parser) parseAnnotationFFIDeclaration(annotToken lexer.Token, lang string, extraAnnots []*AnnotationEntry) Statement {
	stmt := &ExternStatement{
		Token:      annotToken,
		Lang:       lang,
		Parameters: []*Parameter{},
		Results:    []*Parameter{},
	}
	// FFI 額外註解條目記錄到語義副表（side-table），不掛載到節點。
	if len(extraAnnots) > 0 {
		p.sem.SetRawAnnotations(stmt, extraAnnots)
	}

	// 函式名稱（可以 _ 開頭表示私有）
	if p.currentToken.Type != lexer.IDENT {
		msg := fmt.Sprintf("line %d, column %d: expected identifier after #{%s} directive, got %s instead",
			p.currentToken.Line, p.currentToken.Column, lang, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	stmt.Name = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
	p.nextToken() // skip name

	// 預期 '='
	if p.currentToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected '=' after FFI name, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip =

	// 解析 (params)
	params, ok := p.parseExternParamList()
	if !ok {
		return nil
	}
	stmt.Parameters = params

	// 跳過 NEWLINE（多行定義）
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	// 解析 (results) — 選擇性
	if p.currentToken.Type == lexer.LPAREN {
		results, ok := p.parseExternParamList()
		if !ok {
			return nil
		}
		stmt.Results = results
	}

	p.skipToStatementEnd()
	return stmt
}

// parseAnnotationBody 解析註解體內的鍵值對列表（不含外層 { 和 }）。
// 前置條件: currentToken 為第一個鍵名或 '}'；後置條件: currentToken 為 '}'。
func (p *Parser) parseAnnotationBody() []*AnnotationEntry {
	var entries []*AnnotationEntry

	for {
		// 跳過 NEWLINE
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		if p.currentToken.Type == lexer.RBRACE {
			break
		}

		// 鍵名（IDENT）
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected annotation key, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return entries
		}

		keyTok := p.currentToken
		key := p.currentToken.Literal
		p.nextToken() // skip key

		// 檢查是否有 = value
		if p.currentToken.Type == lexer.ASSIGN {
			p.nextToken() // skip =
			val := p.parseAnnotationValue()
			if val != nil {
				entries = append(entries, &AnnotationEntry{
					Key:   key,
					Value: val,
					Token: keyTok,
				})
			}
		} else {
			// 獨立布爾鍵（無值）
			entries = append(entries, &AnnotationEntry{
				Key:   key,
				Value: nil,
				Token: keyTok,
			})
		}

		// 跳過 NEWLINE
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		// 逗號分隔或結束
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // skip ,
			continue
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		// 未預期的 token
		msg := fmt.Sprintf("line %d, column %d: expected ',' or '}' in annotation, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		break
	}

	return entries
}

// parseAnnotationValue 解析註解值。
// 支援：整數、字串、識別字、陣列 [a, b, ...]、範圍 [0..256) 等。
func (p *Parser) parseAnnotationValue() AnnotationValue {
	// 範圍語法：以 [ 或 ( 開頭，後跟 value..value) 或 ]
	if p.currentToken.Type == lexer.LPAREN {
		return p.parseAnnotationRange(false) // ( = left exclusive
	}
	if p.currentToken.Type == lexer.LBRACKET {
		openTok := p.currentToken
		// 可能是陣列或範圍，需向前看
		// 暫存 parser 狀態以便回溯（含 ring 讀游標，不回寫 lexer 狀態）
		saveState := p.saveState()

		// 嘗試解析為範圍
		p.nextToken() // skip [
		// 跳過 NEWLINE
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		// 如果第一個元素後面是 ..，就是範圍
		firstVal := p.parseAnnotationSimpleValue()
		if firstVal != nil && p.currentToken.Type == lexer.ELLIPSIS {
			// 是範圍
			p.nextToken() // skip ..
			endVal := p.parseAnnotationSimpleValue()
			// 期望 ) 或 ]
			rightInc := false
			if p.currentToken.Type == lexer.RBRACKET {
				rightInc = true
				p.nextToken() // skip ]
			} else if p.currentToken.Type == lexer.RPAREN {
				rightInc = false
				p.nextToken() // skip )
			} else {
				msg := fmt.Sprintf("line %d, column %d: expected ']' or ')' to close range, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
			}
			return &AnnotationRangeValue{
				Token:    openTok,
				Start:    firstVal,
				End:      endVal,
				LeftInc:  true, // [
				RightInc: rightInc,
			}
		}
		// 不是範圍，回溯並解析為陣列
		p.restoreState(saveState)
		return p.parseAnnotationArray()
	}

	return p.parseAnnotationSimpleValue()
}

// parseAnnotationSimpleValue 解析簡單註解值：整數、字串、識別字、布爾。
func (p *Parser) parseAnnotationSimpleValue() AnnotationValue {
	switch p.currentToken.Type {
	case lexer.INT:
		n, err := strconv.ParseInt(p.currentToken.Literal, 10, 64)
		if err != nil {
			n = 0
		}
		val := &AnnotationIntValue{
			Token: p.currentToken,
			Value: n,
		}
		p.nextToken()
		return val
	case lexer.FLOAT:
		// 浮點數暫時以字串形式儲存
		val := &AnnotationIdentValue{
			Token: p.currentToken,
			Value: p.currentToken.Literal,
		}
		p.nextToken()
		return val
	case lexer.STRING:
		val := &AnnotationStringValue{
			Token: p.currentToken,
			Value: p.currentToken.Literal,
		}
		p.nextToken()
		return val
	case lexer.TRUE:
		val := &AnnotationBoolValue{
			Token: p.currentToken,
		}
		p.nextToken()
		return val
	case lexer.FALSE:
		// false 作為布爾值，但鍵存在表示 true，這裡用特殊處理
		val := &AnnotationBoolValue{
			Token: p.currentToken,
		}
		p.nextToken()
		return val
	case lexer.IDENT:
		// 检查是否为裸路径（如 assets/win-icon.ico）
		// 当 IDENT 后跟 QUO('/') 或 DOT('.') 时，拼接为完整路径
		firstTok := p.currentToken
		if p.peekToken.Type == lexer.QUO || p.peekToken.Type == lexer.DOT {
			var pathParts []string
			pathParts = append(pathParts, firstTok.Literal)
			p.nextToken() // skip first IDENT
			for p.currentToken.Type == lexer.QUO || p.currentToken.Type == lexer.DOT {
				if p.currentToken.Type == lexer.QUO {
					pathParts = append(pathParts, "/")
				} else {
					pathParts = append(pathParts, ".")
				}
				p.nextToken() // skip / or .
				if p.currentToken.Type != lexer.IDENT {
					break
				}
				pathParts = append(pathParts, p.currentToken.Literal)
				p.nextToken() // skip IDENT
			}
			val := &AnnotationStringValue{
				Token: firstTok,
				Value: strings.Join(pathParts, ""),
			}
			return val
		}
		// 普通 IDENT 值
		val := &AnnotationIdentValue{
			Token: p.currentToken,
			Value: p.currentToken.Literal,
		}
		p.nextToken()
		return val
	default:
		msg := fmt.Sprintf("line %d, column %d: expected annotation value, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
}

// parseAnnotationArray 解析陣列值 [elem, elem, ...]。
func (p *Parser) parseAnnotationArray() AnnotationValue {
	arrTok := p.currentToken
	p.nextToken() // skip [

	var elements []AnnotationValue

	for {
		// 跳過 NEWLINE
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		if p.currentToken.Type == lexer.RBRACKET {
			break
		}

		// 元素可以是簡單值或巢狀陣列/範圍
		elem := p.parseAnnotationValue()
		if elem != nil {
			elements = append(elements, elem)
		}

		// 跳過 NEWLINE
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // skip ,
			continue
		}
		if p.currentToken.Type == lexer.RBRACKET {
			break
		}
		// 未預期的 token
		msg := fmt.Sprintf("line %d, column %d: expected ',' or ']' in array, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		break
	}

	p.nextToken() // skip ]
	return &AnnotationArrayValue{
		Token:    arrTok,
		Elements: elements,
	}
}

// parseAnnotationRange 解析範圍值 (start..end) 或 (start..end]。
// leftInc 為 false 表示左邊是 ( （排他）。
func (p *Parser) parseAnnotationRange(leftInc bool) AnnotationValue {
	rangeTok := p.currentToken
	p.nextToken() // skip ( or [

	// 跳過 NEWLINE
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	startVal := p.parseAnnotationSimpleValue()

	// 期望 ..
	if p.currentToken.Type != lexer.ELLIPSIS {
		msg := fmt.Sprintf("line %d, column %d: expected '..' in range, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip ..

	endVal := p.parseAnnotationSimpleValue()

	// 期望 ) 或 ]
	rightInc := false
	if p.currentToken.Type == lexer.RBRACKET {
		rightInc = true
		p.nextToken() // skip ]
	} else if p.currentToken.Type == lexer.RPAREN {
		rightInc = false
		p.nextToken() // skip )
	} else {
		msg := fmt.Sprintf("line %d, column %d: expected ']' or ')' to close range, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
	}

	return &AnnotationRangeValue{
		Token:    rangeTok,
		Start:    startVal,
		End:      endVal,
		LeftInc:  leftInc,
		RightInc: rightInc,
	}
}
