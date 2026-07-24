package parser

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

type Parser struct {
	lexer    *lexer.Lexer
	errors   []string
	warnings []string

	currentToken      lexer.Token
	peekToken         lexer.Token
	prevToken         lexer.Token
	ctx               contextStack                 // replaces inForCond, inMatchCond, inMatchArm, inExprContext
	comments          []lexer.Token                // collected comment tokens
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
// matchedIsOption 判斷 match 主表達式是否為 Option 變數（用於觸發完整性檢查）
func (p *Parser) matchedIsOption(matched Expression) bool {
	if matched == nil {
		return false // 裸 match 不需完整分支
	}
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := p.varDeclTypes[ident.Value]; ok {
			return strings.HasPrefix(t, "?")
		}
	}
	return false // 未知變數預設不觸發完整性檢查
}

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
		tok := p.lexer.LookAhead(skip)
		if tok.Type != lexer.NEWLINE {
			break
		}
		skip++
	}
	tok1 := p.lexer.LookAhead(skip)
	tok2 := p.lexer.LookAhead(skip + 1)

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
		tok3 := p.lexer.LookAhead(skip + 2)
		tok4 := p.lexer.LookAhead(skip + 3)
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
		tok3 := p.lexer.LookAhead(skip + 2)
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
			t := p.lexer.LookAhead(i)
			if t.Type == lexer.NEWLINE || t.Type == lexer.RBRACE || t.Type == lexer.EOF {
				return blockStruct
			}
			if t.Type == lexer.COMMA {
				// Look at next: if IDENT, could still be struct (more fields) or tagged enum (more variants)
				// Use the previous token: if previous was IDENT (type-like), it's struct; if previous was IDENT (variant), could be either
				// Heuristic: scan 2 ahead — if IDENT then IDENT after (i.e. ", IDENT IDENT"), it's likely struct
				prev := p.lexer.LookAhead(i - 1)
				next1 := p.lexer.LookAhead(i + 1)
				next2 := p.lexer.LookAhead(i + 2)
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

	// peekToken is 1st token after {, LookAhead(0) is 2nd, LookAhead(1) is 3rd, etc.
	var tok1, tok2 lexer.Token
	base := 0 // LookAhead offset base for tok1
	if p.peekToken.Type != lexer.NEWLINE {
		tok1 = p.peekToken
		base = -1 // peekToken is before LookAhead(0)
		tok2 = p.lexer.LookAhead(0)
	} else {
		skip := 0
		for {
			t := p.lexer.LookAhead(skip)
			if t.Type != lexer.NEWLINE {
				tok1 = t
				base = skip
				break
			}
			skip++
		}
		tok2 = p.lexer.LookAhead(base + 1)
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
				tok3 = p.lexer.LookAhead(1)
			} else {
				tok3 = p.lexer.LookAhead(base + 2)
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
			tok3 = p.lexer.LookAhead(1)
			tok4 = p.lexer.LookAhead(2)
		} else {
			tok3 = p.lexer.LookAhead(base + 2)
			tok4 = p.lexer.LookAhead(base + 3)
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
				tok4b = p.lexer.LookAhead(2)
				tok5b = p.lexer.LookAhead(3)
			} else {
				tok4b = p.lexer.LookAhead(base + 3)
				tok5b = p.lexer.LookAhead(base + 4)
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
					tok5 = p.lexer.LookAhead(3)
					tok6 = p.lexer.LookAhead(4)
				} else {
					tok5 = p.lexer.LookAhead(base + 4)
					tok6 = p.lexer.LookAhead(base + 5)
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
			t := p.lexer.LookAhead(i)
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
		tok3 := p.lexer.LookAhead(base + 2)
		if tok3.Type == lexer.NEWLINE || tok3.Type == lexer.RBRACE {
			return blockStruct
		}
		if tok3.Type == lexer.COMMA {
			return blockTaggedEnum
		}
		// 3+ tokens before newline — could be struct with modifier or tagged enum
		// Scan forward to find comma (tagged enum) or newline (struct)
		for i := base + 3; i < base+15; i++ {
			t := p.lexer.LookAhead(i)
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
				t := p.lexer.LookAhead(i)
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
	currentToken lexer.Token
	peekToken    lexer.Token
	prevToken    lexer.Token
	lexerState   lexer.LexerState
	ctx          contextStack  // snapshot of context stack
	comments     []lexer.Token // snapshot of collected comments
}

func New(lexer *lexer.Lexer) *Parser {
	p := &Parser{
		lexer:  lexer,
		errors: []string{},
		ctx:    contextStack{CTX_GLOBAL},
	}

	p.nextToken()
	p.nextToken()

	return p
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

// resolveReceiverType 解析方法調用接收者的型別，用於推斷方法返回型別。
// 支援：局部變數 (Identifier)、self 欄位 (DotExpression{self, field})、
// 以及 self.field.subfield (嵌套 DotExpression{DotExpression{self, field}, subfield})
func (p *Parser) resolveReceiverType(receiver Expression) string {
	if ident, ok := receiver.(*Identifier); ok {
		if t, ok := p.varDeclTypes[ident.Value]; ok {
			return strings.TrimPrefix(t, "?")
		}
		// self in method body: resolve via methodStructStack.
		// self is added to def.Parameters after parseFunctionBody,
		// so varDeclTypes does not have it during body parsing.
		// This allows inferTypeFromCallExpr to resolve self.method() calls
		// (e.g., val = .get(key) in str-map.contains -> val inferred as ?str).
		if ident.Value == "self" && len(p.methodStructStack) > 0 {
			return p.methodStructStack[len(p.methodStructStack)-1]
		}
	}
	if dot, ok := receiver.(*DotExpression); ok {
		// self.field → 查詢 struct 欄位型別
		if selfIdent, ok := dot.Receiver.(*Identifier); ok && selfIdent.Value == "self" {
			if len(p.methodStructStack) > 0 {
				structName := p.methodStructStack[len(p.methodStructStack)-1]
				if fields, ok := p.structFields[structName]; ok {
					if fieldType, ok := fields[dot.Property]; ok {
						return strings.TrimPrefix(fieldType, "?")
					}
				}
			}
		}
		// self.field.subfield → 遞迴解析 receiver 型別，再查其 struct 欄位
		if innerDot, ok := dot.Receiver.(*DotExpression); ok {
			innerType := p.resolveReceiverType(innerDot)
			if innerType != "" {
				if fields, ok := p.structFields[innerType]; ok {
					if fieldType, ok := fields[dot.Property]; ok {
						return strings.TrimPrefix(fieldType, "?")
					}
				}
			}
		}
	}
	return ""
}

// inferTypeFromCallExpr 嘗試從函數/方法調用推斷返回型別。
// 僅推斷 option 型別（?type），避免泛型/聯合型別的特化問題。
// 返回空字串表示無法推斷。
func (p *Parser) inferTypeFromCallExpr(call *CallExpression) string {
	if p.funcSignatures == nil {
		return ""
	}
	fnName := ""
	if ident, ok := call.Function.(*Identifier); ok {
		fnName = ident.Value
	} else if dot, ok := call.Function.(*DotExpression); ok {
		receiverType := p.resolveReceiverType(dot.Receiver)
		if receiverType != "" {
			// Map types are stored as [K]V in varDeclTypes but function
			// signatures use hashmap-K-V. Convert for lookup.
			if strings.HasPrefix(receiverType, "[") {
				if idx := strings.Index(receiverType, "]"); idx > 0 {
					keyPart := receiverType[1:idx]
					valPart := receiverType[idx+1:]
					receiverType = "hashmap-" + keyPart + "-" + valPart
				}
			}
			fnName = receiverType + "." + dot.Property
		} else {
			// Multi-level dot expression (e.g. encoding.base64.decode):
			// receiver is a module path, not a variable. Try matching
			// just the last property name against funcSignatures.
			fnName = dot.Property
		}
	}
	if fnName == "" {
		return ""
	}
	if rets, ok := p.funcSignatures[fnName]; ok && len(rets) == 1 {
		// 僅推斷 option 型別，避免泛型/聯合型別的特化問題
		if strings.HasPrefix(rets[0], "?") {
			return rets[0]
		}
	}
	return ""
}

func (p *Parser) saveState() parserState {
	commentsCopy := make([]lexer.Token, len(p.comments))
	copy(commentsCopy, p.comments)
	return parserState{
		currentToken: p.currentToken,
		peekToken:    p.peekToken,
		prevToken:    p.prevToken,
		lexerState:   p.lexer.SaveState(),
		ctx:          p.ctx.copy(),
		comments:     commentsCopy,
	}
}

func (p *Parser) restoreState(state parserState) {
	p.currentToken = state.currentToken
	p.peekToken = state.peekToken
	p.prevToken = state.prevToken
	p.lexer.RestoreState(state.lexerState)
	p.ctx = state.ctx
	p.comments = state.comments
}

func (p *Parser) nextToken() {
	p.prevToken = p.currentToken

	// Collect any comments at peek position before they become currentToken
	for p.peekToken.Type == lexer.COMMENT {
		p.comments = append(p.comments, p.peekToken)
		p.peekToken = p.lexer.NextToken()
	}

	// Advance: non-comment peek becomes current
	p.currentToken = p.peekToken

	// Read new peek, collecting any comments
	for {
		p.peekToken = p.lexer.NextToken()
		if p.peekToken.Type != lexer.COMMENT {
			break
		}
		p.comments = append(p.comments, p.peekToken)
	}
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
	return p.errors
}

func (p *Parser) peekError(t lexer.TokenType) {
	msg := fmt.Sprintf("line %d, column %d: expected next token to be %s, got %s instead",
		p.currentToken.Line,
		p.currentToken.Column,
		t.String(),
		p.peekToken.Type.String())
	p.errors = append(p.errors, msg)
}

// isTypeName checks if the given literal is a known type name.
// Used to support concise declarations like `i64` on its own line.
func isTypeName(literal string) bool {
	switch literal {
	case "i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"f32", "f64",
		"byte", "bool", "str":
		return true
	}
	return false
}

// isBuiltinTypeName reports whether name is a builtin type name usable as a
// map key or value type (e.g. [str]i64).
func isBuiltinTypeName(name string) bool {
	switch name {
	case "str", "i64", "i32", "i16", "i8",
		"u64", "u32", "u16", "u8",
		"bool", "byte", "char",
		"f64", "f32":
		return true
	}
	return false
}

// isRegisteredTypeName reports whether name is a registered struct or enum
// type name (usable as a map key or value type).
func (p *Parser) isRegisteredTypeName(name string) bool {
	if p.structFields != nil {
		if _, ok := p.structFields[name]; ok {
			return true
		}
	}
	if p.enumVariantNames != nil {
		if _, ok := p.enumVariantNames[name]; ok {
			return true
		}
	}
	return false
}

func (p *Parser) ParseProgram() *Program {
	program := &Program{Statements: []Statement{}}
	for p.currentToken.Type != lexer.EOF {
		doc := p.collectDocComments()
		stmt := p.parseStatement()
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
	program.Warnings = append([]string{}, p.warnings...)

	return program
}

func (p *Parser) parseStatement() Statement {
	switch p.currentToken.Type {
	case lexer.NEWLINE:
		return nil

	case lexer.IF:
		stmt := p.parseExpressionStatement()
		p.skipToStatementEnd()
		return stmt

	case lexer.FOR:
		return p.parseForStatement()
	case lexer.SWITCH:
		return p.parseSwitchStatement()
	case lexer.TILDE:
		if p.peekToken.Type == lexer.MATCH {
			return p.parseTildeMatchStatement()
		}
		return p.parseExpressionStatement()
	case lexer.BREAK:
		stmt := p.parseBreakStatement()
		p.skipToStatementEnd()
		return stmt
	case lexer.CONTINUE:
		stmt := p.parseContinueStatement()
		p.skipToStatementEnd()
		return stmt
	case lexer.USE:
		return p.parseUseStatement()
	case lexer.FFI:
		return p.parseFFIDeclaration()
	case lexer.HASH_LBRACE:
		return p.parseAnnotationStatement()
	case lexer.LABEL:
		return p.parseLabeledStatement()
	case lexer.AT:
		return p.parseExportStatement()
	case lexer.IDENT:
		// 檢查介面實作/繼承：user json, fmt { name str } 或 db enter, leave { close() }
		// 也支援跨模組限定名：stmt-mysql sql.stmt { ... }
		if p.peekToken.Type == lexer.IDENT {
			// 用 LookAhead 掃過介面名列表，找到 { 後分類區塊型別
			// 支援 dotted 型別名（sql.stmt）和逗號分隔列表（json, fmt）
			skip := 0
			for {
				tok := p.lexer.LookAhead(skip)
				// dotted 型別名：sql.stmt, http.request 等
				if tok.Type == lexer.DOT {
					skip++
					if p.lexer.LookAhead(skip).Type != lexer.IDENT {
						break
					}
					skip++
					continue
				}
				if tok.Type == lexer.COMMA {
					skip++
					if p.lexer.LookAhead(skip).Type != lexer.IDENT {
						break
					}
					skip++
					continue
				}
				break
			}
			// LookAhead(skip) 應為 { 或其他
			if p.lexer.LookAhead(skip).Type == lexer.LBRACE {
				// 找到 {，往後看分類區塊內容
				contentSkip := skip + 1
				for {
					tok := p.lexer.LookAhead(contentSkip)
					if tok.Type != lexer.NEWLINE {
						break
					}
					contentSkip++
				}
				tok1 := p.lexer.LookAhead(contentSkip)
				tok2 := p.lexer.LookAhead(contentSkip + 1)

				// 介面繼承：method() 或 t.method() 形式
				if tok1.Type == lexer.IDENT && (tok2.Type == lexer.LPAREN || tok2.Type == lexer.DOT) {
					return p.parseInterfaceDefinition()
				}
				// 結構體實作
				return p.parseStructDefinition()
			}
		}

		if p.peekToken.Type == lexer.IDENT {
			// Check for labeled bare range-for: outer i <- (a..b] { }
			state := p.saveState()
			p.nextToken() // skip label
			if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ARROW {
				p.restoreState(state)
				return p.parseForStatement()
			}
			p.restoreState(state)
			stmt := p.parseLetStatement()
			if stmt != nil {
				if !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
					p.skipToStatementEnd()
				}
				return stmt
			}
		} else if p.peekToken.Type == lexer.ASSIGN {
			// 先檢查是否為函數定義：name = (params) { ... }
			if p.isFunctionDefinition() {
				return p.parseFunctionDefinition()
			}
			// 再檢查是否為具名函式型別定義：name = (params)(results)?
			// 僅在 `(` 開頭且非函式定義（無 { body }）時嘗試
			if p.isFunctionTypeAlias() {
				stmt := p.parseFunctionTypeAlias()
				if stmt != nil {
					p.skipToStatementEnd()
				}
				return stmt
			}
			// 等號語法型別別名：name = []type 或 name = ?type
			// 例：bytes = []byte
			if p.looksLikeEqualsTypeAlias() {
				stmt := p.parseTypeAlias()
				if stmt != nil {
					p.skipToStatementEnd()
				}
				return stmt
			}
			stmt := p.parseLetStatement()
			if stmt != nil {
				if !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
					p.skipToStatementEnd()
				}
				return stmt
			}
		} else if p.peekToken.Type == lexer.LBRACKET {
			// 检查是否为索引 a[i]、切片 a[..] 或数组类型标注 a [3] / a [3]u16
			state := p.saveState()
			p.nextToken() // skip IDENT
			p.nextToken() // skip LBRACKET
			isRange := p.currentToken.Type == lexer.ELLIPSIS ||
				((p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT) && p.peekToken.Type == lexer.ELLIPSIS)
			// Check for array declaration: [N]type followed by =, or [N] followed by = [...]
			isArrayDecl := false
			if !isRange && (p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT) {
				p.nextToken() // skip first token
				// Handle simple infix expression: 160+16, n*2, etc.
				if p.currentToken.Type == lexer.ADD || p.currentToken.Type == lexer.SUB ||
					p.currentToken.Type == lexer.MUL || p.currentToken.Type == lexer.QUO ||
					p.currentToken.Type == lexer.MOD || p.currentToken.Type == lexer.AND ||
					p.currentToken.Type == lexer.OR || p.currentToken.Type == lexer.XOR ||
					p.currentToken.Type == lexer.SHL || p.currentToken.Type == lexer.SHR {
					p.nextToken() // skip operator
					if p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT {
						p.nextToken() // skip second operand
					}
				}
				if p.currentToken.Type == lexer.RBRACKET {
					p.nextToken() // skip ]
					if p.currentToken.Type == lexer.IDENT {
						// Has element type: a [3]u16 = [...] or a [3]u16 (type annotation)
						p.nextToken() // skip element type
						isArrayDecl = true
						if p.currentToken.Type == lexer.ASSIGN {
							// Has assignment: name [N]type = value
						}
					} else if p.currentToken.Type == lexer.ASSIGN && p.peekToken.Type == lexer.LBRACKET {
						// No element type but RHS is array literal: a [3] = [1, 2, 3]
						isArrayDecl = true
					} else if p.currentToken.Type == lexer.LBRACKET {
						// Nested array type: a [N][M]T = [...]
						// Skip the inner [M]T to confirm it's a type annotation
						p.nextToken() // skip inner [
						if p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT {
							p.nextToken() // skip M
						}
						if p.currentToken.Type == lexer.RBRACKET {
							p.nextToken() // skip inner ]
							if p.currentToken.Type == lexer.IDENT {
								p.nextToken() // skip T
								isArrayDecl = true
							}
						}
					}
				}
			}
			// 檢查是否為索引 a[i]：[ 後是 expr，沒有 ..
			isIndex := !isRange && !isArrayDecl && p.currentToken.Type != lexer.RBRACKET
			p.restoreState(state)

			if isIndex || isRange {
				// 索引 a[i] 或切片 a[..] — 交給表達式解析
				// 不在此處處理
			} else {
				// a [3] = [...] 或 v []u8 = [...]
				stmt := p.parseLetStatement()
				if stmt != nil {
					if !p.ctx.contains(CTX_MATCH_ARM) {
						p.skipToStatementEnd()
					}
					return stmt
				}
			}
		} else if p.peekToken.Type == lexer.MAP {
			// m map[K]V = { ... } — explicit map type annotation
			stmt := p.parseLetStatement()
			if stmt != nil {
				if !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
					p.skipToStatementEnd()
				}
				return stmt
			}
		}

		if p.peekToken.Type == lexer.QUESTION {
			state := p.saveState()
			p.nextToken()
			if p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.IDENT {
				// currentToken = ?，parseLetStatement 內會用 prevToken 當變數名
				stmt := p.parseLetStatement()
				if stmt != nil {
					for p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.NEWLINE {
						if p.currentToken.Type == lexer.NEWLINE {
							break
						}
						p.nextToken()
					}
				}
				return stmt
			}
			p.restoreState(state) // 恢復到 name，交給表達式解析
		}

		if p.peekToken.Type == lexer.FOR {
			return p.parseForStatement()
		}
		if (p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.LESS) && p.isFunctionDefinition() {
			return p.parseFunctionDefinition()
		}
		if p.peekToken.Type == lexer.LBRACE {
			// 檢查 { 內的第一個 token 決定型別：枚舉/介面/結構體/標籤列舉
			switch p.classifyBlock() {
			case blockEnum:
				return p.parseEnumDefinition()
			case blockTaggedEnum:
				return p.parseTaggedEnumDefinition()
			case blockIface:
				return p.parseInterfaceDefinition()
			case blockStruct:
				return p.parseStructDefinition()
			}
		}

		// 方法定義：user.foo: (a int) \{ ... \}
		if p.peekToken.Type == lexer.DOT {
			state := p.saveState()
			structToken := p.currentToken
			p.nextToken() // skip IDENT (struct name)
			if p.currentToken.Type == lexer.DOT {
				p.nextToken() // skip DOT
				if p.currentToken.Type == lexer.IDENT && (p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.LESS) {
					if p.isFunctionDefinition() {
						p.restoreState(state)
						return p.parseMethodDefinition(structToken)
					}
				}
				// 方法定義冒號語法：user.foo: (a int) { ... }
				if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.COLON {
					p.restoreState(state)
					return p.parseColonMethodDefinition(structToken)
				}
			}
			p.restoreState(state)
		}

		// Type-only declaration with same name: i64, i8, etc. on its own line or followed by ;
		if p.peekToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.SEMICOLON {
			if isTypeName(p.currentToken.Literal) {
				stmt := p.parseLetStatement()
				if stmt != nil {
					if !p.ctx.contains(CTX_MATCH_ARM) {
						p.skipToStatementEnd()
					}
					return stmt
				}
			}
		}

		// 冒號語法函數定義：foo: (a int) { ... }
		if p.peekToken.Type == lexer.COLON {
			state := p.saveState()
			p.nextToken() // skip IDENT → COLON
			if p.peekToken.Type == lexer.LPAREN {
				p.restoreState(state) // back to IDENT
				stmt := p.parseColonFunctionDefinition()
				if !p.ctx.contains(CTX_MATCH_ARM) {
					p.skipToStatementEnd()
				}
				return stmt
			}
			p.restoreState(state)
		}

		// 範圍遍歷：i <- (a..b] { 或 i <- [a..b] {
		if p.peekToken.Type == lexer.ARROW && !p.ctx.contains(CTX_FOR_COND) {
			return p.parseForStatement()
		}

		// 多重賦值：existing, exist-n = read-file(path)
		if p.peekToken.Type == lexer.COMMA {
			return p.parseMultiAssignStatement()
		}

		return p.parseExpressionStatement()

	case lexer.RETURN:
		return p.parseReturnStatement()

	case lexer.LBRACE:
		// { body } * N 計數循環（新式語法，取代舊的 N * { }）
		if p.isCountedLoopBlockFirst() {
			return p.parseCountedLoopBlockFirst()
		}
		// { body } (cond) 條件循環 或 { body } () 無限循環（新式語法，取代 !! { } 與 for cond { }）
		if p.isCondLoopBlockFirst() {
			return p.parseCondLoopBlockFirst()
		}
		bt := p.classifyBlockAtCurrent()
		if bt == blockMatch {
			tok := p.currentToken
			// parseBareMatchExpr manages its own CTX_MATCH_ARM context (pushing
			// it for arm conditions and inline bodies, but NOT for block bodies).
			// If CTX_MATCH_ARM is already on the stack — leaked from an outer
			// bare match's arm-body parsing — it would prevent standalone
			// if-then (cond -> body) inside arm block bodies, causing
			// `bend < 0 -> return` to be mis-parsed as separate statements.
			// Temporarily strip CTX_MATCH_ARM so the inner bare match starts clean.
			savedCtx := p.ctx.copy()
			p.ctx = p.ctx.filterOut(CTX_MATCH_ARM)
			expr := p.parseBareMatchExpr()
			p.ctx = savedCtx
			if expr != nil {
				return &ExpressionStatement{Token: tok, Expression: expr}
			}
			return nil
		}
		// 裸條件 : { body } 語法 — 將 { 作為 ForStatement 的主體
		// 前面的表達式在 parseExpressionStatement 中已解析
		// 這裡回退並由 transpiler 處理
		return p.parseExpressionStatement()

	case lexer.RBRACE:
		return nil

	case lexer.NOT:
		// ! { } → 無限循環（!! 的單驚嘆號變體，向後相容舊語法）
		if p.peekToken.Type == lexer.LBRACE {
			return p.parseBangLoop()
		}
		return p.parseExpressionStatement()

	case lexer.BANG_BANG:
		// 無限循環 !! { }
		if p.peekToken.Type == lexer.LBRACE {
			return p.parseBangLoop()
		}
		return p.parseExpressionStatement()

	case lexer.MUL:
		// `*` 在語句起始位置時是 break 簡寫。
		// 與 `*` 作為指針解引用或乘法區分：只有當 `*` 出現在語句第一個
		// token 且後面是 NEWLINE/EOF/LABEL/IDENT/SEMICOLON 時視為 break。
		return p.parseStarBreakOrExpr()

	case lexer.STAR_STAR:
		// `**` 是 continue 簡寫。形如 `** #1` 或 `**` 後接換行。
		return p.parseStarStarContinue()

	case lexer.LBRACKET:
		// [n]t.method-name(…) { — 陣列型別方法定義
		if p.isArrayTypeMethodDefinition() {
			return p.parseArrayTypeMethodDefinition()
		}
		return p.parseExpressionStatement()

	default:
		return p.parseExpressionStatement()
	}
}

// parseMethodDefinition 解析方法定義：user.foo: (a int) { ... }
// 脫糖為 FunctionDefinition，名稱為 "user.foo"，並插入 self 為首個參數
func (p *Parser) parseMethodDefinition(structToken lexer.Token) Statement {
	// 前進到方法名
	p.nextToken() // skip struct name → DOT
	p.nextToken() // skip DOT → method name (IDENT)

	// 此時 currentToken = IDENT("foo"), peekToken = LPAREN
	methodName := p.currentToken.Literal
	fullName := structToken.Literal + "." + methodName

	// 推入方法上下文，供 let 型別推斷解析 self.field 型別
	p.methodStructStack = append(p.methodStructStack, structToken.Literal)

	// 調用 parseFunctionDefinition 解析主體
	fd := p.parseFunctionDefinition()

	// 彈出方法上下文
	if len(p.methodStructStack) > 0 {
		p.methodStructStack = p.methodStructStack[:len(p.methodStructStack)-1]
	}

	if fd == nil {
		return nil
	}

	funcDef, ok := fd.(*FunctionDefinition)
	if !ok {
		return fd
	}

	// 對泛型結構體模板的方法（如 hashmap-str-tmpl.put），
	// 清除 detectImplicitGeneric 誤加的函數級 GenericParams。
	// 這些方法使用的單字母型別（如 v）是結構體級泛型參數，
	// 由 monomorphizeGenericStructs 處理，不應被 monomorphizeGenerics 的過濾器移除。
	if strings.HasSuffix(structToken.Literal, "-tmpl") && len(funcDef.GenericParams) > 0 {
		funcDef.GenericParams = []*Identifier{}
	}

	// 修改名稱
	funcDef.Name = fullName

	// 修復 funcSignatures 鍵：parseFunctionBody 以原始方法名存儲，需更新為完整名
	if p.funcSignatures != nil {
		if rets, ok := p.funcSignatures[methodName]; ok {
			delete(p.funcSignatures, methodName)
			p.funcSignatures[fullName] = rets
		}
	}

	// 插入 self 參數
	selfParam := &Parameter{
		Token: structToken,
		Name:  "self",
		Type:  buildType(structToken.Literal, structToken),
	}
	funcDef.Parameters = append([]*Parameter{selfParam}, funcDef.Parameters...)

	return funcDef
}

// isArrayTypeMethodDefinition 檢測是否為陣列/切片型別方法定義：[n]t.method(…)、[]t.method(…)、[?]t.method(…) {
func (p *Parser) isArrayTypeMethodDefinition() bool {
	state := p.saveState()
	defer p.restoreState(state)

	p.nextToken() // skip [
	if p.currentToken.Type == lexer.RBRACKET {
		// []t.method — 切片型別
		p.nextToken() // skip ]
		if p.currentToken.Type != lexer.IDENT {
			return false
		}
	} else if p.currentToken.Type == lexer.QUESTION {
		// [?]t.method — 可空切片型別
		p.nextToken() // skip ?
		if p.currentToken.Type != lexer.RBRACKET {
			return false
		}
		p.nextToken() // skip ]
		if p.currentToken.Type != lexer.IDENT {
			return false
		}
	} else if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.INT {
		// [n]t.method — 陣列型別
		p.nextToken() // skip size
		if p.currentToken.Type != lexer.RBRACKET {
			return false
		}
		p.nextToken() // skip ]
		if p.currentToken.Type != lexer.IDENT {
			return false
		}
	} else {
		return false
	}
	p.nextToken() // skip element type
	if p.currentToken.Type != lexer.DOT {
		return false
	}
	p.nextToken() // skip .
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	p.nextToken() // skip method name

	// 可選 =
	if p.currentToken.Type == lexer.ASSIGN {
		p.nextToken() // skip =
	}

	// (params)
	if p.currentToken.Type != lexer.LPAREN {
		return false
	}
	p.nextToken() // skip (
	for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
		p.nextToken()
	}
	if p.currentToken.Type != lexer.RPAREN {
		return false
	}
	p.nextToken() // skip )

	// 可選 NEWLINE
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	// 可選回傳型別：…) i64 {
	if p.currentToken.Type == lexer.IDENT {
		p.nextToken()
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
	}
	// 可選結果參數：…)(r i64) {
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}

	return p.currentToken.Type == lexer.LBRACE
}

