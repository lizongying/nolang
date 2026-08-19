// match.go — match 表达式与 ~match 语句解析、arm 探测。
package parser

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// classifyBlock 分類 `{ body }` 的型別（預測：不消耗 token，只讀 peekToken）
// 必須在 p.peekToken == LBRACE 時呼叫。
// 使用有限預測：檢查 { 後第一個非 NEWLINE token + 第二個 token。
// matchedIsOption 判斷 match 主表達式是否為 Option 變數（用於觸發完整性檢查）
func (p *Parser) matchedIsOption(matched Expression) bool {
	if matched == nil {
		return false // 裸 match 不需完整分支
	}
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := p.sem.VarTypes[ident.Value]; ok {
			return strings.HasPrefix(t, "?")
		}
	}
	return false // 未知變數預設不觸發完整性檢查
}

// isBuiltinOption returns true if matched is a built-in optional (?i64, ?str, etc.).
// Built-in optionals cannot produce err, so err branch should not be required.
func (p *Parser) isBuiltinOption(matched Expression) bool {
	if matched == nil {
		return false
	}
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := p.sem.VarTypes[ident.Value]; ok {
			if strings.HasPrefix(t, "?") {
				base := t[1:]
				switch base {
				case "i64", "i32", "i16", "i8", "u64", "u32", "u16", "u8",
					"f64", "f32", "str", "bool", "byte":
					return true
				}
			}
		}
	}
	return false
}

// parseTildeMatchStatement parses old-style ~match:
//
//	~match x { case err: ... case nil: ... default: ... }
//
// Desugars to match expression: x { err-> ... nil-> ... -> ... }
func (p *Parser) parseTildeMatchStatement() Statement {
	p.saveWarning(fmt.Sprintf("line %d, column %d: '~match/case/default' is deprecated, use 'x: { pattern-> ... }' instead",
		p.currentToken.Line, p.currentToken.Column))

	tok := p.currentToken
	p.nextToken() // skip ~
	p.nextToken() // skip match

	// Parse matched expression
	cond := p.parseExpression(LOWEST)

	// Expect {
	if p.currentToken.Type != lexer.LBRACE {
		p.saveError(fmt.Sprintf("line %d, column %d: expected '{' after ~match expression, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return nil
	}
	p.nextToken() // skip {

	// Parse case arms → desugar to if/elif/else chain
	var arms []matchArm
	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
			break
		}

		var ma matchArm
		if p.currentToken.Type == lexer.CASE {
			p.nextToken() // skip case
			// Parse pattern: err, nil, or expression
			if p.currentToken.Type == lexer.IDENT {
				ma.condition = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
				p.nextToken()
			} else {
				ma.condition = p.parseExpression(LOWEST)
			}
		} else if p.currentToken.Type == lexer.DEFAULT {
			ma.isWildcard = true
			p.nextToken() // skip default
		} else {
			p.saveError(fmt.Sprintf("line %d, column %d: expected 'case' or 'default', got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
			p.skipToStatementEnd()
			continue
		}

		// Expect :
		if p.currentToken.Type == lexer.COLON {
			p.nextToken() // skip :
		}

		// Parse body until next case/default/}
		var bodyStmts []Statement
		for p.currentToken.Type != lexer.CASE && p.currentToken.Type != lexer.DEFAULT &&
			p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
			if p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
				continue
			}
			doc := p.collectDocComments()
			stmt := p.parseStatement()
			if stmt != nil {
				setDoc(stmt, doc)
				bodyStmts = append(bodyStmts, stmt)
			} else {
				p.nextToken()
			}
		}

		ma.body = &BlockStatement{
			Token:      tok,
			Statements: bodyStmts,
		}

		arms = append(arms, ma)
	}

	// Record } position and skip it
	rbracePos := lexer.Position{}
	if p.currentToken.Type == lexer.RBRACE {
		rbracePos = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
		p.nextToken()
	}

	if len(arms) == 0 {
		p.saveError(fmt.Sprintf("line %d, column %d: empty ~match statement", tok.Line, tok.Column))
		return nil
	}

	// 產出表層 AST（SurfaceMatch），desugar 延後到 lowering pass 執行。
	sm := p.newSurfaceMatch(tok, cond, arms)
	sm.RBracePos = rbracePos
	return &ExpressionStatement{Token: tok, Expression: sm}
}

