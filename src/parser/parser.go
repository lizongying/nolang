package parser

import (
	"github.com/lizongying/nolang/lexer"
)

type Parser struct {
	lexer *lexer.Lexer
	diags []Diagnostic // structured errors + warnings, in source order

	currentToken lexer.Token
	peekToken    lexer.Token
	prevToken    lexer.Token

	tk       []lexer.Token // 定长 ring buffer（注释包含）：最近从 lexer 拉取的 token
	tkFilled int           // 已从 lexer 拉取的 token 总数（注释包含，单调递增）
	cur      int           // currentToken 在 tk 中的绝对索引（注释包含）
	peek     int           // peekToken 的绝对索引

	ctx               contextStack                 // replaces inForCond, inMatchCond, inMatchArm, inExprContext
	comments          []lexer.Token                // collected comment tokens
	reportedIllegal   map[string]bool              // 已報告的 ILLEGAL token 位置（避免重複）
	varDeclTypes      map[string]string            // 變數名稱 → 型別字串（含 ? 前綴表示 Option）
	enumVariantNames  map[string][]string          // 枚舉類型名 → 枚舉值名列表
	funcSignatures    map[string][]string          // 函數名 → 結果型別字串列表（用於 let 型別推斷）
	structFields      map[string]map[string]string // struct 名 → 欄位名 → 型別字串
	methodStructStack []string                     // 當前方法所屬的 struct 名稱棧
	declaredVars      map[string]bool              // 已宣告的變數名（用於避免重複推斷）
	typeAliasNames    map[string]bool              // 已定義的類型別名名稱（用於等號語法偵測）

	// pendingAnnotations 暫存待附加到宣告的註解條目
	pendingAnnotations []*AnnotationEntry

	// Filename is the source file name (e.g. "sqlite.no").
	// Used for diagnostics and error reporting.
	Filename string

	// AllowAnonymousFnType controls whether anonymous function type syntax
	// (e.g. `cb ()()`) is permitted in parameter lists. When false, only
	// named function type aliases (e.g. `test-cb = ()` then
	// `f = (cb test-cb) {}`) are accepted. Defaults to false (zero value).
	AllowAnonymousFnType bool
}

// blockType — { body } 內部的型別分類
type blockType int

const (
	blockUnknown    blockType = iota
	blockStruct               // name { field type\n }
	blockEnum                 // name { a, b, c }
	blockIface                // name { method() }
	blockTaggedEnum           // name { a t, b u }
	blockMatch                // { pattern-> body } or { cond-> body }
)