// parseArrayTypeMethodDefinition 解析陣列/切片型別方法定義：[n]t.method(…)、[]t.method(…)、[?]t.method(…) {
func (p *Parser) parseArrayTypeMethodDefinition() Statement {
	def := &FunctionDefinition{
		Token: p.currentToken,
		FuncSignature: FuncSignature{
			GenericParams: []*Identifier{},
			Parameters:    []*Parameter{},
			Results:       []*Parameter{},
		},
	}

	// 建立型別字串 [n]t、[]t 或 [?]t
	p.nextToken() // skip [
	var arrayType string
	var elemToken lexer.Token
	if p.currentToken.Type == lexer.RBRACKET {
		// []t — 切片型別
		arrayType = "[]"
		p.nextToken() // skip ]
		elemToken = p.currentToken
		arrayType += elemToken.Literal
		p.nextToken() // skip element type
	} else if p.currentToken.Type == lexer.QUESTION {
		// [?]t — 可空切片型別
		arrayType = "[?]"
		p.nextToken() // skip ?
		if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected ']' in nullable slice type, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		p.nextToken() // skip ]
		elemToken = p.currentToken
		arrayType += elemToken.Literal
		p.nextToken() // skip element type
	} else {
		// [n]t — 陣列型別
		sizeToken := p.currentToken
		arrayType = "[" + sizeToken.Literal + "]"
		p.nextToken() // skip size
		if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected ']' in array type, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		p.nextToken() // skip ]
		elemToken = p.currentToken
		arrayType += elemToken.Literal
		p.nextToken() // skip element type
		// Infer implicit generic params from the receiver type (e.g. n and t in [n]t)
		addImplicitGeneric(sizeToken.Literal, def)
		addImplicitGeneric(elemToken.Literal, def)
	}

	if p.currentToken.Type != lexer.DOT {
		msg := fmt.Sprintf("line %d, column %d: expected '.' after type, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip .

	// 方法名
	if p.currentToken.Type != lexer.IDENT {
		msg := fmt.Sprintf("line %d, column %d: expected method name, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	methodName := p.currentToken.Literal
	def.Name = arrayType + "." + methodName
	p.nextToken() // skip method name

	// 新語法需要 = 作為函數定義標記
	if p.currentToken.Type == lexer.ASSIGN {
		p.nextToken() // skip =
	}

	// 解析參數列表
	if p.currentToken.Type != lexer.LPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected '('",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip (

	if p.currentToken.Type != lexer.RPAREN {
		for {
			if p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
				continue
			}
			if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
				msg := fmt.Sprintf("line %d, column %d: expected parameter name, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			paramName := p.currentToken.Literal
			paramToken := p.currentToken
			p.nextToken()

			paramType := ""
			isOption := false
			if p.currentToken.Type == lexer.QUESTION {
				isOption = true
				p.nextToken()
			}
			if p.currentToken.Type == lexer.LBRACKET {
				p.nextToken()
				if p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT {
					paramType = "[" + p.currentToken.Literal + "]"
					p.nextToken()
				} else if p.currentToken.Type == lexer.QUESTION {
					// [?] — infer array size from literal
					paramType = "[?]"
					p.nextToken()
				} else {
					paramType = "[]"
				}
				if p.currentToken.Type == lexer.RBRACKET {
					p.nextToken()
				}
				if p.currentToken.Type == lexer.IDENT {
					paramType = paramType + p.currentToken.Literal
					p.nextToken()
				}
			} else if p.currentToken.Type == lexer.IDENT {
				paramType = p.currentToken.Literal
				p.nextToken()
				// Support dotted/qualified type names: sql.result, etc.
				for p.currentToken.Type == lexer.DOT {
					paramType += "."
					p.nextToken()
					if p.currentToken.Type == lexer.IDENT {
						paramType += p.currentToken.Literal
						p.nextToken()
					}
				}
			} else if !isOption {
				msg := fmt.Sprintf("line %d, column %d: expected parameter type, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			if isOption {
				paramType = "?" + paramType
			}

			param := &Parameter{
				Token: paramToken,
				Name:  paramName,
				Type:  buildType(paramType, paramToken),
			}

			// 參數默認值：name type = expr
			if p.currentToken.Type == lexer.ASSIGN {
				p.nextToken() // skip =
				param.DefaultExpr = p.parseExpression(LOWEST)
			}

			def.Parameters = append(def.Parameters, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}
			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			p.nextToken()
		}
	}

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected ')'",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip )

	// 跳過 NEWLINE
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	// 可選回傳型別：…) i64 {
	if p.currentToken.Type == lexer.IDENT {
		result := &Parameter{
			Token: p.currentToken,
			Name:  "",
			Type:  buildType(p.currentToken.Literal, p.currentToken),
		}
		def.Results = append(def.Results, result)
		p.nextToken()
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
	}

	// 可選結果參數：…)(r i64) {
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		if p.currentToken.Type != lexer.RPAREN {
			for {
				if p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
					continue
				}
				if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
					msg := fmt.Sprintf("line %d, column %d: expected parameter name, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}

				paramName := p.currentToken.Literal
				paramToken := p.currentToken
				p.nextToken()

				paramType := ""
				isOption := false
				if p.currentToken.Type == lexer.QUESTION {
					isOption = true
					p.nextToken()
				}
				if p.currentToken.Type == lexer.LBRACKET {
					p.nextToken()
					if p.currentToken.Type == lexer.INT || p.currentToken.Type == lexer.IDENT {
						paramType = "[" + p.currentToken.Literal + "]"
						p.nextToken()
					} else if p.currentToken.Type == lexer.QUESTION {
						// [?] — infer array size from literal
						paramType = "[?]"
						p.nextToken()
					} else {
						paramType = "[]"
					}
					if p.currentToken.Type == lexer.RBRACKET {
						p.nextToken()
					}
					if p.currentToken.Type == lexer.IDENT {
						paramType = paramType + p.currentToken.Literal
						p.nextToken()
					}
				} else if p.currentToken.Type == lexer.IDENT {
					paramType = p.currentToken.Literal
					p.nextToken()
					// Support dotted/qualified type names: sql.result, etc.
					for p.currentToken.Type == lexer.DOT {
						paramType += "."
						p.nextToken()
						if p.currentToken.Type == lexer.IDENT {
							paramType += p.currentToken.Literal
							p.nextToken()
						}
					}
				} else if !isOption {
					msg := fmt.Sprintf("line %d, column %d: expected parameter type, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}
				if isOption {
					paramType = "?" + paramType
				}

				def.Results = append(def.Results, &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  buildType(paramType, paramToken),
				})

				if p.currentToken.Type == lexer.RPAREN {
					break
				}
				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected comma or ')'",
						p.currentToken.Line, p.currentToken.Column)
					p.saveError(msg)
					return nil
				}
				p.nextToken()
			}
		}

		if p.currentToken.Type != lexer.RPAREN {
			msg := fmt.Sprintf("line %d, column %d: expected ')'",
				p.currentToken.Line, p.currentToken.Column)
			p.saveError(msg)
			return nil
		}
		p.nextToken()
	}

	// 推斷隱式泛型參數
	for _, param := range def.Parameters {
		detectImplicitGeneric(param.Type, def)
	}
	for _, param := range def.Results {
		detectImplicitGeneric(param.Type, def)
	}

	// 解析主體
	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected '{'",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return nil
	}
	p.ctx.push(CTX_FUNC_BODY)
	def.Body = p.parseBlockStatement()
	p.ctx.pop()

	// Move inline comment on the same line as { from OpeningBraceComment to
	// the function definition's Comment field, so the formatter outputs it
	// after the { on the same line.
	if def.Body.OpeningBraceComment != nil && len(def.Body.OpeningBraceComment.List) > 0 {
		setComment(def, def.Body.OpeningBraceComment)
		def.Body.OpeningBraceComment = nil
	}
	// Also check TrailingComments (fallback for empty blocks where the comment
	// was collected as a trailing comment before the OpeningBraceComment fix)
	if def.Body.TrailingComments != nil && len(def.Body.TrailingComments.List) > 0 &&
		def.Body.TrailingComments.List[0].Pos.Line == def.Body.Token.Line {
		setComment(def, def.Body.TrailingComments)
		def.Body.TrailingComments = nil
	}

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// 插入 self 參數
	selfParam := &Parameter{
		Token: elemToken,
		Name:  "self",
		Type:  buildType(arrayType, elemToken),
	}
	def.Parameters = append([]*Parameter{selfParam}, def.Parameters...)

	return def
}

func (p *Parser) isFunctionDefinition() bool {
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	if p.peekToken.Type != lexer.ASSIGN && p.peekToken.Type != lexer.LESS {
		return false
	}

	state := p.saveState()

	// 跳过 IDENT 令牌
	p.nextToken()

	// 跳過選擇性泛型參數：foo<N>: (...)
	if p.currentToken.Type == lexer.LESS {
		for p.currentToken.Type != lexer.GREATER && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}
		if p.currentToken.Type != lexer.GREATER {
			p.restoreState(state)
			return false
		}
		p.nextToken()
	}

	// 跳過 ASSIGN 令牌
	if p.currentToken.Type != lexer.ASSIGN {
		p.restoreState(state)
		return false
	}
	p.nextToken()

	// 跳過 LPAREN 令牌
	if p.currentToken.Type != lexer.LPAREN {
		p.restoreState(state)
		return false
	}
	p.nextToken()

	isFunctionDef := false

	// 无参数: foo() { ... } 或 foo()(r) { ... }
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
		// 跳過回傳型別或結果參數
		if p.currentToken.Type == lexer.IDENT {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.LPAREN {
			p.nextToken()
			for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				p.nextToken()
			}
		}
		if p.currentToken.Type == lexer.LBRACE {
			isFunctionDef = true
		}
		p.restoreState(state)
		return isFunctionDef
	}

	// 有参数: 跳过 (id, id, ...) 直到 RPAREN
	// 當參數型別為函式型別（如 `cb ()()` 或 `cb (n i64)(r i64)`）時，
	// 允許 LPAREN/RPAREN 巢狀結構通過。函式型別以 `(` 開頭（無 fn 關鍵字），
	// 故在參數名 IDENT 之後遇到 LPAREN 即視為函式型別起點。函式型別可帶
	// 選用的結果列表 `(...)`，因此在關閉參數列表後需允許緊接的一個 `(...)`
	// 作為結果列表。
	parenDepth := 0
	prevIdent := false           // 上一個 token 是 IDENT（可能是參數名，後接函式型別 `(`）
	allowFnResultsParen := false // 允許函式型別關閉參數列表後的結果列表 LPAREN
	skippingDefault := false     // 正在跳過參數默認值表達式
	for (p.currentToken.Type != lexer.RPAREN || parenDepth > 0) && p.currentToken.Type != lexer.EOF {
		// 跳過參數默認值表達式：遇到 ASSIGN 後，跳到下一個 COMMA 或 RPAREN（depth 0）
		if skippingDefault {
			if p.currentToken.Type == lexer.COMMA && parenDepth == 0 {
				skippingDefault = false
			} else if p.currentToken.Type == lexer.RPAREN && parenDepth == 0 {
				skippingDefault = false
				continue // 不消耗 RPAREN，讓迴圈條件判斷
			} else if p.currentToken.Type == lexer.LPAREN {
				parenDepth++
			} else if p.currentToken.Type == lexer.RPAREN && parenDepth > 0 {
				parenDepth--
			}
			p.nextToken()
			continue
		}
		if p.currentToken.Type == lexer.ASSIGN {
			skippingDefault = true
			prevIdent = false
			allowFnResultsParen = false
			p.nextToken()
			continue
		}
		if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN &&
			p.currentToken.Type != lexer.INT &&
			p.currentToken.Type != lexer.QUESTION &&
			p.currentToken.Type != lexer.COMMA &&
			p.currentToken.Type != lexer.LBRACKET && p.currentToken.Type != lexer.RBRACKET &&
			p.currentToken.Type != lexer.ELLIPSIS &&
			p.currentToken.Type != lexer.NEWLINE &&
			!(p.currentToken.Type == lexer.LPAREN && prevIdent) &&
			!(p.currentToken.Type == lexer.LPAREN && allowFnResultsParen) &&
			!(p.currentToken.Type == lexer.RPAREN && parenDepth > 0) {
			p.restoreState(state)
			return false
		}
		switch p.currentToken.Type {
		case lexer.IDENT:
			prevIdent = true
			allowFnResultsParen = false
		case lexer.LPAREN:
			if prevIdent {
				parenDepth++
				prevIdent = false
			} else if allowFnResultsParen {
				parenDepth++
				allowFnResultsParen = false
			}
		case lexer.RPAREN:
			if parenDepth > 0 {
				parenDepth--
				// 關閉函式型別的某一組括號後，允許緊接的結果列表 (...)
				if parenDepth == 0 {
					allowFnResultsParen = true
				}
			}
		default:
			prevIdent = false
			allowFnResultsParen = false
		}
		p.nextToken()
	}

	// 检查后面是否是 LBRACE（允許中間有回傳型別或結果參數）
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
		// 跳過 NEWLINE（多行定義）
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		// 跳過選擇性回傳型別：fib(n i64) i64 {
		if p.currentToken.Type == lexer.IDENT {
			p.nextToken()
		}
		// 跳過 NEWLINE（多行定義）
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		// 跳過結果參數：fib(n i64)(r i64) {
		if p.currentToken.Type == lexer.LPAREN {
			p.nextToken()
			for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				p.nextToken()
			}
		}
		if p.currentToken.Type == lexer.LBRACE {
			isFunctionDef = true
		}
	}

	p.restoreState(state)
	return isFunctionDef
}

// isFunctionTypeAlias 判斷當前位置是否為具名函式型別定義：name = (params)(results)?
// 判斷依據：
//  1. currentToken 為 IDENT，peekToken 為 ASSIGN，且其後緊接 LPAREN
//  2. 第一組 `(...)` 後緊接 NEWLINE/EOF/SEMICOLON（無結果列表），或緊接另一組 `(...)`
//     （結果列表），結果列表後再緊接 NEWLINE/EOF/SEMICOLON
//  3. `(...)` 內容確實為合法的函式型別參數列表（透過 parseFunctionType 驗證）
//
// 此方法不消耗任何 token（呼叫前後 parser 狀態相同）。
// 注意：LookAhead(0) 回傳 peekToken 之後的下一個 token（即 currentToken 之後第二個）。
func (p *Parser) isFunctionTypeAlias() bool {
	// Step 1: 結構檢查（快速篩選，避免對非匹配結構呼叫 parseFunctionType）
	if !p.isFunctionTypeAliasShape() {
		return false
	}
	// Step 2: 內容驗證（透過 parseFunctionType 確認 `(...)` 內容為合法函式型別參數）
	// 使用 saveState/restoreState 確保不影響 parser 狀態。
	// 注意：不依賴 parseFunctionType 的游標位置（其會跳過 NEWLINE 尋找結果列表），
	// 僅以錯誤計數判斷內容是否合法。
	state := p.saveState()
	errCount := len(p.errors)
	p.nextToken() // IDENT → ASSIGN
	p.nextToken() // ASSIGN → LPAREN
	p.parseFunctionType()
	parseFailed := len(p.errors) > errCount
	if parseFailed {
		// 回滾試解析期間產生的錯誤
		p.errors = p.errors[:errCount]
	}
	p.restoreState(state)
	return !parseFailed
}

// isFunctionTypeAliasShape 以 token 掃描方式判斷當前位置是否符合
// `name = (params)(results)?` 的括號結構與陳述句邊界。
// 不檢查 `(...)` 內容是否為合法函式型別參數（由 isFunctionTypeAlias 的 Step 2 負責）。
// 不消耗任何 token。
func (p *Parser) isFunctionTypeAliasShape() bool {
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	if p.peekToken.Type != lexer.ASSIGN {
		return false
	}
	// `=` 之後必須是 `(`（LookAhead(0) = peekToken 之後的 token）
	if p.lexer.LookAhead(0).Type != lexer.LPAREN {
		return false
	}

	// 透過 LookAhead 掃描：位置 0=`(`, 1=第一個 ( 內的 token 或 `)`, ...
	// 我們需要找到第一組 `(...)` 的結尾 `)`，然後檢查其後。
	// 掃描時追蹤括號深度，遇到深度歸零的 `)` 後檢查下一個 token。
	depth := 0
	i := 0
	// 先找到 `(`（位置 0）
	for {
		t := p.lexer.LookAhead(i)
		switch t.Type {
		case lexer.LPAREN:
			depth++
		case lexer.RPAREN:
			depth--
			if depth == 0 {
				// 找到第一組 `(...)` 的結尾 `)`
				// 檢查下一個 token
				next := p.lexer.LookAhead(i + 1)
				switch next.Type {
				case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
					return true
				case lexer.LPAREN:
					// 可能是結果列表 `(results)`
					// 掃描第二組 `(...)`
					depth2 := 0
					j := i + 1
					for {
						t2 := p.lexer.LookAhead(j)
						switch t2.Type {
						case lexer.LPAREN:
							depth2++
						case lexer.RPAREN:
							depth2--
							if depth2 == 0 {
								// 第二組 `)` 後必須是陳述句結束
								next2 := p.lexer.LookAhead(j + 1)
								switch next2.Type {
								case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
									return true
								default:
									return false
								}
							}
						case lexer.EOF:
							return false
						}
						j++
						if j > 256 { // 安全上限
							return false
						}
					}
				default:
					return false
				}
			}
		case lexer.EOF:
			return false
		}
		i++
		if i > 256 { // 安全上限
			return false
		}
	}
}

// parseFunctionTypeAlias 解析具名函式型別定義：name = (params)(results)?
// 前置條件: currentToken 為 IDENT（名稱），peekToken 為 ASSIGN
// 後置條件: currentToken 為函式型別後的第一個 token（通常為 NEWLINE/EOF）
// 回傳 *TypeAlias{ Name, Type: *FunctionType{...} }
func (p *Parser) parseFunctionTypeAlias() Statement {
	name := p.currentToken.Literal
	nameToken := p.currentToken
	ta := &TypeAlias{Token: nameToken, Name: name}

	p.nextToken() // skip IDENT → ASSIGN
	if p.currentToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected '=' in function type alias, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip ASSIGN → LPAREN

	if p.currentToken.Type != lexer.LPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected '(' after '=' in function type alias, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	ft := p.parseFunctionType()
	ta.Type = ft
	return ta
}