// parseBareMatchExpr 解析裸 `{ cond-> body }` match（無 matched expression）
func (p *Parser) parseBareMatchExpr() Expression {
	tok := p.currentToken // LBRACE
	openBraceLine := tok.Line
	p.nextToken() // skip {

	// Separate comments on the same line as { (opening brace comments)
	// from doc comments for arm bodies.
	var openingComments *CommentGroup
	if len(p.comments) > 0 && p.comments[0].Line == openBraceLine {
		group := &CommentGroup{}
		i := 0
		for i < len(p.comments) && p.comments[i].Line == openBraceLine {
			c := p.comments[i]
			comment := &Comment{
				Pos:    posFromToken(c),
				End:    lexer.Position{Line: c.Line, Column: c.Column + len(c.Literal)},
				Kind:   NormalComment,
				Text:   c.Literal,
				Marker: c.Marker,
			}
			group.List = append(group.List, comment)
			i++
		}
		if len(group.List) > 0 {
			group.Start = group.List[0].Pos
			group.End = group.List[len(group.List)-1].End
		}
		openingComments = group
		p.comments = p.comments[i:]
	}

	var arms []matchArm

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// Skip newlines and semicolons between arms
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.SEMICOLON {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
			break
		}

		// Collect doc comments before the arm condition. Comments between
		// arms (or between { and the first arm) are buffered in p.comments
		// by advanceCollect during nextToken. Without collecting them here,
		// they would float in p.comments and eventually be mis-attached to
		// a later arm's body or lost.
		armDoc := p.collectDocComments()

		var ma matchArm
		ma.pos = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
		if p.currentToken.Type == lexer.COLON {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.UNDERSCORE {
			ma.isWildcard = true
			p.nextToken()
		} else if p.currentToken.Type == lexer.RARROW {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RARROW &&
			(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok") {
			// ok-> → val branch (specific, not catch-all)
			// err-> / nil-> → option pattern
			if p.currentToken.Literal == "ok" {
				ma.isWildcard = true
				ma.isDotVal = true
			} else {
				ma.condition = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
			}
			p.nextToken()
		} else if p.currentToken.Type == lexer.NIL && p.peekToken.Type == lexer.RARROW {
			// nil-> → option pattern
			ma.condition = &NilLiteral{Token: p.currentToken}
			p.nextToken()
		} else {
			// Parse condition as full boolean expression
			p.ctx.push(CTX_MATCH_ARM)
			ma.condition = p.parseExpression(LOWEST)
			p.ctx.pop()
		}

		// Expect -> or :
		if p.currentToken.Type == lexer.RARROW {
			p.nextToken()
		} else if p.currentToken.Type == lexer.COLON {
			p.nextToken()
		} else {
			return nil
		}

		// Statement or expression body
		var bodyStmts []Statement
		bodyBlock := &BlockStatement{Token: tok}
		var parsedBlock *BlockStatement // tracks block from parseBlockStatement for comment preservation
		if p.currentToken.Type == lexer.LBRACE {
			// Explicit block form: -> { ... }
			// 多行 arm body 必須使用大括號，以便 option-match desugar 能正確插入 `it` 綁定。
			// 若 { } 內容本身是 bare match（如 `-> { cond -> body }`），先嘗試 parseStatement
			// 處理 inline bare match；若失敗（如 block 含 match 後接其他語句），則回退為
			// parseBlockStatement 解析為敘述區塊。
			if p.classifyBlockAtCurrent() != blockMatch {
				ma.isBlockBody = true
				// 不推送 CTX_MATCH_ARM：block body 內的 `cond -> return` 應視為 standalone if-then，
				// 而非 match arm 分隔符。CTX_MATCH_ARM 僅用於 arm condition 與 inline body。
				parsedBlock = p.parseBlockStatement()
				if parsedBlock != nil {
					bodyStmts = parsedBlock.Statements
				}
				if p.currentToken.Type == lexer.RBRACE {
					p.nextToken()
				}
			} else {
				// Try inline bare match first; fall back to block on failure.
				// Don't push CTX_MATCH_ARM here: parseStatement will see LBRACE
				// and call parseBareMatchExpr, which manages its own context.
				// Pushing CTX_MATCH_ARM here would leak into nested bare match
				// arm block bodies, breaking standalone if-then (cond -> body).
				bodyOpenTok := p.currentToken // arm body's { token
				armState := p.saveState()
				stmt := p.parseStatement()
				if stmt != nil {
					ma.isBlockBody = true
					// When parseStatement returns a BlockStatement, it means the
					// inner parseBareMatchExpr failed and fell back to
					// parseBlockStatement. Extract the block's statements
					// directly instead of nesting the entire block, which
					// would cause the formatter to wrap the arm body in an
					// extra layer of braces.
					if bs, ok := stmt.(*BlockStatement); ok {
						parsedBlock = bs
						bodyStmts = bs.Statements
					} else {
						bodyStmts = append(bodyStmts, stmt)
					}
					// Record the arm body's { and } positions so the formatter
					// can correctly determine whether TrailingComments are
					// inside or outside the block. Without this, bodyBlock.Token
					// retains the outer match's { position, causing comments
					// before the arm body's { to be misclassified as "inside".
					if parsedBlock == nil {
						bodyBlock.Token = bodyOpenTok
						bodyBlock.RBrace = lexer.Position{Line: p.prevToken.Line, Column: p.prevToken.Column}
					}
					if p.currentToken.Type == lexer.RBRACE {
						p.nextToken()
					}
				} else {
					// parseStatement failed — restore and parse as block
					p.restoreState(armState)
					ma.isBlockBody = true
					// 不推送 CTX_MATCH_ARM：fallback 為普通 block，允許 standalone if-then
					parsedBlock = p.parseBlockStatement()
					if parsedBlock != nil {
						bodyStmts = parsedBlock.Statements
					}
					if p.currentToken.Type == lexer.RBRACE {
						p.nextToken()
					}
				}
			}
		} else if p.currentToken.Type == lexer.NEWLINE {
			// Block form (newline-separated statements, no braces)
			ma.isBlockBody = true
			bodyBlock.IsInline = true
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			// 不在 CTX_MATCH_ARM 下解析 body 語句（與大括號臂體一致）：
			// 臂邊界由 isArmStart() 判定；否則 body 內 standalone if-then 的 `->`
			// 會截斷語句並被誤判為新臂，導致整個 match 解析失敗（見 expr.go 同步註釋）。
			//
			// 對 wildcard 臂（->），isArmStart() 的 scanForArrowAtDepth0 會將
			// body 內的 standalone if-then（如 `pos >= 0 -> { ... }`）誤判為新臂，
			// 導致 body 被截斷、match 解析失敗。wildcard 語義上是 catch-all，
			// 必為最後一個臂，因此跳過 isArmStart() 檢查，直接解析到 }。
			checkArmStart := !ma.isWildcard
			for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF &&
				(!checkArmStart || !p.isArmStart()) &&
				!isOptionPatternStart(p) {
				if p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
					continue
				}
				doc := p.collectDocComments()
				s := p.parseStatement()
				if s != nil {
					setDoc(s, doc)
					p.attachInlineComment(s)
					bodyStmts = append(bodyStmts, s)
				}
			}
		} else {
			// Inline statement form（單行 body）
			// 使用 parseStatement 以支援 let 賦值（如 `cond -> a = 1`）與表達式（如 `cond -> print(1)`）
			// 對 catch-all arm（wildcard，如 `-> cond -> body`），不推送 CTX_MATCH_ARM，
			// 允許 body 中的 -> 被解析為 standalone if-then，而非 match arm 分隔符。
			bodyBlock.IsInline = true
			if !ma.isWildcard {
				p.ctx.push(CTX_MATCH_ARM)
			}
			// peek 預計算可能已將 body 語句同行行尾註釋（如 `return  ; D`）
			// 收集到 p.comments。若 collectDocComments 直接取走，行尾註釋
			// 會被誤當 doc comment，導致 formatter 輸出位置錯誤（跑到上一行）。
			// 這裡先暫存同行註釋，待 parseStatement 解析完 body 後由
			// attachInlineComment 正確附加為 inline comment。
			bodyLine := p.currentToken.Line
			var inlineComments []lexer.Token
			for len(p.comments) > 0 && p.comments[len(p.comments)-1].Line == bodyLine {
				inlineComments = append([]lexer.Token{p.comments[len(p.comments)-1]}, inlineComments...)
				p.comments = p.comments[:len(p.comments)-1]
			}
			doc := p.collectDocComments()
			// 重新放入同行註釋，讓 attachInlineComment 處理
			p.comments = append(p.comments, inlineComments...)
			stmt := p.parseStatement()
			if !ma.isWildcard {
				p.ctx.pop()
			}
			if stmt != nil {
				setDoc(stmt, doc)
				p.attachInlineComment(stmt)
				bodyStmts = append(bodyStmts, stmt)
			}
		}

		bodyBlock.Statements = bodyStmts
		// Preserve comments (TrailingComments) from the parsed block so that
		// comment-only block bodies (e.g. `c == 46 -> { // 允許 }`) are not lost.
		if parsedBlock != nil {
			bodyBlock.TrailingComments = parsedBlock.TrailingComments
			bodyBlock.ClosingBraceComment = parsedBlock.ClosingBraceComment
			p.sem.SetOpeningBraceComment(bodyBlock, p.sem.OpeningBraceCommentOf(parsedBlock))
			bodyBlock.RBrace = parsedBlock.RBrace
			// Preserve the actual { token so formatters can check comment
			// line numbers against the real opening brace position.
			bodyBlock.Token = parsedBlock.Token
		}
		// Attach arm doc comments collected before the arm condition.
		// For inline bodies (single statement, no braces): set as Doc on
		// the first statement so the formatter outputs it before the arm
		// condition via the canInline + hasDocComment path.
		// For block bodies (explicit { }): store as TrailingComments on
		// the body block; the formatter checks comment line numbers
		// against the block's opening brace to decide whether to emit
		// them before the condition (outside) or inside the block (inside).
		if armDoc != nil {
			if bodyBlock.IsInline && len(bodyStmts) > 0 {
				setDoc(bodyStmts[0], armDoc)
			} else if bodyBlock.TrailingComments == nil {
				bodyBlock.TrailingComments = armDoc
			} else {
				merged := &CommentGroup{}
				merged.List = append(merged.List, armDoc.List...)
				merged.List = append(merged.List, bodyBlock.TrailingComments.List...)
				merged.Start = armDoc.Start
				merged.End = bodyBlock.TrailingComments.End
				bodyBlock.TrailingComments = merged
			}
		}
		ma.body = bodyBlock
		arms = append(arms, ma)
	}

	// Record } position and skip it
	var rbracePos lexer.Position
	if p.currentToken.Type == lexer.RBRACE {
		rbracePos = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
		p.nextToken()
	}

	if len(arms) == 0 {
		return nil
	}

	// Check option match branch completeness
	hasErrArm, hasNilArm, hasValArm, hasElseArm := false, false, false, false
	for _, a := range arms {
		if a.isWildcard {
			if a.isDotVal {
				hasValArm = true
			} else {
				hasElseArm = true
			}
		} else {
			switch c := a.condition.(type) {
			case *Identifier:
				if c.Value == "err" {
					hasErrArm = true
				} else if c.Value == "nil" {
					hasNilArm = true
				}
			case *NilLiteral:
				hasNilArm = true
			}
		}
	}
	// Check option match branch completeness (3 occurrences, keep in sync)
	isBuiltinOpt2 := p.isBuiltinOption(nil)
	if !hasElseArm && ((!isBuiltinOpt2 && !hasErrArm) || !hasNilArm || !hasValArm) {
		if p.matchedIsOption(nil) {
			var missing []string
			if !isBuiltinOpt2 && !hasErrArm {
				missing = append(missing, "err")
			}
			if !hasNilArm {
				missing = append(missing, "nil")
			}
			if !hasValArm {
				missing = append(missing, "ok")
			}
			p.saveError(fmt.Sprintf("line %d, column %d: option match must handle all branches: err, nil, ok (missing: %s)",
				tok.Line, tok.Column, strings.Join(missing, ", ")))
			return nil
		}
	}

	if p.ctx.contains(CTX_EXPR) {
		if !p.validateMatchArmReturns(tok, arms) {
			return nil
		}
	}

	// 產出表層 AST（SurfaceMatch），保留開括號同行註釋，desugar 延後執行。
	sm := p.newSurfaceMatch(tok, nil, arms)
	sm.OpeningBraceComment = openingComments
	sm.RBracePos = rbracePos
	return sm
}