// classifyBlock 分類 `{ body }` 的型別（預測：不消耗 token，只讀 peekToken）
// 必須在 p.peekToken == LBRACE 時呼叫。
// 使用有限預測：檢查 { 後第一個非 NEWLINE token + 第二個 token。
func (p *Parser) classifyBlock() blockType {
	if p.peekToken.Type != lexer.LBRACE {
		return blockUnknown
	}
	// 預測 { 後的第一個非 NEWLINE token（lexer 已在 { 後）
	skip := 0
	for {
		tok := p.look(skip)
		if tok.Type != lexer.NEWLINE {
			break
		}
		skip++
	}
	tok1 := p.look(skip)
	tok2 := p.look(skip + 1)

	// Tokens that only appear in match arms, not struct/enum/iface
	switch tok1.Type {
	case lexer.UNDERSCORE, lexer.RARROW, lexer.COLON, lexer.LPAREN:
		return blockMatch
	case lexer.INT, lexer.FLOAT, lexer.STRING, lexer.BYTE, lexer.CHAR, lexer.REGEX, lexer.TRUE, lexer.FALSE:
		return blockMatch
	}

	if tok1.Type != lexer.IDENT && tok1.Type != lexer.NIL {
		return blockUnknown
	}
	switch tok2.Type {
	case lexer.COMMA:
		return blockEnum
	case lexer.ASSIGN:
		// enum 顯式賦值：Name { VARIANT = value, ... }
		return blockEnum
	case lexer.LPAREN:
		return blockIface
	case lexer.DOT:
		// Generic-receiver method form: t.method(...)
		// e.g. ord { t.gt(b t) (res bool) }
		return blockIface
	case lexer.RARROW:
		return blockMatch
	case lexer.COLON:
		// Distinguish struct field/literal from match arm
		tok3 := p.look(skip + 2)
		tok4 := p.look(skip + 3)
		// Struct definition: name : type\n (type is IDENT)
		if (tok3.Type == lexer.IDENT || tok3.Type == lexer.PTR) &&
			(tok4.Type == lexer.NEWLINE || tok4.Type == lexer.RBRACE || tok4.Type == lexer.COMMA) {
			return blockStruct
		}
		// Struct literal: name : <literal_value>\n (value is STRING/INT/BYTE/BOOL/NIL)
		if (tok3.Type == lexer.STRING || tok3.Type == lexer.INT || tok3.Type == lexer.BYTE ||
			tok3.Type == lexer.TRUE || tok3.Type == lexer.FALSE || tok3.Type == lexer.NIL) &&
			(tok4.Type == lexer.NEWLINE || tok4.Type == lexer.RBRACE || tok4.Type == lexer.COMMA) {
			return blockStruct
		}
		// Struct literal with expression value: name : <expr> , or name : <expr> }
		// Scan forward to find `,` or `}` (skipping nested brackets); if we hit `->`
		// before any top-level `,`/`}`, it's a match arm. Otherwise it's a struct literal.
		if tok3.Type == lexer.IDENT {
			// Heuristic: only treat as struct literal if tok4 looks like an operator
			// (|, &, ^, +, -, *, /, %, <<, >>) suggesting "name : expr" where expr is
			// a compound expression that will be followed by , or }.
			if tok4.Type == lexer.OR || tok4.Type == lexer.AND || tok4.Type == lexer.XOR ||
				tok4.Type == lexer.ADD || tok4.Type == lexer.SUB || tok4.Type == lexer.MUL ||
				tok4.Type == lexer.QUO || tok4.Type == lexer.MOD ||
				tok4.Type == lexer.SHL || tok4.Type == lexer.SHR {
				return blockStruct
			}
			// Struct literal field with function call value: name : func(args)
			// Match arms never have "name: ident(" form, so LPAREN → struct literal.
			if tok4.Type == lexer.LPAREN {
				return blockStruct
			}
		}
		return blockMatch
	case lexer.EQUALS, lexer.NOT_EQUALS, lexer.LESS, lexer.GREATER,
		lexer.LESS_EQUALS, lexer.GREATER_EQUALS, lexer.LAND, lexer.LOR:
		return blockMatch
	case lexer.LBRACKET:
		// [N]type or []type — struct field with array/slice type (e.g., bytes [16]byte)
		return blockStruct
	case lexer.IDENT, lexer.NIL:
		// Distinguish struct field (name type\n) from tagged enum variant (name type, ...)
		tok3 := p.look(skip + 2)
		if tok3.Type == lexer.NEWLINE || tok3.Type == lexer.RBRACE {
			return blockStruct
		}
		if tok3.Type == lexer.COMMA {
			// Pure tagged enum: read, write, append, ...
			// (struct fields are "name type," — IDENT+IDENT+COMMA, not IDENT+COMMA)
			if tok1.Type == lexer.NIL {
				return blockTaggedEnum
			}
			// For IDENT, need to check if next is COMMA (tagged enum) or IDENT (struct field)
			// peek one more: if IDENT, struct; if COMMA, tagged enum
			// (already at COMMA here means the simple case read, write, ...)
			return blockTaggedEnum
		}
		// 3+ tokens before newline — could be struct with modifier or tagged enum
		// Scan forward to find comma (tagged enum) or newline (struct).
		// For struct fields: pattern is IDENT IDENT , (skip IDENT type, then comma)
		// For tagged enum: pattern is IDENT , (then next IDENT, or end)
		for i := skip + 3; i < skip+30; i++ {
			t := p.look(i)
			if t.Type == lexer.NEWLINE || t.Type == lexer.RBRACE || t.Type == lexer.EOF {
				return blockStruct
			}
			if t.Type == lexer.COMMA {
				// Look at next: if IDENT, could still be struct (more fields) or tagged enum (more variants)
				// Use the previous token: if previous was IDENT (type-like), it's struct; if previous was IDENT (variant), could be either
				// Heuristic: scan 2 ahead — if IDENT then IDENT after (i.e. ", IDENT IDENT"), it's likely struct
				prev := p.look(i - 1)
				next1 := p.look(i + 1)
				next2 := p.look(i + 2)
				if prev.Type == lexer.IDENT && next1.Type == lexer.IDENT {
					// ", IDENT IDENT" → could be struct field continuation or tagged enum variant with no payload
					// If next2 is COMMA or RBRACE/NEWLINE: tagged enum (variant, ...)
					// If next2 is anything else: ambiguous, but likely struct
					if next2.Type == lexer.COMMA || next2.Type == lexer.RBRACE || next2.Type == lexer.NEWLINE {
						return blockTaggedEnum
					}
					// Otherwise keep scanning — likely struct
				} else {
					return blockTaggedEnum
				}
			}
		}
		return blockUnknown
	default:
		return blockUnknown
	}
}

