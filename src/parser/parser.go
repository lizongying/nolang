package parser

import (
	"fmt"
	"os"
	"strings"

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
	warnedSemiEat     map[int]bool                 // 已警告過的「; 註釋疑似吞代碼」token 絕對索引（回溯重放去重）
	reportedIllegal   map[string]bool              // 已報告的 ILLEGAL token 位置（避免重複）
	sem               *SemanticContext            // 語義副表（類型推斷 + 註解/平台鍵/embed/泛型）
	funcSignatures    map[string][]string          // 函數名 → 結果型別字串列表（用於 let 型別推斷）
	methodSignatures  map[string][]string          // 結構體方法 → 結果型別字串列表（鍵：module.struct.method）
	structFields      map[string]map[string]string // struct 名 → 欄位名 → 型別字串
	methodStructStack []string                     // 當前方法所屬的 struct 名稱棧
	typeAliasNames    map[string]bool              // 已定義的類型別名名稱（用於等號語法偵測）

	// curFuncName tracks the current function being parsed, for function-scoped
	// variable type tracking. Empty when parsing module-level code.
	curFuncName string

	// idxLocalTypes 是「安全索引專用」的解析器本地型別表（函數作用域）：
	// lowering 預掃描區域變數容器型別（如 `av = a.to-vec()`）後寫入此處，
	// 僅供 isSafeIndexBase / inferIndexElemType 查詢。刻意不寫入
	// sem.FuncVarTypes / VarTypes，避免 `a`/`av` 等常見名竄改 std 函式
	// 區域變數型別（跨函數型別污染會讓 str.to-i8 等 std 函式 codegen 崩潰）。
	idxLocalTypes map[string]map[string]string

	// pendingAnnotations 暫存待附加到宣告的註解條目
	pendingAnnotations []*AnnotationEntry

	// pendingOverloadDefs 收集被 #{...} 註解修飾、緊接其後的「同名多載」函式
	// 定義（nolang 以連續 `name = ...` 表達 arity 多載）。parseAnnotationStatement
	// 解析首個定義後，會繼續掃描並解析後續同名定義、為其標記 BuiltinGroup，
	// 暫存於此；呼叫方（ParseProgram / parseBlockStatement）在取得註解陳述後
	// 將其接續附加到陳述序列，確保多載群組的每個定義都進入 AST。
	pendingOverloadDefs []Statement

	// Filename is the source file name (e.g. "sqlite.no").
	// Used for diagnostics and error reporting.
	Filename string

	// AllowAnonymousFnType controls whether anonymous function type syntax
	// (e.g. `cb ()()`) is permitted in parameter lists. When false, only
	// named function type aliases (e.g. `test-cb = ()` then
	// `f = (cb test-cb) {}`) are accepted. Defaults to false (zero value).
	AllowAnonymousFnType bool

	// SkipUnwrapLowering 控制是否跳過 UnwrapAssignStatement 的 lowering
	//（展開為 __unwrap_N 區塊）。預設 false：編譯器需要展開後的 IR。
	// `no fmt` 設定為 true，使 formatter 取得 surface AST 中的
	// UnwrapAssignStatement 節點，直接渲染 `?=` / `=`，避免輸出不可重解析的
	// __unwrap_N 區塊（見 src/fmt 與 no fmt 的 reparse/idempotency 測試）。
	SkipUnwrapLowering bool

	// SkipSafeIndexLowering 控制是否跳過安全索引的 lowering（#\{index-out=DEF\}
	// 展開為 match 區塊、option 回傳函式內裸 `x = v[i]` 自動上拋展開為 `x ?= v[i]`）。
	// 預設 false：編譯器需要展開後的 IR。
	// `no fmt` 設定為 true，使 formatter 取得 surface AST 中的 `x = v[i]` 與
	// 獨立行上的 `#{index-out = DEF}` 註解，直接渲染原始寫法，避免輸出不可重解析
	// 的 `__idx_out_N` 區塊（見 no fmt 的 reparse/idempotency 測試）。
	SkipSafeIndexLowering bool

	// LegacyBlockOverflowPropagation 啟用「區塊級 #{overflow=...}」的**遺留**
	// 傳播語意（propagateBlockScopedOverflow）。預設 false。
	//
	// 現行語意：`#{overflow=...}` 是**行注解**——只作用於其上緊跟的那一條陳述
	//（見 applyLineOverflowAnnotations），不向區塊其餘陳述或巢狀區塊傳播，
	// 函式/方法定義上方的註解也不再涵蓋整個函式體。這使註解所見即所得，
	// 不會被 formatter 去重折疊、也不會因傳播疏漏而靜默改變整數語意。
	//
	// 設為 true 可還原舊的區塊作用域行為，僅供遷移/校驗工具
	//（cmd/lineoverflow）以「傳播 ON」重解析原始碼、對照逐行注解是否等價。
	LegacyBlockOverflowPropagation bool
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

// skipFieldAnnotation returns the look-index of the token that follows the
// `#{...}` group beginning at look-index start (the `#{` itself), together with
// that token and the one after it. Newlines between the group and its member
// are skipped. ok is false when the group is unterminated or runs into EOF.
//
// Only look-ahead is used: no token is consumed, so classifyBlock stays a pure
// predicate.
func (p *Parser) skipFieldAnnotation(start int) (int, lexer.Token, lexer.Token, bool) {
	var zero lexer.Token
	const maxLook = 256
	i := start
	depth := 0
	for n := 0; n < maxLook; n++ {
		t := p.look(i)
		if t.Type == lexer.EOF {
			return 0, zero, zero, false
		}
		switch t.Type {
		case lexer.HASH_LBRACE:
			depth++
		case lexer.RBRACE:
			depth--
			if depth <= 0 {
				i++ // step past the closing `}`
				for m := 0; m < maxLook; m++ {
					t := p.look(i)
					if t.Type == lexer.EOF {
						return 0, zero, zero, false
					}
					if t.Type != lexer.NEWLINE {
						return i, t, p.look(i + 1), true
					}
					i++
				}
				return 0, zero, zero, false
			}
		}
		i++
	}
	return 0, zero, zero, false
}

// isFieldTypeStart reports whether tok can begin the type of a `name <type>`
// struct field. Used to confirm that a member following a leading field
// annotation really is a field and not a statement.
func isFieldTypeStart(t lexer.TokenType) bool {
	switch t {
	case lexer.IDENT, lexer.PTR, lexer.MUL, lexer.AND, lexer.QUESTION, lexer.LBRACKET:
		return true
	}
	return false
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
		tok := p.look(skip)
		if tok.Type != lexer.NEWLINE {
			break
		}
		skip++
	}
	tok1 := p.look(skip)
	tok2 := p.look(skip + 1)

	// The FIRST member of a struct may carry a field annotation:
	//
	//     holder {
	//         #{inline} p pt
	//     }
	//
	// tok1 is then HASH_LBRACE, which fell through to blockUnknown, so the whole
	// `{ ... }` was parsed as a block statement and the struct was never
	// registered ("'holder' is not defined"). Skip the annotation group and let
	// the annotated member decide. The skip is only accepted when the member
	// that follows has a field-declaration shape, so a statement block that
	// merely starts with an annotation is left untouched.
	if tok1.Type == lexer.HASH_LBRACE {
		if s, t1, t2, ok := p.skipFieldAnnotation(skip); ok {
			if t1.Type == lexer.IDENT && isFieldTypeStart(t2.Type) {
				skip, tok1, tok2 = s, t1, t2
			}
		}
	}

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
		// 首個成員為裸變體（`fail,`）：可能是 C 風格枚舉（blockEnum），也可能是
		// 「首變體為單元變體」的標籤列舉（如 `a-res { fail, ok(v i64) }`）。
		// 於深度 0 掃描是否存在「變體名後接 `(`」的成員來消歧。
		if p.blockHasParenVariant(skip) {
			return blockTaggedEnum
		}
		return blockEnum
	case lexer.ASSIGN:
		// enum 顯式賦值：Name { VARIANT = value, ... }
		return blockEnum
	case lexer.LPAREN:
		// 區分介面方法 (name(...)) 與帶括號載荷欄位的標籤列舉變體 (ok(v t), err(e str))。
		// 介面每個成員都是 `name(...)`；標籤列舉至少含一個「裸變體」（IDENT/NIL 後
		// 不接 `(`，如 `nil,`）。`#{buildin}` 前置註解亦強制視為標籤列舉。
		if p.blockIsTaggedEnumWithParens(skip) || p.blockIsTaggedEnumAllParens(skip) {
			return blockTaggedEnum
		}
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
		// Struct literal field with dot expression value: name : .field or name : obj.field
		// Match arms never have "name: .expr" form, so DOT → struct literal.
		if tok3.Type == lexer.DOT {
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
			// Struct literal field with nested struct literal value: name : TypeName{...}
			// Match arms never have "name: ident{" form, so LBRACE → struct literal.
			if tok4.Type == lexer.LBRACE {
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
	case lexer.MUL, lexer.AND, lexer.QUESTION:
		// Field type with a prefix operator: `f *T`, `f &T` (a VIEW borrow) or
		// `f ?T` / `f ?&T`. Without this case the block was classified
		// blockUnknown and the whole `{ ... }` was parsed as a block statement,
		// so the struct was never registered ("'holder' is not defined").
		// `&` is ambiguous with a match arm (`a & b -> ...`), so require the
		// prefix chain to be followed by a type name and then a field
		// terminator (NEWLINE / RBRACE / COMMA), which no match arm has.
		i := skip + 2
		for p.look(i).Type == lexer.MUL || p.look(i).Type == lexer.AND ||
			p.look(i).Type == lexer.QUESTION {
			i++
		}
		if t := p.look(i); t.Type != lexer.IDENT && t.Type != lexer.PTR {
			return blockMatch
		}
		switch p.look(i + 1).Type {
		case lexer.NEWLINE, lexer.RBRACE, lexer.COMMA:
			return blockStruct
		}
		return blockMatch
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

// blockIsTaggedEnumWithParens 判斷 `name { ... }` 區塊（首個成員形如 `name(...)`）是否
// 實為標籤列舉（變體帶括號載荷欄位），而非介面（方法）。於深度 0 掃描成員名：若出現
// IDENT/NIL 之後不接 `(` 的裸變體（如 `nil,`），即為標籤列舉。`#{buildin}` 前置註解
// 亦強制視為標籤列舉（內建列舉如 option 的載荷由 runtime 提供）。
// start 為 tok1（`{` 後首個非 NEWLINE token）的 look 索引。
func (p *Parser) blockIsTaggedEnumWithParens(start int) bool {
	for _, e := range p.pendingAnnotations {
		if e != nil && e.Key == "buildin" {
			return true
		}
	}
	depth := 0
	for i := start; i < start+200; i++ {
		t := p.look(i)
		switch t.Type {
		case lexer.EOF:
			return false
		case lexer.LPAREN, lexer.LBRACKET, lexer.LBRACE:
			depth++
		case lexer.RPAREN, lexer.RBRACKET:
			depth--
			if depth < 0 {
				return false
			}
		case lexer.RBRACE:
			if depth == 0 {
				return false // 區塊結束，未見裸變體
			}
			depth--
		case lexer.IDENT, lexer.NIL:
			if depth == 0 && p.look(i+1).Type != lexer.LPAREN {
				return true
			}
		}
	}
	return false
}

// blockHasParenVariant 於深度 0 掃描 `name { ... }` 區塊，判斷是否存在「變體名後接 `(`」
// 的成員（如 ok(v i64)）。用於把首個成員為裸變體的區塊（`fail, ok(v i64)`）從
// C 風格枚舉（blockEnum）糾正為標籤列舉（blockTaggedEnum）。
// start 為 tok1 的 look 索引（可為 -1，對應 peekToken）。
func (p *Parser) blockHasParenVariant(start int) bool {
	depth := 0
	for i := start; i < start+200; i++ {
		t := p.look(i)
		switch t.Type {
		case lexer.EOF:
			return false
		case lexer.LPAREN:
			if depth == 0 && i-1 >= -1 {
				if prev := p.look(i - 1); prev.Type == lexer.IDENT || prev.Type == lexer.NIL {
					return true
				}
			}
			depth++
		case lexer.LBRACKET, lexer.LBRACE:
			depth++
		case lexer.RPAREN, lexer.RBRACKET:
			depth--
			if depth < 0 {
				return false
			}
		case lexer.RBRACE:
			if depth == 0 {
				return false
			}
			depth--
		}
	}
	return false
}

// blockIsTaggedEnumAllParens 判斷「所有成員皆為 name(...) 形態」的區塊是標籤列舉還是介面
// （如 `shape { circle(r f64), rect(w f64, h f64) }` vs `enter { enter() }`）。
// 消歧規則（有界確定性前瞻，使用者無需多寫任何東西）：
//   - 任一成員在 `)` 後緊跟 `(`（結果組，如 `t.gt(b t) (res bool)`）→ 介面；
//   - 否則若至少一個成員的參數列表非空（如 `circle(r f64)`）→ 標籤列舉；
//   - 否則（全部為空參數 `name()`）→ 介面。
//
// 即：無結果組的介面方法請使用空參數列表（std 中僅有的介面 enter()/leave() 即如此）。
func (p *Parser) blockIsTaggedEnumAllParens(start int) bool {
	hasNonEmptyParams := false
	for i := start; i < start+200; i++ {
		t := p.look(i)
		switch t.Type {
		case lexer.EOF, lexer.RBRACE:
			return hasNonEmptyParams
		case lexer.LPAREN:
			prev := lexer.Token{}
			if i-1 >= -1 {
				prev = p.look(i - 1)
			}
			if prev.Type != lexer.IDENT {
				continue
			}
			if p.look(i+1).Type != lexer.RPAREN {
				hasNonEmptyParams = true
			}
			// 找匹配的 `)`；其後若緊跟 `(` 則為介面的結果組。
			d := 0
			for k := i; k < i+200; k++ {
				switch p.look(k).Type {
				case lexer.LPAREN:
					d++
				case lexer.RPAREN:
					d--
					if d == 0 {
						if p.look(k+1).Type == lexer.LPAREN {
							return false
						}
						i = k
						goto nextMember
					}
				case lexer.EOF:
					return hasNonEmptyParams
				}
			}
		nextMember:
		}
	}
	return hasNonEmptyParams
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
	case lexer.NOT:
		// Bare match arm starting with !expr (negated condition),
		// e.g. { ! fs.is-file(filename) -> ... }
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
		// 同 classifyBlock：首個成員為裸變體時，掃描是否存在帶括號載荷變體來區分
		// C 風格枚舉與標籤列舉。
		if p.blockHasParenVariant(base) {
			return blockTaggedEnum
		}
		return blockEnum
	case lexer.ASSIGN:
		// enum 顯式賦值：Name { VARIANT = value, ... }
		return blockEnum
	case lexer.LPAREN:
		if p.blockIsTaggedEnumWithParens(base) || p.blockIsTaggedEnumAllParens(base) {
			return blockTaggedEnum
		}
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
		// Struct literal field with dot expression value: name : .field or name : obj.field
		// Match arms never have "name: .expr" form, so DOT → struct literal.
		if tok3.Type == lexer.DOT {
			return blockStruct
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
			// Struct literal field with nested struct literal value: name : TypeName{...}
			// Match arms never have "name: ident{" form, so LBRACE → struct literal.
			if tok4.Type == lexer.LBRACE {
				return blockStruct
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
	semVarTypes     map[string]string   // snapshot of variable type table
	semEnumVariants map[string][]string // snapshot of enum variant table
	semDeclaredVars map[string]bool     // snapshot of declared variable set
	semFuncVarTypes     map[string]map[string]string // snapshot of per-function var types
	semFuncDeclaredVars map[string]map[string]bool   // snapshot of per-function declared vars
}

func New(lx *lexer.Lexer) *Parser {
	p := &Parser{
		lexer:           lx,
		tk:              make([]lexer.Token, tokenBufferSize),
		cur:             -1,
		peek:            -1,
		ctx:             contextStack{CTX_GLOBAL},
		warnedSemiEat:   map[int]bool{},
		reportedIllegal: map[string]bool{},
		sem:             NewSemanticContext(),
	}

	p.nextToken()

	return p
}

// setVarType 記錄變數宣告型別到 varDeclTypes。
// varDeclTypes 在 New() 中已初始化，此處無需 nil 檢查。
// 集中管理寫入點，避免散佈的 lazy init 模式。
// When inside a function body (curFuncName != ""), also records in the
// per-function FuncVarTypes map to prevent same-named locals in different
// functions from colliding during lowering.
func (p *Parser) setVarType(name, typ string) {
	p.sem.SetVarType(name, typ)
	if p.curFuncName != "" {
		p.sem.SetFuncVarType(p.curFuncName, name, typ)
	}
}

// SetExternSignatures 注入外部（跨文件）函數簽名和 struct 欄位型別，
// 供 parseLetStatement 的型別推斷使用。由 transpiler 在解析前呼叫。
func (p *Parser) SetExternSignatures(funcSigs map[string][]string, methodSigs map[string][]string, structFields map[string]map[string]string) {
	if p.funcSignatures == nil {
		p.funcSignatures = make(map[string][]string)
	}
	for k, v := range funcSigs {
		p.funcSignatures[k] = v
	}
	if p.methodSignatures == nil {
		p.methodSignatures = make(map[string][]string)
	}
	for k, v := range methodSigs {
		p.methodSignatures[k] = v
	}
	if p.structFields == nil {
		p.structFields = make(map[string]map[string]string)
	}
	for k, v := range structFields {
		p.structFields[k] = v
	}
}

// SetExternEnumVariants 注入外部（跨文件）枚舉型別的變體名列表，
// 供 match desugar 使用，使跨模組枚舉 match 能正確識別型別。
// 由 transpiler 在解析前呼叫。
func (p *Parser) SetExternEnumVariants(enumVariants map[string][]string) {
	if p.sem.EnumVariants == nil {
		p.sem.EnumVariants = make(map[string][]string)
	}
	for k, v := range enumVariants {
		if _, exists := p.sem.EnumVariants[k]; !exists {
			p.sem.EnumVariants[k] = v
		}
	}
}

func (p *Parser) saveState() parserState {
	commentsCopy := make([]lexer.Token, len(p.comments))
	copy(commentsCopy, p.comments)
	// 深拷貝類型推斷符號表（位於語義副表 sem 內），避免試探性解析中的寫入
	// 在 restoreState 後污染後續解析。
	varDeclTypesCopy := make(map[string]string, len(p.sem.VarTypes))
	for k, v := range p.sem.VarTypes {
		varDeclTypesCopy[k] = v
	}
	enumVariantsCopy := make(map[string][]string, len(p.sem.EnumVariants))
	for k, v := range p.sem.EnumVariants {
		cp := make([]string, len(v))
		copy(cp, v)
		enumVariantsCopy[k] = cp
	}
	declaredVarsCopy := make(map[string]bool, len(p.sem.DeclaredVars))
	for k, v := range p.sem.DeclaredVars {
		declaredVarsCopy[k] = v
	}
	// Deep copy per-function var types
	funcVarTypesCopy := make(map[string]map[string]string, len(p.sem.FuncVarTypes))
	for fn, vars := range p.sem.FuncVarTypes {
		cp := make(map[string]string, len(vars))
		for k, v := range vars {
			cp[k] = v
		}
		funcVarTypesCopy[fn] = cp
	}
	// Deep copy per-function declared vars
	funcDeclaredVarsCopy := make(map[string]map[string]bool, len(p.sem.FuncDeclaredVars))
	for fn, vars := range p.sem.FuncDeclaredVars {
		cp := make(map[string]bool, len(vars))
		for k, v := range vars {
			cp[k] = v
		}
		funcDeclaredVarsCopy[fn] = cp
	}
	return parserState{
		cur:                 p.cur,
		peek:                p.peek,
		prevToken:           p.prevToken,
		ctx:                 p.ctx.copy(),
		comments:            commentsCopy,
		semVarTypes:         varDeclTypesCopy,
		semEnumVariants:     enumVariantsCopy,
		semDeclaredVars:     declaredVarsCopy,
		semFuncVarTypes:     funcVarTypesCopy,
		semFuncDeclaredVars: funcDeclaredVarsCopy,
	}
}

// lexState 是純前瞻（lookahead）用的輕量狀態快照：只記錄 token 游標與
// comments/ctx 的長度，不深拷貝語義符號表。僅可用於「保證不寫入 sem 表、
// 且 context push/pop 平衡」的掃描式判定（如 isFunctionDefinition）。
type lexState struct {
	cur       int
	peek      int
	prevToken lexer.Token
	nComments int
	ctxDepth  int
}

func (p *Parser) saveLexState() lexState {
	return lexState{
		cur:       p.cur,
		peek:      p.peek,
		prevToken: p.prevToken,
		nComments: len(p.comments),
		ctxDepth:  len(p.ctx),
	}
}

func (p *Parser) restoreLexState(s lexState) {
	p.cur = s.cur
	p.peek = s.peek
	p.currentToken = p.tokAt(s.cur)
	p.peekToken = p.tokAt(s.peek)
	p.prevToken = s.prevToken
	if len(p.comments) > s.nComments {
		p.comments = p.comments[:s.nComments]
	}
	if len(p.ctx) > s.ctxDepth {
		p.ctx = p.ctx[:s.ctxDepth]
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
	p.sem.VarTypes = state.semVarTypes
	p.sem.EnumVariants = state.semEnumVariants
	p.sem.DeclaredVars = state.semDeclaredVars
	p.sem.FuncVarTypes = state.semFuncVarTypes
	p.sem.FuncDeclaredVars = state.semFuncDeclaredVars
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
	case *MultiAssignStatement:
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
	case *MultiAssignStatement:
		if s.Value != nil {
			return s.Value.EndPos().Line
		}
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
	case *BlockStatement:
		// 裸 `{ ... }` guard 區塊（statement 位置、無條件）在 parseStatement 的
		// LBRACE 分支直接回傳 BlockStatement；setDoc 若不認識它，區塊上方的
		// doc 註釋會被丟棄（如 path.no 的 `; '.' 必須在 '/' 之後`）。
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
	case *MultiAssignStatement:
		s.Doc = doc
	case *UnwrapAssignStatement:
		// `v ?= expr` 的前置註釋也要掛上，否則 fmt 會把註釋吞掉。
		s.Doc = doc
	case *TypeAlias:
		s.Doc = doc
	case *AnnotationStatement:
		s.Doc = doc
		if os.Getenv("NOLANG_FMTDBG") != "" && doc != nil {
			fmt.Fprintf(os.Stderr, "[DBG setDoc] AnnotationStatement got doc=%q\n", doc.List[0].Text)
		}
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

func dbgLeakState(tag string, prog *Program) {
	bodyLen := -1
	topLets := 0
	for _, st := range prog.Statements {
		if fd, ok := st.(*FunctionDefinition); ok && fd.Name == "txt.from-hex" && fd.Body != nil {
			bodyLen = len(fd.Body.Statements)
		}
		if ls, ok := st.(*LetStatement); ok && ls.Name != nil {
			if ls.Name.Value == "n" || ls.Name.Value == "out2" || ls.Name.Value == "out" {
				topLets++
			}
		}
	}
	fmt.Fprintf(os.Stderr, "[debug-self][%s] from-hex bodyLen=%d topLeakedLets=%d totalTop=%d\n", tag, bodyLen, topLets, len(prog.Statements))
}

func (p *Parser) ParseProgram() *Program {
	program := &Program{Statements: []Statement{}}
	for p.currentToken.Type != lexer.EOF {
		doc := p.collectDocComments()
		stmt := p.recoverStatement()
		if stmt != nil {
			setDoc(stmt, doc)
			p.attachInlineComment(stmt)
			if os.Getenv("NOLANG_DEBUG_SELF") != "" && strings.Contains(p.Filename, "txt") {
				nm := ""
				switch v := stmt.(type) {
				case *FunctionDefinition:
					nm = v.Name
				case *LetStatement:
					if v.Name != nil {
						nm = v.Name.Value
					}
				}
				fmt.Fprintf(os.Stderr, "[debug-self][parseLoop] %T name=%q bodyLen=%d\n", stmt, nm, func() int {
					if fd, ok := stmt.(*FunctionDefinition); ok && fd.Body != nil {
						return len(fd.Body.Statements)
					}
					return -1
				}())
			}
			program.Statements = append(program.Statements, stmt)
		}
		// 交付多載掃描中已消費的後續同名定義（見 parseAnnotationStatement）。
		if len(p.pendingOverloadDefs) > 0 {
			program.Statements = append(program.Statements, p.pendingOverloadDefs...)
			p.pendingOverloadDefs = nil
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

	// 語義副表：連接 parser 增量推斷結果，並執行獨立 Resolver pass
	// （平台鍵/泛型參數/embed 由註解收尾計算），實現解析/语义分离。
	program.Sem = p.sem
	if os.Getenv("NOLANG_DEBUG_SELF") != "" && strings.Contains(p.Filename, "txt") {
		dbgLeakState("PRE-Resolve", program)
	}
	ResolveProgram(program)
	if os.Getenv("NOLANG_DEBUG_SELF") != "" && strings.Contains(p.Filename, "txt") {
		dbgLeakState("POST-Resolve", program)
	}

	// Lowering pass：將解析期產出的表層 match 節點（SurfaceMatch）展開為核心
	// AST（IfExpression 鏈）。必須在 Resolver 之後執行，因為 desugar 需要
	// sem 中的類型推斷結果；也必須在拷貝 Warnings 之前執行。
	if os.Getenv("NOLANG_DEBUG_IT") != "" {
		fmt.Fprintf(os.Stderr, "[debug-it] ParseProgram about to lower: %d statements, filename=%s\n", len(program.Statements), p.Filename)
	}
	p.lowerProgram(program)
	if os.Getenv("NOLANG_DEBUG_SELF") != "" && strings.Contains(p.Filename, "txt") {
		dbgLeakState("POST-lower", program)
	}

	// 將「安全索引專用」本地型別表（idxLocalTypes）掛到語義副表匯出，供
	// checker 的 ValidateUnhandledIndex 複現 isSafeIndexBase 的型別查詢（parser
	// 實例在 ParseProgram 返回後即被丟棄）。
	program.Sem.IdxLocalTypes = p.idxLocalTypes

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
	// Deduplicate: skip if the same error at the same position was already reported.
	// This prevents duplicate errors when restoreState retries parsing after a failed attempt.
	for _, d := range p.diags {
		if d.Pos == pos && d.Message == clean {
			return
		}
	}
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

// saveWarningWithCode 记录一条带指定诊断码的警告（与 saveWarning 相同，但允许
// 指定 Code，便于按码过滤/选择性输出，如高风险的 W_CHAIN_IF）。
func (p *Parser) saveWarningWithCode(code, msg string) {
	pos, clean := stripLocPrefix(msg)
	p.diags = append(p.diags, Diagnostic{
		Filename: p.Filename,
		Pos:      pos,
		Severity: SeverityWarning,
		Code:     code,
		Message:  clean,
	})
}

func (p *Parser) Warnings() []string {
	return formatDiags(p.diags, SeverityWarning)
}

// WarnSemiSwallow 是「單個 ; 行尾註釋疑似吞掉代碼」警告的穩定診斷碼。
const WarnSemiSwallow = "W_SEMI_EAT"

// WarnChainedIf 是「链式 -> 条件（a -> b -> c）」不推荐写法的警告诊断码。
// 链式 if 容易产生微妙的缩域/作用域问题（曾导致 str.replace-n 静默生成错误
// 代码：尾部语句被错误并入条件块），建议把多个条件用 &&/|| 合并，或改用 { }
// 块写法。lint 提示，不影响编译（修复 parser/lowering 后链式 -> 已能正确嵌套）。
const WarnChainedIf = "W_CHAIN_IF"

// WarningsByCode 返回指定診斷碼的警告訊息（格式化後）。
// 供編譯入口（transpiler/builder）選擇性輸出高危警告（如 W_SEMI_EAT）。
func (p *Parser) WarningsByCode(code string) []string {
	var out []string
	for _, d := range p.diags {
		if d.Severity == SeverityWarning && d.Code == code {
			out = append(out, d.Error())
		}
	}
	return out
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