// isArmStart checks if the current token starts a new match arm
func (p *Parser) isArmStart() bool {
	switch p.currentToken.Type {
	case lexer.INT, lexer.UNDERSCORE, lexer.COLON, lexer.RARROW:
		return true
	case lexer.IDENT:
		if p.peekToken.Type == lexer.COLON || p.peekToken.Type == lexer.RARROW {
			return true
		}
		// nil || err -> body: combined option patterns
		if p.peekToken.Type == lexer.LOR &&
			(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok") {
			return true
		}
		// Complex boolean condition: e.g. dot-count == 1 ->, x > 0 ->
		// Scan forward to find -> at depth 0 before NEWLINE/RBRACE
		if p.peekToken.Type == lexer.EQUALS || p.peekToken.Type == lexer.NOT_EQUALS ||
			p.peekToken.Type == lexer.LESS || p.peekToken.Type == lexer.GREATER ||
			p.peekToken.Type == lexer.LESS_EQUALS || p.peekToken.Type == lexer.GREATER_EQUALS ||
			p.peekToken.Type == lexer.LAND || p.peekToken.Type == lexer.LOR {
			return p.scanForArrowAtDepth0()
		}
		return false
	case lexer.NIL:
		if p.peekToken.Type == lexer.COLON || p.peekToken.Type == lexer.RARROW {
			return true
		}
		// nil || err -> body: combined option patterns
		if p.peekToken.Type == lexer.LOR {
			return true
		}
		return false
	case lexer.NOT:
		return p.peekToken.Type == lexer.RARROW
	case lexer.QUESTION:
		return p.peekToken.Type == lexer.RARROW
	case lexer.DOT:
		return p.peekToken.Type == lexer.RARROW
	}
	return false
}