// classifyBlockAtCurrent 分類 `{ body }` 的型別，當 currentToken == LBRACE 時呼叫。
func (p *Parser) classifyBlockAtCurrent() blockType {
	if p.currentToken.Type != lexer.LBRACE {
		return blockUnknown
	}

	// peekToken is 1st token after {, look(0) is 2nd, look(1) is 3rd, etc.
	var tok1, tok2 lexer.Token
	base := 0 // look offset base for tok1
	if p.peekToken.Type != lexer.NEWLINE {
		tok1 = p.peekToken
		base = -1 // peekToken is before look(0)
		tok2 = p.look(0)
	} else {
		skip := 0
		for {
			t := p.look(skip)
			if t.Type != lexer.NEWLINE {
				tok1 = t
				base = skip
				break
			}
			skip++
		}
		tok2 = p.look(base + 1)
	}

	switch tok1.Type {
	case lexer.UNDERSCORE, lexer.RARROW, lexer.COLON, lexer.LPAREN:
		return blockMatch
	case lexer.INT, lexer.FLOAT, lexer.STRING, lexer.BYTE, lexer.CHAR, lexer.REGEX, lexer.TRUE, lexer.FALSE:
		return blockMatch
	case lexer.DOT:
		// Bare match arm starting with .field (self.field access),
		// e.g. { .scheme == 'http' -> ... }
		// But if .field is followed by = (assignment), it's a statement block,
		// not a match arm. e.g. -> { .connected = false ... }
		// Also, .method() (LPAREN after IDENT) is a method call statement,
		// not a match arm pattern.
		if tok2.Type == lexer.IDENT {
			var tok3 lexer.Token
			if base == -1 {
				tok3 = p.look(1)
			} else {
				tok3 = p.look(base + 2)
			}
			if tok3.Type == lexer.ASSIGN {
				return blockUnknown // statement block, not match
			}
			if tok3.Type == lexer.LPAREN {
				return blockUnknown // method call statement, not match
			}
		}
		return blockMatch
	case lexer.RBRACE:
		// 空 {} 可能是結構體字面量（如 bigint{}）或空匹配
		// 在表達式上下文中處理為結構體字面量
		if !p.ctx.contains(CTX_MATCH_COND) {
			return blockStruct
		}
		return blockMatch
	}

	if tok1.Type != lexer.IDENT && tok1.Type != lexer.NIL && tok1.Type != lexer.IN {
		return blockUnknown
	}

	switch tok2.Type {
	case lexer.COMMA:
		return blockEnum
	case lexer.ASSIGN:
		// enum 顯式賦值：Name { VARIANT = value, ... }
		return blockEnum
	case lexer.LPAREN:
		return blockIface
	case lexer.RARROW:
		return blockMatch
	case lexer.COLON:
		// Distinguish struct field/literal from match arm
		var tok3, tok4 lexer.Token
		if base == -1 {
			tok3 = p.look(1)
			tok4 = p.look(2)
		} else {
			tok3 = p.look(base + 2)
			tok4 = p.look(base + 3)
		}
		// Struct definition: name : type\n
		if (tok3.Type == lexer.IDENT || tok3.Type == lexer.PTR) &&
			(tok4.Type == lexer.NEWLINE || tok4.Type == lexer.RBRACE || tok4.Type == lexer.COMMA) {
			return blockStruct
		}
		// Struct literal: name : <literal_value>\n
		if (tok3.Type == lexer.STRING || tok3.Type == lexer.INT || tok3.Type == lexer.BYTE ||
			tok3.Type == lexer.TRUE || tok3.Type == lexer.FALSE || tok3.Type == lexer.NIL) &&
			(tok4.Type == lexer.NEWLINE || tok4.Type == lexer.RBRACE || tok4.Type == lexer.COMMA) {
			return blockStruct
		}
		// Struct literal: name : -<int>\n (unary minus on integer literal)
		if tok3.Type == lexer.SUB {
			var tok4b, tok5b lexer.Token
			if base == -1 {
				tok4b = p.look(2)
				tok5b = p.look(3)
			} else {
				tok4b = p.look(base + 3)
				tok5b = p.look(base + 4)
			}
			if tok4b.Type == lexer.INT &&
				(tok5b.Type == lexer.NEWLINE || tok5b.Type == lexer.RBRACE || tok5b.Type == lexer.COMMA) {
				return blockStruct
			}
		}
		// Struct literal with bitwise/arithmetic expression: name : <IDENT> <OP> <IDENT>
		// e.g. mode: o-wronly | o-creat → name: IDENT OR IDENT
		// Without this, the parser falls back to blockMatch and parses the field
		// as the condition of an if-expression.
		if tok3.Type == lexer.IDENT {
			if tok4.Type == lexer.OR || tok4.Type == lexer.AND || tok4.Type == lexer.XOR ||
				tok4.Type == lexer.ADD || tok4.Type == lexer.SUB || tok4.Type == lexer.MUL ||
				tok4.Type == lexer.QUO || tok4.Type == lexer.MOD ||
				tok4.Type == lexer.SHL || tok4.Type == lexer.SHR {
				return blockStruct
			}
			// Struct literal field with function call value: name : func(args)
			// Match arms never have "name: ident(" form, so LPAREN → struct literal.
			if tok4.Type == lexer.LPAREN {
				return blockStruct
			}
			// Struct literal: name : EnumName.Variant ... (then operator, comma, or brace)
			// e.g. mode: FileMode.WRITE | FileMode.CREATE
			//      perm: FilePerm.PERM_600
			if tok4.Type == lexer.DOT {
				var tok5, tok6 lexer.Token
				if base == -1 {
					tok5 = p.look(3)
					tok6 = p.look(4)
				} else {
					tok5 = p.look(base + 4)
					tok6 = p.look(base + 5)
				}
				// name: EnumName.Variant [op] ... → struct literal
				if tok5.Type == lexer.IDENT {
					if tok6.Type == lexer.OR || tok6.Type == lexer.AND || tok6.Type == lexer.XOR ||
						tok6.Type == lexer.ADD || tok6.Type == lexer.SUB || tok6.Type == lexer.MUL ||
						tok6.Type == lexer.QUO || tok6.Type == lexer.MOD ||
						tok6.Type == lexer.SHL || tok6.Type == lexer.SHR ||
						tok6.Type == lexer.NEWLINE || tok6.Type == lexer.RBRACE || tok6.Type == lexer.COMMA {
						return blockStruct
					}
				}
			}
		}
		return blockMatch
	case lexer.DOT:
		// IDENT.method(...) -> ... : method call expression as match arm condition.
		// Scan forward to find RARROW at depth 0 (match arm). If NEWLINE/EOF/RBRACE
		// is encountered at depth 0 first, it's a statement block, not a match.
		depth := 0
		for i := base + 2; i < base+40; i++ {
			t := p.look(i)
			switch t.Type {
			case lexer.LPAREN, lexer.LBRACE, lexer.LBRACKET:
				depth++
			case lexer.RPAREN, lexer.RBRACKET:
				if depth == 0 {
					return blockUnknown
				}
				depth--
			case lexer.RBRACE:
				if depth == 0 {
					return blockUnknown
				}
				depth--
			case lexer.RARROW:
				if depth == 0 {
					return blockMatch
				}
			case lexer.NEWLINE, lexer.EOF:
				if depth == 0 {
					return blockUnknown
				}
			}
		}
		return blockUnknown
	case lexer.EQUALS, lexer.NOT_EQUALS, lexer.LESS, lexer.GREATER,
		lexer.LESS_EQUALS, lexer.GREATER_EQUALS, lexer.LAND, lexer.LOR:
		return blockMatch
	case lexer.IDENT, lexer.NIL:
		// Distinguish struct field (name type\n) from tagged enum variant (name type, ...)
		tok3 := p.look(base + 2)
		if tok3.Type == lexer.NEWLINE || tok3.Type == lexer.RBRACE {
			return blockStruct
		}
		if tok3.Type == lexer.COMMA {
			return blockTaggedEnum
		}
		// 3+ tokens before newline — could be struct with modifier or tagged enum
		// Scan forward to find comma (tagged enum) or newline (struct)
		for i := base + 3; i < base+15; i++ {
			t := p.look(i)
			if t.Type == lexer.COMMA {
				return blockTaggedEnum
			}
			if t.Type == lexer.NEWLINE || t.Type == lexer.RBRACE || t.Type == lexer.EOF {
				return blockStruct
			}
		}
		return blockUnknown
	default:
		// For patterns like `i % 4 == 0 -> ...` or `a + b > 10 -> ...`,
		// scan forward to find RARROW at depth 0 (match arm condition).
		// Also handle `in[i] == ... -> ...` where `in` is the IN keyword
		// used as a parameter name.
		if tok1.Type == lexer.IDENT || tok1.Type == lexer.IN {
			// If tok2 is an opening bracket, start depth at 1 to account
			// for it, since the scan starts AFTER tok2. Without this,
			// patterns like `s[i] > 0 -> ...` are misclassified because
			// the closing `]` is seen at depth 0, returning blockUnknown
			// instead of blockMatch. This causes the bare match block to
			// be parsed incorrectly, leaking subsequent statements to the
			// top level.
			depth := 0
			if tok2.Type == lexer.LPAREN || tok2.Type == lexer.LBRACE || tok2.Type == lexer.LBRACKET {
				depth = 1
			}
			for i := base + 2; i < base+40; i++ {
				t := p.look(i)
				switch t.Type {
				case lexer.LPAREN, lexer.LBRACE, lexer.LBRACKET:
					depth++
				case lexer.RPAREN, lexer.RBRACKET:
					if depth == 0 {
						return blockUnknown
					}
					depth--
				case lexer.RBRACE:
					if depth == 0 {
						return blockUnknown
					}
					depth--
				case lexer.RARROW:
					if depth == 0 {
						return blockMatch
					}
				case lexer.NEWLINE, lexer.EOF:
					if depth == 0 {
						return blockUnknown
					}
				}
			}
		}
		return blockUnknown
	}
}