func (p *Parser) parseUseStatement() Statement {
	stmt := &UseStatement{Token: p.currentToken}

	// use path.fn [alias]
	// path: std/math, github.com/utils/math, /utils/math
	// fn: add, println, etc.
	p.nextToken() // skip USE

	// 解析路徑：由 IDENT、/、. 組成的序列
	var parts []string

	// 處理前導 /
	if p.currentToken.Type == lexer.QUO {
		parts = append(parts, "/")
		p.nextToken()
	}

	for {
		// use path 段接受 IDENT，以及可能作為路徑名稱的關鍵字（如 map）
		if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.MAP {
			msg := fmt.Sprintf("line %d, column %d: expected identifier in use path, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		parts = append(parts, p.currentToken.Literal)
		p.nextToken()

		// 預期 / 或 .
		if p.currentToken.Type == lexer.QUO {
			// / → 繼續路徑
			parts = append(parts, "/")
			p.nextToken()
		} else if p.currentToken.Type == lexer.DOT {
			// DOT 後面是 IDENT + (NEWLINE/EOF/IDENT) → 函數名分隔符
			// 否則（DOT + IDENT + /）→ 路徑的一部分（如 github.com）
			if p.peekToken.Type == lexer.IDENT {
				// 向後看第二個 token
				state := p.saveState()
				p.nextToken() // skip .
				p.nextToken() // skip potential func name
				isFn := p.currentToken.Type == lexer.NEWLINE ||
					p.currentToken.Type == lexer.EOF ||
					p.currentToken.Type == lexer.IDENT ||
					p.currentToken.Type == lexer.AS ||
					p.currentToken.Type == lexer.RBRACE
				p.restoreState(state)
				// currentToken 現在恢復到 DOT
				if isFn {
					// 這是函數名分隔符：消費 DOT + funcName
					p.nextToken() // skip .
					stmt.Function = p.currentToken.Literal
					p.nextToken() // skip funcName
					// 可選別名
					if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.AS {
						if p.currentToken.Type == lexer.AS {
							stmt.AsKeyword = true
							// # module.fn as alias → skip "as" and use next IDENT
							p.nextToken()
							if p.currentToken.Type == lexer.IDENT {
								stmt.Alias = p.currentToken.Literal
								p.nextToken()
							}
						} else {
							// # module.fn alias → use directly
							stmt.Alias = p.currentToken.Literal
							p.nextToken()
						}
					}
					stmt.Path = joinPathParts(parts)
					return stmt
				}
			}
			// 路徑中的 DOT（如 github.com）
			parts = append(parts, ".")
			p.nextToken() // skip .
			continue
		} else {
			break
		}
	}

	// 沒有函數名的情況（不應該發生，但兼容處理）
	stmt.Path = joinPathParts(parts)
	return stmt
}

func (p *Parser) parseExportStatement() Statement {
	stmt := &ExportStatement{Token: p.currentToken}

	// @ path.fn [alias]
	p.nextToken() // skip AT

	// 解析路徑：由 IDENT、/、. 組成的序列
	var parts []string

	// 處理前導 /
	if p.currentToken.Type == lexer.QUO {
		parts = append(parts, "/")
		p.nextToken()
	}

	for {
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected identifier in export path, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		parts = append(parts, p.currentToken.Literal)
		p.nextToken()

		// 預期 / 或 .
		if p.currentToken.Type == lexer.QUO {
			// / → 繼續路徑
			parts = append(parts, "/")
			p.nextToken()
		} else if p.currentToken.Type == lexer.DOT {
			// DOT 後面是 IDENT + (NEWLINE/EOF/IDENT) → 函數名分隔符
			if p.peekToken.Type == lexer.IDENT {
				state := p.saveState()
				p.nextToken() // skip .
				p.nextToken() // skip potential func name
				isFn := p.currentToken.Type == lexer.NEWLINE ||
					p.currentToken.Type == lexer.EOF ||
					p.currentToken.Type == lexer.IDENT ||
					p.currentToken.Type == lexer.AS ||
					p.currentToken.Type == lexer.RBRACE
				p.restoreState(state)
				if isFn {
					p.nextToken() // skip .
					stmt.Function = p.currentToken.Literal
					p.nextToken() // skip funcName
					// 可選別名
					if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.AS {
						if p.currentToken.Type == lexer.AS {
							stmt.AsKeyword = true
							p.nextToken()
							if p.currentToken.Type == lexer.IDENT {
								stmt.Alias = p.currentToken.Literal
								p.nextToken()
							}
						} else {
							stmt.Alias = p.currentToken.Literal
							p.nextToken()
						}
					}
					stmt.Path = joinPathParts(parts)
					return stmt
				}
			}
			// 路徑中的 DOT（如 github.com）
			parts = append(parts, ".")
			p.nextToken() // skip .
			continue
		} else {
			break
		}
	}

	// 沒有函數名的情況（不應該發生，但兼容處理）
	stmt.Path = joinPathParts(parts)
	return stmt
}

// joinPathParts 將解析出的路徑片段拼接為路徑字串
func joinPathParts(parts []string) string {
	var sb strings.Builder
	for _, part := range parts {
		sb.WriteString(part)
	}
	return sb.String()
}

// parseMultiAssignStatement parses multi-variable assignment:
//
//	existing, exist-n = read-file(path)
//
// The left-side variables are treated as new definitions if not already defined.
func (p *Parser) parseMultiAssignStatement() Statement {
	var targets []Expression

	// First variable name (already confirmed as IDENT by caller)
	targets = append(targets, &Identifier{Token: p.currentToken, Value: p.currentToken.Literal})
	p.nextToken() // skip first IDENT

	// Additional variables separated by commas
	for p.currentToken.Type == lexer.COMMA {
		p.nextToken() // skip COMMA
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected variable name after ',', got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		targets = append(targets, &Identifier{Token: p.currentToken, Value: p.currentToken.Literal})
		p.nextToken() // skip IDENT
	}

	if p.currentToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected '=' after multi-variable list",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return nil
	}
	tok := p.currentToken
	p.nextToken() // skip =

	value := p.parseExpression(LOWEST)
	if !p.ctx.contains(CTX_MATCH_ARM) {
		p.skipToStatementEnd()
	}

	return &MultiAssignStatement{
		Token:   tok,
		Targets: targets,
		Value:   value,
	}
}

func (p *Parser) parseLetStatement() Statement {
	// 保存当前令牌，用于变量名
	var nameToken lexer.Token
	if p.currentToken.Type == lexer.QUESTION {
		// 可空类型的情况，使用前一个令牌作为变量名
		nameToken = p.prevToken
	} else {
		// 普通情况，使用当前令牌作为变量名
		nameToken = p.currentToken
	}

	stmt := &LetStatement{Token: nameToken}

	// 直接使用当前的 IDENT 令牌作为变量名
	stmt.Name = &Identifier{
		Token: nameToken,
		Value: nameToken.Literal,
	}

	// 陣列/切片型別: a [3] / a [3]u16 / v []u8
	var letIsOption bool
	if p.peekToken.Type == lexer.LBRACKET {
		bracketToken := p.peekToken
		p.nextToken() // skip [ → current = LBRACKET
		p.nextToken() // consume [ → current = first content token

		// Check for map type: [K]V where K is a builtin or registered type name
		if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RBRACKET &&
			(isBuiltinTypeName(p.currentToken.Literal) || p.isRegisteredTypeName(p.currentToken.Literal)) {
			keyName := p.currentToken.Literal
			keyTok := p.currentToken
			p.nextToken() // skip K → current = ]
			p.nextToken() // skip ] → current = V
			if p.currentToken.Type == lexer.IDENT {
				valName := p.currentToken.Literal
				valTok := p.currentToken
				p.nextToken()
				stmt.Type = &MapType{
					Token: bracketToken,
					Key:   &NamedType{Token: keyTok, Value: keyName},
					Value: &NamedType{Token: valTok, Value: valName},
				}
			}
		} else {
			hasSize := false
			var sizeExpr Expression
			if p.currentToken.Type == lexer.QUESTION {
				// [?] — infer array size from literal
				hasSize = true
				// Size stays nil (nil means inferred)
				p.nextToken() // skip ? → current = ]
			} else if p.currentToken.Type != lexer.RBRACKET {
				// Parse expression for array size (e.g., 160+16, n*2, 3, etc.)
				sizeExpr = p.parseExpression(LOWEST)
				hasSize = true
				// Advance past the last token of the expression to reach ]
				if p.currentToken.Type != lexer.RBRACKET {
					p.nextToken()
				}
			}
			// ] 關閉（無 INT 時 current 已是 ]）
			if p.currentToken.Type == lexer.RBRACKET {
				p.nextToken()
				// 可選元素型別: [3]u16 或 []u8
				if p.currentToken.Type == lexer.IDENT {
					elemType := p.currentToken.Literal
					elem := &NamedType{Token: p.currentToken, Value: elemType}
					if hasSize {
						stmt.Type = &ArrayType{Token: bracketToken, Size: sizeExpr, Elem: elem}
					} else {
						stmt.Type = &SliceType{Token: bracketToken, Elem: elem}
					}
					p.nextToken()
				} else if p.currentToken.Type == lexer.LBRACKET {
					// Nested array type: [N][M]T — recursively parse element type
					elemType, ok := p.parseTypeExpression()
					if ok {
						if hasSize {
							stmt.Type = &ArrayType{Token: bracketToken, Size: sizeExpr, Elem: elemType}
						} else {
							stmt.Type = &SliceType{Token: bracketToken, Elem: elemType}
						}
					}
				} else {
					if hasSize {
						stmt.Type = &ArrayType{Token: bracketToken, Size: sizeExpr, Elem: &NamedType{Token: bracketToken, Value: "i64"}}
					} else {
						stmt.Type = &SliceType{Token: bracketToken, Elem: &NamedType{Token: bracketToken, Value: "i64"}}
					}
				}
			}
		}
	} else if p.peekToken.Type == lexer.MAP {
		// Explicit map type: m map[K]V
		mapTok := p.peekToken
		p.nextToken() // skip to map keyword → current = MAP
		p.nextToken() // skip map → current = [
		if p.currentToken.Type == lexer.LBRACKET {
			p.nextToken() // skip [
			if p.currentToken.Type == lexer.IDENT {
				keyName := p.currentToken.Literal
				keyTok := p.currentToken
				p.nextToken() // skip K → current = ]
				if p.currentToken.Type == lexer.RBRACKET {
					p.nextToken() // skip ] → current = V
					if p.currentToken.Type == lexer.IDENT {
						valName := p.currentToken.Literal
						valTok := p.currentToken
						p.nextToken()
						stmt.Type = &MapType{
							Token: mapTok,
							Key:   &NamedType{Token: keyTok, Value: keyName},
							Value: &NamedType{Token: valTok, Value: valName},
						}
					}
				}
			}
		}
	}

	if stmt.Type == nil && slices.Contains([]string{"byte", "bool", "char", "str", "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64"}, stmt.Name.Value) {
		stmt.Type = &NamedType{
			Token: nameToken,
			Value: nameToken.Literal,
		}
	}

	// 检查是否是可空类型
	if p.currentToken.Type == lexer.QUESTION {
		letIsOption = true
		p.nextToken() // 跳过 ?
	} else if p.peekToken.Type == lexer.QUESTION {
		p.nextToken() // 跳过 ?
		if p.peekToken.Type == lexer.IDENT {
			letIsOption = true
		}
	}

	// 解析类型（支援 current 或 peek 為型別）
	typeToken := p.peekToken
	if typeToken.Type != lexer.IDENT && p.currentToken.Type == lexer.IDENT {
		typeToken = p.currentToken
	}
	// 如果當前已經是 =，則 peek 的 IDENT 是值（表達式）而不是型別
	// 例如: a = bigint{}  -> 這裡 bigint 是結構體字面量的型別名，不是型別註記
	// 而: a bigint       -> 這裡 bigint 是型別註記
	// 例外: a ?i64 = 42 — currentToken 為 i64，peekToken 為 =；typeToken 已設為 currentToken
	//      僅在 option（?type）解析路徑下允許此繞過，避免 `n = value` 把變數名 n 誤當型別
	if typeToken.Type == lexer.IDENT && stmt.Type == nil && (p.peekToken.Type != lexer.ASSIGN || (letIsOption && typeToken == p.currentToken)) {
		typeName := typeToken.Literal
		if letIsOption {
			typeName = "?" + typeName
		}
		stmt.Type = buildType(typeName, typeToken)
		if typeToken == p.peekToken {
			p.nextToken()
		}
	}

	// 解析赋值运算符
	if p.currentToken.Type == lexer.ASSIGN {
		// 陣列/切片註記後 current 已是 =，common push 會推進到值
	} else if p.currentToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.NEWLINE ||
		p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
		// 只有型別宣告，無賦值，直接返回
		if p.currentToken.Type == lexer.IDENT {
			p.nextToken()
		}
		if stmt.Type != nil {
			if p.varDeclTypes == nil {
				p.varDeclTypes = make(map[string]string)
			}
			p.varDeclTypes[stmt.Name.Value] = typeString(stmt.Type)
		}
		return stmt
	} else if p.peekToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected assignment operator, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.peekToken.Type.String())
		p.saveError(msg)
		return nil
	} else {
		p.nextToken() // 跳过 ASSIGN 令牌
	}

	p.nextToken()

	p.ctx.push(CTX_EXPR)
	if mt, isMap := stmt.Type.(*MapType); isMap && p.currentToken.Type == lexer.LBRACE {
		stmt.Value = p.parseMapLiteral(mt)
	} else {
		stmt.Value = p.parseExpression(LOWEST)
	}
	p.ctx.pop()

	if stmt.Value == nil {
		if stmt.Type != nil {
			typeStr := typeString(stmt.Type)

			// 使用默认值
			switch typeStr {
			case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "byte":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "0",
				}

			case "f32", "f64":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "0.0",
				}

			case "str":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "",
				}

			case "bool":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "false",
				}

			case "char":
				stmt.Value = &Identifier{
					Token: nameToken,
					Value: "\x00",
				}

			default:

			}

		} else {
			msg := fmt.Sprintf("line %d, column %d: expected expression, got nil instead",
				p.currentToken.Line, p.currentToken.Column)
			p.saveError(msg)
			return nil
		}
	}

	// char 类型：将裸字符 Identifier 或单字符 StringLiteral 转换为 CharLiteral
	if stmt.Type != nil {
		typeStr := typeString(stmt.Type)
		if typeStr == "char" {
			if ident, ok := stmt.Value.(*Identifier); ok && len([]rune(ident.Value)) == 1 {
				stmt.Value = &CharLiteral{
					Token: ident.Token,
					Value: ident.Value,
				}
			} else if str, ok := stmt.Value.(*StringLiteral); ok && len([]rune(str.Value)) == 1 {
				stmt.Value = &CharLiteral{
					Token: str.Token,
					Value: str.Value,
				}
			}
		}
	}

	// 数组上下文：将 [1, 2, 3]（SliceLiteral）转为 ArrayLiteral
	if at, ok := stmt.Type.(*ArrayType); ok {
		if at.Size == nil {
			// [?] — infer size from literal
			if slice, ok := stmt.Value.(*SliceLiteral); ok {
				size := int64(len(slice.Elements))
				stmt.Value = &ArrayLiteral{
					Token:    slice.Token,
					Size:     &IntegerLiteral{Token: slice.Token, Value: size, Raw: "?"},
					Elements: slice.Elements,
				}
			}
		} else if intLit, ok := at.Size.(*IntegerLiteral); ok && intLit.Value > 0 {
			if slice, ok := stmt.Value.(*SliceLiteral); ok {
				stmt.Value = &ArrayLiteral{
					Token:    slice.Token,
					Size:     &IntegerLiteral{Token: slice.Token, Value: intLit.Value, Raw: intLit.Raw},
					Elements: slice.Elements,
				}
			}
		}
	}

	// 記錄變數宣告型別供後續 match 完整性檢查使用
	if stmt.Type != nil {
		if p.varDeclTypes == nil {
			p.varDeclTypes = make(map[string]string)
		}
		p.varDeclTypes[stmt.Name.Value] = typeString(stmt.Type)
	}

	if stmt.Type == nil {
		switch v := stmt.Value.(type) {
		case *IntegerLiteral:
			// 十六進位字面量（0xNN）推斷為 byte，十進位整數推斷為 i64
			inferredType := ValueTypeInt64.String()
			raw := v.Token.Literal
			if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
				inferredType = ValueTypeByte.String()
			}
			stmt.Type = &NamedType{
				Token: nameToken,
				Value: inferredType,
			}

		case *FloatLiteral:
			stmt.Type = &NamedType{
				Token: nameToken,
				Value: ValueTypeFloat64.String(),
			}

		case *StringLiteral:
			stmt.Type = &NamedType{
				Token: nameToken,
				Value: ValueTypeString.String(),
			}

		case *BooleanLiteral:
			stmt.Type = &NamedType{
				Token: nameToken,
				Value: ValueTypeBool.String(),
			}

		case *CharLiteral:
			stmt.Type = &NamedType{
				Token: nameToken,
				Value: ValueTypeChar.String(),
			}
			stmt.Value = &CharLiteral{
				Token: v.Token,
				Value: v.Value,
			}

		case *SliceLiteral:
			// 从元素推断切片类型
			elemValue := "i64"
			if len(v.Elements) > 0 {
				switch v.Elements[0].(type) {
				case *IntegerLiteral:
					elemValue = "i64"
				case *FloatLiteral:
					elemValue = "f64"
				case *StringLiteral:
					elemValue = "str"
				case *BooleanLiteral:
					elemValue = "bool"
				default:
					elemValue = "i64"
				}
			}
			stmt.Type = &SliceType{
				Token: nameToken,
				Elem:  &NamedType{Token: nameToken, Value: elemValue},
			}

		case *SliceExpression:
			// 切片表達式總是走 clone 路徑（generateSliceViewAssignment needClone=true），
			// 不再推斷 SliceType。型別由 varLLVMType 從 RHS 推導（%vec 或 %str-long）。

		case *ArrayLiteral:
		case *StructLiteral:

		case *CallExpression:
			// 從函數/方法調用推斷返回型別（僅首次宣告，不覆蓋已有型別）
			// 例外：option 型別（?type）必須始終更新，因為 match desugar 依賴它
			// 來為 ok arm 生成正確的 it 型別窄化
			if p.declaredVars == nil || !p.declaredVars[stmt.Name.Value] {
				if inferred := p.inferTypeFromCallExpr(v); inferred != "" {
					stmt.Type = buildType(inferred, nameToken)
					if p.varDeclTypes == nil {
						p.varDeclTypes = make(map[string]string)
					}
					p.varDeclTypes[stmt.Name.Value] = inferred
				}
			} else {
				// 已宣告過，但仍需更新 option 型別以支援 match desugar 的型別窄化
				if inferred := p.inferTypeFromCallExpr(v); inferred != "" && strings.HasPrefix(inferred, "?") {
					if p.varDeclTypes == nil {
						p.varDeclTypes = make(map[string]string)
					}
					p.varDeclTypes[stmt.Name.Value] = inferred
				}
			}

		}
	}

	// 記錄已宣告的變數（用於避免重複型別推斷）
	if p.declaredVars == nil {
		p.declaredVars = make(map[string]bool)
	}
	p.declaredVars[stmt.Name.Value] = true

	return stmt
}
func (p *Parser) parseReturnStatement() Statement {
	stmt := &ReturnStatement{Token: p.currentToken}

	// 跳过 RETURN 令牌
	p.nextToken()

	// return 后面不跟返回值，仅用于终止函数
	// 函数通过修改入参（具名结果参数 / out-param）来传递结果
	// 禁止 `return <value>`：RETURN 之后若紧跟表达式起始符（非语句结束符），
	// 说明写了「带返回值的 return」，而 Nolang 不允许返回值，必须报错。
	if !isReturnTerminator(p.currentToken.Type) {
		p.saveError(fmt.Sprintf("line %d, column %d: `return` 後不能跟返回值；Nolang 函數結果通過具名結果參數（out-param）傳出，請用裸 `return` 提前返回並在函數體內給結果參數賦值",
			p.currentToken.Line, p.currentToken.Column))
	}
	stmt.ReturnValue = nil

	return stmt
}

// parseSwitchStatement parses old-style switch:
//
//	switch x { case 1: ... case 2: ... default: ... }
//
// Desugars to match expression: x { 1-> ... 2-> ... -> ... }
func (p *Parser) parseSwitchStatement() Statement {
	p.saveWarning(fmt.Sprintf("line %d, column %d: 'switch/case/default' is deprecated, use 'x: { 1-> ... }' instead",
		p.currentToken.Line, p.currentToken.Column))

	tok := p.currentToken
	p.nextToken() // skip switch

	// Parse matched expression
	cond := p.parseExpression(LOWEST)

	// Expect {
	if p.currentToken.Type != lexer.LBRACE {
		p.saveError(fmt.Sprintf("line %d, column %d: expected '{' after switch expression, got %s",
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
			ma.condition = p.parseExpression(LOWEST)
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

		if len(bodyStmts) == 1 {
			if es, ok := bodyStmts[0].(*ExpressionStatement); ok {
				ma.body = &BlockStatement{
					Token:      tok,
					Statements: bodyStmts,
				}
				_ = es
			} else {
				ma.body = &BlockStatement{
					Token:      tok,
					Statements: bodyStmts,
				}
			}
		} else {
			ma.body = &BlockStatement{
				Token:      tok,
				Statements: bodyStmts,
			}
		}

		arms = append(arms, ma)
	}

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	if len(arms) == 0 {
		p.saveError(fmt.Sprintf("line %d, column %d: empty switch statement", tok.Line, tok.Column))
		return nil
	}

	// Desugar to match expression: cond { arm1 | arm2 | ... }
	result := p.buildMatchDesugar(tok, cond, arms)
	return &ExpressionStatement{Token: tok, Expression: result}
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

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	if len(arms) == 0 {
		p.saveError(fmt.Sprintf("line %d, column %d: empty ~match statement", tok.Line, tok.Column))
		return nil
	}

	// Desugar to match expression: cond { arm1 | arm2 | ... }
	result := p.buildMatchDesugar(tok, cond, arms)
	return &ExpressionStatement{Token: tok, Expression: result}
}

func (p *Parser) parseContinueStatement() Statement {
	stmt := &ContinueStatement{Token: p.currentToken}

	// 跳过 continue 关键字
	p.nextToken()

	// 可选的循环名称（#N 数字標籤或 IDENT 文本標籤）
	if p.currentToken.Type == lexer.LABEL || p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseBreakStatement() Statement {
	stmt := &BreakStatement{Token: p.currentToken}

	// 跳过 break 关键字
	p.nextToken()

	// 可选的循环名称（#N 数字標籤或 IDENT 文本標籤）
	if p.currentToken.Type == lexer.LABEL || p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		p.nextToken()
	}

	return stmt
}

// parseStandaloneBody parses the body of a standalone if-then (cond -> body).
// It handles block bodies ({ stmts }), statement keywords (return, break, etc.),
// let/multi-assign bodies, and single expressions.
func (p *Parser) parseStandaloneBody(tok lexer.Token) *BlockStatement {
	if p.currentToken.Type == lexer.LBRACE {
		bs := p.parseBlockStatement()
		p.nextToken() // skip body's }
		return bs
	}
	if p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.RBRACE ||
		p.currentToken.Type == lexer.EOF || p.currentToken.Type == lexer.SEMICOLON {
		return &BlockStatement{Token: tok}
	}
	if p.currentToken.Type == lexer.RETURN || p.currentToken.Type == lexer.BREAK ||
		p.currentToken.Type == lexer.CONTINUE || p.currentToken.Type == lexer.MUL ||
		p.currentToken.Type == lexer.STAR_STAR ||
		(p.currentToken.Type == lexer.IDENT && (p.peekToken.Type == lexer.ASSIGN || p.peekToken.Type == lexer.COMMA)) {
		bodyStmt := p.parseStatement()
		if bodyStmt != nil {
			return &BlockStatement{Token: tok, Statements: []Statement{bodyStmt}}
		}
		return &BlockStatement{Token: tok}
	}
	body := p.parseExpression(LOWEST)
	return &BlockStatement{
		Token:      tok,
		Statements: []Statement{&ExpressionStatement{Token: tok, Expression: body}},
	}
}

// wrapStandaloneChain handles chained -> in standalone if-then.
// When the body is a single expression and -> follows on the same line,
// the expression becomes the condition of a nested standalone if-then.
// e.g., a -> b -> c -> d is parsed as if(a){if(b){if(c){d}}}
func (p *Parser) wrapStandaloneChain(tok lexer.Token, body *BlockStatement) *BlockStatement {
	for len(body.Statements) == 1 {
		es, ok := body.Statements[0].(*ExpressionStatement)
		if !ok {
			break
		}
		if p.currentToken.Type != lexer.RARROW || p.prevToken.Type == lexer.NEWLINE {
			break
		}

		innerCond := es.Expression
		p.nextToken() // skip ->
		nextBody := p.parseStandaloneBody(tok)
		body = &BlockStatement{
			Token: tok,
			Statements: []Statement{
				&ExpressionStatement{
					Token: tok,
					Expression: &IfExpression{
						Token:        tok,
						Condition:    innerCond,
						Consequence:  nextBody,
						IsStandalone: true,
					},
				},
			},
		}
	}
	return body
}

func (p *Parser) parseExpressionStatement() Statement {
	tok := p.currentToken

	// Standalone wildcard if-then: -> body (without preceding condition)
	// Must check before parseExpression, which would consume -> via its default case.
	if tok.Type == lexer.RARROW && !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
		p.nextToken() // skip ->

		conseq := p.parseStandaloneBody(tok)
		conseq = p.wrapStandaloneChain(tok, conseq)

		var altBody *BlockStatement
		if p.currentToken.Type == lexer.RARROW && p.prevToken.Type != lexer.NEWLINE {
			p.nextToken() // skip ->
			altBody = p.parseStandaloneBody(tok)
			altBody = p.wrapStandaloneChain(tok, altBody)
		}

		return &ExpressionStatement{
			Token: tok,
			Expression: &IfExpression{
				Token:        tok,
				Condition:    &IntegerLiteral{Token: tok, Value: 1}, // wildcard marker
				Consequence:  conseq,
				Alternative:  altBody,
				IsStandalone: true,
			},
		}
	}

	firstExpr := p.parseExpression(LOWEST)
	stmt := &ExpressionStatement{
		Token:      tok,
		Expression: firstExpr,
	}

	// Multi-assign with expression target: expr, ident = call()
	// e.g., fields[n], pos = parse-field(s, pos)
	if p.currentToken.Type == lexer.COMMA {
		targets := []Expression{firstExpr}
		for p.currentToken.Type == lexer.COMMA {
			p.nextToken() // skip COMMA
			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected variable name after ',', got %s",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			targets = append(targets, &Identifier{Token: p.currentToken, Value: p.currentToken.Literal})
			p.nextToken() // skip IDENT
		}
		if p.currentToken.Type != lexer.ASSIGN {
			msg := fmt.Sprintf("line %d, column %d: expected '=' after multi-variable list",
				p.currentToken.Line, p.currentToken.Column)
			p.saveError(msg)
			return nil
		}
		assignTok := p.currentToken
		p.nextToken() // skip =
		value := p.parseExpression(LOWEST)
		if !p.ctx.contains(CTX_MATCH_ARM) {
			p.skipToStatementEnd()
		}
		return &MultiAssignStatement{
			Token:   assignTok,
			Targets: targets,
			Value:   value,
		}
	}

	// cond: {} → 只解析為 struct literal 或 match expression，不再作為 for-loop
	// 條件循環必須使用 for 關鍵字；新式寫法僅保留 ! {}（無限循環）和 {} * n（次數循環）
	state := p.saveState()
	if p.currentToken.Type == lexer.COLON && p.peekToken.Type == lexer.LBRACE &&
		!p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) {
		p.nextToken() // skip :

		bt := p.classifyBlockAtCurrent()

		// Try struct literal first; if it fails, restore state and try match.
		if bt == blockStruct {
			structState := p.saveState()
			if result := p.parseStructLiteral(stmt.Expression); result != nil {
				return &ExpressionStatement{Token: tok, Expression: result}
			}
			p.restoreState(structState)
		}

		// Try match expression (always, since cond: {} is only match)
		matchState := p.saveState()
		if me := p.parseMatchExprFrom(stmt.Expression); me != nil {
			return &ExpressionStatement{Token: tok, Expression: me}
		}
		p.restoreState(matchState)

		// struct/match 都失敗，嘗試解析為 while-loop: cond: { body }
		// 或 if-else: cond: { body } else: { else_body }（舊式語法向後相容）
		// 此時 currentToken 應為 LBRACE（matchState 保存時的位置）
		if p.currentToken.Type == lexer.LBRACE {
			body := p.parseBlockStatement()
			p.nextToken() // skip body's }
			// 檢查 else: { } 分支（跳過可能的 NEWLINE）
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.ELSE {
				p.nextToken() // skip else
				if p.currentToken.Type == lexer.COLON {
					p.nextToken() // skip :
				}
				if p.currentToken.Type == lexer.LBRACE {
					elseBody := p.parseBlockStatement()
					p.nextToken() // skip else body's }
					return &ExpressionStatement{
						Token: tok,
						Expression: &IfExpression{
							Token:       tok,
							Condition:   stmt.Expression,
							Consequence: body,
							Alternative: elseBody,
						},
					}
				}
			}
			// while-loop: cond: { body }
			forStmt := &ForStatement{Token: tok}
			forStmt.Condition = stmt.Expression
			forStmt.Body = body
			return forStmt
		}

		// If all attempts failed, restore and fall through
		p.restoreState(state)
	}

	// Standalone if-then: cond -> body (without enclosing { })
	if p.currentToken.Type == lexer.RARROW && !p.ctx.contains(CTX_MATCH_ARM) && !p.ctx.contains(CTX_FOR_COND) {
		p.nextToken() // skip ->

		conseq := p.parseStandaloneBody(tok)
		conseq = p.wrapStandaloneChain(tok, conseq)

		// Check for else: -> elseBody (only if -> immediately follows body, not on a new line)
		var altBody *BlockStatement
		if p.currentToken.Type == lexer.RARROW && p.prevToken.Type != lexer.NEWLINE {
			p.nextToken() // skip ->
			altBody = p.parseStandaloneBody(tok)
			altBody = p.wrapStandaloneChain(tok, altBody)
		}

		return &ExpressionStatement{
			Token: tok,
			Expression: &IfExpression{
				Token:        tok,
				Condition:    stmt.Expression,
				Consequence:  conseq,
				Alternative:  altBody,
				IsStandalone: true,
			},
		}
	}

	return stmt
}

// 优先级常量
const (
	LOWEST      = iota
	COMMA       // ,
	CONDITIONAL // ?:
	LOGICAL_OR  // ||
	LOGICAL_AND // &&
	EQUALS      // ==
	LESSGREATER // >, <, <=, >=
	SUM         // +, -
	PRODUCT     // *, /, %
	PREFIX      // !, -
	CALL        // function call
)

var precedences = map[lexer.TokenType]int{
	lexer.COMMA:          COMMA,
	lexer.QUESTION:       CONDITIONAL,
	lexer.AS:             CONDITIONAL, // `as` cast: lower than arithmetic, higher than assignment
	lexer.LOR:            LOGICAL_OR,
	lexer.LAND:           LOGICAL_AND,
	lexer.EQUALS:         EQUALS,
	lexer.NOT_EQUALS:     EQUALS,
	lexer.LESS:           LESSGREATER,
	lexer.LESS_EQUALS:    LESSGREATER,
	lexer.GREATER:        LESSGREATER,
	lexer.GREATER_EQUALS: LESSGREATER,
	lexer.SHL:            PRODUCT,
	lexer.SHR:            PRODUCT,
	lexer.AND:            PRODUCT,
	lexer.OR:             SUM,
	lexer.XOR:            SUM,
	lexer.ADD:            SUM,
	lexer.SUB:            SUM,
	lexer.MUL:            PRODUCT,
	lexer.QUO:            PRODUCT,
	lexer.MOD:            PRODUCT,
	lexer.LPAREN:         CALL,
}