// scanForArrowAtDepth0 scans forward from the current position to find -> (RARROW)
// at parenthesis/bracket depth 0, before encountering NEWLINE or RBRACE.
// Returns true if such an arrow is found, false otherwise.
func (p *Parser) scanForArrowAtDepth0() bool {
	depth := 0
	skip := 0
	for {
		tok := p.look(skip)
		switch tok.Type {
		case lexer.NEWLINE, lexer.SEMICOLON, lexer.RBRACE, lexer.EOF:
			return false
		case lexer.LPAREN, lexer.LBRACKET:
			depth++
		case lexer.RPAREN, lexer.RBRACKET:
			depth--
			if depth < 0 {
				return false
			}
		case lexer.RARROW:
			if depth == 0 {
				return true
			}
		}
		skip++
	}
}

// isRangePatternStart 檢查當前 `[` 是否為 range pattern 的開始（如 [0..60)、[0..100]）。
// 用於 match arm inline body 上下文中，區分「下一個 arm 的 range pattern」與「當前 body 的切片操作」。
// 判斷依據：從 `[` 之後掃描到對應的 `]` 或 `)`，中途是否出現 `..` (ELLIPSIS)。
// 注意：currentToken 已是 `[`，所以 depth 從 1 開始計算。
func (p *Parser) isRangePatternStart() bool {
	if p.currentToken.Type != lexer.LBRACKET {
		return false
	}
	// currentToken 是 `[`，peekToken 是 `[` 之後的第一個 token。
	// look(n) 返回 peekToken 之後第 n 個 token（即 currentToken 之後第 n+1 個）。
	// 因此從 peekToken 開始掃描，用 look(n) 取得後續 token。
	depth := 1 // currentToken `[` 已計入
	hasEllipsis := false
	skip := -1 // -1 表示 peekToken
	for {
		var tok lexer.Token
		if skip < 0 {
			tok = p.peekToken
		} else {
			tok = p.look(skip)
		}
		if tok.Type == lexer.EOF {
			return false
		}
		if tok.Type == lexer.NEWLINE {
			// range pattern 不跨行；遇到換行表示不是 arm 開頭
			return false
		}
		switch tok.Type {
		case lexer.LBRACKET:
			depth++
		case lexer.RBRACKET:
			depth--
			if depth == 0 {
				return hasEllipsis
			}
		case lexer.RPAREN:
			// `[a..b)` 形式：`[` 開頭但以 `)` 結尾。
			// 此時 depth==1（`[` 未被 `]` 配對），若已看到 `..` 則為 range pattern。
			if depth == 1 {
				return hasEllipsis
			}
		case lexer.ELLIPSIS:
			if depth == 1 {
				hasEllipsis = true
			}
		}
		skip++
	}
}