type parserState struct {
	cur          int // ring cursor (absolute token index of currentToken)
	peek         int // ring cursor of peekToken
	prevToken    lexer.Token
	ctx          contextStack      // snapshot of context stack
	comments     []lexer.Token     // snapshot of collected comments
	varDeclTypes map[string]string // snapshot of variable type table
	declaredVars map[string]bool   // snapshot of declared variable set
}

func New(lx *lexer.Lexer) *Parser {
	p := &Parser{
		lexer:           lx,
		tk:              make([]lexer.Token, tokenBufferSize),
		cur:             -1,
		peek:            -1,
		ctx:             contextStack{CTX_GLOBAL},
		reportedIllegal: map[string]bool{},
		varDeclTypes:    map[string]string{},
		declaredVars:    map[string]bool{},
	}

	p.nextToken()

	return p
}

// setVarType 記錄變數宣告型別到 varDeclTypes。
// varDeclTypes 在 New() 中已初始化，此處無需 nil 檢查。
// 集中管理寫入點，避免散佈的 lazy init 模式。
func (p *Parser) setVarType(name, typ string) {
	p.varDeclTypes[name] = typ
}

// SetExternSignatures 注入外部（跨文件）函數簽名和 struct 欄位型別，
// 供 parseLetStatement 的型別推斷使用。由 transpiler 在解析前呼叫。
func (p *Parser) SetExternSignatures(funcSigs map[string][]string, structFields map[string]map[string]string) {
	if p.funcSignatures == nil {
		p.funcSignatures = make(map[string][]string)
	}
	for k, v := range funcSigs {
		p.funcSignatures[k] = v
	}
	if p.structFields == nil {
		p.structFields = make(map[string]map[string]string)
	}
	for k, v := range structFields {
		p.structFields[k] = v
	}
}