func (p *Parser) peekPrecedence() int {
	if p.ctx.contains(CTX_MATCH_ARM) && p.peekToken.Type == lexer.RARROW {
		return LOWEST
	}
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) currentPrecedence() int {
	if p.ctx.contains(CTX_MATCH_ARM) && p.currentToken.Type == lexer.RARROW {
		return LOWEST
	}
	if p, ok := precedences[p.currentToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) parseExpression(precedence int) Expression {
	// Skip newlines within expressions (multi-line array/slice literals, etc.)
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	var leftExp Expression

	switch p.currentToken.Type {
	case lexer.IDENT, lexer.IN:
		leftExp = p.parseIdentifier()
		// 處理後綴 ++ / --
		isIncDec := false
		if p.currentToken.Type == lexer.INC {
			leftExp = &InfixExpression{
				Token:    p.currentToken,
				Left:     leftExp,
				Operator: "++",
				Right:    nil,
			}
			p.nextToken()
			isIncDec = true
		} else if p.currentToken.Type == lexer.DEC {
			leftExp = &InfixExpression{
				Token:    p.currentToken,
				Left:     leftExp,
				Operator: "--",
				Right:    nil,
			}
			p.nextToken()
			isIncDec = true
		}
		// expr: { ... } or expr { ... } → match or struct literal
		hasColonBeforeBrace := p.currentToken.Type == lexer.COLON && p.peekToken.Type == lexer.LBRACE
		// Only consume `:` from `: {` at the top expression level (precedence == LOWEST)
		// to avoid stealing `:` from bare condition for-loops like `i < n: { body }`
		// where the `: {` follows the right operand of an infix expression.
		// Save state before consuming `:` so we can restore it if the block
		// turns out to be a bare condition for-loop (e.g. `n: { body }`).
		var preColonState parserState
		colonConsumed := false
		if hasColonBeforeBrace && precedence == LOWEST && !p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) && !isIncDec {
			preColonState = p.saveState()
			p.nextToken() // skip :
			colonConsumed = true
		}
		if p.currentToken.Type == lexer.LBRACE && !p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) && !isIncDec {
			if p.classifyBlockAtCurrent() == blockMatch && !hasColonBeforeBrace {
				p.saveWarning(fmt.Sprintf("line %d, column %d: 'x { ... }' is deprecated, use 'x: { ... }' instead",
					p.currentToken.Line, p.currentToken.Column))
			}
			blockHandled := false
			if p.classifyBlockAtCurrent() == blockStruct {
				result := p.parseStructLiteral(leftExp)
				if result != nil {
					leftExp = result
					blockHandled = true
				} else {
					state := p.saveState()
					me := p.parseMatchExprFrom(leftExp)
					if me != nil {
						leftExp = me
						blockHandled = true
					} else {
						p.restoreState(state)
					}
				}
			} else {
				state := p.saveState()
				me := p.parseMatchExprFrom(leftExp)
				if me != nil {
					leftExp = me
					blockHandled = true
				} else {
					p.restoreState(state)
				}
			}
			// If the block was not a struct or match, restore `:` so that
			// parseExpressionStatement can handle `cond: { body }` as a for-loop.
			if !blockHandled && colonConsumed {
				p.restoreState(preColonState)
			}
		}

	case lexer.INT:
		leftExp = p.parseIntegerLiteral()

	case lexer.FLOAT:
		leftExp = p.parseFloatLiteral()

	case lexer.BYTE:
		leftExp = p.parseByteLiteral()

	case lexer.STRING:
		leftExp = p.parseStringLiteral()

	case lexer.CHAR:
		leftExp = &CharLiteral{
			Token: p.currentToken,
			Value: p.currentToken.Literal,
		}
		p.nextToken()

	case lexer.REGEX:
		leftExp = &RegexLiteral{
			Token:   p.currentToken,
			Pattern: p.currentToken.Literal,
			Flags:   p.currentToken.Raw,
		}
		p.nextToken()

	case lexer.TRUE:
		expr := &BooleanLiteral{
			Token: p.currentToken,
			Value: true,
		}
		p.nextToken()
		leftExp = expr

	case lexer.FALSE:
		expr := &BooleanLiteral{
			Token: p.currentToken,
			Value: false,
		}
		p.nextToken()
		leftExp = expr

	case lexer.NIL:
		leftExp = p.parseNilLiteral()

	case lexer.ELLIPSIS:
		// ..identifier represents super.identifier
		if p.peekToken.Type == lexer.IDENT {
			superTok := p.currentToken
			p.nextToken() // skip .. → now at IDENT
			leftExp = &DotExpression{
				Token:    p.currentToken,
				Receiver: &Identifier{Token: superTok, Value: "super"},
				Property: p.currentToken.Literal,
			}
			p.nextToken()
		} else {
			p.nextToken()
			return nil
		}

	case lexer.DOT:
		// . alone = self, .property = self.property
		tok := p.currentToken
		p.nextToken() // consume DOT → now at token after .
		if p.currentToken.Type == lexer.IDENT {
			// .property → DotExpression(self.property)
			leftExp = &DotExpression{
				Token:    p.currentToken,
				Receiver: &Identifier{Token: tok, Value: "self"},
				Property: p.currentToken.Literal,
			}
			p.nextToken()
		} else {
			// . alone → self
			leftExp = &Identifier{Token: tok, Value: "self"}
		}

	case lexer.AS:
		// `as` is now handled as a postfix cast operator after the prefix
		// expression is parsed (see AS handling after the infix loop below).
		// If we reach here, `as` appeared where an expression was expected,
		// which is a parse error. Skip it and return nil.
		msg := fmt.Sprintf("line %d, column %d: unexpected 'as' at start of expression",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		p.nextToken()
		return nil

	case lexer.PTR:
		leftExp = p.parsePointerType()

	case lexer.QUESTION:
		// ? = nil
		leftExp = &NilLiteral{Token: p.currentToken}
		p.nextToken()

	case lexer.SUB:
		leftExp = p.parsePrefixExpression()

	case lexer.TILDE:
		// ~expr = bitwise NOT (prefix operator)
		// Note: at statement level, ~match is handled separately in parseStatement.
		// Here we are in expression context, so ~ is always bitwise NOT.
		leftExp = p.parsePrefixExpression()

	case lexer.NOT:
		// ! = false (standalone), !expr = prefix NOT
		switch p.peekToken.Type {
		case lexer.NEWLINE, lexer.SEMICOLON, lexer.EOF, lexer.RPAREN, lexer.RBRACE, lexer.RBRACKET:
			leftExp = &BooleanLiteral{Token: p.currentToken, Value: false}
			p.nextToken()
		default:
			leftExp = p.parsePrefixExpression()
		}

	case lexer.BANG_BANG:
		// !! → true (standalone boolean)
		leftExp = &BooleanLiteral{Token: p.currentToken, Value: true}
		p.nextToken()

	case lexer.LPAREN:
		// Detect anonymous function: (a i64, b i64) { ... }
		if p.isFunctionLiteral() {
			leftExp = p.parseFunctionLiteral()
		} else {
			leftExp = p.parseGroupedExpression()
		}

	case lexer.IF:
		leftExp = p.parseIfExpression()

	case lexer.RUN:
		tok := p.currentToken
		p.nextToken() // consume 'run'
		// Parse the following expression — should be a CallExpression
		callExpr := p.parseExpression(LOWEST)
		leftExp = &RunExpression{
			Token: tok,
			Call:  callExpr,
		}

	case lexer.AWY:
		tok := p.currentToken
		p.nextToken() // consume 'awy'
		rightExpr := p.parseExpression(LOWEST)
		leftExp = &AwaitExpression{
			Token: tok,
			Right: rightExpr,
		}

	// case lexer.FUNC:
	// 	// 打印调试信息
	// 	leftExp = p.parseFunctionLiteral()
	// 	// 打印调试信息
	// 	// 函数字面量解析完成后，currentToken 已经指向了函数体后面的令牌
	// 	// 不需要再调用 p.nextToken()
	case lexer.LBRACKET:
		// 切片语法：[1, 2, 3]
		leftExp = p.parseSliceLiteral()

	case lexer.LBRACE:
		if p.classifyBlockAtCurrent() == blockStruct {
			// 匿名結構體字面量：{ field: value, ... }
			result := p.parseStructLiteral(nil)
			if result != nil {
				leftExp = result
			} else {
				p.nextToken()
				return nil
			}
		} else if p.classifyBlockAtCurrent() == blockMatch {
			leftExp = p.parseBareMatchExpr()
		} else {
			p.nextToken()
			return nil
		}

	default:
		p.nextToken()
		return nil
	}

	// 处理点操作符、函数调用、切片和结构体字面量
	for p.currentToken.Type == lexer.DOT || p.currentToken.Type == lexer.LPAREN || p.currentToken.Type == lexer.LBRACKET || p.currentToken.Type == lexer.LBRACE {
		if p.currentToken.Type == lexer.DOT {
			p.nextToken()
			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected identifier after dot, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			dotExpr := &DotExpression{
				Token:    p.currentToken,
				Receiver: leftExp,
				Property: p.currentToken.Literal,
			}
			p.nextToken()
			leftExp = dotExpr
		} else if p.currentToken.Type == lexer.LESS {
			// 泛型引數：arr_to_vec<N>(...)
			callExpr := &CallExpression{
				Token:       p.currentToken,
				Function:    leftExp,
				GenericArgs: []Expression{},
				Arguments:   []Expression{},
			}
			p.nextToken()
			for {
				arg := p.parseArgument()
				if arg != nil {
					callExpr.GenericArgs = append(callExpr.GenericArgs, arg)
				}
				if p.currentToken.Type == lexer.GREATER {
					p.nextToken()
					break
				}
				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected ',' or '>' in generic args, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}
				p.nextToken()
			}
			if p.currentToken.Type != lexer.LPAREN {
				msg := fmt.Sprintf("line %d, column %d: expected '(' after generic args, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			p.nextToken()
			// 解析實參
			if p.currentToken.Type != lexer.RPAREN {
				for {
					arg := p.parseArgument()
					if arg != nil {
						callExpr.Arguments = append(callExpr.Arguments, arg)
					}
					if p.currentToken.Type == lexer.COMMA {
						p.nextToken()
					} else if p.currentToken.Type == lexer.RPAREN {
						p.nextToken()
						break
					} else {
						msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
							p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
						p.saveError(msg)
						return nil
					}
				}
			}
			leftExp = callExpr
		} else if p.currentToken.Type == lexer.LPAREN {
			// 检查是否为切片范围语法 nums(1..3)
			state := p.saveState()
			p.nextToken() // skip (
			isRange := p.currentToken.Type == lexer.ELLIPSIS ||
				(p.currentToken.Type == lexer.INT && p.peekToken.Type == lexer.ELLIPSIS) ||
				(p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ELLIPSIS)
			p.restoreState(state)
			if isRange {
				leftExp = p.parseSliceExpression(leftExp)
			} else {
				leftExp = p.parseCallExpression(leftExp)
			}
		} else if p.currentToken.Type == lexer.LBRACKET {
			// Detect array literal: 5[1, 2, 3, 4, 5] vs index/slice: nums[1..3]
			if _, isInt := leftExp.(*IntegerLiteral); isInt {
				state := p.saveState()
				p.nextToken() // skip [
				hasComma := false
				depth := 1
				for depth > 0 && p.currentToken.Type != lexer.EOF {
					if p.currentToken.Type == lexer.COMMA {
						hasComma = true
						break
					}
					if p.currentToken.Type == lexer.RBRACKET {
						depth--
					}
					if p.currentToken.Type == lexer.LBRACKET {
						depth++
					}
					p.nextToken()
				}
				p.restoreState(state)
				if hasComma {
					leftExp = p.parseArrayLiteral(leftExp)
					continue
				}
			}
			leftExp = p.parseSliceExpression(leftExp)
		} else if p.currentToken.Type == lexer.LBRACE {
			// Struct literal: user { name: 'abc' age: 20 }
			// 在 for 條件上下文中，{ 是循環體，不應解析為結構體字面量
			if !p.ctx.contains(CTX_FOR_COND) && !p.ctx.contains(CTX_MATCH_COND) && p.classifyBlockAtCurrent() == blockStruct {
				result := p.parseStructLiteral(leftExp)
				if result != nil {
					leftExp = result
				} else {
					break
				}
			} else {
				break
			}
		}
	}

	// 处理中缀运算符与 `as` 类型转换（不包括三元表达式）
	// `as` 的优先级低于算术（PRODUCT/SUM/...），高于赋值；
	// 循环允许 (expr as Type) op expr 形式，例如 (r[4]*5) as *byte 后跟 +
	// （`as` 僅允許用於 FFI 指標型別轉換，非指標型別會報錯）
	for p.currentToken.Type != lexer.EOF &&
		!(p.ctx.contains(CTX_MATCH_ARM) && p.currentToken.Type == lexer.RARROW) &&
		p.currentPrecedence() > precedence {

		// `as` 类型转换：expr as Type
		// 限制：`as` 僅允許用於 FFI 指標型別轉換（如 *byte、**byte、*i64），
		// 不允許整數型別轉換（如 u32、i64）— 整數內部皆為 i64，無需顯式轉換。
		if p.currentToken.Type == lexer.AS {
			asTok := p.currentToken
			p.nextToken() // skip as
			// 解析目標型別 — 使用 parseExternType 以支援 *T / **T 等 FFI 指標型別語法
			typ, ok := p.parseExternType()
			if !ok || typ == nil {
				msg := fmt.Sprintf("line %d, column %d: expected type after 'as', got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			// 檢查目標型別是否為指標型別（PointerType）
			if _, isPtr := typ.(*PointerType); !isPtr {
				msg := fmt.Sprintf("line %d, column %d: 'as' cast is only allowed for FFI pointer types (e.g., *byte, **byte), got %s",
					asTok.Line, asTok.Column, typ.String())
				p.saveError(msg)
				// 仍建立 CastExpression 以維持後續解析流程，但已記錄錯誤
			}
			leftExp = &CastExpression{
				Token: asTok,
				Expr:  leftExp,
				Type:  typ,
			}
			continue
		}

		// 一般中缀运算符
		t := p.currentToken.Type
		if !(t == lexer.LAND || t == lexer.LOR || t == lexer.ADD || t == lexer.SUB ||
			t == lexer.MUL || t == lexer.QUO || t == lexer.MOD ||
			t == lexer.EQUALS || t == lexer.NOT_EQUALS ||
			t == lexer.LESS || t == lexer.LESS_EQUALS ||
			t == lexer.GREATER || t == lexer.GREATER_EQUALS ||
			t == lexer.AND || t == lexer.OR || t == lexer.XOR ||
			t == lexer.SHL || t == lexer.SHR) {
			break
		}
		leftExp = p.parseInfixExpression(leftExp)
	}

	// 处理三元表达式（最低优先级）
	// 只有当当前 precedence 允许时才处理 `?`，否则 `?` 属于外层表达式
	// （如 `a > 5 ? b : c` 中 `?` 不应绑定到 `5`，而应绑定到 `a > 5`）
	if p.currentToken.Type == lexer.QUESTION && precedence <= CONDITIONAL {
		return p.parseConditionalExpression(leftExp)
	}

	// if p.currentToken.Type == lexer.INC {
	// 	fmt.Println("INC", p.currentToken)
	// 	leftExp = p.parseConditionalExpression(leftExp)
	// }

	// 處理賦值: u.name = value 或 a[i] = value
	if p.currentToken.Type == lexer.ASSIGN {
		if _, ok := leftExp.(*DotExpression); ok {
			tok := p.currentToken
			p.nextToken()
			val := p.parseExpression(LOWEST)
			leftExp = &AssignExpression{
				Token: tok,
				Left:  leftExp,
				Value: val,
			}
		} else if _, ok := leftExp.(*IndexExpression); ok {
			tok := p.currentToken
			p.nextToken()
			val := p.parseExpression(LOWEST)
			leftExp = &AssignExpression{
				Token: tok,
				Left:  leftExp,
				Value: val,
			}
		}
	}

	return leftExp
}

func (p *Parser) saveError(msg string) {
	p.errors = append(p.errors, msg)
}

func (p *Parser) saveWarning(msg string) {
	p.warnings = append(p.warnings, msg)
}

func (p *Parser) Warnings() []string {
	return p.warnings
}

// skipToStatementEnd advances tokens until a statement boundary is reached.
func (p *Parser) skipToStatementEnd() {
	for p.currentToken.Type != lexer.EOF && !isStatementBoundary(p.currentToken.Type) {
		p.nextToken()
	}
}

// isReturnTerminator reports whether t is a valid token to immediately follow a
// bare `return` statement (end-of-line, end-of-file, closing brace, or semicolon).
// Anything else (e.g. INT, IDENT, LPAREN) means the source wrote
// `return <value>`, which Nolang forbids — results must be communicated through
// named result parameters (out-params), not a value return.
func isReturnTerminator(t lexer.TokenType) bool {
	return t == lexer.NEWLINE || t == lexer.EOF || t == lexer.RBRACE || t == lexer.SEMICOLON
}

// isStatementBoundary returns true if the token type marks the start of a new statement.
func isStatementBoundary(t lexer.TokenType) bool {
	switch t {
	case lexer.IF, lexer.IDENT, lexer.RBRACE, lexer.FOR,
		lexer.RETURN, lexer.BREAK, lexer.CONTINUE,
		lexer.LPAREN, lexer.LBRACE, lexer.SEMICOLON,
		lexer.DOT, lexer.NOT, lexer.INT, lexer.STRING,
		lexer.TRUE, lexer.FALSE, lexer.NIL, lexer.USE, lexer.AT,
		lexer.SWITCH, lexer.TILDE, lexer.FLOAT, lexer.BYTE,
		lexer.LBRACKET, lexer.FFI, lexer.HASH_LBRACE,
		lexer.REGEX,
		// Shorthand forms and loop labels that can begin a statement
		// (without these, `skipToStatementEnd` swallows them after a
		// preceding `break`/`continue`/`return`).
		lexer.MUL, lexer.STAR_STAR, lexer.BANG_BANG, lexer.LABEL,
		// -> can begin a standalone wildcard if-then (-> body)
		lexer.RARROW:
		return true
	}
	return false
}

func isForCompOp(t lexer.TokenType) bool {
	return t == lexer.LESS || t == lexer.GREATER ||
		t == lexer.LESS_EQUALS || t == lexer.GREATER_EQUALS ||
		t == lexer.EQUALS || t == lexer.NOT_EQUALS
}

func (p *Parser) parseIdentifier() Expression {
	expr := &Identifier{
		Token: p.currentToken,
		Value: p.currentToken.Literal,
	}
	p.nextToken() // 前进令牌
	return expr
}

// forward
func (p *Parser) parseIntegerLiteral() Expression {
	lit := &IntegerLiteral{Token: p.currentToken}

	// Auto-detect base: 0xNNNN = hex, otherwise base 10
	base := 10
	raw := p.currentToken.Literal
	if len(raw) > 2 && raw[0] == '0' && (raw[1] == 'x' || raw[1] == 'X') {
		base = 0 // auto-detect from 0x prefix
	}
	value, err := strconv.ParseInt(raw, base, 64)
	if err != nil {
		// 嘗試以 uint64 解析（用於 u64 字面量，如 18446744073709551615）
		if uval, uerr := strconv.ParseUint(raw, base, 64); uerr == nil {
			value = int64(uval)
		} else {
			msg := fmt.Sprintf("line %d, column %d: could not parse %q as integer",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Literal)
			p.saveError(msg)
			p.nextToken()
			return nil
		}
	}

	lit.Value = value
	p.nextToken()
	return lit
}

// forward
func (p *Parser) parseByteLiteral() Expression {
	lit := &ByteLiteral{Token: p.currentToken}
	// xNN → 整數值
	val := int64(0)
	for _, c := range p.currentToken.Literal[1:] {
		if c >= '0' && c <= '9' {
			val = val*16 + int64(c-'0')
		} else if c >= 'a' && c <= 'f' {
			val = val*16 + int64(c-'a'+10)
		} else if c >= 'A' && c <= 'F' {
			val = val*16 + int64(c-'A'+10)
		}
	}
	lit.Value = val
	p.nextToken()
	return lit
}

func (p *Parser) parseFloatLiteral() Expression {
	lit := &FloatLiteral{Token: p.currentToken}
	lit.Raw = p.currentToken.Literal

	value, err := strconv.ParseFloat(p.currentToken.Literal, 64)
	if err != nil {
		msg := fmt.Sprintf("line %d, column %d: could not parse %q as float",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Literal)
		p.saveError(msg)
		p.nextToken()
		return nil
	}

	lit.Value = value
	p.nextToken()
	return lit
}

func (p *Parser) parseStringLiteral() Expression {
	expr := &StringLiteral{
		Token: p.currentToken,
		Value: p.currentToken.Literal,
	}
	p.nextToken() // 前进令牌
	return expr
}

func (p *Parser) parseNilLiteral() Expression {
	expr := &NilLiteral{Token: p.currentToken}
	p.nextToken()
	return expr
}

func (p *Parser) parsePrefixExpression() Expression {
	expr := &PrefixExpression{
		Token:    p.currentToken,
		Operator: p.currentToken.Literal,
	}

	p.nextToken()
	expr.Right = p.parseExpression(PREFIX)

	return expr
}

// 中缀表达式
func (p *Parser) parseInfixExpression(left Expression) Expression {
	expr := &InfixExpression{
		Token:    p.currentToken,
		Left:     left,
		Operator: p.currentToken.Literal,
	}
	precedence := p.currentPrecedence()
	p.nextToken()
	// Skip newlines before the right operand (multi-line expressions like 'str' +
	// 'continuation')
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	expr.Right = p.parseExpression(precedence)

	// 处理三元表达式（最低优先级）
	if p.currentToken.Type == lexer.QUESTION {
		return p.parseConditionalExpression(expr)
	}

	return expr
}

// 三元运算符
func (p *Parser) parseConditionalExpression(condition Expression) Expression {
	expr := &ConditionalExpression{
		Token:     p.currentToken,
		Condition: condition,
	}

	p.nextToken() // 跳过 QUESTION

	expr.Consequence = p.parseExpression(CONDITIONAL)

	if p.currentToken.Type != lexer.COLON {
		msg := fmt.Sprintf("line %d, column %d: expected colon in conditional expression, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	p.nextToken() // 跳过 COLON

	// 解析 alternative（假值表达式）
	expr.Alternative = p.parseExpression(CONDITIONAL)

	return expr
}

func (p *Parser) parseGroupedExpression() Expression {
	tok := p.currentToken
	p.nextToken()
	expr := p.parseExpression(LOWEST)

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected right parenthesis, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	p.nextToken() // 跳过右括号
	return &GroupedExpression{
		Token:      tok,
		Expression: expr,
	}
}

// parseMatchExpression parses `match <expr> { arm* }` and desugars to if/elif/else chain.
// parseMatchExprFrom 從既有表達式開始解析 match（不用 match 關鍵字）
// 用於 expr { pattern: body } 形式
func (p *Parser) parseMatchExprFrom(matched Expression) Expression {
	tok := p.currentToken // LBRACE
	p.nextToken()         // skip {

	var arms []matchArm

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// Skip newlines and semicolons between arms
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.SEMICOLON {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE || p.currentToken.Type == lexer.EOF {
			break
		}

		var ma matchArm
		ma.pos = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
		if p.currentToken.Type == lexer.COLON {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.UNDERSCORE {
			ma.isWildcard = true
			p.nextToken()
		} else if p.currentToken.Type == lexer.RARROW {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.DOT && p.peekToken.Type == lexer.RARROW {
			// .-> → val branch (specific, not catch-all)
			ma.isWildcard = true
			ma.isDotVal = true
			p.nextToken() // consume DOT
		} else if p.currentToken.Type == lexer.IDENT && p.currentToken.Literal == "ok" && p.peekToken.Type == lexer.LPAREN {
			// ok(cond) -> body → conditional val arm
			// Desugars to: matched == ok && cond (built in buildMatchDesugar)
			p.nextToken() // skip ok
			p.nextToken() // skip (
			p.ctx.push(CTX_MATCH_ARM)
			okCond := p.parseExpression(LOWEST)
			p.ctx.pop()
			if p.currentToken.Type != lexer.RPAREN {
				return nil
			}
			p.nextToken() // skip )
			ma.isRawCond = true
			ma.condition = okCond
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
		} else if p.currentToken.Type == lexer.IDENT && p.currentToken.Literal == "ok" &&
			(p.peekToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.RBRACE) {
			// ok without -> (e.g. "nil -> err -> ok\n body...")
			// Treat as ok val branch with body on subsequent lines
			ma.isWildcard = true
			ma.isDotVal = true
			p.nextToken()
		} else if (p.currentToken.Type == lexer.NIL ||
			(p.currentToken.Type == lexer.IDENT &&
				(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok"))) &&
			p.peekToken.Type == lexer.LOR {
			// nil || err -> body → combined option patterns
			// Collect all patterns joined by ||
			// Note: nil is a NIL token, err/ok are IDENT tokens
			firstPat := "nil"
			if p.currentToken.Type == lexer.IDENT {
				firstPat = p.currentToken.Literal
			}
			patterns := []string{firstPat}
			p.nextToken() // skip first pattern
			p.nextToken() // skip ||
			for p.currentToken.Type == lexer.NIL ||
				(p.currentToken.Type == lexer.IDENT &&
					(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok")) {
				if p.currentToken.Type == lexer.NIL {
					patterns = append(patterns, "nil")
				} else {
					patterns = append(patterns, p.currentToken.Literal)
				}
				p.nextToken()
				if p.currentToken.Type == lexer.LOR {
					p.nextToken()
				} else {
					break
				}
			}
			ma.multiOptionPatterns = patterns
		} else if p.currentToken.Type == lexer.NOT && p.peekToken.Type == lexer.RARROW {
			// !-> → err branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "err"}
			p.nextToken()
		} else if p.currentToken.Type == lexer.QUESTION && p.peekToken.Type == lexer.RARROW {
			// ?-> → nil branch
			ma.condition = &Identifier{Token: p.currentToken, Value: "nil"}
			p.nextToken()
		} else if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.INT ||
			p.currentToken.Type == lexer.FLOAT || p.currentToken.Type == lexer.STRING ||
			p.currentToken.Type == lexer.NIL || p.currentToken.Type == lexer.TRUE || p.currentToken.Type == lexer.FALSE ||
			p.currentToken.Type == lexer.BYTE {
			// 解析 match 條件（僅主要表達式，避免 | 被當作 OR）
			switch p.currentToken.Type {
			case lexer.INT:
				ma.condition = p.parseIntegerLiteral()
			case lexer.FLOAT:
				ma.condition = p.parseFloatLiteral()
			case lexer.BYTE:
				ma.condition = p.parseByteLiteral()
			case lexer.STRING:
				ma.condition = p.parseStringLiteral()
			case lexer.IDENT:
				ma.condition = p.parseIdentifier()
			case lexer.NIL:
				ma.condition = p.parseNilLiteral()
			case lexer.TRUE, lexer.FALSE:
				ma.condition = &BooleanLiteral{Token: p.currentToken, Value: p.currentToken.Type == lexer.TRUE}
				p.nextToken()
			}
		} else {
			p.ctx.push(CTX_MATCH_ARM)
			ma.condition = p.parseExpression(LOWEST)
			p.ctx.pop()
		}

		// 使用 -> 作為分隔符，: 僅向後相容
		if p.currentToken.Type == lexer.RARROW {
			p.nextToken()
		} else if p.currentToken.Type == lexer.COLON {
			p.nextToken()
		} else if !ma.isWildcard {
			return nil
		}

		// Statement or expression body
		var bodyStmts []Statement
		bodyBlock := &BlockStatement{Token: tok}
		var parsedBlock *BlockStatement // tracks block from parseBlockStatement for comment preservation
		if p.currentToken.Type == lexer.LBRACE && p.classifyBlockAtCurrent() != blockMatch {
			// Explicit block form: -> { ... }
			// 多行 arm body 必須使用大括號，以便 option-match desugar 能正確插入 `it` 綁定。
			// 不推送 CTX_MATCH_ARM：block body 內的 `cond -> body` 應視為 standalone if-then，
			// 而非 match arm 分隔符。CTX_MATCH_ARM 僅用於 arm condition 與 inline body。
			ma.isBlockBody = true
			parsedBlock = p.parseBlockStatement()
			if parsedBlock != nil {
				bodyStmts = parsedBlock.Statements
			}
			if p.currentToken.Type == lexer.RBRACE {
				p.nextToken()
			}
		} else if p.currentToken.Type == lexer.NEWLINE {
			// Block form (newline-separated statements, no braces)
			ma.isBlockBody = true
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			// Read statements until next arm or }
			p.ctx.push(CTX_MATCH_ARM)
			for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF &&
				!p.isArmStart() {
				// Skip NEWLINE
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
			p.ctx.pop()
		} else {
			// Inline statement form（單行 body）
			// 使用 parseStatement 以支援 let 賦值（如 `cond -> a = 1`）與表達式（如 `cond -> print(1)`）
			// But if the body is actually another option pattern (e.g. "nil -> err -> ok"),
			// treat the current arm as having an empty body (fallthrough).
			if isOptionPatternStart(p) {
				// Empty body — next token is a new arm pattern
			} else {
				p.ctx.push(CTX_MATCH_ARM)
				doc := p.collectDocComments()
				stmt := p.parseStatement()
				p.ctx.pop()
				if stmt != nil {
					setDoc(stmt, doc)
					p.attachInlineComment(stmt)
					bodyStmts = append(bodyStmts, stmt)
				}
				// Set arm body end position for inline form (current token is just past the body)
				bodyBlock.RBrace = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
			}
		}

		bodyBlock.Statements = bodyStmts
		// Preserve comments (TrailingComments) from the parsed block so that
		// comment-only block bodies (e.g. `c == 46 -> { // 允許 }`) are not lost.
		if parsedBlock != nil {
			bodyBlock.TrailingComments = parsedBlock.TrailingComments
			bodyBlock.ClosingBraceComment = parsedBlock.ClosingBraceComment
			bodyBlock.OpeningBraceComment = parsedBlock.OpeningBraceComment
			bodyBlock.RBrace = parsedBlock.RBrace
		}
		ma.body = bodyBlock
		arms = append(arms, ma)
	}

	if len(arms) == 0 {
		return nil
	}

	// Check option match branch completeness
	hasErrArm, hasNilArm, hasValArm, hasElseArm := false, false, false, false
	for _, a := range arms {
		if len(a.multiOptionPatterns) > 0 {
			// Combined option patterns: nil || err → mark all as covered
			for _, pat := range a.multiOptionPatterns {
				if pat == "err" {
					hasErrArm = true
				} else if pat == "nil" {
					hasNilArm = true
				} else if pat == "ok" {
					hasValArm = true
				}
			}
			continue
		}
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
	if !hasElseArm && (!hasErrArm || !hasNilArm || !hasValArm) {
		if p.matchedIsOption(matched) {
			var missing []string
			if !hasErrArm {
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

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// Build if/elif/else chain
	if p.ctx.contains(CTX_EXPR) {
		if !p.validateMatchArmReturns(tok, arms) {
			return nil
		}
	}
	return p.buildMatchDesugar(tok, matched, arms)
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

		var ma matchArm
		ma.pos = lexer.Position{Line: p.currentToken.Line, Column: p.currentToken.Column}
		if p.currentToken.Type == lexer.COLON {
			ma.isWildcard = true
		} else if p.currentToken.Type == lexer.UNDERSCORE {
			ma.isWildcard = true
			p.nextToken()
		} else if p.currentToken.Type == lexer.RARROW {
			ma.isWildcard = true
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
				armState := p.saveState()
				stmt := p.parseStatement()
				if stmt != nil {
					ma.isBlockBody = true
					bodyStmts = append(bodyStmts, stmt)
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
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			p.ctx.push(CTX_MATCH_ARM)
			for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF &&
				!p.isArmStart() {
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
			p.ctx.pop()
		} else {
			// Inline statement form（單行 body）
			// 使用 parseStatement 以支援 let 賦值（如 `cond -> a = 1`）與表達式（如 `cond -> print(1)`）
			// 對 catch-all arm（wildcard，如 `-> cond -> body`），不推送 CTX_MATCH_ARM，
			// 允許 body 中的 -> 被解析為 standalone if-then，而非 match arm 分隔符。
			if !ma.isWildcard {
				p.ctx.push(CTX_MATCH_ARM)
			}
			doc := p.collectDocComments()
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
			bodyBlock.OpeningBraceComment = parsedBlock.OpeningBraceComment
			bodyBlock.RBrace = parsedBlock.RBrace
		}
		ma.body = bodyBlock
		arms = append(arms, ma)
	}

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
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
	if !hasElseArm && (!hasErrArm || !hasNilArm || !hasValArm) {
		if p.matchedIsOption(nil) {
			var missing []string
			if !hasErrArm {
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

	result := p.buildBareMatchDesugar(tok, arms)
	if result != nil {
		if ifExpr, ok := result.(*IfExpression); ok {
			ifExpr.OpeningBraceComment = openingComments
		}
	}
	return result
}

// buildBareMatchDesugar 建立 if/elif/else 鏈（無 matched expression，條件直接使用）
func (p *Parser) buildBareMatchDesugar(tok lexer.Token, arms []matchArm) Expression {
	if len(arms) == 0 {
		return nil
	}

	var ifExpr *IfExpression
	for i := len(arms) - 1; i >= 0; i-- {
		arm := arms[i]
		if arm.isWildcard {
			if ifExpr == nil {
				ifExpr = &IfExpression{
					Token:       tok,
					Condition:   &IntegerLiteral{Token: tok, Value: 1},
					Consequence: arm.body,
					IsBareMatch: true,
				}
			} else {
				if ifExpr.Alternative == nil {
					ifExpr.Alternative = arm.body
				}
			}
		} else {
			newIf := &IfExpression{
				Token:       tok,
				Condition:   arm.condition,
				Consequence: arm.body,
				Alternative: nil,
				IsBareMatch: true,
			}
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
		tok := p.lexer.LookAhead(skip)
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
}

// returnKind — match arm body 的最後一個表達式回傳值分類
type returnKind int

const (
	returnNever    returnKind = iota // 不會回傳值（最後一行不是表達式，如迴圈）
	returnNil                        // nil 字面量
	returnErr                        // err() 呼叫
	returnConcrete                   // 具體值（i64, str, bool 等）
)

// returnTypeInfo — 回傳值分類資訊
type returnTypeInfo struct {
	kind     returnKind
	typeName string // 僅 returnConcrete 有效
}

// classifyExprReturnKind 分類表達式的回傳值
func (p *Parser) classifyExprReturnKind(expr Expression) returnTypeInfo {
	switch e := expr.(type) {
	case *NilLiteral:
		return returnTypeInfo{kind: returnNil}
	case *Identifier:
		if e.Value == "err" {
			return returnTypeInfo{kind: returnErr}
		}
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	case *IntegerLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "i64"}
	case *FloatLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "f64"}
	case *StringLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "str"}
	case *BooleanLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "bool"}
	case *ByteLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "byte"}
	case *CharLiteral:
		return returnTypeInfo{kind: returnConcrete, typeName: "char"}
	case *CallExpression:
		if ident, ok := e.Function.(*Identifier); ok && ident.Value == "err" {
			return returnTypeInfo{kind: returnErr}
		}
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	case *InfixExpression:
		return p.classifyInfixReturn(e)
	case *IfExpression:
		return p.classifyIfExprReturn(e)
	default:
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	}
}

// classifyInfixReturn 分類中綴表達式的回傳值
func (p *Parser) classifyInfixReturn(expr *InfixExpression) returnTypeInfo {
	switch expr.Operator {
	case "+", "-", "*", "/", "%":
		leftInfo := p.classifyExprReturnKind(expr.Left)
		rightInfo := p.classifyExprReturnKind(expr.Right)
		if leftInfo.kind == returnConcrete && rightInfo.kind == returnConcrete &&
			leftInfo.typeName != "unknown" && rightInfo.typeName != "unknown" {
			if leftInfo.typeName == rightInfo.typeName {
				return leftInfo
			}
			return returnTypeInfo{kind: returnConcrete, typeName: "i64"}
		}
		// String concatenation: str + unknown (e.g. str + method-call) → str
		if expr.Operator == "+" {
			if leftInfo.typeName == "str" || rightInfo.typeName == "str" {
				return returnTypeInfo{kind: returnConcrete, typeName: "str"}
			}
		}
		return returnTypeInfo{kind: returnConcrete, typeName: "i64"}
	case "==", "!=", "<", ">", "<=", ">=":
		return returnTypeInfo{kind: returnConcrete, typeName: "bool"}
	case "&&", "||":
		return returnTypeInfo{kind: returnConcrete, typeName: "bool"}
	default:
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	}
}

// classifyIfExprReturn 分類條件表達式的回傳值
func (p *Parser) classifyIfExprReturn(expr *IfExpression) returnTypeInfo {
	var consInfo, altInfo returnTypeInfo
	if expr.Consequence != nil && len(expr.Consequence.Statements) > 0 {
		last := expr.Consequence.Statements[len(expr.Consequence.Statements)-1]
		if es, ok := last.(*ExpressionStatement); ok {
			consInfo = p.classifyExprReturnKind(es.Expression)
		} else {
			consInfo = returnTypeInfo{kind: returnNever}
		}
	} else {
		consInfo = returnTypeInfo{kind: returnNever}
	}
	if expr.Alternative != nil && len(expr.Alternative.Statements) > 0 {
		last := expr.Alternative.Statements[len(expr.Alternative.Statements)-1]
		if es, ok := last.(*ExpressionStatement); ok {
			altInfo = p.classifyExprReturnKind(es.Expression)
		} else {
			altInfo = returnTypeInfo{kind: returnNever}
		}
	} else {
		altInfo = returnTypeInfo{kind: returnNever}
	}
	if consInfo.kind == returnNever || altInfo.kind == returnNever {
		return returnTypeInfo{kind: returnNever}
	}
	if consInfo.kind == returnNil && altInfo.kind == returnNil {
		return returnTypeInfo{kind: returnNil}
	}
	if consInfo.kind == returnErr && altInfo.kind == returnErr {
		return returnTypeInfo{kind: returnErr}
	}
	if consInfo.kind == returnConcrete && altInfo.kind == returnConcrete {
		if consInfo.typeName == altInfo.typeName {
			return consInfo
		}
		if consInfo.typeName == "unknown" || altInfo.typeName == "unknown" {
			return returnTypeInfo{kind: returnConcrete, typeName: consInfo.typeName}
		}
		return returnTypeInfo{kind: returnConcrete, typeName: "unknown"}
	}
	return returnTypeInfo{kind: returnConcrete, typeName: "option"}
}

// validateMatchArmReturns 驗證賦值語境下 match arm 的回傳值一致性
func (p *Parser) validateMatchArmReturns(tok lexer.Token, arms []matchArm) bool {
	if len(arms) == 0 {
		return true
	}

	var firstConcreteType string

	for _, arm := range arms {
		if len(arm.body.Statements) == 0 {
			msg := fmt.Sprintf("line %d, column %d: match arm has no body, cannot determine return value", tok.Line, tok.Column)
			p.saveError(msg)
			return false
		}
		last := arm.body.Statements[len(arm.body.Statements)-1]
		es, ok := last.(*ExpressionStatement)
		if !ok {
			msg := fmt.Sprintf("line %d, column %d: match arm in expression context must end with an expression", tok.Line, tok.Column)
			p.saveError(msg)
			return false
		}

		info := p.classifyExprReturnKind(es.Expression)
		switch info.kind {
		case returnNever:
			msg := fmt.Sprintf("line %d, column %d: match arm never returns a value", tok.Line, tok.Column)
			p.saveError(msg)
			return false
		case returnConcrete:
			if info.typeName != "unknown" {
				if firstConcreteType == "" {
					firstConcreteType = info.typeName
				} else if firstConcreteType != info.typeName {
					msg := fmt.Sprintf("line %d, column %d: match arm has inconsistent return types: %s vs %s", tok.Line, tok.Column, firstConcreteType, info.typeName)
					p.saveError(msg)
					return false
				}
			}
		}
	}

	return true
}

// buildMatchDesugar 建立 if/elif/else 鏈
//
// 對 option match（含 err/nil arm），直接使用 `matched == err` / `matched == nil`
// 比較，由 transpiler 的 generateInfixI1 識別 %option 變數並生成 tag 比較的 LLVM IR。
// wildcard arm（含 ok/val/->）作為 else 分支。
func (p *Parser) buildMatchDesugar(tok lexer.Token, matched Expression, arms []matchArm) Expression {
	if len(arms) == 0 {
		return nil
	}

	// Determine element type from option type for per-arm `it` type inference.
	// For ?i64, elemType = "i64"
	elemType := ""
	if ident, ok := matched.(*Identifier); ok {
		if t, ok := p.varDeclTypes[ident.Value]; ok {
			if strings.HasPrefix(t, "?") {
				elemType = strings.TrimPrefix(t, "?")
			} else if _, ok := p.enumVariantNames[t]; ok {
				// For enum match, set elemType to trigger per-arm it binding path
				elemType = t
			}
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
	matchedIsEnum := false
	if matchIdent, ok := matched.(*Identifier); ok {
		if t, ok := p.varDeclTypes[matchIdent.Value]; ok {
			_, matchedIsEnum = p.enumVariantNames[t]
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
		if itStmt != nil && !hasRawCond && elemType != "" {
			var armType string
			skipItBinding := false
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
				} else {
					// Compute complement: which variants remain for -> else arm
					if matchedIsEnum {
						// For enum types, compute complement dynamically
						var remaining []string
						if variants, ok := p.enumVariantNames[elemType]; ok {
							for _, v := range variants {
								if !enumListedVariants[v] {
									remaining = append(remaining, v)
								}
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
				}
			} else if _, ok := arm.condition.(*NilLiteral); ok {
				armType = "nil"
			} else if arm.isDotVal {
				// ok-> arm (dotVal wildcard): it should be the unwrapped elemType
				armType = "ok"
			}
			if armType != "" {
				// Use per-arm position so walker/index can distinguish synthetic bindings
				var armTok lexer.Token
				if arm.condition != nil {
					pos := arm.condition.Pos()
					armTok = lexer.Token{Type: lexer.IDENT, Literal: "it", Line: pos.Line, Column: pos.Column}
				} else if len(arm.body.Statements) > 0 {
					pos := arm.body.Statements[0].Pos()
					armTok = lexer.Token{Type: lexer.IDENT, Literal: "it", Line: pos.Line, Column: pos.Column}
				} else {
					armTok = tok
				}
				if armIt := p.buildItBindingForArm(armTok, matched, armType, elemType); armIt != nil {
					// Set the synthetic end position to cover the arm body
					bodyEnd := arm.body.EndPos()
					if bodyEnd.Line == 0 && bodyEnd.Column == 0 && len(arm.body.Statements) > 0 {
						bodyEnd = arm.body.Statements[len(arm.body.Statements)-1].EndPos()
					}
					armIt.SyntheticEnd = bodyEnd
					arm.body = p.prependStmt(arm.body, armIt)
				}
			} else if !skipItBinding {
				arm.body = p.prependStmt(arm.body, itStmt)
			}
		} else if itStmt != nil && !hasRawCond {
			arm.body = p.prependStmt(arm.body, itStmt)
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
						Token:       tok,
						Condition:   cond,
						Consequence: arm.body,
						Alternative: defaultBody,
						IsBareMatch: true,
						MatchedExpr: matched,
					}
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
						IsBareMatch: true,
						MatchedExpr: matched,
					}
					ifExpr = newIf
				} else {
					ifExpr.Alternative = arm.body
				}
			}
		} else {
			// 構造 match 條件
			var cond Expression
			if len(arm.multiOptionPatterns) > 0 {
				// Combined option patterns: (matched == p1) || (matched == p2) || ...
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
			} else if arm.isRawCond {
				// ok(cond) arm: condition is (matched == ok) && cond
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
			} else {
				// matched == condition
				// 對 option 變數，condition 為 err/nil 時由 transpiler 生成 tag 比較
				cond = &InfixExpression{
					Token:    tok,
					Left:     matched,
					Operator: "==",
					Right:    arm.condition,
				}
			}
			newIf := &IfExpression{
				Token:       tok,
				Condition:   cond,
				Consequence: arm.body,
				Alternative: nil,
				IsBareMatch: true,
				MatchedExpr: matched,
			}
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
			// 唯一 arm 是 wildcard：用 if 1 {} 包裝（無法避免）
			ifExpr = &IfExpression{
				Token:       tok,
				Condition:   &IntegerLiteral{Token: tok, Value: 1},
				Consequence: defaultBody,
				IsBareMatch: true,
				MatchedExpr: matched,
			}
			if defaultDotValBody == defaultBody {
				ifExpr.DotValBody = defaultBody
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
			IsBareMatch: true,
			MatchedExpr: matched,
		}
	}

	return ifExpr
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
	_, ok := matched.(*Identifier)
	if !ok {
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

// buildItBindingForArm creates `it = matched` LetStatement with the correct type
// for the specific match arm. For option types (e.g., ?i64):
//
//	err arm -> it: err (variant type)
//	nil arm -> it: nil (variant type)
//	ok arm  -> it: elemType (e.g., i64 for ?i64)
func (p *Parser) buildItBindingForArm(tok lexer.Token, matched Expression, armType string, elemType string) *LetStatement {
	ident, ok := matched.(*Identifier)
	if !ok {
		return nil
	}
	t, ok := p.varDeclTypes[ident.Value]
	if !ok || t == "" {
		return nil
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
		case "ok_err":
			typeStr = elemType + " | err"
		case "ok_nil":
			typeStr = elemType + " | nil"
		case "ok_err_nil":
			typeStr = elemType + " | err | nil"
		default:
			return nil
		}
	} else if _, ok := p.enumVariantNames[t]; ok {
		// Enum type: armType is the variant name or union expression (e.g., "status1" or "status2 | status3")
		typeStr = armType
	} else {
		return nil
	}

	return &LetStatement{
		Token:       tok,
		Name:        &Identifier{Token: tok, Value: "it"},
		Value:       matched,
		Type:        &NamedType{Value: typeStr},
		IsSynthetic: true,
	}
}

// prependStmt prepends a statement to a BlockStatement, returning a new BlockStatement.
func (p *Parser) prependStmt(body *BlockStatement, stmt Statement) *BlockStatement {
	if body == nil {
		return &BlockStatement{Statements: []Statement{stmt}}
	}
	stmts := make([]Statement, 0, len(body.Statements)+1)
	stmts = append(stmts, stmt)
	stmts = append(stmts, body.Statements...)
	return &BlockStatement{Token: body.Token, Statements: stmts}
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

	// Skip }
	if p.currentToken.Type == lexer.RBRACE {
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
	if !hasElseArm && (!hasErrArm || !hasNilArm || !hasValArm) {
		var missing []string
		if !hasErrArm {
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
	if p.currentToken.Type == lexer.NIL {
		return true
	}
	if p.currentToken.Type == lexer.IDENT &&
		(p.currentToken.Literal == "err" || p.currentToken.Literal == "nil" || p.currentToken.Literal == "ok") {
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

func (p *Parser) parseIfExpression() Expression {
	expr := &IfExpression{Token: p.currentToken}
	p.saveWarning(fmt.Sprintf("line %d, column %d: 'if/elif/else' is deprecated, use '{ <cond> -> <body> }' instead",
		expr.Token.Line, expr.Token.Column))

	// 跳过 if 关键字
	p.nextToken()

	// 解析条件表达式（不强制要求括号）
	expr.Condition = p.parseExpression(LOWEST)

	// 跳过条件表达式后面的所有令牌，直到找到左花括号
	for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
		p.nextToken()
	}

	// 解析左花括号
	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	expr.Consequence = p.parseBlockStatement()

	// 跳过换行符，查找 elif/else 关键字
	for p.peekToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	if p.peekToken.Type == lexer.ELSE {
		p.nextToken()
		p.nextToken() // 跳过 else 关键字

		// 跳过 else 关键字后面的所有令牌，直到找到左花括号
		for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}

		// 解析左花括号
		if p.currentToken.Type != lexer.LBRACE {
			msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		expr.Alternative = p.parseBlockStatement()

	} else if p.peekToken.Type == lexer.ELIF {
		// elif → desugar to else { if <cond> { <body> } [more elif/else] }
		expr.Alternative = p.parseElifBlock()
	}

	// Consume the } that closes the last body, so that the outer
	// parseBlockStatement/skipToStatementEnd doesn't see a premature RBRACE.
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	return expr
}

// parseElifBlock desugars `elif <cond> { <body> } [more elif/else]`
// into a BlockStatement containing a nested IfExpression.
// This lets all generators handle elif without any changes.
func (p *Parser) parseElifBlock() *BlockStatement {
	p.nextToken() // skip token before ELIF (e.g., } or NEWLINE)
	p.nextToken() // skip ELIF → current = first token of condition

	// Bare elif with no condition (e.g., `elif { body }`): treat as else block
	if p.currentToken.Type == lexer.LBRACE {
		body := p.parseBlockStatement()

		// Skip newlines, check for more elif/else (same as the normal path below)
		for p.peekToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		var alternative *BlockStatement
		if p.peekToken.Type == lexer.ELSE {
			p.nextToken()
			p.nextToken() // skip else
			for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.LBRACE {
				alternative = p.parseBlockStatement()
			}
		} else if p.peekToken.Type == lexer.ELIF {
			alternative = p.parseElifBlock()
		}

		// Consume the } that closes the last body in the chain
		if p.currentToken.Type == lexer.RBRACE {
			p.nextToken()
		}

		// If there's an alternative after bare elif, wrap body in an IfExpression
		// so the else/more elif stays attached
		if alternative != nil {
			nestedIf := &IfExpression{
				Token:       body.Token,
				Condition:   &Identifier{Token: p.currentToken, Value: "true"},
				Consequence: body,
				Alternative: alternative,
			}
			return &BlockStatement{
				Token: body.Token,
				Statements: []Statement{
					&ExpressionStatement{Token: body.Token, Expression: nestedIf},
				},
			}
		}

		// Simple bare elif (no more elif/else): return body directly as else block
		return body
	}

	// Parse condition
	condition := p.parseExpression(LOWEST)

	// Skip to LBRACE
	for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
		p.nextToken()
	}

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace in elif, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	consequence := p.parseBlockStatement()

	// Skip newlines, check for more elif/else
	for p.peekToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	var alternative *BlockStatement
	if p.peekToken.Type == lexer.ELSE {
		p.nextToken()
		p.nextToken() // skip else

		for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
			p.nextToken()
		}

		if p.currentToken.Type != lexer.LBRACE {
			msg := fmt.Sprintf("line %d, column %d: expected left brace in else, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		alternative = p.parseBlockStatement()

	} else if p.peekToken.Type == lexer.ELIF {
		alternative = p.parseElifBlock()
	}

	// Consume the } that closes the last body in the elif/else chain
	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// Build nested if: if <cond> { ... } [else { ... }]
	nestedIf := &IfExpression{
		Token:       consequence.Token,
		Condition:   condition,
		Consequence: consequence,
		Alternative: alternative,
	}

	// Wrap in a block statement so it plugs into IfExpression.Alternative
	return &BlockStatement{
		Token: consequence.Token,
		Statements: []Statement{
			&ExpressionStatement{
				Token:      consequence.Token,
				Expression: nestedIf,
			},
		},
	}
}

func (p *Parser) parseBlockStatement() *BlockStatement {
	block := &BlockStatement{Token: p.currentToken, Statements: []Statement{}}
	openBraceLine := p.currentToken.Line

	p.nextToken()

	// Separate comments on the same line as { (opening brace comments)
	// from doc comments for the first statement.
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
	block.OpeningBraceComment = openingComments

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		doc := p.collectDocComments()
		stmt := p.parseStatement()
		if stmt != nil {
			setDoc(stmt, doc)
			p.attachInlineComment(stmt)
			block.Statements = append(block.Statements, stmt)
		} else {
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

	block.TrailingComments = p.collectDocComments()

	return block
}

// parseLoopBody parses a loop body which can be either:
//   - Block statement: { ... }
//   - Single statement: print(i)
func (p *Parser) parseLoopBody() *BlockStatement {
	if p.currentToken.Type == lexer.LBRACE {
		block := p.parseBlockStatement()
		p.nextToken() // skip body's }
		return block
	}
	// Skip leading newlines
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	// Single-statement body
	stmt := p.parseStatement()
	if stmt == nil {
		p.saveError(fmt.Sprintf("line %d, column %d: expected loop body statement, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return &BlockStatement{Token: p.currentToken}
	}
	return &BlockStatement{
		Token:      p.currentToken,
		Statements: []Statement{stmt},
		IsInline:   true, // single-statement inline body (no braces)
	}
}

func (p *Parser) parseForStatement() Statement {
	stmt := &ForStatement{Token: p.currentToken}

	// Bare range-for: i <- (a..b]: { } — 不使用 for 關鍵字
	// Also handle labeled: outer i <- (a..b]: { }
	if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ARROW {
		ir := &IterationExpr{Variable: p.currentToken.Literal, Token: p.peekToken}
		p.nextToken() // skip IDENT (variable)
		p.nextToken() // skip ARROW (<-)
		p.parseForRange(ir)
		stmt.IterRange = ir
		if p.currentToken.Type == lexer.COLON {
			p.nextToken() // skip :
		} else {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'i <- range { }' is deprecated, use 'i <- range: { }' instead",
				ir.Token.Line, ir.Token.Column))
		}
		stmt.Body = p.parseLoopBody()
		return stmt
	}
	if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.IDENT {
		state := p.saveState()
		stmt.Label = p.currentToken.Literal
		p.nextToken() // skip label
		if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.ARROW {
			ir := &IterationExpr{Variable: p.currentToken.Literal, Token: p.peekToken}
			p.nextToken() // skip IDENT (variable)
			p.nextToken() // skip ARROW (<-)
			p.parseForRange(ir)
			stmt.IterRange = ir
			if p.currentToken.Type == lexer.COLON {
				p.nextToken() // skip :
			} else {
				p.saveWarning(fmt.Sprintf("line %d, column %d: 'i <- range { }' is deprecated, use 'i <- range: { }' instead",
					ir.Token.Line, ir.Token.Column))
			}
			stmt.Body = p.parseLoopBody()
			return stmt
		}
		p.restoreState(state)
	}

	// 检查命名循环：label for ...
	// 此时 currentToken 可能是 label（IDENT）或 for（FOR）
	// 如果是 IDENT + 下一个是 FOR，则作为 label 处理
	if p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		p.nextToken()
	}

	// 此时 currentToken 应该是 FOR
	if p.currentToken.Type != lexer.FOR {
		p.saveError(fmt.Sprintf("line %d, column %d: expected 'for', got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return nil
	}

	p.nextToken() // 跳过 for 关键字

	// 检查是否是无限循环 for { }
	if p.currentToken.Type == lexer.LBRACE {
		stmt.Condition = nil
		stmt.Body = p.parseBlockStatement()
		p.nextToken() // skip body's }
		p.saveWarning(fmt.Sprintf("line %d, column %d: 'for { }' is deprecated, use '{ } ()' infinite loop instead",
			stmt.Token.Line, stmt.Token.Column))
		return stmt
	}

	p.ctx.push(CTX_FOR_COND)
	init := p.parseStatement()
	p.ctx.pop()

	// 檢查 range for: for i <- [a..b] / (a..b] / [a..b) / (a..b) 或 for i in ...
	if p.currentToken.Type == lexer.IN || p.currentToken.Type == lexer.ARROW {
		if p.currentToken.Type == lexer.IN {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'for i in ...' is deprecated, use 'i <- ...' instead",
				p.currentToken.Line, p.currentToken.Column))
		}
		if es, ok := init.(*ExpressionStatement); ok {
			if ident, ok := es.Expression.(*Identifier); ok {
				ir := &IterationExpr{Variable: ident.Value, Token: p.currentToken}
				p.nextToken() // skip IN/ARROW
				p.parseForRange(ir)
				stmt.IterRange = ir
				goto parseBody
			}
		}
	}

parseBody:
	// 消耗冒號 (for range-for and while/for with condition)
	hasColon := p.currentToken.Type == lexer.COLON
	if hasColon {
		p.nextToken() // skip :
	}

	// 檢查是否為比較運算子：for i < n: { }
	// 此時 init 是 "i"，currentToken 是 "<"
	if init != nil && !hasColon && p.currentToken.Type != lexer.LBRACE {
		if _, ok := init.(*ExpressionStatement); ok {
			if isForCompOp(p.currentToken.Type) {
				// 將 init + 比較運算子 + 右運算元 組合成條件
				// 回退到 init 的位置，重新解析完整條件
				if es, ok := init.(*ExpressionStatement); ok {
					if ident, ok := es.Expression.(*Identifier); ok {
						// init 是簡單標識符，用它作為左運算元
						leftExpr := ident
						compOp := p.currentToken.Literal
						p.nextToken() // skip < > <= >= == !=
						rightExpr := p.parseExpression(LOWEST)
						condExpr := &InfixExpression{
							Token:    leftExpr.Token,
							Left:     leftExpr,
							Operator: compOp,
							Right:    rightExpr,
						}
						stmt.Condition = condExpr
						if p.currentToken.Type == lexer.COLON {
							p.nextToken() // skip :
						}
						stmt.Init = nil
						// Skip newlines before {
						for p.currentToken.Type == lexer.NEWLINE {
							p.nextToken()
						}
						if p.currentToken.Type == lexer.LBRACE {
							stmt.Body = p.parseBlockStatement()
							p.nextToken() // skip body's }
							return stmt
						}
					} else {
						// init 是簡單標識符但不是 < n[:] { } 模式，回退
						// 重置 init 為完整的 i < n 表達式
						// 因為左邊已經被 parseStatement 消費了，我們需要重新構造
					}
				}
			}
		}
	}

	// 根據 currentToken 類型分流處理 init
	if p.currentToken.Type == lexer.SEMICOLON || p.currentToken.Type == lexer.COMMA {
		// C-style for: for init, cond, update { }（也接受 ; 向後相容）
		stmt.Init = init
		if p.currentToken.Type == lexer.SEMICOLON {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'for init; cond; update' is deprecated, use commas ',' instead",
				p.currentToken.Line, p.currentToken.Column))
		}
		p.nextToken() // skip ; or ,
		stmt.Condition = p.parseExpression(LOWEST)
		p.nextToken()
		update := p.parseExpressionStatement()
		if update != nil {
			stmt.Update = update
		}
	} else if p.currentToken.Type == lexer.LBRACE {
		// for condition { } or for { } — init 是完整表達式
		if es, ok := init.(*ExpressionStatement); ok {
			stmt.Condition = es.Expression
			// Warn only for 'while' (deprecated); 'for cond { }' is a valid form
			// for conditional loops where range-for (i <- [a..b]: {}) doesn't apply
			// (e.g. non-unit step or complex conditions).
			if stmt.IterRange == nil && !hasColon && stmt.Token.Literal == "while" {
				p.saveWarning(fmt.Sprintf("line %d, column %d: 'while condition { }' is deprecated, use 'for condition { }' instead",
					stmt.Token.Line, stmt.Token.Column))
			}
		}
		if stmt.Token.Literal == "for" && stmt.IterRange == nil {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'for cond { }' is deprecated, use '{ } (cond)' loop instead",
				stmt.Token.Line, stmt.Token.Column))
		}
	} else {
		// for condition (NL) { } — condition 是完整表達式，後面有換行
		if init != nil {
			if es, ok := init.(*ExpressionStatement); ok {
				stmt.Condition = es.Expression
			}
		}
		// Warn only for 'while' (deprecated); 'for cond { }' is a valid form
		// for conditional loops where range-for doesn't apply.
		if stmt.Condition != nil && stmt.IterRange == nil &&
			stmt.Token.Literal == "while" && !hasColon {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'while condition { }' is deprecated, use 'for condition { }' instead",
				stmt.Token.Line, stmt.Token.Column))
		}
		if stmt.Condition != nil && stmt.IterRange == nil &&
			stmt.Token.Literal == "for" && !hasColon {
			p.saveWarning(fmt.Sprintf("line %d, column %d: 'for cond { }' is deprecated, use '{ } (cond)' loop instead",
				stmt.Token.Line, stmt.Token.Column))
		}
		// Skip newlines before {
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
	}

	stmt.Body = p.parseLoopBody()
	return stmt
}

// parseForRange 解析 <- 或 in 後的 range 表達式
func (p *Parser) parseForRange(ir *IterationExpr) {
	// 解析 range: [a..b], (a..b], [a..b), (a..b)
	leftInc := false
	// 字串遍歷: for i in 'abc'
	if p.currentToken.Type == lexer.STRING {
		ir.RangeStr = p.currentToken.Literal
		p.nextToken() // skip string
		ir.Range = &RangeExpression{
			Token:    p.currentToken,
			LeftInc:  true,
			RightInc: false,
		}
		return
	}

	if p.currentToken.Type == lexer.LBRACKET {
		// Peek ahead: [a..b] = range, [1, 2, 3] = slice literal
		state := p.saveState()
		p.nextToken() // skip [
		p.parseExpression(LOWEST)

		if p.currentToken.Type == lexer.ELLIPSIS {
			// Range: [a..b] — restore state, use existing range logic
			p.restoreState(state)
			leftInc = true
			tok := p.currentToken
			p.nextToken() // skip [

			start := p.parseExpression(LOWEST)

			// 拒絕浮點數區間邊界
			if _, ok := start.(*FloatLiteral); ok {
				msg := fmt.Sprintf("line %d, column %d: float range boundary not supported, use integers",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
				return
			}

			if p.currentToken.Type != lexer.ELLIPSIS {
				msg := fmt.Sprintf("line %d, column %d: expected '..' in range expression, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return
			}
			p.nextToken() // skip ..

			end := p.parseExpression(LOWEST)

			// 拒絕浮點數區間邊界
			if _, ok := end.(*FloatLiteral); ok {
				msg := fmt.Sprintf("line %d, column %d: float range boundary not supported, use integers",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
				return
			}

			rightInc := false
			if p.currentToken.Type == lexer.RBRACKET {
				rightInc = true
			} else if p.currentToken.Type == lexer.RPAREN {
				rightInc = false
			} else {
				msg := fmt.Sprintf("line %d, column %d: expected ']' or ')' in range expression, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return
			}
			p.nextToken() // skip ] or )

			ir.Range = &RangeExpression{
				Token:    tok,
				Start:    start,
				End:      end,
				LeftInc:  leftInc,
				RightInc: rightInc,
			}
			return
		} else if p.currentToken.Type == lexer.COMMA || p.currentToken.Type == lexer.RBRACKET {
			// 匿名切片: [1, 2, 3]
			p.restoreState(state)
			sliceExpr := p.parseSliceLiteral()
			if sliceLit, ok := sliceExpr.(*SliceLiteral); ok {
				ir.RangeExpr = sliceLit
				return
			}
			return
		} else {
			p.restoreState(state)
			msg := fmt.Sprintf("line %d, column %d: expected '..' for range or ','/'}' for slice, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return
		}
	} else if p.currentToken.Type == lexer.LPAREN {
		leftInc = false
	} else if p.currentToken.Type == lexer.IDENT {
		// 陣列/切片遍歷: for i in a
		ir.RangeExpr = &Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
		p.nextToken() // skip identifier
		return
	} else {
		msg := fmt.Sprintf("line %d, column %d: expected '[' or '(' or string in range expression, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}
	tok := p.currentToken
	p.nextToken() // skip [ or (

	start := p.parseExpression(LOWEST)

	// 拒絕浮點數區間邊界
	if _, ok := start.(*FloatLiteral); ok {
		msg := fmt.Sprintf("line %d, column %d: float range boundary not supported, use integers",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return
	}

	if p.currentToken.Type != lexer.ELLIPSIS {
		msg := fmt.Sprintf("line %d, column %d: expected '..' in range expression, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}
	p.nextToken() // skip ..

	end := p.parseExpression(LOWEST)

	// 拒絕浮點數區間邊界
	if _, ok := end.(*FloatLiteral); ok {
		msg := fmt.Sprintf("line %d, column %d: float range boundary not supported, use integers",
			p.currentToken.Line, p.currentToken.Column)
		p.saveError(msg)
		return
	}

	rightInc := false
	if p.currentToken.Type == lexer.RBRACKET {
		rightInc = true
	} else if p.currentToken.Type == lexer.RPAREN {
		rightInc = false
	} else {
		msg := fmt.Sprintf("line %d, column %d: expected ']' or ')' in range expression, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}
	p.nextToken() // skip ] or )

	ir.Range = &RangeExpression{
		Token:    tok,
		Start:    start,
		End:      end,
		LeftInc:  leftInc,
		RightInc: rightInc,
	}
}

// parseBangLoop 解析 !! { } 無限循環
func (p *Parser) parseBangLoop() Statement {
	stmt := &ForStatement{Token: p.currentToken}
	// !! / ! 後直接接 {
	bangTok := p.currentToken
	p.nextToken() // skip !! / !
	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	p.saveWarning(fmt.Sprintf("line %d, column %d: '%s { }' is deprecated, use '{ } ()' infinite loop instead",
		bangTok.Line, bangTok.Column, bangTok.Literal))
	return stmt
}

// parseStarBreakOrExpr handles a `*` at the start of a statement.
// It is treated as a `break` shorthand (`*` or `* #1`) when the next
// token indicates statement termination (NEWLINE/EOF/SEMICOLON/RBRACE)
// or a label (LABEL/IDENT). Otherwise it falls back to normal expression
// parsing (pointer dereference, multiplication, etc.).
func (p *Parser) parseStarBreakOrExpr() Statement {
	// Look ahead one token: if it is NEWLINE/EOF/SEMICOLON/RBRACE/LABEL/IDENT,
	// treat `*` as break.
	switch p.peekToken.Type {
	case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON, lexer.RBRACE, lexer.LABEL, lexer.IDENT:
		stmt := &BreakStatement{Token: p.currentToken}
		p.nextToken() // skip MUL
		if p.currentToken.Type == lexer.LABEL || p.currentToken.Type == lexer.IDENT {
			stmt.Label = p.currentToken.Literal
			p.nextToken()
		}
		return stmt
	}
	return p.parseExpressionStatement()
}

// parseStarStarContinue handles a `**` at the start of a statement as
// a `continue` shorthand, optionally followed by a label.
func (p *Parser) parseStarStarContinue() Statement {
	stmt := &ContinueStatement{Token: p.currentToken}
	p.nextToken() // skip STAR_STAR
	if p.currentToken.Type == lexer.LABEL || p.currentToken.Type == lexer.IDENT {
		stmt.Label = p.currentToken.Literal
		p.nextToken()
	}
	return stmt
}

// parseLabeledStatement 解析帶 #N 標籤的循環語句：
//
//	#1 i <- [0..256): { ... }   bare range-for
//	#1!! { ... }                infinite loop
//	#1 { ... } * N              counted loop (N 為常數計數)
//	#1 x == 1: { ... }          conditional
//
// 標籤名存到 ForStatement.Label，可被 break/continue 引用。
func (p *Parser) parseLabeledStatement() Statement {
	label := p.currentToken.Literal
	p.nextToken() // skip LABEL token

	var stmt Statement
	switch p.currentToken.Type {
	case lexer.BANG_BANG:
		stmt = p.parseBangLoop()
	case lexer.LBRACE:
		// Counted loop: #1 { ... } * N
		if p.isCountedLoopBlockFirst() {
			stmt = p.parseCountedLoopBlockFirst()
		} else if p.isCondLoopBlockFirst() {
			// Conditional/infinite loop: #1 { ... } (cond) / #1 { ... } ()
			stmt = p.parseCondLoopBlockFirst()
		} else {
			p.saveError(fmt.Sprintf("line %d, column %d: expected loop body after label #%s, got { without '* N' or '(cond)'",
				p.currentToken.Line, p.currentToken.Column, label))
			return nil
		}
	case lexer.IDENT, lexer.UNDERSCORE:
		// Two possibilities:
		//   bare range-for:  #1 i <- [0..256): { ... }
		//   conditional:     #1 x == 1: { ... }
		// Disambiguate by checking whether the second token is ARROW.
		if p.peekToken.Type == lexer.ARROW {
			stmt = p.parseForStatement()
		} else {
			// Conditional: parse an expression, then expect : and a block.
			exprTok := p.currentToken
			expr := p.parseExpression(LOWEST)
			if p.currentToken.Type == lexer.COLON {
				p.nextToken() // skip :
			}
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			bs := &BlockStatement{Token: p.currentToken}
			if p.currentToken.Type == lexer.LBRACE {
				bs = p.parseBlockStatement()
				p.nextToken() // skip body's }
			}
			stmt = &ExpressionStatement{
				Token: exprTok,
				Expression: &IfExpression{
					Token:       exprTok,
					Condition:   expr,
					Consequence: bs,
				},
			}
		}
	default:
		p.saveError(fmt.Sprintf("line %d, column %d: expected loop body after label #%s, got %s",
			p.currentToken.Line, p.currentToken.Column, label, p.currentToken.Type.String()))
		return nil
	}

	if stmt == nil {
		return nil
	}
	// Attach the label to the ForStatement (or wrap the conditional
	// expression statement in a ForStatement with Label set, so that
	// break/continue to this label works uniformly).
	if fs, ok := stmt.(*ForStatement); ok {
		fs.Label = label
		return fs
	}
	// Conditional: turn the ExpressionStatement into a labeled ForStatement
	// whose body is the IfExpression.
	es, ok := stmt.(*ExpressionStatement)
	if !ok {
		return stmt
	}
	ifExpr, ok := es.Expression.(*IfExpression)
	if !ok {
		return stmt
	}
	fs := &ForStatement{
		Token: es.Token,
		Label: label,
		Body:  ifExpr.Consequence,
	}
	// Store the IfExpression in Condition so the AST still carries the
	// original conditional semantics; transpiler can recognise this pattern.
	fs.Condition = ifExpr
	return fs
}

// isCountedLoopBlockFirst 報告當前位置是否為 `{ ... } * N` 計數循環。
// 前提：p.currentToken.Type == LBRACE。
// 掃描匹配的大括號後，檢查是否跟著 MUL INT（INT 後須為 NEWLINE/EOF/SEMICOLON，
// 確保是語句級計數而非乘法延續）。
func (p *Parser) isCountedLoopBlockFirst() bool {
	depth := 1
	// token 索引：1 = peekToken，k >= 2 => LookAhead(k-2)
	i := 1
	for {
		var tok lexer.Token
		if i == 1 {
			tok = p.peekToken
		} else {
			tok = p.lexer.LookAhead(i - 2)
		}
		if tok.Type == lexer.EOF {
			return false
		}
		if tok.Type == lexer.LBRACE {
			depth++
		} else if tok.Type == lexer.RBRACE {
			depth--
			if depth == 0 {
				// 找到匹配的 }，檢查後續 * [(-)? INT] <stmt-end>
				mulTok := p.lexer.LookAhead(i - 1)
				if mulTok.Type != lexer.MUL {
					return false
				}
				// 可選負號
				signTok := p.lexer.LookAhead(i)
				intIdx := i
				if signTok.Type == lexer.SUB {
					intIdx = i + 1
				}
				intTok := p.lexer.LookAhead(intIdx)
				if intTok.Type != lexer.INT {
					return false
				}
				// INT 後必須是語句結束，避免與 `} * n`（n 為變數的乘法）等歧義
				afterInt := p.lexer.LookAhead(intIdx + 1)
				switch afterInt.Type {
				case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON, lexer.RBRACE:
					return true
				}
				return false
			}
		}
		i++
	}
}

// parseCountedLoopBlockFirst 解析 `{ body } * N` 計數循環。
// 前提：p.currentToken.Type == LBRACE，且 isCountedLoopBlockFirst() 為 true。
func (p *Parser) parseCountedLoopBlockFirst() Statement {
	stmt := &ForStatement{Token: p.currentToken} // LBRACE
	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	// 期望 * INT
	if p.currentToken.Type != lexer.MUL {
		p.saveError(fmt.Sprintf("line %d, column %d: expected '*' after block in counted loop, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return stmt
	}
	p.nextToken() // skip *
	// 可選負號
	negative := false
	if p.currentToken.Type == lexer.SUB {
		negative = true
		p.nextToken() // skip -
	}
	if p.currentToken.Type != lexer.INT {
		p.saveError(fmt.Sprintf("line %d, column %d: expected integer count after '*' in counted loop, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return stmt
	}
	intToken := p.currentToken
	value, err := strconv.ParseInt(intToken.Literal, 10, 64)
	if err != nil {
		msg := fmt.Sprintf("line %d, column %d: could not parse %q as integer",
			intToken.Line, intToken.Column, intToken.Literal)
		p.saveError(msg)
		return nil
	}
	if negative {
		value = -value
		intToken.Literal = "-" + intToken.Literal
	}
	stmt.CountExpr = &IntegerLiteral{
		Token: intToken,
		Value: value,
	}
	p.nextToken() // skip INT
	return stmt
}

// isCondLoopBlockFirst 報告當前位置是否為 `{ ... } (cond)` 條件循環或 `{ ... } ()` 無限循環。
// 前提：p.currentToken.Type == LBRACE。
// 掃描匹配的大括號後，檢查是否跟著匹配的括號，且括號後為語句結束。
// `()` 空括號 → 無限循環；`(cond)` → 條件循環。
func (p *Parser) isCondLoopBlockFirst() bool {
	// token at index k: k==1 → peekToken; k>=2 → LookAhead(k-2)
	tokAt := func(k int) lexer.Token {
		if k == 1 {
			return p.peekToken
		}
		return p.lexer.LookAhead(k - 2)
	}
	// 掃描匹配的大括號（當前 token 是開頭的 {）
	depth := 1
	k := 1
	for {
		t := tokAt(k)
		if t.Type == lexer.EOF {
			return false
		}
		if t.Type == lexer.LBRACE {
			depth++
		} else if t.Type == lexer.RBRACE {
			depth--
			if depth == 0 {
				break
			}
		}
		k++
	}
	// k 在匹配的 } 處；下一個 token (k+1) 應為 LPAREN
	if tokAt(k+1).Type != lexer.LPAREN {
		return false
	}
	// 掃描匹配的括號，從 k+2 開始
	pdepth := 1
	m := k + 2
	for {
		t := tokAt(m)
		if t.Type == lexer.EOF {
			return false
		}
		if t.Type == lexer.LPAREN {
			pdepth++
		} else if t.Type == lexer.RPAREN {
			pdepth--
			if pdepth == 0 {
				break
			}
		}
		m++
	}
	// m 在匹配的 ) 處；下一個 token (m+1) 必須是語句結束
	switch tokAt(m + 1).Type {
	case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON, lexer.RBRACE:
		return true
	}
	return false
}

// parseCondLoopBlockFirst 解析 `{ body } (cond)` 條件循環或 `{ body } ()` 無限循環。
// 前提：p.currentToken.Type == LBRACE，且 isCondLoopBlockFirst() 為 true。
// `()` → 無限循環（Condition 為 nil）；`(cond)` → 條件循環（Condition 已設置）。
func (p *Parser) parseCondLoopBlockFirst() Statement {
	stmt := &ForStatement{Token: p.currentToken} // LBRACE
	stmt.Body = p.parseBlockStatement()
	p.nextToken() // skip body's }
	// 期望 (
	if p.currentToken.Type != lexer.LPAREN {
		p.saveError(fmt.Sprintf("line %d, column %d: expected '(' after block in loop, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return stmt
	}
	p.nextToken() // skip (
	// 空括號 () → 無限循環（Condition 保持 nil）
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken() // skip )
		return stmt
	}
	// 解析條件表達式
	p.ctx.push(CTX_FOR_COND)
	stmt.Condition = p.parseExpression(LOWEST)
	p.ctx.pop()
	if p.currentToken.Type != lexer.RPAREN {
		p.saveError(fmt.Sprintf("line %d, column %d: expected ')' to close loop condition, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String()))
		return stmt
	}
	p.nextToken() // skip )
	return stmt
}

func (p *Parser) parseArrayLiteral(size Expression) Expression {
	arr := &ArrayLiteral{
		Token:    p.currentToken,
		Size:     size,
		Elements: []Expression{},
	}

	p.nextToken() // 跳过 LBRACKET

	for p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.EOF {
		elem := p.parseExpression(LOWEST)
		if elem != nil {
			arr.Elements = append(arr.Elements, elem)
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过 COMMA
		} else if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected comma or right bracket in array literal, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
	}

	p.nextToken() // 跳过 RBRACKET
	return arr
}

// nums[..], nums[1..], nums[..3], nums[1..3], nums[1..3), nums(1..3)
func (p *Parser) parseSliceExpression(left Expression) Expression {
	tok := p.currentToken // LBRACKET or LPAREN
	leftInc := tok.Type == lexer.LBRACKET

	p.nextToken() // skip [ or (

	if p.currentToken.Type == lexer.ELLIPSIS {
		// [..], [..end], (..), (..end) — 範圍切片
		p.nextToken() // skip ..
		var endExpr Expression
		if p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.RPAREN {
			endExpr = p.parseExpression(LOWEST)
		}
		rightInc := p.currentToken.Type == lexer.RBRACKET
		if p.currentToken.Type == lexer.RBRACKET || p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
		return &SliceExpression{
			Token: tok, Left: left,
			Range: &RangeExpression{Token: tok, End: endExpr, LeftInc: leftInc, RightInc: rightInc},
		}
	}

	// [expr] — 索引: arr[i], vec[i], str[i], map[key]
	// 或 [start..end] — 範圍切片
	index := p.parseExpression(LOWEST)

	if p.currentToken.Type == lexer.ELLIPSIS {
		// [start..end] 範圍切片
		p.nextToken() // skip ..
		var end Expression
		if p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.RPAREN {
			end = p.parseExpression(LOWEST)
		}
		rightInc := p.currentToken.Type == lexer.RBRACKET
		if p.currentToken.Type == lexer.RBRACKET || p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
		return &SliceExpression{
			Token: tok, Left: left,
			Range: &RangeExpression{Token: tok, Start: index, End: end,
				LeftInc: leftInc, RightInc: rightInc},
		}
	}

	// [expr] — 索引
	if p.currentToken.Type == lexer.RBRACKET || p.currentToken.Type == lexer.RPAREN {
		p.nextToken() // skip ] or )
	}
	return &IndexExpression{Token: tok, Left: left, Index: index}
}

func (p *Parser) parseSliceLiteral() Expression {
	slice := &SliceLiteral{
		Token:    p.currentToken,
		Elements: []Expression{},
	}

	p.nextToken() // 跳过 LBRACKET

	for p.currentToken.Type != lexer.RBRACKET && p.currentToken.Type != lexer.EOF {
		// Skip newlines between elements
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACKET {
			break
		}

		elem := p.parseExpression(LOWEST)
		if elem != nil {
			slice.Elements = append(slice.Elements, elem)
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过 COMMA
			// Skip newlines after comma before next element
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
		} else if p.currentToken.Type != lexer.RBRACKET {
			// Skip newlines before closing bracket
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type != lexer.RBRACKET {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right bracket in slice literal, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
		}
	}

	p.nextToken() // 跳过 RBRACKET
	return slice
}

// parseMapLiteral parses a map literal: { k1:v1, k2:v2, ... }
// Supports comma or newline as separator between pairs.
// Pre-condition: p.currentToken is LBRACE.
// Post-condition: p.currentToken is the token after the closing RBRACE.
func (p *Parser) parseMapLiteral(mapType *MapType) Expression {
	ml := &MapLiteral{
		Token:   p.currentToken,
		Pairs:   []MapPair{},
		MapType: mapType,
	}
	p.nextToken() // skip {

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// skip leading newlines
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}

		// save key's first token for MapPair.Token
		keyTok := p.currentToken

		// parse key
		key := p.parseExpression(LOWEST)
		if key == nil {
			return nil
		}

		// expect ':'
		if p.currentToken.Type != lexer.COLON {
			msg := fmt.Sprintf("line %d, column %d: expected ':' in map literal, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		p.nextToken() // skip :

		// parse value
		val := p.parseExpression(LOWEST)
		if val == nil {
			return nil
		}

		ml.Pairs = append(ml.Pairs, MapPair{
			Token: keyTok,
			Key:   key,
			Value: val,
		})

		// skip trailing newlines
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // skip ,
		}
	}

	p.nextToken() // skip }
	return ml
}

// parseInterfaceDefinition 解析介面宣告：name { method(), method(), ... }
func (p *Parser) parseEnumDefinition() Statement {
	if p.enumVariantNames == nil {
		p.enumVariantNames = make(map[string][]string)
	}

	ed := &EnumDefinition{
		Token:  p.currentToken,
		Name:   p.currentToken.Literal,
		Values: []*EnumValue{},
	}

	p.nextToken() // skip name
	p.nextToken() // skip LBRACE

	nextVal := int64(0)
	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected enum value name, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		variantName := p.currentToken.Literal
		p.nextToken() // skip variant name

		// 支援顯式賦值：VARIANT = <int>
		var variantValue int64 = nextVal
		explicit := false
		if p.currentToken.Type == lexer.ASSIGN {
			explicit = true
			p.nextToken() // skip =
			intExpr := p.parseIntegerLiteral()
			if intLit, ok := intExpr.(*IntegerLiteral); ok {
				variantValue = intLit.Value
			} else {
				msg := fmt.Sprintf("line %d, column %d: expected integer value after '=', got %s",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
		} else {
			nextVal++
		}

		ev := &EnumValue{
			Token:    p.currentToken,
			Name:     variantName,
			Value:    variantValue,
			Explicit: explicit,
		}

		ed.Values = append(ed.Values, ev)
		p.enumVariantNames[ed.Name] = append(p.enumVariantNames[ed.Name], variantName)
	}

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken() // skip }
	}
	return ed
}

func (p *Parser) parseInterfaceDefinition() Statement {
	id := &InterfaceDefinition{
		Token:   p.currentToken,
		Name:    p.currentToken.Literal,
		Methods: []*InterfaceMethod{},
	}

	p.nextToken() // 跳过 name

	// 檢查介面繼承：db enter, leave { ... }
	for p.currentToken.Type == lexer.IDENT {
		id.Implements = append(id.Implements, p.currentToken.Literal)
		p.nextToken() // 跳过 interface name
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过逗號
		}
	}

	p.nextToken() // 跳过 LBRACE

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected method name in interface, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		method := &InterfaceMethod{
			Token: p.currentToken,
			Name:  p.currentToken.Literal,
		}
		p.nextToken() // skip first identifier (receiver or method name)

		// Generic-receiver form: t.method(...)
		// The first IDENT is the receiver type (e.g. "t"), and the second
		// IDENT is the method name. Marked as IsGenericReceiver for codegen.
		if p.currentToken.Type == lexer.DOT && p.peekToken.Type == lexer.IDENT {
			method.Receiver = method.Name
			method.IsGenericReceiver = true
			p.nextToken() // skip DOT
			method.Name = p.currentToken.Literal
			method.Token = p.currentToken
			p.nextToken() // skip method name
		}

		if p.currentToken.Type != lexer.LPAREN {
			msg := fmt.Sprintf("line %d, column %d: expected '(' after interface method name, got %s",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
		p.nextToken() // skip (

		// Parse method parameters: (n ..t), (n i64), (n), etc.
		for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
			for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.COMMA {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				break
			}

			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected parameter name in interface method, got %s",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			paramName := p.currentToken.Literal
			paramToken := p.currentToken
			p.nextToken()

			// Variadic parameter: n ..t
			if p.currentToken.Type == lexer.ELLIPSIS {
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					typeStr := "[]" + p.currentToken.Literal
					method.Parameters = append(method.Parameters, &Parameter{
						Token: paramToken,
						Name:  paramName,
						Type:  buildType(typeStr, paramToken),
					})
					method.IsVariadic = true
					p.nextToken()
					// Variadic must be the last parameter
					for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
						p.nextToken()
					}
					break
				}
			}

			// Regular type annotation: n i64, n ?t, n []t, n [N]t
			if p.currentToken.Type == lexer.IDENT {
				method.Parameters = append(method.Parameters, &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  buildType(p.currentToken.Literal, p.currentToken),
				})
				p.nextToken()
			} else if p.currentToken.Type == lexer.QUESTION {
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					typeStr := "?" + p.currentToken.Literal
					method.Parameters = append(method.Parameters, &Parameter{
						Token: paramToken,
						Name:  paramName,
						Type:  buildType(typeStr, paramToken),
					})
					p.nextToken()
				}
			} else if p.currentToken.Type == lexer.LBRACKET {
				p.nextToken() // skip [
				if p.currentToken.Type == lexer.RBRACKET {
					// []t
					p.nextToken()
					if p.currentToken.Type == lexer.IDENT {
						typeStr := "[]" + p.currentToken.Literal
						method.Parameters = append(method.Parameters, &Parameter{
							Token: paramToken,
							Name:  paramName,
							Type:  buildType(typeStr, paramToken),
						})
						p.nextToken()
					}
				} else {
					// [N]t
					sizeLit := p.currentToken.Literal
					p.nextToken()
					if p.currentToken.Type == lexer.RBRACKET {
						p.nextToken()
						if p.currentToken.Type == lexer.IDENT {
							typeStr := "[" + sizeLit + "]" + p.currentToken.Literal
							method.Parameters = append(method.Parameters, &Parameter{
								Token: paramToken,
								Name:  paramName,
								Type:  buildType(typeStr, paramToken),
							})
							p.nextToken()
						}
					}
				}
			} else {
				// No type annotation, just parameter name
				method.Parameters = append(method.Parameters, &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  nil,
				})
			}
		}

		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken() // skip )
		}

		// Optional result declaration: (res type)
		// e.g.  gt(x t) (res bool)
		if p.currentToken.Type == lexer.LPAREN && p.peekToken.Type == lexer.IDENT {
			p.nextToken() // skip (
			for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
				if p.currentToken.Type != lexer.IDENT {
					msg := fmt.Sprintf("line %d, column %d: expected result name in interface method, got %s",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return nil
				}
				resName := p.currentToken.Literal
				resTok := p.currentToken
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					method.Results = append(method.Results, &Parameter{
						Token: resTok,
						Name:  resName,
						Type:  buildType(p.currentToken.Literal, p.currentToken),
					})
					p.nextToken()
				} else {
					method.Results = append(method.Results, &Parameter{
						Token: resTok,
						Name:  resName,
						Type:  nil,
					})
				}
				if p.currentToken.Type == lexer.COMMA {
					p.nextToken()
				}
			}
			if p.currentToken.Type == lexer.RPAREN {
				p.nextToken() // skip )
			}
		}

		id.Methods = append(id.Methods, method)
	}

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken() // skip }
	}
	return id
}

// parseTaggedEnumDefinition 解析標籤列舉：option { val i64, nil bool, err str }
func (p *Parser) parseTaggedEnumDefinition() Statement {
	ted := &TaggedEnumDefinition{
		Token:    p.currentToken,
		Name:     p.currentToken.Literal,
		Variants: []*TaggedEnumVariant{},
	}

	p.nextToken() // skip name
	p.nextToken() // skip LBRACE

	idx := int64(0)
	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// 跳過換行和逗號
		for p.currentToken.Type == lexer.NEWLINE || p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.NIL {
			msg := fmt.Sprintf("line %d, column %d: expected variant name in tagged enum, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		variant := &TaggedEnumVariant{
			Token: p.currentToken,
			Name:  p.currentToken.Literal,
			Index: idx,
		}
		p.nextToken() // skip variant name

		// 解析型別
		var variantTypeStr string
		if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.NIL {
			variantTypeStr = p.currentToken.Literal
			p.nextToken()
		} else if p.currentToken.Type == lexer.LBRACKET {
			// []type 或 [N]type
			p.nextToken()
			if p.currentToken.Type == lexer.INT {
				variantTypeStr = "[" + p.currentToken.Literal + "]"
				p.nextToken()
			} else if p.currentToken.Type == lexer.IDENT {
				variantTypeStr = "[" + p.currentToken.Literal + "]"
				p.nextToken()
			} else {
				variantTypeStr = "[]"
			}
			if p.currentToken.Type == lexer.RBRACKET {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.IDENT {
				variantTypeStr = variantTypeStr + p.currentToken.Literal
				p.nextToken()
			}
		}
		if variantTypeStr != "" {
			variant.Type = buildType(variantTypeStr, variant.Token)
		}

		ted.Variants = append(ted.Variants, variant)
		idx++
	}

	return ted
}

// builtInTypeNames are the built-in type names that can be used in type aliases.
// When `name = IDENT` is seen at the top level and IDENT is one of these,
// it is treated as a type alias rather than a let statement.
var builtInTypeNames = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true,
	"u8": true, "u16": true, "u32": true, "u64": true,
	"f32": true, "f64": true,
	"byte": true, "bool": true, "str": true, "any": true,
}

// looksLikeEqualsTypeAlias reports whether the current statement looks like an
// equals-syntax type alias: name = type | type | ..., name = []type,
// name = [N]type, name = ?type, or name = known-type-name
// The current token is the alias name (IDENT), peekToken is ASSIGN.
// Note: LookAhead(0) returns the token AFTER peekToken (=), i.e. the first RHS token.
func (p *Parser) looksLikeEqualsTypeAlias() bool {
	if p.ctx.contains(CTX_FUNC_BODY) {
		return false
	}
	t1 := p.lexer.LookAhead(0) // first RHS token (after '=')
	switch t1.Type {
	case lexer.IDENT:
		// name = IDENT | IDENT | ... → union type alias
		// name = IDENT              → single type alias (if IDENT is a known type)
		t2 := p.lexer.LookAhead(1)
		if t2.Type == lexer.OR {
			// Distinguish union type alias from bitwise-or expression.
			// A union type alias has the form: type (| type)* where each
			// type is IDENT, []T, [N]T, ?T, or (params)(results).
			// A bitwise-or expression like `bv | (1 << bt)` contains
			// arithmetic operators (other than |) at bracket depth 0.
			// Scan the RHS: at depth 0, only `|` and statement-end
			// tokens are allowed; any other operator means it's an
			// expression, not a type alias.
			return p.scanIsUnionTypeAlias()
		}
		if t2.Type == lexer.NEWLINE || t2.Type == lexer.EOF || t2.Type == lexer.SEMICOLON {
			return builtInTypeNames[t1.Literal] || (p.typeAliasNames != nil && p.typeAliasNames[t1.Literal])
		}
		return false
	case lexer.LBRACKET:
		// name = []type or name = [N]type
		t2 := p.lexer.LookAhead(1)
		if t2.Type == lexer.RBRACKET {
			// []type — check that next is IDENT (type name)
			t3 := p.lexer.LookAhead(2)
			if t3.Type == lexer.IDENT {
				return true
			}
			return false
		}
		if t2.Type == lexer.INT {
			t3 := p.lexer.LookAhead(2)
			if t3.Type == lexer.RBRACKET {
				t4 := p.lexer.LookAhead(3)
				if t4.Type == lexer.IDENT {
					return true
				}
			}
		}
		return false
	case lexer.QUESTION:
		// name = ?type — ? at expression start is not valid
		t2 := p.lexer.LookAhead(1)
		if t2.Type == lexer.IDENT {
			return true
		}
		return false
	default:
		return false
	}
}

// scanIsUnionTypeAlias scans the RHS of `name = ... | ...` to determine
// whether it is a union type alias (type (| type)*) or a bitwise-or
// expression. The scan starts from LookAhead(0) (the first RHS token)
// and examines tokens at bracket depth 0:
//   - `|` separates union members (allowed)
//   - NEWLINE/EOF/SEMICOLON end the statement (→ union type alias)
//   - `[`/`(`/`{` increase depth; `]`/`)`/`}` decrease depth
//   - Any other BINARY operator at depth 0 (ADD, SUB, MUL, SHL, AND, etc.)
//     means it's a bitwise-or expression, not a type alias
//   - IDENT, INT, FLOAT, STRING, etc. are part of a type/operand
//
// Special handling for `(` at depth 0: a function type's params start
// with IDENT/IN/`)`, so if the token inside `(` is something else (INT,
// FLOAT, etc.), it's a grouped expression, not a type → return false.
func (p *Parser) scanIsUnionTypeAlias() bool {
	depth := 0
	i := 0
	for {
		t := p.lexer.LookAhead(i)
		switch t.Type {
		case lexer.LPAREN:
			if depth == 0 {
				// Distinguish function type (params) from grouped expression.
				// Function type params start with IDENT, IN, or `)` (empty).
				inside := p.lexer.LookAhead(i + 1)
				switch inside.Type {
				case lexer.IDENT, lexer.IN, lexer.RPAREN:
					// Likely function type — treat as type, enter parens
				default:
					// Grouped expression like (1 << bt) — not a type alias
					return false
				}
			}
			depth++
		case lexer.LBRACKET, lexer.LBRACE:
			depth++
		case lexer.RBRACKET, lexer.RPAREN, lexer.RBRACE:
			if depth == 0 {
				// Unmatched closing bracket — end of construct
				return true
			}
			depth--
		case lexer.NEWLINE, lexer.EOF, lexer.SEMICOLON:
			if depth == 0 {
				return true
			}
		case lexer.ADD, lexer.SUB, lexer.MUL, lexer.QUO, lexer.MOD,
			lexer.SHL, lexer.SHR, lexer.AND, lexer.XOR,
			lexer.EQUALS, lexer.NOT_EQUALS, lexer.LESS, lexer.LESS_EQUALS,
			lexer.GREATER, lexer.GREATER_EQUALS, lexer.LOR, lexer.LAND,
			lexer.ASSIGN, lexer.COLON,
			lexer.QUESTION, lexer.ARROW, lexer.RARROW:
			if depth == 0 {
				// An arithmetic/comparison operator at depth 0 means
				// this is a bitwise-or expression, not a type alias.
				return false
			}
		}
		i++
		if i > 1024 {
			return false
		}
	}
}

// parseTypeAlias parses a type alias or union type alias statement.
//
//	int = i8 | i16 | ... | u64   →  TypeAlias{Name:"int", Union:[i8,i16,...,u64]}
//	float = f32 | f64            →  TypeAlias{Name:"float", Union:[f32,f64]}
//	num = int | float            →  TypeAlias{Name:"num", Union:[int,float]}
//	my-int = i64                 →  TypeAlias{Name:"my-int", Type:NamedType{"i64"}}
//	bytes = []byte               →  TypeAlias{Name:"bytes", Type:SliceType{byte}}
//	buf = [16]u8                 →  TypeAlias{Name:"buf", Type:ArrayType{16,u8}}
//
// The first type must be a concrete type. If it is followed by `|`, we
// collect the rest of the union.
func (p *Parser) parseTypeAlias() Statement {
	name := p.currentToken.Literal
	nameToken := p.currentToken
	ta := &TypeAlias{Token: nameToken, Name: name}

	p.nextToken() // skip alias name → ASSIGN
	// Skip ASSIGN
	if p.currentToken.Type == lexer.ASSIGN {
		p.nextToken() // skip =
	}
	typ, ok := p.parseTypeExpression()
	if !ok {
		msg := fmt.Sprintf("line %d, column %d: expected type after %q in type alias",
			p.currentToken.Line, p.currentToken.Column, name)
		p.saveError(msg)
		return nil
	}
	// After parseTypeExpression, currentToken is the first non-type token
	// (e.g. `|`, NEWLINE, EOF).
	if p.currentToken.Type == lexer.OR {
		ut := &UnionType{Token: nameToken, Types: []Type{typ}}
		for p.currentToken.Type == lexer.OR {
			p.nextToken() // skip |
			// Allow newlines between | and the next type
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			next, ok := p.parseTypeExpression()
			if !ok {
				msg := fmt.Sprintf("line %d, column %d: expected type after '|'",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
				return nil
			}
			ut.Types = append(ut.Types, next)
		}
		ta.Union = ut
	} else {
		ta.Type = typ
	}
	// Record the type alias name for future detection
	if p.typeAliasNames == nil {
		p.typeAliasNames = make(map[string]bool)
	}
	p.typeAliasNames[name] = true
	return ta
}

// parseTypeExpression parses a single type (used by type aliases). It
// returns the parsed Type and whether parsing succeeded. The current
// token is left at the first token that is NOT part of the type
// (typically NEWLINE, EOF, OR, or a statement-end token).
func (p *Parser) parseTypeExpression() (Type, bool) {
	startTok := p.currentToken
	switch p.currentToken.Type {
	case lexer.LPAREN:
		// Function type: (params) (results)?
		return p.parseFunctionType(), true
	case lexer.MAP:
		// Explicit map type: map[K]V
		mapTok := p.currentToken
		p.nextToken() // skip map
		if p.currentToken.Type != lexer.LBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected '[' after 'map', got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil, false
		}
		p.nextToken() // skip [
		// parse key type
		keyType, ok := p.parseTypeExpression()
		if !ok {
			return nil, false
		}
		if p.currentToken.Type != lexer.RBRACKET {
			msg := fmt.Sprintf("line %d, column %d: expected ']' in map type, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil, false
		}
		p.nextToken() // skip ]
		// parse value type
		valType, ok := p.parseTypeExpression()
		if !ok {
			return nil, false
		}
		return &MapType{Token: mapTok, Key: keyType, Value: valType}, true
	case lexer.IDENT:
		// Could be:
		//   - NamedType (e.g., "i64")
		//   - PointerType (e.g., "ptr i64") — though typical syntax is
		//     "ptr" followed by an IDENT.
		if p.peekToken.Type == lexer.IDENT {
			// Check for "ptr T" form
			if p.currentToken.Literal == "ptr" {
				inner := p.peekToken
				p.nextToken() // skip "ptr"
				p.nextToken() // skip inner type
				return &PointerType{Token: startTok, Type: &NamedType{Token: inner, Value: inner.Literal}}, true
			}
		}
		name := p.currentToken.Literal
		t := p.currentToken
		p.nextToken()
		return &NamedType{Token: t, Value: name}, true
	case lexer.LBRACKET:
		p.nextToken() // skip [
		if p.currentToken.Type == lexer.RBRACKET {
			// []T
			p.nextToken() // skip ]
			elemName := p.currentToken.Literal
			elemTok := p.currentToken
			p.nextToken()
			return &SliceType{Token: startTok, Elem: &NamedType{Token: elemTok, Value: elemName}}, true
		}
		if p.currentToken.Type == lexer.QUESTION {
			// [?]T
			p.nextToken() // skip ?
			p.nextToken() // skip ]
			elemName := p.currentToken.Literal
			elemTok := p.currentToken
			p.nextToken()
			return &SliceType{Token: startTok, Elem: &NullableType{Token: elemTok, Type: &NamedType{Token: elemTok, Value: elemName}}}, true
		}
		// [K]V — MapType when K is a builtin or registered type name;
		// otherwise [N]T — ArrayType with size.
		if p.currentToken.Type == lexer.IDENT && p.peekToken.Type == lexer.RBRACKET &&
			(isBuiltinTypeName(p.currentToken.Literal) || p.isRegisteredTypeName(p.currentToken.Literal)) {
			keyName := p.currentToken.Literal
			keyTok := p.currentToken
			p.nextToken() // skip K
			p.nextToken() // skip ]
			valName := p.currentToken.Literal
			valTok := p.currentToken
			p.nextToken()
			return &MapType{Token: startTok, Key: &NamedType{Token: keyTok, Value: keyName}, Value: &NamedType{Token: valTok, Value: valName}}, true
		}
		// [N]T
		sizeTok := p.currentToken
		p.nextToken() // skip size
		p.nextToken() // skip ]
		// [N][M]T — nested array type
		if p.currentToken.Type == lexer.LBRACKET {
			elemType, ok := p.parseTypeExpression()
			if ok {
				return &ArrayType{Token: startTok, Size: &Identifier{Token: sizeTok, Value: sizeTok.Literal}, Elem: elemType}, true
			}
			return nil, false
		}
		elemName := p.currentToken.Literal
		elemTok := p.currentToken
		p.nextToken()
		return &ArrayType{Token: startTok, Size: &Identifier{Token: sizeTok, Value: sizeTok.Literal}, Elem: &NamedType{Token: elemTok, Value: elemName}}, true
	case lexer.QUESTION:
		// ?T
		p.nextToken() // skip ?
		innerName := p.currentToken.Literal
		innerTok := p.currentToken
		p.nextToken()
		return &NullableType{Token: startTok, Type: &NamedType{Token: innerTok, Value: innerName}}, true
	}
	return nil, false
}

// parseFunctionType parses a function type: (params) (results)?
// Pre-condition: p.currentToken is the opening '(' token.
// Post-condition: p.currentToken is the first token after the function type
// (typically NEWLINE, COMMA, RPAREN, or LBRACE).
// Each entry in params/results can be either "name type" or just "type"
// (name-less form, stored with empty Name).
func (p *Parser) parseFunctionType() *FunctionType {
	ft := &FunctionType{Token: p.currentToken}

	if p.currentToken.Type != lexer.LPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected '(' to start function type, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return ft
	}
	p.nextToken() // skip (

	// parse params
	if p.currentToken.Type != lexer.RPAREN {
		for {
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RPAREN {
				break
			}
			param, ok := p.parseFunctionTypeEntry()
			if !ok {
				return ft
			}
			ft.Params = append(ft.Params, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}
			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return ft
			}
			p.nextToken() // skip COMMA
		}
	}
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
	}

	// Optional results list: (results)
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		if p.currentToken.Type != lexer.RPAREN {
			for {
				for p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
				}
				if p.currentToken.Type == lexer.RPAREN {
					break
				}
				result, ok := p.parseFunctionTypeEntry()
				if !ok {
					return ft
				}
				ft.Results = append(ft.Results, result)

				if p.currentToken.Type == lexer.RPAREN {
					break
				}
				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return ft
				}
				p.nextToken() // skip COMMA
			}
		}
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}

	return ft
}

// parseFunctionTypeEntry parses one entry inside a function type's param or
// result list. Supports two forms:
//   - "name type"  (e.g. `n i64`)
//   - "type"       (name-less, e.g. `i64`)
//
// Pre-condition: p.currentToken is the first token of the entry (IDENT or IN).
// Post-condition: p.currentToken is the first token after the entry
// (typically COMMA or RPAREN).
func (p *Parser) parseFunctionTypeEntry() (*Parameter, bool) {
	if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
		msg := fmt.Sprintf("line %d, column %d: expected parameter name or type, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil, false
	}
	firstTok := p.currentToken
	firstLit := p.currentToken.Literal
	p.nextToken()

	// If next token starts a type, firstLit was the param name.
	if p.currentToken.Type == lexer.IDENT ||
		p.currentToken.Type == lexer.LBRACKET ||
		p.currentToken.Type == lexer.QUESTION {
		paramType, ok := p.parseParamTypeAfterName()
		if !ok {
			return nil, false
		}
		return &Parameter{Token: firstTok, Name: firstLit, Type: paramType}, true
	}

	// If next token is DOT, firstLit was the start of a dotted type name
	// (e.g. sql.result as a name-less type).
	if p.currentToken.Type == lexer.DOT {
		paramType := firstLit
		for p.currentToken.Type == lexer.DOT {
			paramType += "."
			p.nextToken()
			if p.currentToken.Type == lexer.IDENT {
				paramType += p.currentToken.Literal
				p.nextToken()
			}
		}
		return &Parameter{Token: firstTok, Name: "", Type: &NamedType{Token: firstTok, Value: paramType}}, true
	}

	// Otherwise firstLit is a name-less simple type (NamedType).
	return &Parameter{Token: firstTok, Name: "", Type: &NamedType{Token: firstTok, Value: firstLit}}, true
}

// parseParamTypeAfterName parses a single parameter type after the parameter
// name has been consumed. Supports: ?type, []type, [N]type, [?]type, named
// type, and (params)(results) function type. Returns the parsed Type and whether
// parsing succeeded. On success, p.currentToken is the first token after
// the type (typically COMMA, RPAREN, or NEWLINE).
func (p *Parser) parseParamTypeAfterName() (Type, bool) {
	// Function type: (params) (results)?
	// 受 anonymous-fn-type 開關控制：開關關閉時，參數列表中的匿名函式型別
	// 語法（如 `cb ()()`）被拒絕，應改用具名函式型別定義。
	if p.currentToken.Type == lexer.LPAREN {
		if !p.AllowAnonymousFnType {
			msg := fmt.Sprintf("line %d, column %d: anonymous function type syntax '()' is disabled; use a named function type alias or enable 'anonymous-fn-type' in mod.jsonc",
				p.currentToken.Line, p.currentToken.Column)
			p.saveError(msg)
			return nil, false
		}
		return p.parseFunctionType(), true
	}

	paramType := ""
	isOption := false
	if p.currentToken.Type == lexer.QUESTION {
		isOption = true
		p.nextToken()
	}
	if p.currentToken.Type == lexer.LBRACKET {
		p.nextToken() // skip [
		if p.currentToken.Type == lexer.INT {
			paramType = "[" + p.currentToken.Literal + "]"
			p.nextToken()
		} else if p.currentToken.Type == lexer.IDENT {
			paramType = "[" + p.currentToken.Literal + "]"
			p.nextToken()
		} else if p.currentToken.Type == lexer.QUESTION {
			paramType = "[?]"
			p.nextToken()
		} else {
			paramType = "[]"
		}
		if p.currentToken.Type == lexer.RBRACKET {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.IDENT {
			paramType = paramType + p.currentToken.Literal
			p.nextToken()
			// Support dotted element type: []sql.result
			for p.currentToken.Type == lexer.DOT {
				paramType += "."
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					paramType += p.currentToken.Literal
					p.nextToken()
				}
			}
		}
	} else if p.currentToken.Type == lexer.IDENT {
		paramType = p.currentToken.Literal
		p.nextToken()
		// Support dotted/qualified type names: sql.result, http.request, etc.
		for p.currentToken.Type == lexer.DOT {
			paramType += "."
			p.nextToken()
			if p.currentToken.Type == lexer.IDENT {
				paramType += p.currentToken.Literal
				p.nextToken()
			}
		}
	} else if !isOption {
		msg := fmt.Sprintf("line %d, column %d: expected parameter type, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil, false
	}

	if isOption {
		paramType = "?" + paramType
	}

	return buildType(paramType, p.currentToken), true
}

func (p *Parser) parseStructDefinition() Statement {
	sd := &StructDefinition{
		Token:  p.currentToken,
		Name:   p.currentToken.Literal,
		Fields: []*StructField{},
	}

	p.nextToken() // 跳过 struct name

	// 檢查介面實作：user json, fmt { ... } 或 stmt-mysql sql.stmt { ... }
	for p.currentToken.Type == lexer.IDENT {
		implName := p.currentToken.Literal
		p.nextToken() // 跳过 interface name
		// 支援 dotted/qualified 介面名：sql.stmt, sql.db 等
		for p.currentToken.Type == lexer.DOT {
			p.nextToken() // skip DOT
			if p.currentToken.Type == lexer.IDENT {
				implName += "." + p.currentToken.Literal
				p.nextToken() // skip IDENT after DOT
			}
		}
		sd.Implements = append(sd.Implements, implName)
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken() // 跳过逗號
		}
	}

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected '{' after struct name, got %s",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // 跳过 LBRACE

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// 跳過換行
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}

		// 支援欄位前的 #{...} 註解
		var fieldAnnotations []*AnnotationEntry
		if p.currentToken.Type == lexer.HASH_LBRACE {
			annotToken := p.currentToken
			p.nextToken() // skip #{
			fieldAnnotations = p.parseAnnotationBody()
			if p.currentToken.Type != lexer.RBRACE {
				msg := fmt.Sprintf("line %d, column %d: expected '}' to close field annotation, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			p.nextToken() // skip }
			_ = annotToken
			// 跳過換行
			for p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
			}
			if p.currentToken.Type == lexer.RBRACE {
				break
			}
		}

		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected field name in struct definition, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		field := &StructField{
			Token:       p.currentToken,
			Name:        p.currentToken.Literal,
			Annotations: fieldAnnotations,
		}

		p.nextToken() // 跳过 field name

		// [N]type 或 []type（陣列/切片）
		if p.currentToken.Type == lexer.LBRACKET {
			p.nextToken() // skip [
			if p.currentToken.Type == lexer.INT {
				// [N]type
				val, _ := strconv.ParseInt(p.currentToken.Literal, 10, 64)
				field.ArraySize = val
				p.nextToken() // skip N
			} else {
				// []type（無數字 = 切片）
				field.IsSlice = true
			}
			if p.currentToken.Type == lexer.RBRACKET {
				p.nextToken() // skip ]
			}
			if p.currentToken.Type == lexer.IDENT {
				elemType := p.currentToken.Literal
				p.nextToken() // skip element type
				// Support dotted/qualified element type: []sql.result
				for p.currentToken.Type == lexer.DOT {
					elemType += "."
					p.nextToken()
					if p.currentToken.Type == lexer.IDENT {
						elemType += p.currentToken.Literal
						p.nextToken()
					}
				}
				field.Type = buildType(elemType, p.currentToken)
			}
		} else if p.currentToken.Type == lexer.MUL {
			// Pointer type syntax: *byte, *i64, etc.
			p.nextToken() // skip *
			if p.currentToken.Type == lexer.IDENT {
				field.Type = buildType("*"+p.currentToken.Literal, p.currentToken)
				p.nextToken() // skip type name
			}
		} else if p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.PTR {
			// 普通类型定义 (including ptr keyword)
			typeStr := p.currentToken.Literal
			typeTok := p.currentToken
			p.nextToken() // 跳过 type
			// Support dotted/qualified type names: sql.result, etc.
			for p.currentToken.Type == lexer.DOT {
				typeStr += "."
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					typeStr += p.currentToken.Literal
					p.nextToken()
				}
			}
			field.Type = buildType(typeStr, typeTok)
		} else if p.currentToken.Type == lexer.COLON {
			// colon syntax: "field : type" or "field : value" (struct literal)
			p.nextToken() // 跳过 COLON
			if p.currentToken.Type == lexer.MUL {
				// Pointer type: field : *byte
				p.nextToken() // skip *
				if p.currentToken.Type == lexer.IDENT {
					field.Type = buildType("*"+p.currentToken.Literal, p.currentToken)
					p.nextToken()
				}
			} else if (p.currentToken.Type == lexer.IDENT || p.currentToken.Type == lexer.PTR) &&
				(p.peekToken.Type == lexer.NEWLINE || p.peekToken.Type == lexer.RBRACE || p.peekToken.Type == lexer.COMMA || p.peekToken.Type == lexer.EOF) {
				// Simple type name after colon → treat as type annotation
				field.Type = buildType(p.currentToken.Literal, p.currentToken)
				p.nextToken()
			} else {
				// Complex expression after colon → treat as value assignment
				field.Value = p.parseExpression(LOWEST)
			}
		}

		// Parse field modifiers: read-only, sealed
		for p.currentToken.Type == lexer.IDENT {
			mod := p.currentToken.Literal
			if mod == "read-only" {
				field.ReadOnly = true
				p.nextToken()
			} else if mod == "sealed" {
				field.Sealed = true
				p.nextToken()
			} else {
				break
			}
		}

		sd.Fields = append(sd.Fields, field)

		// 跳过逗号分隔符
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
	}

	p.nextToken() // 跳过 RBRACE

	// 收集 struct 欄位型別，供方法調用型別推斷使用
	if p.structFields == nil {
		p.structFields = make(map[string]map[string]string)
	}
	fields := make(map[string]string)
	for _, f := range sd.Fields {
		if f.Type != nil {
			typeStr := f.Type.String()
			if f.ArraySize > 0 {
				typeStr = fmt.Sprintf("[%d]%s", f.ArraySize, typeStr)
			} else if f.IsSlice {
				typeStr = "[]" + typeStr
			}
			fields[f.Name] = typeStr
		}
	}
	p.structFields[sd.Name] = fields

	return sd
}

func (p *Parser) parseStructLiteral(typeExpr Expression) Expression {
	// 處理匿名結構體：typeExpr 為 nil 時，用空字串作為 type（由 codegen 推斷）
	var typeName string
	if typeExpr != nil {
		ident, ok := typeExpr.(*Identifier)
		if !ok {
			// Not a valid struct literal type expression; caller should handle as match
			return nil
		}
		typeName = ident.Value
	}
	sl := &StructLiteral{
		Token:  p.currentToken,
		Type:   typeName,
		Fields: []*StructField{},
	}

	p.nextToken() // 跳过 LBRACE

	for p.currentToken.Type != lexer.RBRACE && p.currentToken.Type != lexer.EOF {
		// 跳過換行
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RBRACE {
			break
		}
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected field name in struct literal, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}

		field := &StructField{
			Token: p.currentToken,
			Name:  p.currentToken.Literal,
		}

		p.nextToken() // 跳过 field name

		if p.currentToken.Type == lexer.COLON {
			p.nextToken() // 跳过 COLON
			field.Value = p.parseExpression(LOWEST)
		}

		sl.Fields = append(sl.Fields, field)

		// 跳过逗号分隔符
		if p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		}
	}

	p.nextToken() // 跳过 RBRACE
	return sl
}

// isFunctionLiteral checks if the current LPAREN starts a function literal:
// (a i64, b i64) { ... } or (a, b) { ... }
func (p *Parser) isFunctionLiteral() bool {
	state := p.saveState()
	defer p.restoreState(state)

	// current = LPAREN
	p.nextToken() // skip (

	// Empty params: () { ... }
	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken() // skip )
		return p.currentToken.Type == lexer.LBRACE
	}

	// Must start with IDENT
	if p.currentToken.Type != lexer.IDENT {
		return false
	}
	p.nextToken() // skip first param name

	// (a i64, ...) — typed param
	if p.currentToken.Type == lexer.IDENT {
		return true
	}

	// (a, b, ...) or (a) — scan to closing ) then check for {
	for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
		p.nextToken()
	}
	if p.currentToken.Type != lexer.RPAREN {
		return false
	}
	p.nextToken() // skip )
	return p.currentToken.Type == lexer.LBRACE
}

func (p *Parser) parseFunctionLiteral() Expression {
	lit := &FunctionLiteral{Token: p.currentToken, FuncSignature: FuncSignature{Parameters: []*Parameter{}}}

	// currentToken is LPAREN (already positioned by caller)
	p.nextToken() // skip (

	if p.currentToken.Type != lexer.RPAREN {
		for {
			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected parameter name to be identifier",
					p.currentToken.Line, p.currentToken.Column)
				p.saveError(msg)
				return nil
			}

			param := &Parameter{
				Token: p.currentToken,
				Name:  p.currentToken.Literal,
				Type:  nil,
			}

			p.nextToken()

			// Optional type annotation: (a i64, b str)
			if p.currentToken.Type == lexer.IDENT {
				param.Type = buildType(p.currentToken.Literal, p.currentToken)
				p.nextToken()
			}

			lit.Parameters = append(lit.Parameters, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}

			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}

			p.nextToken()
		}
	}

	p.nextToken()

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}

	lit.Body = p.parseBlockStatement()

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	return lit
}

func (p *Parser) parseFunctionDefinition() Statement {
	def := &FunctionDefinition{
		Token: p.currentToken,
		Name:  p.currentToken.Literal,
		FuncSignature: FuncSignature{
			GenericParams: []*Identifier{},
			Parameters:    []*Parameter{},
			Results:       []*Parameter{},
		},
	}

	p.nextToken()

	// 泛型參數：arr_to_vec<N>: (...) 或 arr_to_vec<N, M>: (...)
	if p.currentToken.Type == lexer.LESS {
		p.nextToken()
		for {
			if p.currentToken.Type != lexer.IDENT {
				msg := fmt.Sprintf("line %d, column %d: expected generic parameter name, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			def.GenericParams = append(def.GenericParams, &Identifier{Token: p.currentToken, Value: p.currentToken.Literal})
			p.nextToken()
			if p.currentToken.Type == lexer.GREATER {
				p.nextToken()
				break
			}
			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected ',' or '>' in generic parameters, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return nil
			}
			p.nextToken()
		}
	}

	// 新語法要求 = 作爲函數定義標記
	if p.currentToken.Type != lexer.ASSIGN {
		msg := fmt.Sprintf("line %d, column %d: expected '=' after function name, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken()

	p.parseFunctionBody(def)

	return def
}

// parseFunctionBody parses the parameter list, result parameters, and body block.
// It assumes currentToken is the token after the '=' or ':' that introduces the body.
func (p *Parser) parseFunctionBody(def *FunctionDefinition) {
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
	} else {
		msg := fmt.Sprintf("line %d, column %d: expected left parenthesis, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}

	if p.currentToken.Type != lexer.RPAREN {
		for {
			if p.currentToken.Type == lexer.NEWLINE {
				p.nextToken()
				continue
			}
			if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
				msg := fmt.Sprintf("line %d, column %d: expected parameter name, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return
			}

			paramName := p.currentToken.Literal
			paramToken := p.currentToken
			p.nextToken()

			paramType := ""

			// 可變參數：a ..int（.. 後跟型別）
			if p.currentToken.Type == lexer.ELLIPSIS {
				p.nextToken()
				if p.currentToken.Type == lexer.IDENT {
					typeName := p.currentToken.Literal
					paramType = "[]" + typeName // ..int → []int 切片
					param := &Parameter{
						Token: paramToken,
						Name:  paramName,
						Type:  buildType(paramType, paramToken),
					}
					def.Parameters = append(def.Parameters, param)
					def.IsVariadic = true
					p.nextToken()
					// 跳過剩餘參數直到 RPAREN
					for p.currentToken.Type != lexer.RPAREN && p.currentToken.Type != lexer.EOF {
						p.nextToken()
					}
					break
				}
			}

			// 支援 ?type option 型別、[]type 切片、[N]type 陣列、fn (...) 函式型別
			paramTypeParsed, ok := p.parseParamTypeAfterName()
			if !ok {
				return
			}

			param := &Parameter{
				Token: paramToken,
				Name:  paramName,
				Type:  paramTypeParsed,
			}

			// 參數默認值：name type = expr
			if p.currentToken.Type == lexer.ASSIGN {
				p.nextToken() // skip =
				param.DefaultExpr = p.parseExpression(LOWEST)
			}

			def.Parameters = append(def.Parameters, param)

			if p.currentToken.Type == lexer.RPAREN {
				break
			}

			if p.currentToken.Type != lexer.COMMA {
				msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
					p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
				p.saveError(msg)
				return
			}

			p.nextToken()
		}
	}

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected right parenthesis, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}

	p.nextToken()

	// 跳過 NEWLINE（多行定義：回傳型別在下一行）
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken()
		if p.currentToken.Type != lexer.RPAREN {
			for {
				if p.currentToken.Type == lexer.NEWLINE {
					p.nextToken()
					continue
				}
				if p.currentToken.Type != lexer.IDENT && p.currentToken.Type != lexer.IN {
					msg := fmt.Sprintf("line %d, column %d: expected parameter name, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return
				}

				paramName := p.currentToken.Literal
				paramToken := p.currentToken
				p.nextToken()

				// 支援 ?type、[]type 切片、[N]type 陣列、[?]type、fn (...) 函式型別作為結果類型
				paramTypeParsed, ok := p.parseParamTypeAfterName()
				if !ok {
					return
				}

				param := &Parameter{
					Token: paramToken,
					Name:  paramName,
					Type:  paramTypeParsed,
				}
				def.Results = append(def.Results, param)

				if p.currentToken.Type == lexer.RPAREN {
					break
				}

				if p.currentToken.Type != lexer.COMMA {
					msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
						p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
					p.saveError(msg)
					return
				}

				p.nextToken()
			}
		}

		if p.currentToken.Type != lexer.RPAREN {
			msg := fmt.Sprintf("line %d, column %d: expected right parenthesis, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return
		}

		p.nextToken()

	}

	// 解析回傳型別：fib(n i64) i64 {  → 在 { 前的 IDENT 為回傳型別
	if p.currentToken.Type == lexer.IDENT {
		retTypeStr := p.currentToken.Literal
		retTypeTok := p.currentToken
		p.nextToken()
		// Support dotted/qualified type names: sql.result, etc.
		for p.currentToken.Type == lexer.DOT {
			retTypeStr += "."
			p.nextToken()
			if p.currentToken.Type == lexer.IDENT {
				retTypeStr += p.currentToken.Literal
				p.nextToken()
			}
		}
		result := &Parameter{
			Token: retTypeTok,
			Name:  "",
			Type:  buildType(retTypeStr, retTypeTok),
		}
		def.Results = append(def.Results, result)
	}

	// 從參數型別中推斷隱式泛型參數（單字母 a-z 做為陣列大小）
	for _, param := range def.Parameters {
		detectImplicitGeneric(param.Type, def)
	}
	for _, param := range def.Results {
		detectImplicitGeneric(param.Type, def)
	}

	// 註冊參數與結果型別到 varDeclTypes，使 match desugar 能為 option 型別參數
	// 生成正確的 `it` 綁定（如 `x ?i64` 在 `x: { ok -> result = it }` 中需要 it: i64）。
	if p.varDeclTypes == nil {
		p.varDeclTypes = make(map[string]string)
	}
	for _, param := range def.Parameters {
		if param.Type != nil {
			p.varDeclTypes[param.Name] = typeString(param.Type)
		}
	}
	for _, param := range def.Results {
		if param.Type != nil && param.Name != "" {
			p.varDeclTypes[param.Name] = typeString(param.Type)
		}
	}

	if p.currentToken.Type != lexer.LBRACE {
		msg := fmt.Sprintf("line %d, column %d: expected left brace, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return
	}

	p.ctx.push(CTX_FUNC_BODY)
	def.Body = p.parseBlockStatement()
	p.ctx.pop()

	// Move inline comment on the same line as { from OpeningBraceComment to
	// the function definition's Comment field, so the formatter outputs it
	// after the { on the same line.
	if def.Body.OpeningBraceComment != nil && len(def.Body.OpeningBraceComment.List) > 0 {
		setComment(def, def.Body.OpeningBraceComment)
		def.Body.OpeningBraceComment = nil
	}
	// Also check TrailingComments (fallback for empty blocks where the comment
	// was collected as a trailing comment before the OpeningBraceComment fix)
	if def.Body.TrailingComments != nil && len(def.Body.TrailingComments.List) > 0 &&
		def.Body.TrailingComments.List[0].Pos.Line == def.Body.Token.Line {
		setComment(def, def.Body.TrailingComments)
		def.Body.TrailingComments = nil
	}

	if p.currentToken.Type == lexer.RBRACE {
		p.nextToken()
	}

	// 收集函數簽名（結果型別），供後續 let 型別推斷使用
	if len(def.Results) > 0 {
		if p.funcSignatures == nil {
			p.funcSignatures = make(map[string][]string)
		}
		rets := make([]string, len(def.Results))
		for i, r := range def.Results {
			rets[i] = typeString(r.Type)
		}
		p.funcSignatures[def.Name] = rets
	}
}

// parseFFIDeclaration 解析 FFI 宣告：
//
//	#c
//	_name = (params) (results)
//
// #c（或 #cpp 等）獨立一行，標記下一個宣告為 FFI 綁定。
// 名稱以 _ 開頭表示私有（不導出），C ABI 符號自動去除前綴 _ 並將連字號轉為底線。
// 參數/結果型別支援 C 風格指針語法（*T、**T、***T，必須有具體型別）以及一般具名型別。
func (p *Parser) parseFFIDeclaration() Statement {
	// 讀取 FFI 指令（#c、#cpp 等），取得語言名稱
	lang := p.currentToken.Literal
	stmt := &ExternStatement{
		Token:      p.currentToken,
		Lang:       lang,
		Parameters: []*Parameter{},
		Results:    []*Parameter{},
	}
	p.nextToken() // skip FFI token

	// 跳過 NEWLINE（#c 獨立一行，宣告在後續行）
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	// 函式名稱（可以 _ 開頭表示私有）
	if p.currentToken.Type != lexer.IDENT {
		msg := fmt.Sprintf("line %d, column %d: expected identifier after #%s directive, got %s instead",
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

// parseExternParamList 解析 extern 宣告中的單一 (name type, ...) 參數列表。
// 前置條件: currentToken 為 '('；後置條件: currentToken 為 ')' 後的下一個 token。
func (p *Parser) parseExternParamList() ([]*Parameter, bool) {
	var params []*Parameter
	if p.currentToken.Type != lexer.LPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected '(' in extern declaration, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil, false
	}
	p.nextToken() // skip (

	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
		return params, true
	}

	for {
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		if p.currentToken.Type == lexer.RPAREN {
			break
		}

		// 參數名稱
		if p.currentToken.Type != lexer.IDENT {
			msg := fmt.Sprintf("line %d, column %d: expected parameter name in extern declaration, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil, false
		}
		nameTok := p.currentToken
		name := p.currentToken.Literal
		p.nextToken() // skip name

		// 參數型別
		paramType, ok := p.parseExternType()
		if !ok {
			return nil, false
		}

		params = append(params, &Parameter{Token: nameTok, Name: name, Type: paramType})

		if p.currentToken.Type == lexer.RPAREN {
			break
		}
		if p.currentToken.Type != lexer.COMMA {
			msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis in extern declaration, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil, false
		}
		p.nextToken() // skip COMMA
	}

	if p.currentToken.Type != lexer.RPAREN {
		msg := fmt.Sprintf("line %d, column %d: expected right parenthesis in extern declaration, got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil, false
	}
	p.nextToken() // skip )

	return params, true
}

// parseExternType 解析 extern 宣告中的單一型別。支援：
//   - *T、**T、***T 等 C 風格指針型別（必須有具體型別 T）
//   - 具名型別、?type、[]type、[N]type 等（重用 parseParamTypeAfterName）
//
// 不再支援 ptr 關鍵字；FFI 中一律使用 *T 語法。
func (p *Parser) parseExternType() (Type, bool) {
	// 計算指針間接層數：* = 1 層，** = 2 層，*** = 3 層，……
	// 語法器將 * 詞法為 MUL，** 詞法為 STAR_STAR。
	pointerCount := 0
	var starTok lexer.Token
	for {
		if p.currentToken.Type == lexer.MUL {
			if pointerCount == 0 {
				starTok = p.currentToken
			}
			pointerCount++
			p.nextToken()
		} else if p.currentToken.Type == lexer.STAR_STAR {
			if pointerCount == 0 {
				starTok = p.currentToken
			}
			pointerCount += 2
			p.nextToken()
		} else {
			break
		}
	}

	if pointerCount > 0 {
		// 解析基礎型別（必須有具體型別）
		baseType, ok := p.parseParamTypeAfterName()
		if !ok {
			return nil, false
		}
		// 由內向外包裹 PointerType
		result := baseType
		for i := 0; i < pointerCount; i++ {
			result = &PointerType{Token: starTok, Type: result}
		}
		return result, true
	}

	// 其餘型別重用既有參數型別解析邏輯
	t, ok := p.parseParamTypeAfterName()
	if !ok {
		return nil, false
	}
	return t, true
}

// parseColonFunctionDefinition parses a colon-syntax function definition:
// foo: (a int) { ... }
func (p *Parser) parseColonFunctionDefinition() Statement {
	def := &FunctionDefinition{
		Token: p.currentToken,
		Name:  p.currentToken.Literal,
		FuncSignature: FuncSignature{
			GenericParams: []*Identifier{},
			Parameters:    []*Parameter{},
			Results:       []*Parameter{},
		},
		ColonSyntax: true,
	}

	p.nextToken() // skip IDENT → COLON
	if p.currentToken.Type != lexer.COLON {
		msg := fmt.Sprintf("line %d, column %d: expected ':', got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip COLON

	// Skip newlines
	for p.currentToken.Type == lexer.NEWLINE {
		p.nextToken()
	}

	p.parseFunctionBody(def)

	return def
}

// parseColonMethodDefinition parses a method definition with colon syntax:
// user.foo: (a int) { ... }
func (p *Parser) parseColonMethodDefinition(structToken lexer.Token) Statement {
	p.nextToken() // skip struct name → DOT
	p.nextToken() // skip DOT → method name (IDENT)
	methodName := p.currentToken.Literal
	fullName := structToken.Literal + "." + methodName

	p.nextToken() // skip method name → COLON
	if p.currentToken.Type != lexer.COLON {
		msg := fmt.Sprintf("line %d, column %d: expected ':', got %s instead",
			p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
		p.saveError(msg)
		return nil
	}
	p.nextToken() // skip COLON

	// 推入方法上下文，供 resolveReceiverType 解析 self
	p.methodStructStack = append(p.methodStructStack, structToken.Literal)

	def := &FunctionDefinition{
		Token: structToken,
		Name:  fullName,
		FuncSignature: FuncSignature{
			GenericParams: []*Identifier{},
			Parameters:    []*Parameter{},
			Results:       []*Parameter{},
		},
		ColonSyntax: true,
	}

	p.parseFunctionBody(def)

	// 彈出方法上下文
	if len(p.methodStructStack) > 0 {
		p.methodStructStack = p.methodStructStack[:len(p.methodStructStack)-1]
	}

	// Insert self parameter
	selfParam := &Parameter{
		Token: structToken,
		Name:  "self",
		Type:  buildType(structToken.Literal, structToken),
	}
	def.Parameters = append([]*Parameter{selfParam}, def.Parameters...)

	return def
}

func (p *Parser) parseCallExpression(function Expression) Expression {
	expr := &CallExpression{
		Token:     p.currentToken,
		Function:  function,
		Arguments: []Expression{},
	}

	p.nextToken()

	if p.currentToken.Type == lexer.RPAREN {
		p.nextToken()
		return expr
	}

	for {
		// 跳過換行，支持多行函數調用參數
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}
		arg := p.parseArgument()
		if arg != nil {
			expr.Arguments = append(expr.Arguments, arg)
		}

		// 跳過結尾的換行
		for p.currentToken.Type == lexer.NEWLINE {
			p.nextToken()
		}

		if p.currentToken.Type == lexer.COMMA {
			p.nextToken()
		} else if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
			break
		} else {
			msg := fmt.Sprintf("line %d, column %d: expected comma or right parenthesis, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return nil
		}
	}

	return expr
}

// parseArgument 解析函数调用的参数
func (p *Parser) parseArgument() Expression {
	// 根据当前令牌类型解析不同的表达式
	switch p.currentToken.Type {
	case lexer.STRING:
		return p.parseExpression(LOWEST)
	case lexer.INT:
		return p.parseExpression(LOWEST)
	case lexer.FLOAT:
		return p.parseExpression(LOWEST)
	case lexer.BYTE:
		return p.parseExpression(LOWEST)
	case lexer.TRUE:
		return p.parseExpression(LOWEST)
	case lexer.FALSE:
		return p.parseExpression(LOWEST)
	case lexer.NIL:
		return p.parseExpression(LOWEST)
	case lexer.IDENT:
		// 使用 parseExpression 处理标识符及后续可能的点操作、函数调用、切片和中缀运算符
		return p.parseExpression(LOWEST)
	case lexer.NEWLINE:
		// 跳过换行，支持多行函数调用参数
		p.nextToken()
		return p.parseArgument()
	case lexer.LPAREN:
		return p.parseExpression(LOWEST)
	default:
		// 如果是其他类型，尝试使用 LOWEST 优先级解析
		return p.parseExpression(LOWEST)
	}
}

// exprToType 將 Expression 轉換為 Type，用於 NullableType/PointerType 的內部型別
func exprToType(expr Expression) Type {
	if t, ok := expr.(Type); ok {
		return t
	}
	if ident, ok := expr.(*Identifier); ok {
		return &NamedType{Token: ident.Token, Value: ident.Value}
	}
	return &NamedType{
		Token: lexer.Token{Line: expr.Pos().Line, Column: expr.Pos().Column},
		Value: fmt.Sprintf("%v", expr),
	}
}

func (p *Parser) parsePointerType() Expression {
	pt := &PointerType{Token: p.currentToken}
	p.nextToken() // skip ptr
	// ptr(type)
	if p.currentToken.Type == lexer.LPAREN {
		p.nextToken() // skip (
		typeExpr := p.parseExpression(LOWEST)
		pt.Type = exprToType(typeExpr)
		if p.currentToken.Type == lexer.RPAREN {
			p.nextToken()
		}
	}
	return pt
}

func (p *Parser) parseNullableType(expression Expression) Expression {
	nullable := &NullableType{
		Token: p.peekToken,
		Type:  exprToType(expression),
	}

	p.nextToken() // 跳过 QUESTION 令牌

	return nullable
}

func (p *Parser) expectPeek(t lexer.TokenType) bool {
	if p.peekToken.Type == t {
		p.nextToken()
		return true
	}

	p.peekError(t)
	return false
}

// buildType 將型別字串轉換為 Type 節點
func buildType(typeStr string, tok lexer.Token) Type {
	if typeStr == "" {
		return nil
	}
	// 處理 ? 前綴（option 型別）
	if typeStr[0] == '?' {
		inner := buildType(typeStr[1:], tok)
		if inner == nil {
			return nil
		}
		return &NullableType{Token: tok, Type: inner}
	}
	// 處理 ptr 前綴（指標型別）
	if strings.HasPrefix(typeStr, "ptr ") {
		inner := buildType(typeStr[4:], tok)
		if inner == nil {
			return nil
		}
		return &PointerType{Token: tok, Type: inner}
	}
	// 處理 * 前綴（指標型別，簡寫）
	if typeStr[0] == '*' {
		inner := buildType(typeStr[1:], tok)
		if inner == nil {
			return nil
		}
		return &PointerType{Token: tok, Type: inner}
	}
	// 處理 [] 前綴（切片型別）
	if strings.HasPrefix(typeStr, "[]") {
		elem := buildType(typeStr[2:], tok)
		if elem == nil {
			elem = &NamedType{Token: tok, Value: "i64"}
		}
		return &SliceType{Token: tok, Elem: elem}
	}
	// 處理 [n] 或 [ident] 或 [?] 前綴（陣列型別）
	if len(typeStr) > 2 && typeStr[0] == '[' {
		end := strings.IndexByte(typeStr, ']')
		if end > 0 {
			sizeStr := typeStr[1:end]
			var sizeExpr Expression
			if sizeStr != "" && sizeStr != "?" {
				if val, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
					sizeExpr = &IntegerLiteral{Token: tok, Value: val, Raw: sizeStr}
				} else {
					sizeExpr = &Identifier{Token: tok, Value: sizeStr}
				}
			}
			elemStr := typeStr[end+1:]
			elem := buildType(elemStr, tok)
			if elem == nil {
				elem = &NamedType{Token: tok, Value: elemStr}
			}
			return &ArrayType{Token: tok, Size: sizeExpr, Elem: elem}
		}
	}
	// 簡單名稱型別
	return &NamedType{Token: tok, Value: typeStr}
}

// addImplicitGeneric 將單字母 a-z 加入泛型參數列表（防重複）
func addImplicitGeneric(name string, def *FunctionDefinition) {
	if len(name) != 1 || name[0] < 'a' || name[0] > 'z' {
		return
	}
	for _, gp := range def.GenericParams {
		if gp.Value == name {
			return
		}
	}
	def.GenericParams = append(def.GenericParams, &Identifier{Value: name})
}

// detectImplicitGeneric 從型別節點中推斷隱式泛型參數（單字母 a-z）
func detectImplicitGeneric(t Type, def *FunctionDefinition) {
	if t == nil {
		return
	}
	switch typ := t.(type) {
	case *NamedType:
		// 單字母 a-z 視為泛型型別參數
		if len(typ.Value) == 1 && typ.Value[0] >= 'a' && typ.Value[0] <= 'z' {
			addImplicitGeneric(typ.Value, def)
		}
	case *NullableType:
		detectImplicitGeneric(typ.Type, def)
	case *ArrayType:
		// 陣列大小中的單字母 a-z 視為泛型大小參數
		if ident, ok := typ.Size.(*Identifier); ok {
			if len(ident.Value) == 1 && ident.Value[0] >= 'a' && ident.Value[0] <= 'z' {
				addImplicitGeneric(ident.Value, def)
			}
		}
		// 檢查元素型別
		detectImplicitGeneric(typ.Elem, def)
	case *SliceType:
		detectImplicitGeneric(typ.Elem, def)
	case *PointerType:
		detectImplicitGeneric(typ.Type, def)
	}
}

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

// attachAnnotations 將註解條目附加到宣告語句上。
func (p *Parser) attachAnnotations(stmt Statement, entries []*AnnotationEntry) {
	params := extractGenericParams(entries)
	platformKeys := ExtractPlatformKeys(entries)
	switch s := stmt.(type) {
	case *LetStatement:
		s.Annotations = entries
		s.GenericParams = params
		s.PlatformKeys = platformKeys
	case *StructDefinition:
		s.Annotations = entries
		s.GenericParams = params
		s.PlatformKeys = platformKeys
	case *FunctionDefinition:
		// 方法定義：從 #{generic=[K,V]} 註解提取泛型型別參數
		s.Annotations = entries
		for _, name := range params {
			s.GenericParams = append(s.GenericParams, &Identifier{Value: name})
		}
		s.PlatformKeys = platformKeys
	case *ExpressionStatement:
		s.Annotations = entries
		s.PlatformKeys = platformKeys
	}
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
		Token:       annotToken,
		Lang:        lang,
		Parameters:  []*Parameter{},
		Results:     []*Parameter{},
		Annotations: extraAnnots,
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
		// 可能是陣列或範圍，需向前看
		// 暫存當前位置以便回溯
		saveState := p.lexer.SaveState()
		saveCur := p.currentToken
		savePeek := p.peekToken

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
				Token:    saveCur,
				Start:    firstVal,
				End:      endVal,
				LeftInc:  true, // [
				RightInc: rightInc,
			}
		}
		// 不是範圍，回溯並解析為陣列
		p.lexer.RestoreState(saveState)
		p.currentToken = saveCur
		p.peekToken = savePeek
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