// matchArm — match 的一個分支（用於 parseMatchExprFrom 和 buildMatchDesugar）
type matchArm struct {
	condition           Expression
	isWildcard          bool
	isDotVal            bool // .-> → specific val branch (not catch-all)
	isRawCond           bool // ok(cond) → condition is a full boolean expr, use directly (no matched == wrapping)
	body                *BlockStatement
	isBlockBody         bool           // true = block form (newline after ->), false = inline expression form
	pos                 lexer.Position // position of condition or -> for diagnostic use
	multiOptionPatterns []string       // nil || err → ["nil", "err"]; combined option patterns joined by ||
	multiValuePatterns  []Expression   // 1 || 3 || 5 → [1, 3, 5]; combined value patterns joined by ||
}

func (p *Parser) parseMatchExpression() Expression {
	tok := p.currentToken
	p.saveWarning(fmt.Sprintf("line %d, column %d: 'match' keyword is deprecated, use '<expr>: { ... }' instead",
		tok.Line, tok.Column))
	p.nextToken() // skip match

	// Determine form: match <expr> { ... } or match { ... }
	hasMatched := p.currentToken.Type != lexer.LBRACE

	var matched Expression
	if hasMatched {
		// Form 1: match <expr> { pattern: body ... }
		p.ctx.push(CTX_MATCH_COND)
		matched = p.parseExpression(LOWEST)
		p.ctx.pop()

		// Skip to {
		for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}
	}
	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected '{' in match expression, got %s",
			tok.Line, tok.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip {

	// Collect all arms first
	type matchArm struct {
		condition  Expression // if-condition (form 2) or pattern (form 1)
		isWildcard bool
		isDotVal   bool // .| → specific val branch (not catch-all)
		body       *BlockStatement
	}
	var arms []matchArm

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// Skip newlines and semicolons between arms
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.SEMICOLON {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
			break
		}

		// Parse condition/pattern (bare `:` means wildcard/default arm)
		var ma matchArm
		if p.currentToken.Type == lexer.COLON {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.UNDERSCORE {
			ma.isWildcard = true
			p.nextToken()
		} else if p.currentToken.Type == lexer.RARROW {
			// 裸 -> → 預設分支（option match 的 val 分支）
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.DOT && p.peekToken.Type == lexer.RARROW {
			// .-> → val branch (specific, not catch-all)
			ma.isWildcard = true
			ma.isDotVal = true
			p.nextToken() // consume DOT
		} else if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RARROW &&
			(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok") {
			// err-> nil-> → option pattern
			// ok-> → val branch (specific, not catch-all)
			if p.currentToken.Literal == "ok" {
				ma.isWildcard = true
				ma.isDotVal = true
			} else {
				ma.condition = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
			}
			p.nextToken()
		} else if p.currentToken.Type == lexer.NOT && p.peekToken.Type == lexer.RARROW {
			// !-> → err branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "err"}
			p.nextToken()
		} else if p.currentToken.Type == lexer.QUESTION && p.peekToken.Type == lexer.RARROW {
			// ?-> → nil branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "nil"}
			p.nextToken()
		} else {
			p.ctx.push(CTX_MATCH_ARM)
			ma.condition = p.parseExpression(LOWEST)
			p.ctx.pop()
		}

		// 支援 err-> nil-> 模式（option match 簡寫）
		isOptionPattern := false
		if !ma.isWildcard && ma.condition != nil {
			if ident, ok := ma.condition.(*Identifier); ok {
				if ident.Value == "err" || ident.Value == "nil" {
					if p.currentToken.Type == lexer.RARROW {
						isOptionPattern = true
					}
				}
			}
		}

		// Expect : 或 ->（option pattern）
		if p.currentToken.Type == lexer.RARROW {
			if isOptionPattern {
				p.nextToken() // skip ->
			} else {
				msg := fmt.Sprintf("line %d, column %d: expected ':' after match pattern, got '->' instead",
					tok.Line, tok.Column)
				p.saveError(msg)
				return nil
			}
		} else if p.currentToken.Type != lexer.COLON {
			msg := fmt.Sprintf("line %d, column %d: expected ':' after match pattern, got %s",
				tok.Line, tok.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		} else {
			p.nextToken() // skip :
		}

		// Block form (-> { ... }), statement form (newline after :), or expression form (inline)
		if p.currentToken.Type == lexer.LBRACE {
			// Explicit block form: parse as block statement so option-match
			// desugar can correctly insert `it` binding at the block head.
			ma.body = p.parseBlockStatement()
			// parseBlockStatement stops at } but does not consume it; advance past.
			if p.currentToken.Type == lexer.RBRACE {
				p.nextToken()
			}
		} else if p.currentToken.Type == lexer.NEWLINE {
			// Statement form: parse block until next arm or }
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			ma.body = &BlockStatement{Token: p.currentToken}
			for p.currentToken.Type != lexer.RBRACE &&
				p.currentToken.Type != lexer.EOF &&
				!isArmStart(p) {
				// Skip newlines
				for p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
				}
				if isArmStart(p) || p.currentToken.Type == lexer.RBRACE {
					break
				}
				// Parse one statement directly (NOT parseStatement, which has token-advancing loops)
				var stmt Statement
				switch p.currentToken.Type {
				case lexer.IDENT:
					if p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.IDENT ||
						p.peekToken.Type == lexer.LBRACKET || p.peekToken.Type == lexer.QUESTION {
						stmt = p.parseLetStatement()
					} else if p.peekToken.Type == lexer.LPAREN {
						stmt = p.parseExpressionStatement()
					} else {
						stmt = p.parseExpressionStatement()
					}
				case lexer.IF:
					stmt = p.parseExpressionStatement()
				case lexer.RETURN:
					stmt = p.parseReturnStatement()
				case lexer.FOR:
					stmt = p.parseForStatement()
				default:
					stmt = p.parseExpressionStatement()
				}
				if stmt != nil {
					ma.body.Statements = append(ma.body.Statements, stmt)
				}
			}
		} else {
			// Expression form: single expression
			p.ctx.push(CTX_MATCH_ARM)
			expr := p.parseExpression(LOWEST)
			p.ctx.pop()
			ma.body = &BlockStatement{
				Token: tok,
				Statements: []Statement{
					&ExpressionStatement{
						Token:      tok,
						Expression: expr,
					},
				},
			}
		}

		arms = append(arms, ma)
	}

	// Record } position and skip it
	var rbracePos lexer.Position
	if p.currentToken.Type == lexer.RBRACE {
		rbracePos = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
		p.nextToken()
	}

	if len(arms) == 0 {
		msg := fmt.Sprintf("line %d, column %d: empty match expression", tok.Line, tok.Column)
		p.saveError(msg)
		return nil
	}

	// Check option match branch completeness
	hasErrArm, hasNilArm, hasValArm, hasElseArm := false, false, false, false
	for _, a := range arms {
		if a.isWildcard {
			if a.isDotVal {
				hasValArm = true
			} else {
				hasElseArm = true
			}
		} else {
			switch c := a.condition.(type) {
			case *Identifier:
				if c.Value == "err" {
					hasErrArm = true
				} else if c.Value == "nil" {
					hasNilArm = true
				}
			case *NilLiteral:
				hasNilArm = true
			}
		}
	}
	// Check option match branch completeness (3 occurrences, keep in sync)
	isBuiltinOpt3 := p.isBuiltinOption(matched)
	if !hasElseArm && ((!isBuiltinOpt3 && !hasErrArm) || !hasNilArm || !hasValArm) {
		var missing []string
		if !isBuiltinOpt3 && !hasErrArm {
			missing = append(missing, "err")
		}
		if !hasNilArm {
			missing = append(missing, "nil")
		}
		if !hasValArm {
			missing = append(missing, "ok")
		}
		p.saveError(fmt.Sprintf("line %d, column %d: option match must handle all branches: err, nil, ok (missing: %s)",
			tok.Line, tok.Column, strings.Join(missing, ", ")))
		return nil
	}

	// Build if/elif/else chain from collected arms
	var result *IfExpression
	for i := len(arms) - 1; i >= 0; i-- {
		ma := arms[i]
		ifExpr := &IfExpression{
			Token:       tok,
			Condition:   nil,
			Consequence: ma.body,
		}

		if ma.isWildcard {
			// default arm: always-true condition
			if hasMatched {
				ifExpr.Condition = &InfixExpression{
					Token: tok, Left: matched, Operator: "==", Right: matched,
				}
			} else {
				ifExpr.Condition = &InfixExpression{
					Token: tok, Left: &IntegerLiteral{Token: tok, Value: 1},
					Operator: "==", Right: &IntegerLiteral{Token: tok, Value: 1},
				}
			}
		} else if hasMatched {
			// form 1: match <expr> — compare with matched
			ifExpr.Condition = &InfixExpression{
				Token: tok, Left: matched,
				Operator: "==", Right: ma.condition,
			}
		} else {
			// form 2: no match expr — condition is the arm expression directly
			ifExpr.Condition = ma.condition
		}

		if result != nil {
			ifExpr.Alternative = &BlockStatement{
				Token: tok,
				Statements: []Statement{
					&ExpressionStatement{
						Token:      tok,
						Expression: result,
					},
				},
			}
		}
		result = ifExpr
	}

	result.MatchEndPos = rbracePos
	return result
}

// isArmStart checks if the current token starts a new match arm
func isArmStart(p *Parser) bool {
	if p.currentToken.Type == lexer.COLON {
		return true
	}
	if p.currentToken.Type == lexer.UNDERSCORE && p.peekToken.Type == lexer.COLON {
		return true
	}
	if p.currentToken.Type == lexer.INT && p.peekToken.Type == lexer.COLON {
		return true
	}
	if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.COLON {
		return true
	}
	return false
}

// isOptionPatternStart checks if the current token starts an option pattern
// (nil, err, ok, ->, .->, !->, ?->) used in "nil -> err -> ok" fallthrough syntax.
func isOptionPatternStart(p *Parser) bool {
	if p.currentToken.Type == lexer.RARROW {
		return true // -> (wildcard)
	}
	if p.currentToken.Type == lexer.NIL && p.peekToken.Type == lexer.RARROW {
		return true
	}
	if p.currentToken.Type == lexer.IDENT &&
		(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok") &&
		p.peekToken.Type == lexer.RARROW {
		return true
	}
	if p.currentToken.Type == lexer.NOT && p.peekToken.Type == lexer.RARROW {
		return true
	}
	if p.currentToken.Type == lexer.QUESTION && p.peekToken.Type == lexer.RARROW {
		return true
	}
	if p.currentToken.Type == lexer.DOT && p.peekToken.Type == lexer.RARROW {
		return true
	}
	return false
}