func (p *Parser) saveState() parserState {
	commentsCopy := make([]lexer.Token, len(p.comments))
	copy(commentsCopy, p.comments)
	// 深拷貝符號表，避免試探性解析中的 varDeclTypes/declaredVars 寫入
	// 在 restoreState 後污染後續解析。varDeclTypes/declaredVars 在 New() 中已初始化。
	varDeclTypesCopy := make(map[string]string, len(p.varDeclTypes))
	for k, v := range p.varDeclTypes {
		varDeclTypesCopy[k] = v
	}
	declaredVarsCopy := make(map[string]bool, len(p.declaredVars))
	for k, v := range p.declaredVars {
		declaredVarsCopy[k] = v
	}
	return parserState{
		cur:          p.cur,
		peek:         p.peek,
		prevToken:    p.prevToken,
		ctx:          p.ctx.copy(),
		comments:     commentsCopy,
		varDeclTypes: varDeclTypesCopy,
		declaredVars: declaredVarsCopy,
	}
}

func (p *Parser) restoreState(state parserState) {
	p.cur = state.cur
	p.peek = state.peek
	// 从 ring 重读 current/peek（回溯后重新推进时注释会由 advanceCollect 重新收集）
	p.currentToken = p.tokAt(p.cur)
	p.peekToken = p.tokAt(p.peek)
	p.prevToken = state.prevToken
	p.ctx = state.ctx
	p.comments = state.comments
	p.varDeclTypes = state.varDeclTypes
	p.declaredVars = state.declaredVars
}

func (p *Parser) nextToken() {
	p.prevToken = p.currentToken
	if p.peek < 0 {
		// 初始状态：cur/peek 均未定位，各自前进并收集注释
		p.cur, p.currentToken = p.advanceCollect(p.cur)
	} else {
		// cur 直接接管 peek 的位置——cur..peek 之间的注释在上一轮
		// 计算 peek 时已收集过，避免重复收集
		p.cur, p.currentToken = p.peek, p.peekToken
	}
	p.peek, p.peekToken = p.advanceCollect(p.cur)
}

// collectDocComments collects all accumulated comment tokens into a CommentGroup
func (p *Parser) collectDocComments() *CommentGroup {
	if len(p.comments) == 0 {
		return nil
	}
	group := &CommentGroup{}
	for _, c := range p.comments {
		comment := &Comment{
			Pos:    posFromToken(c),
			End:    lexer.Position{Line: c.Line, Column: c.Column + len(c.Literal)},
			Kind:   NormalComment,
			Text:   c.Literal,
			Marker: c.Marker,
		}
		group.List = append(group.List, comment)
	}
	if len(group.List) > 0 {
		group.Start = group.List[0].Pos
		group.End = group.List[len(group.List)-1].End
	}
	p.comments = nil
	return group
}

// setComment sets the Comment field on any Statement that supports it
func setComment(stmt Statement, comment *CommentGroup) {
	if comment == nil || stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *LetStatement:
		s.Comment = comment
	case *ReturnStatement:
		s.Comment = comment
	case *ExpressionStatement:
		s.Comment = comment
	case *FunctionDefinition:
		s.Comment = comment
	case *ForStatement:
		s.Comment = comment
	case *BreakStatement:
		s.Comment = comment
	case *ContinueStatement:
		s.Comment = comment
	case *UseStatement:
		s.Comment = comment
	case *ExportStatement:
		s.Comment = comment
	case *EnumDefinition:
		s.Comment = comment
	case *TaggedEnumDefinition:
		s.Comment = comment
	case *InterfaceDefinition:
		s.Comment = comment
	case *StructDefinition:
		s.Comment = comment
	}
}

// attachInlineComment checks if the first collected comment is on the same line
// as the statement's last token. If so, it's an inline comment — move it to stmt.Comment.
func (p *Parser) attachInlineComment(stmt Statement) {
	if len(p.comments) == 0 {
		return
	}
	stmtLastLine := stmtTokenEndLine(stmt)
	if stmtLastLine > 0 && p.comments[0].Line == stmtLastLine {
		c := p.comments[0]
		comment := &Comment{
			Pos:    posFromToken(c),
			End:    lexer.Position{Line: c.Line, Column: c.Column + len(c.Literal)},
			Kind:   NormalComment,
			Text:   c.Literal,
			Marker: c.Marker,
		}
		group := &CommentGroup{
			List:  []*Comment{comment},
			Start: comment.Pos,
			End:   comment.End,
		}
		setComment(stmt, group)
		p.comments = p.comments[1:]
	}
}

// stmtTokenEndLine returns the line number of the last token in a statement.
func stmtTokenEndLine(stmt Statement) int {
	switch s := stmt.(type) {
	case *LetStatement:
		return s.Name.Token.Line
	case *UseStatement:
		return s.Token.Line
	case *ExportStatement:
		return s.Token.Line
	case *ReturnStatement:
		if s.ReturnValue != nil {
			// We approximate: return value line
			return s.Token.Line
		}
		return s.Token.Line
	case *ExpressionStatement:
		return stmtExprEndLine(s.Expression)
	case *FunctionDefinition:
		// Use the function's body closing brace
		return s.Body.Token.Line
	case *ForStatement:
		// Use the for body's closing brace
		return s.Body.Token.Line
	case *BreakStatement:
		return s.Token.Line
	case *ContinueStatement:
		return s.Token.Line
	case *BlockStatement:
		if len(s.Statements) > 0 {
			return stmtTokenEndLine(s.Statements[len(s.Statements)-1])
		}
		return s.Token.Line
	case *EnumDefinition:
		return s.Token.Line
	case *TaggedEnumDefinition:
		return s.Token.Line
	case *InterfaceDefinition:
		return s.Token.Line
	case *StructDefinition:
		return s.Token.Line
	}
	return 0
}

// stmtExprEndLine returns the end line of an expression.
func stmtExprEndLine(expr Expression) int {
	switch e := expr.(type) {
	case *Identifier:
		return e.Token.Line
	case *IntegerLiteral:
		return e.Token.Line
	case *FloatLiteral:
		return e.Token.Line
	case *BooleanLiteral:
		return e.Token.Line
	case *ByteLiteral:
		return e.Token.Line
	case *StringLiteral:
		return e.Token.Line
	case *CharLiteral:
		return e.Token.Line
	case *NilLiteral:
		return e.Token.Line
	case *PrefixExpression:
		return stmtExprEndLine(e.Right)
	case *InfixExpression:
		return stmtExprEndLine(e.Right)
	case *CallExpression:
		return e.Token.Line
	case *DotExpression:
		return stmtExprEndLine(e.Receiver)
	case *IfExpression:
		if e.Alternative != nil && len(e.Alternative.Statements) > 0 {
			return stmtTokenEndLine(e.Alternative.Statements[len(e.Alternative.Statements)-1])
		}
		if e.Consequence != nil && len(e.Consequence.Statements) > 0 {
			return stmtTokenEndLine(e.Consequence.Statements[len(e.Consequence.Statements)-1])
		}
		return e.Token.Line
	case *FunctionLiteral:
		return e.Body.Token.Line
	case *IndexExpression:
		return e.Token.Line
	case *SliceExpression:
		return e.Token.Line
	case *RangeExpression:
		return e.Token.Line
	case *ArrayLiteral:
		return e.Token.Line
	case *SliceLiteral:
		return e.Token.Line
	case *StructLiteral:
		return e.Token.Line
	case *AssignExpression:
		return e.Token.Line
	case *ConditionalExpression:
		return e.Token.Line
	case *GroupedExpression:
		return e.Token.Line
	}
	return 0
}

// setDoc sets the Doc field on any Statement that supports it
func setDoc(stmt Statement, doc *CommentGroup) {
	if doc == nil || stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *LetStatement:
		s.Doc = doc
	case *ReturnStatement:
		s.Doc = doc
	case *ExpressionStatement:
		s.Doc = doc
	case *FunctionDefinition:
		s.Doc = doc
	case *ForStatement:
		s.Doc = doc
	case *BreakStatement:
		s.Doc = doc
	case *ContinueStatement:
		s.Doc = doc
	case *UseStatement:
		s.Doc = doc
	case *ExportStatement:
		s.Doc = doc
	case *EnumDefinition:
		s.Doc = doc
	case *TaggedEnumDefinition:
		s.Doc = doc
	case *InterfaceDefinition:
		s.Doc = doc
	case *StructDefinition:
		s.Doc = doc
	case *TypeAlias:
		s.Doc = doc
	}
}

func (p *Parser) Errors() []string {
	return formatDiags(p.diags, SeverityError)
}

func (p *Parser) peekError(t lexer.TokenType) {
	p.errorf(p.currentToken, "E_UNEXPECTED_TOKEN",
		"expected next token to be %s, got %s instead",
		t.String(), p.peekToken.Type.String())
}

func (p *Parser) ParseProgram() *Program {
	program := &Program{Statements: []Statement{}}
	for p.currentToken.Type != lexer.EOF {
		doc := p.collectDocComments()
		stmt := p.recoverStatement()
		if stmt != nil {
			setDoc(stmt, doc)
			p.attachInlineComment(stmt)
			program.Statements = append(program.Statements, stmt)
		}

		if stmt == nil {
			// 當陳述句為 nil（例如 NEWLINE）時，將 Doc 註釋還原供下一個陳述句使用
			if doc != nil {
				for _, c := range doc.List {
					p.comments = append(p.comments, lexer.Token{
						Type:    lexer.COMMENT,
						Literal: c.Text,
						Marker:  c.Marker,
						Line:    c.Pos.Line,
						Column:  c.Pos.Column,
					})
				}
			}
			p.nextToken()
		}
	}

	program.TrailingComments = p.collectDocComments()
	program.Warnings = append([]string{}, p.Warnings()...)

	return program
}

// recoverStatement runs parseStatement inside a panic/recover so a deep call
// can abort an unrecoverable statement with (*Parser).fatalf and have the
// Diagnostic recorded here, then the outer loop continues with the next
// statement. Real panics (compiler bugs) are re-raised so they surface in
// tests instead of being swallowed.
func (p *Parser) recoverStatement() (stmt Statement) {
	defer func() {
		if r := recover(); r != nil {
			if pp, ok := r.(*parsePanic); ok {
				p.diags = append(p.diags, *pp.diag)
				stmt = nil
				return
			}
			panic(r)
		}
	}()
	return p.parseStatement()
}

func (p *Parser) saveError(msg string) {
	pos, clean := stripLocPrefix(msg)
	p.diags = append(p.diags, Diagnostic{
		Filename: p.Filename,
		Pos:      pos,
		Severity: SeverityError,
		Code:     "E_GENERAL",
		Message:  clean,
	})
}

func (p *Parser) saveWarning(msg string) {
	pos, clean := stripLocPrefix(msg)
	p.diags = append(p.diags, Diagnostic{
		Filename: p.Filename,
		Pos:      pos,
		Severity: SeverityWarning,
		Code:     "W_GENERAL",
		Message:  clean,
	})
}

func (p *Parser) Warnings() []string {
	return formatDiags(p.diags, SeverityWarning)
}

// skipToStatementEnd advances tokens until a statement boundary is reached.
func (p *Parser) skipToStatementEnd() {
	for p.currentToken.Type != lexer.EOF && !isStatementBoundary(p.currentToken.Type) {
		p.nextToken()
	}
}

func (p *Parser) expectPeek(t lexer.TokenType) bool {
	if p.peekToken.Type == t {
		p.nextToken()
		return true
	}

	p.peekError(t)
	return false
}
