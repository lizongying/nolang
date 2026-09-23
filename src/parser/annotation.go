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
// annotationImmediatelyTrails 判斷當前 token（呼叫時必為 `#{`）是否為「尾隨註解」
// ——即緊跟在「同一行」的前一個非註釋 token 之後。
//
// 為何用 prevToken 的行號，而不是陳述的 Pos()/EndPos()：兩者都不等於「陳述真正
// 結束的那一行」。
//   - Pos() 是陳述的**第一行**。多行陳述（值跨行、區塊收尾的 `}` 自成一行）的尾隨
//     註解與它不同行，於是被誤判為「下一條陳述的前置註解」：`#{index-out=0}` 被
//     套用到錯誤目標（該行越界索引仍被當成未處理），LSP 的「Add #{index-out = 0}」
//     quickfix 追加在行尾的註解也就形同無效。
//   - EndPos() 對呼叫表達式只回到**最後一個引數**（CallExpression.EndPos），
//     所以 `f(\n a,\n b\n) #{...}` 的 `)` 自成一行時仍對不上。
//
// prevToken 由 nextToken 維護，且 advanceCollect 會跳過 COMMENT（註釋只進
// p.comments 緩衝），故 prevToken 恆為前一個**非註釋** token——正是陳述的最後一個
// token。NEWLINE/EOF 一律不算（換行後的 `#{` 是「獨立成行置於目標上方」的前置註解，
// 見 annotationPrefixIllegal 的說明）。
func (p *Parser) annotationImmediatelyTrails() bool {
	if p.currentToken.Type != lexer.HASH_LBRACE {
		return false
	}
	switch p.prevToken.Type {
	case lexer.NEWLINE, lexer.EOF, lexer.ILLEGAL:
		return false
	}
	return p.prevToken.Line == p.currentToken.Line
}

// parseTrailingAnnotation 解析緊跟在陳述句之後的尾隨 #{...} 註解（同一行），
// 回傳註解條目。呼叫方負責將其附加到剛解析的陳述句（parseBlockStatement 會
// 在解析完 stmt 後呼叫）。這支援 `x = v[5] #{index-out=0}` 這類尾隨註解語法，
// 否則尾隨 #{...} 會被 parseAnnotationStatement 當成孤立註解陳述句而遺失。
// 連續的尾隨 #{...}（以空白分隔）會合併為同一組條目。
func (p *Parser) parseTrailingAnnotation() []*AnnotationEntry {
	// currentToken 應為 HASH_LBRACE (#{)
	if p.currentToken.Type != lexer.HASH_LBRACE {
		return nil
	}
	var entries []*AnnotationEntry
	for p.currentToken.Type == lexer.HASH_LBRACE {
		p.nextToken() // skip #{
		more := p.parseAnnotationBody()
		if p.currentToken.Type != lexer.RBRACE {
			msg := fmt.Sprintf("line %d, column %d: expected '}' to close annotation, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			return entries
		}
		p.nextToken() // skip }
		for _, e := range more {
			if e != nil {
				e.Trailing = true
			}
		}
		entries = append(entries, more...)
	}
	// 收斂同名註解：同一行連續的尾隨 #{...}（如 `x = v[i] #{index-out=0} #{index-out=1}`）
	// 被合併為同一組條目，依 key 去重、取最後一次（後面的替換前面的）。
	return dedupeAnnotationEntries(entries)
}

// annotationPrefixIllegal 報告註解群組結尾 `}` 之後、同一行上是否還有程式碼。
//
// nolang 的註解位置規則：`#{...}` 只允許「獨立成行置於目標上方」或「寫在目標
// 同一行後方（尾隨）」兩種寫法。兩者的 `}` 之後在同一行都只會接換行（尾隨註解
// 其後只可能有換行、`;` 行註釋，或另一個註解群組）。若 `}` 之後同一行仍有程式碼，
// 那就是「目標前方同一行」的前綴寫法——非法位置，必須報錯。
func annotationPrefixIllegal(t lexer.TokenType) bool {
	switch t {
	case lexer.NEWLINE, lexer.RBRACE, lexer.EOF, lexer.SEMICOLON, lexer.HASH_LBRACE:
		return false
	}
	return true
}

// errPrefixAnnotation 對「寫在目標前方同一行」的註解群組報錯。訊息維持
// "line %d, column %d: ..." 格式，LSP 的 parseErrorToDiagnostic 才能定位
// （見 lsp/server.go），編譯與 LSP 因此共用同一條規則。
func (p *Parser) errPrefixAnnotation(tok lexer.Token) {
	p.saveError(fmt.Sprintf("line %d, column %d: `#{...}` 註解不能寫在目標前方同一行；請獨立成行置於目標上方，或寫在目標同一行尾隨",
		tok.Line, tok.Column))
}

// skipToBlockOpeningBrace 跳過當前 token 直到左花括號（或 EOF），用於
// `if <cond> { ... }` / `else { ... }` 這種「條件與 `{` 之間」的掃描。
//
// 這個掃描是寬鬆的：它會把條件與 `{` 之間的任何 token 整段吞掉。`#{...}` 註解
// 正好落在那裡時（`if x > 0 #{overflow = wrap} {`）會被完全吞掉——註解既不生效
// 也不報錯，是最難察覺的一種失效。該位置既不在目標上方、也不在目標同一行後方，
// 屬於「前方同一行」的一種，因此比照其他呼叫點報錯。
//
// 迴圈的結束條件刻意與原寫法完全一致（第一個 LBRACE 就停），避免改變既有的
// 寬鬆行為；註解內若出現巢狀 `{`（例如字串以外的結構字面量），depth 不會歸零，
// 也就不會誤報。
func (p *Parser) skipToBlockOpeningBrace() {
	depth := 0
	var annotTok lexer.Token
	for p.currentToken.Type != lexer.LBRACE && p.currentToken.Type != lexer.EOF {
		switch p.currentToken.Type {
		case lexer.HASH_LBRACE:
			if depth == 0 {
				annotTok = p.currentToken
			}
			depth++
		case lexer.RBRACE:
			if depth > 0 {
				depth--
				if depth == 0 && annotationPrefixIllegal(p.peekToken.Type) {
					p.errPrefixAnnotation(annotTok)
				}
			}
		}
		p.nextToken()
	}
}

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
	// 位置檢查：`}` 之後同一行還有程式碼 → 「目標前方同一行」的前綴寫法。
	if annotationPrefixIllegal(p.currentToken.Type) {
		p.errPrefixAnnotation(annotToken)
	}

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
		groupTok := p.currentToken
		p.nextToken() // skip #{
		moreEntries := p.parseAnnotationBody()
		if p.currentToken.Type != lexer.RBRACE {
			msg := fmt.Sprintf("line %d, column %d: expected '}' to close annotation, got %s instead",
				p.currentToken.Line, p.currentToken.Column, p.currentToken.Type.String())
			p.saveError(msg)
			break
		}
		p.nextToken() // skip }
		if annotationPrefixIllegal(p.currentToken.Type) {
			p.errPrefixAnnotation(groupTok)
		}
		entries = append(entries, moreEntries...)
		annotStmt.Entries = entries
	}
	// 收斂同名註解：連續的 #{...} 群組（以空行分隔）被解析期合併為同一節點，
	// 若其中出現同名鍵（如 `#{index-out=0} #{index-out=1}` 或跨行連續
	// `#{index-out=0}` / `#{index-out=1}`），依 key 去重、取最後一次
	// （後面的替換前面的），避免輸出 `#{index-out=0, index-out=1}` 且讓
	// desugar 讀到正確（最後一個）的預設值。
	entries = dedupeAnnotationEntries(entries)
	annotStmt.Entries = entries
	// 取出「註解行之後、目標陳述之前」的註釋：它們夾在註解與目標之間，屬於同一個
	// node。輸出規範為「註釋 → 註解 → 陳述」，故下方把它們掛成目標陳述的 Doc
	// （formatter 先印 Doc、再印附加註解），而不是留給下一條陳述（註釋會整條搬到
	// 下一條陳述上、順序也反了）。
	//
	// 判定必須用行號而非「進入本函式時的緩衝長度」：nextToken 為了算出 peekToken
	// 會預先收集更前方的註釋（見 advanceCollect），跳過註解群組的 `}` 那一步就已
	// 把下一行的註釋收進緩衝，以長度為基準會永遠取不到。
	between := p.takeCommentsBetween(annotToken.Line, p.currentToken.Line)
	restoreBetween := func() {
		if len(between) == 0 {
			return
		}
		// 目標不是可附加的陳述（獨立 AnnotationStatement / 解析失敗）時原樣歸還，
		// 保持「註釋屬於後續陳述的 Doc」的既有行為。
		p.comments = append(append([]lexer.Token{}, between...), p.comments...)
		between = nil
	}

	// 若後續為 IDENT 開頭的宣告，附加註解。
	// 亦含 IN（`in` 為關鍵字但常被當作參數名，如 `in []byte`）：形如
	//   #{overflow = wrap}
	//   in.len() == 0 -> { ... }
	// 若不把 IN 視為可附加，註解會退化成獨立 AnnotationStatement，後續
	// `in.len() == 0 -> ...` 的解析走 blockUnknown 分支，`in` 被吞掉，
	// 格式化輸出變成 `.len() == 0 -> ...`（語意破壞、非冪等）。
	if p.currentToken.Type == lexer.IDENT ||
		p.currentToken.Type == lexer.IN ||
		p.currentToken.Type == lexer.RARROW ||
		(p.currentToken.Type == lexer.LBRACKET && p.isArrayTypeMethodDefinition()) {
		// RARROW 分支處理 wildcard 裸配對臂（`-> body`）：
		// 形如 `#{overflow = wrap}\n -> out[pos] = d - 10 + 97` 的註解
		// 必須附加到後方的 `-> body` 陳述，否則該臂整數運算會退回預設
		// option 模式，產生 %option 後被 trunc 到窄型別而讓 LLVM 報錯。
		p.pendingAnnotations = entries
		stmt := p.parseStatement()
		if stmt != nil {
			p.attachAnnotations(stmt, entries)
			// 註解行與目標陳述之間的註釋屬於同一 node，掛成目標陳述的 Doc
			// （輸出規範：註釋 → 註解 → 陳述）。
			if len(between) > 0 {
				setDoc(stmt, commentGroupFromTokens(between))
				between = nil
			}
			p.pendingAnnotations = nil
			// 將註解同時標記到緊接其後的「同名多載」函式定義。nolang 以連續
			// `name = ...` 表達 arity 多載（如 global.no 的
			//   #{buildin=format, intrinsic}
			//   format = () { }
			//   format = (s str) (out str) { }
			// ），若只標記首個定義，其餘多載（帶命名返回參數的變體）會被當成
			// 普通函式，誤觸發「返回值未賦值」等校驗。這裡掃描後續同名 IDENT
			// 定義並為其標記 BuiltinGroup（僅供校驗跳過，不設 BuiltinStub，
			// 以免 stripBuiltinStubs 將其從 codegen 移除而缺失符號定義）。
			if fd, ok := stmt.(*FunctionDefinition); ok {
				for {
					for p.currentToken.Type == lexer.NEWLINE {
						p.nextToken()
					}
					if p.currentToken.Type != lexer.IDENT || p.currentToken.Literal != fd.Name {
						break
					}
					ov := p.parseStatement()
					if ov == nil {
						break
					}
					// 僅當後續同名陳述確實是同名函式定義時才視為多載並繼承標記
					// （BuiltinGroup 僅供校驗跳過，不設 BuiltinStub，以免
					// stripBuiltinStubs 將其從 codegen 移除而缺失符號定義）。
					if ovd, isFn := ov.(*FunctionDefinition); isFn && ovd.Name == fd.Name {
						ovd.BuiltinGroup = true
						p.pendingOverloadDefs = append(p.pendingOverloadDefs, ov)
						continue
					}
					// 非同名函式定義（如同名呼叫陳述 `format(...)`）：已消費的
					// 語句原樣保留交付，避免語句遺失；並終止多載掃描。
					p.pendingOverloadDefs = append(p.pendingOverloadDefs, ov)
					break
				}
			}
			return stmt
		}
		p.pendingAnnotations = nil
		restoreBetween()
	}

	restoreBetween()
	p.skipToStatementEnd()
	return annotStmt
}

// attachAnnotations 將解析期收集的 #{...} 註解條目記錄到語義副表（side-table）。
// 不再掛載到 AST 節點上；平台鍵/泛型參數/embed 由獨立 Resolver pass
// （ResolveProgram）收尾計算並存入 side-table。
func (p *Parser) attachAnnotations(stmt Statement, entries []*AnnotationEntry) {
	// 收斂同名註解：同一陳述上可能因尾隨/前置多個 #{...} 而帶入同名鍵，
	// 依 key 去重、取最後一次（後面的替換前面的），避免重複印出且讓
	// desugar 讀到正確的預設值。
	entries = dedupeAnnotationEntries(entries)
	// 註解位置規則（統一）：`#{...}` 只允許
	//  1) 獨立成行、置於目標上方（`#{...}` ⏎ 目標），或
	//  2) 寫在目標同一行的後方（尾隨，`目標 #{...}`）。
	// 「目標前方同一行」的前綴寫法（如 `#{index-out=0} x = v[5]`）在解析當下即
	// 報錯（見 annotationPrefixIllegal 的三個呼叫點：parseAnnotationStatement、
	// parseStructDefinition、match 臂首），故這裡對同一行的尾隨註解一律放行——
	// `#{index-out = ...}` 也不例外（尾隨寫法與 LSP 的 quickfix 一致）。
	p.sem.SetRawAnnotations(stmt, entries)
	// 同步將 #{overflow = wrap|clamp0} 攜帶到陳述節點的 OverflowMode 欄位（與
	// applyLineOverflowAnnotations → setStmtOverflowMode 對「獨立 AnnotationStatement
	// 下一條陳述」的處理一致）。原因：merged/lowered 路徑下各標準庫模組由不同 Parser
	// 實例各自解析，註解只寫入「該次解析」的 p.sem side-table；而 ValidateUnhandledOverflow
	// 走 program.Sem 遍歷合併後的 AST，二者常非同一語意實例，導致側表查不到（rawN=0）而
	// 把已標註的整數運算誤報為未處理溢出。節點欄位不依賴 side-table，能在解析期直接隨
	// AST 流轉，徹底消除這類「行注解已存在卻被誤報」的沉默假陽性；同時讓 HIR 重建後
	// 仍能還原模式（g.sem 為 nil 時同樣適用）。formatter 讀取的是 side-table
	// （attachedAnnotations），不讀此欄位，故不會重複印出 #{overflow=...}。
	// 注意：前置註解附加到「緊鄰其後的陳述」，對 `cond -> body` 這類單條件陳述，附加對象
	// 是外層 ExpressionStatement（包住 IfExpression），其臂體內整數運算靠本檢查的
	// enclosingOverflow 由外層欄位向臂體傳遞，故不需逐條處理臂體。
	if mode := p.overflowModeString(entries); mode != "" {
		setStmtOverflowMode(stmt, mode)
	}
	// 同步將 #{overflow = wrap|clamp0} 攜帶到 FunctionDefinition 節點欄位，
	// 使單態化複本能繼承（HIR 模式下 side-table 拷貝無效）。
	if fd, ok := stmt.(*FunctionDefinition); ok {
		for _, e := range entries {
			switch {
			case e.Key == "overflow" && e.Value != nil:
				var raw string
				switch v := e.Value.(type) {
				case *AnnotationIdentValue:
					raw = v.Value
				case *AnnotationStringValue:
					raw = v.Value
				}
				if m := NormalizeOverflowMode(raw); m != "" {
					fd.OverflowMode = m
				}
		case e.Key == "intrinsic":
			// #{intrinsic} 標記函式的命名返回參數由 codegen / 内建 /
			// 出參引用在 nolang 源碼之外賦值，跳過未賦值檢查。
			fd.Intrinsic = true
		case e.Key == "buildin":
			// #{buildin} 標記此函式為標準庫內建聲明：真實實作位於 Go
			// runtime，nolang 函式體不參與校驗與 codegen（編譯器遇到此標記直接
			// 跳過）。函式名即為 Go 側 BuiltinMethod 的鍵。
			fd.BuiltinStub = true
			if v, ok := e.Value.(*AnnotationIdentValue); ok {
				fd.BuiltinName = v.Value
			} else if v, ok := e.Value.(*AnnotationStringValue); ok {
				fd.BuiltinName = v.Value
			}
		}
		}
	}
	// #{buildin} 標註標籤列舉：變體用於匹配/窮盡性檢查，底層表示與構造由
	// runtime/builtin 提供，不產生使用者可見的 struct/union（如 option 的載荷
	// 由 builtin 的 %option 承載）。
	if ted, ok := stmt.(*TaggedEnumDefinition); ok {
		for _, e := range entries {
			if e.Key == "buildin" {
				ted.Builtin = true
			}
		}
	}
}

// applyLineOverflowAnnotations 實作 `#{overflow = ...}` 的「行注解」語意：一個
// 獨立的 AnnotationStatement 只把其 overflow 模式套用到**緊跟其後的下一條陳述**
//（若該陳述尚未自帶 overflow 註解），不向區塊其餘陳述或巢狀區塊傳播。
//
// 之所以需要此 pass：`#{overflow=wrap}` 緊跟的陳述可能以無法被
// parseAnnotationStatement 附加的 token 開頭（如 `.n = .n + 1` 以 DOT 開頭、
// `return x + 1` 以 RETURN 開頭），此時註解會退化成獨立 AnnotationStatement，
// 而其 overflow 條目不會出現在目標陳述的 side-table 上，codegen 逐條陳述讀取
// 溢出模式（overflowModeFromNode）就會漏掉。本 pass 把該模式合併到下一條陳述。
func (p *Parser) applyLineOverflowAnnotations(block *BlockStatement) {
	if block == nil {
		return
	}
	apply := func(stmts []Statement) {
		for i, s := range stmts {
			as, ok := s.(*AnnotationStatement)
			if !ok {
				continue
			}
			entries := p.overflowEntries(as.Entries)
			if entries == nil {
				continue
			}
			mode := p.overflowModeString(entries)
			for j := i + 1; j < len(stmts); j++ {
				next := stmts[j]
				if next == nil {
					continue
				}
				// 連續的獨立註解：若後一條**也帶 overflow**，以最後一條為準
				//（前一條不覆蓋後一條的目標）。但若後一條與 overflow 無關
				//（如緊跟在 `#{overflow=wrap}` 之後的 `#{index-out=0}`），
				// 不能就此中斷——否則 `#{overflow=wrap}` + `#{index-out=0}` 這種
				// std 常見組合會讓 overflow 註解完全失效（陳述仍被
				// ovf-int-default 誤報）。此時略過該註解行，繼續找真正的目標陳述。
				if as2, isAnn := next.(*AnnotationStatement); isAnn {
					if p.overflowEntries(as2.Entries) != nil {
						break
					}
					continue
				}
				// 行注解語意：overflow 模式只寫入下一條陳述的 OverflowMode 欄位
				//（供 codegen 的 overflowModeFromNode 讀取），**不**合併進其註解
				// side-table——否則 formatter 會把同一行 #{overflow=...} 既以獨立
				// 註解陳述、又以附加註解形式各印一次（雙印，破壞冪等）。
				// 註解行的輸出由獨立 AnnotationStatement 節點本身負責（見
				// fmt/stmt.go formatAnnotationStatement）；IDENT 起始、parser 已
				// 直接附加到該陳述的情況則由 attachedAnnotations 輸出，二者皆單次。
				setStmtOverflowMode(next, mode)
				break
			}
		}
	}
	apply(block.Statements)
	// 巢狀：ForStatement 體 / 直接子區塊各自處理（遞迴由 parseBlockStatement
	// 對每個區塊自身呼叫本函式完成，此處僅覆蓋解析時未經 parseBlockStatement
	// 的內聯體，如單陳述迴圈體）。
	for _, s := range block.Statements {
		switch v := s.(type) {
		case *ForStatement:
			if v.Body != nil {
				apply(v.Body.Statements)
			}
		}
	}
}

// applyLineIndexOutAnnotations 實作 `#{index-out = ...}` 的「行注解」語意：一個
// 獨立的 AnnotationStatement 只把其 index-out 條目套用到**緊跟其後的下一條陳述**
//（若該陳述尚未自帶 index-out 註解），不向區塊其餘陳述或巢狀區塊傳播。
//
// 之所以需要此 pass：`#{index-out=0}` 緊跟的陳述可能以無法被
// parseAnnotationStatement 附加的 token 開頭（如 `return arr[i]` 以 RETURN 開頭、
// `.[i] = x` 以 DOT 開頭），此時註解會退化成獨立 AnnotationStatement，而其
// index-out 條目不會出現在目標陳述的 side-table 上，desugar 讀取 AnnotationsOf
// 就會漏掉、把已標註的越界索引誤報為未處理。本 pass 把該條目合併到下一條陳述
// 的 RawAnnotations（供 ResolveProgram 拷貝進 Annotations，desugar 即可找到）。
// 與 applyLineOverflowAnnotations 一致：index-out 條目寫入 side-table，而非註解行
// 欄位——formatter 的 attachedAnnotations 已過濾 index-out 鍵，不會把同一行
// 既以獨立註解、又以附加註解形式各印一次（雙印，破壞冪等）。
func (p *Parser) applyLineIndexOutAnnotations(block *BlockStatement) {
	if block == nil {
		return
	}
	// 把 index-out 條目合併進 for 迴圈體的每一條陳述（desugar 只對 body 內的
	// LetStatement / AssignExpression / ExpressionStatement 生效，不讀 for 本身）。
	propagateToForBody := func(fs *ForStatement, entries []*AnnotationEntry) {
		if fs == nil || fs.Body == nil {
			return
		}
		// 記錄 for 體「自帶」的 index-out（避免重複套用）。
		selfOut := p.indexOutEntries(p.sem.RawAnnotationsOf(fs))
		for _, bs := range fs.Body.Statements {
			if bs == nil {
				continue
			}
			if p.indexOutEntries(p.sem.RawAnnotationsOf(bs)) == nil {
				p.mergeIndexOutAnnotations(bs, entries)
			}
		}
		_ = selfOut
	}
	apply := func(stmts []Statement) {
		for i, s := range stmts {
			as, ok := s.(*AnnotationStatement)
			if !ok {
				continue
			}
			entries := p.indexOutEntries(as.Entries)
			if entries == nil {
				continue
			}
			for j := i + 1; j < len(stmts); j++ {
				next := stmts[j]
				if next == nil {
					continue
				}
				// 連續的獨立註解：以最後一條為準（前一條不覆蓋後一條的目標）。
				if _, isAnn := next.(*AnnotationStatement); isAnn {
					break
				}
				// 行注解語意：index-out 條目合併進下一條陳述的 side-table，
				// 供 desugar（maybeIndexOutAssign / maybeIndexOutReturn）辨識。
				// 若下一條陳述已自帶 index-out，不覆蓋（避免重複套用）。
				if p.indexOutEntries(p.sem.RawAnnotationsOf(next)) == nil {
					p.mergeIndexOutAnnotations(next, entries)
				}
				// 若下一條是 for 迴圈（含 `k <- [0..N):` 計數迴圈），其體內的
				// 索引讀取 `buf[base+k] = data[off+base+k]` 才是真正需要降級的
				// 目標。desugar 只對 LetStatement / AssignExpression / ExpressionStatement
				// 生效，不會讀取 ForStatement 本身的註解，故把 index-out 條目
				// 一併合併進迴圈體的每條陳述，使其體內索引讀取能被正確處理。
				if fs, ok := next.(*ForStatement); ok {
					propagateToForBody(fs, entries)
				}
				break
			}
		}
		// 備用路徑：parseAnnotationStatement 對 IDENT 開頭的 for（如 `i <- [0..]:`）
		// 會把 index-out 直接 attachAnnotations(for, entries) 並回傳 for，註解陳述
		// 被消費、不再出現於 block.Statements。此處補掃描「已自帶 index-out 的 for」，
		// 把註解傳播進其體，使其體內索引讀取能被 desugar 處理（否則越界索引被誤報
		// 未處理）。
		for _, s := range stmts {
			if fs, ok := s.(*ForStatement); ok {
				if entries := p.indexOutEntries(p.sem.RawAnnotationsOf(fs)); entries != nil {
					for _, bs := range fs.Body.Statements {
						if bs == nil {
							continue
						}
						if p.indexOutEntries(p.sem.RawAnnotationsOf(bs)) == nil {
							p.mergeIndexOutAnnotations(bs, entries)
						}
					}
				}
			}
		}
	}
	apply(block.Statements)
	// 巢狀：ForStatement 體各自處理。
	for _, s := range block.Statements {
		if v, ok := s.(*ForStatement); ok && v.Body != nil {
			apply(v.Body.Statements)
		}
	}
}

// indexOutEntries 從一組註解條目中挑出 index-out 鍵的條目。回傳 nil 表示無 index-out 註解。
func (p *Parser) indexOutEntries(entries []*AnnotationEntry) []*AnnotationEntry {
	if len(entries) == 0 {
		return nil
	}
	var out []*AnnotationEntry
	for _, e := range entries {
		if e.Key == "index-out" {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// InlineAnnotationValue 取出 `#{inline}` 註解的布爾值，三種寫法等價關係為：
//
//	#{inline}        present=true, value=true   —— 簡寫，等於 inline=true
//	#{inline=true}   present=true, value=true
//	#{inline=false}  present=true, value=false
//
// present=false 表示該節點完全沒有 inline 註解（維持默認佈局）。
//
// ok=false 表示帶了值但不是布爾（`#{inline=foo}`）：呼叫方應報錯，而不是猜。
// 整數 0/1 視為 false/true（無歧義），其餘型別一律視為不合法。
func InlineAnnotationValue(entries []*AnnotationEntry) (present, value, ok bool) {
	for _, e := range entries {
		if e == nil || e.Key != "inline" {
			continue
		}
		if e.Value == nil {
			return true, true, true // 獨立布爾鍵 `#{inline}`
		}
		switch v := e.Value.(type) {
		case *AnnotationBoolValue:
			return true, v.Value, true
		case *AnnotationIntValue:
			if v.Value == 0 || v.Value == 1 {
				return true, v.Value != 0, true
			}
			return true, false, false
		}
		return true, false, false
	}
	return false, false, true
}

// overflowEntries 從一組註解條目中挑出 overflow 鍵的條目（std 函式普遍以
// `#{overflow = wrap}` 一次性涵蓋整個區塊的整數運算）。回傳 nil 表示無 overflow 註解。
func (p *Parser) overflowEntries(entries []*AnnotationEntry) []*AnnotationEntry {
	if len(entries) == 0 {
		return nil
	}
	var out []*AnnotationEntry
	for _, e := range entries {
		if e.Key == "overflow" {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// propagateBlockScopedOverflow 將區塊內的 #{overflow = ...} 註解沿用到其後、直到
// 被下一個 #{overflow = ...} 覆寫為止的所有陳述（含巢狀區塊），使溢出模式在區塊
// 範圍內生效。std 函式普遍以「區塊前置一次 #{overflow = wrap}」涵蓋該區塊內所有
// 整數運算（如迴圈索引 self[i + j]、裸配對臂條件 n >= 0 - 128），而非逐條陳述標註。
// 這讓 codegen 的 generateStatement 逐條陳述讀取溢出模式時仍能正確套用，避免整數
// 運算退回預設 option 模式產生 %option 後被 trunc 到窄型別而讓 LLVM 報錯。
func (p *Parser) propagateBlockScopedOverflow(block *BlockStatement) {
	if block == nil {
		return
	}
	p.propagateOverflowInStmts(block.Statements, nil)
}

func (p *Parser) propagateOverflowInStmts(stmts []Statement, cur []*AnnotationEntry) {
	// std 慣例：區塊內一次性 `#{overflow = wrap}` 本意涵蓋「整個區塊」的整數運算，
	// 與註解置於區塊頭/中/尾無關。若本區塊直接陳述中僅出現「單一」溢出模式
	// （所有獨立 overflow 註解同模式），則雙向作用域：該模式套用到區塊內所有
	// 未自帶 overflow 註解的陳述（含註解之前的），消除「註解置於運算之後」漏 wrap
	// 的整類 bug（如 path.ext 的 `last-dot == .p.len-bytes() - 1`）。
	// 若出現「多種」溢出模式（需 override 語意），退回原有前向傳播 + override，避免歧義。
	if blockMode := p.blockLevelOverflowMode(stmts); blockMode != nil {
		for _, s := range stmts {
			if _, ok := s.(*AnnotationStatement); ok {
				continue
			}
			if own := p.overflowEntries(p.sem.AnnotationsOf(s)); own == nil {
				p.mergeAnnotations(s, blockMode)
				setStmtOverflowMode(s, p.overflowModeString(blockMode))
			} else {
				setStmtOverflowMode(s, p.overflowModeString(own))
			}
			if fs, ok := s.(*ForStatement); ok && fs.Body != nil {
				p.propagateOverflowInStmts(fs.Body.Statements, blockMode)
			}
			if bs, ok := s.(*BlockStatement); ok {
				p.propagateOverflowInStmts(bs.Statements, blockMode)
			}
		}
		return
	}
	// 多模式或無區塊級註解：維持原有前向傳播 + override。
	for _, s := range stmts {
		// 獨立註解陳述：更新目前生效的溢出模式（即便其本身不產生 IR）。
		if as, ok := s.(*AnnotationStatement); ok {
			if e := p.overflowEntries(as.Entries); e != nil {
				cur = e
			}
			continue
		}
		// 目前生效的溢出模式沿用到本陳述（若本陳述尚未自帶 overflow 註解）。
		if cur != nil && p.overflowEntries(p.sem.AnnotationsOf(s)) == nil {
			p.mergeAnnotations(s, cur)
			setStmtOverflowMode(s, p.overflowModeString(cur))
		}
		// 巢狀區塊/迴圈體繼承目前模式（其內部的自帶註解只影響自身，不向外層洩漏）。
		if fs, ok := s.(*ForStatement); ok && fs.Body != nil {
			p.propagateOverflowInStmts(fs.Body.Statements, cur)
		}
		if bs, ok := s.(*BlockStatement); ok {
			p.propagateOverflowInStmts(bs.Statements, cur)
		}
		// 本陳述自帶 overflow 註解則以此覆寫後續生效模式。
		if own := p.overflowEntries(p.sem.AnnotationsOf(s)); own != nil {
			cur = own
			setStmtOverflowMode(s, p.overflowModeString(own))
		}
	}
}

// blockLevelOverflowMode 回傳本區塊「直接陳述」中「單一」溢出模式的註解條目；
// 若無 overflow 註解、或出現兩種以上不同模式（需 override 語意），回傳 nil。
//
// 同時統計兩種來源：
//   - 獨立 AnnotationStatement（`#{overflow=...}` 自成一句）；
//   - 語句自帶（attached）的 overflow 註解 —— 即註解緊接在該語句之前、由
//     parseAnnotationStatement 附加到語句上的情形。
//
// 後者至關重要：`no fmt` 會把區塊的 overflow 標註統一輸出到區塊開頭，若該區塊
// 第一條陳述以 IDENT 開頭（如 `key = .[i]`），註解會被 attach 而非成為獨立語句。
// 只認獨立 AnnotationStatement 會讓 blockLevelOverflowMode 回 nil，區塊級 wrap
// 整批失效（整數運算退回預設 option 模式 → %option 被 trunc 到窄型別 → LLVM 報錯）。
// 解析期 Annotations 尚未由 ResolveProgram 填充，故必須讀 RawAnnotations。
func (p *Parser) blockLevelOverflowMode(stmts []Statement) []*AnnotationEntry {
	seenMode := ""
	var single []*AnnotationEntry
	consider := func(e []*AnnotationEntry) bool {
		if e == nil {
			return true
		}
		mode := p.overflowModeString(e)
		if mode == "" {
			return true // 無法識別的模式不計入單模式判定
		}
		if seenMode == "" {
			seenMode = mode
			single = e
		} else if mode != seenMode {
			return false // 多模式：退回前向 override
		}
		return true
	}
	for _, s := range stmts {
		if as, ok := s.(*AnnotationStatement); ok {
			if !consider(p.overflowEntries(as.Entries)) {
				return nil
			}
			continue
		}
		if !consider(p.overflowEntries(p.sem.RawAnnotationsOf(s))) {
			return nil
		}
	}
	if seenMode == "" {
		return nil
	}
	return single
}

// overflowModeString 從一組 overflow 註解條目取出正規化模式字串（"wrap"/"clamp0"/...）。
func (p *Parser) overflowModeString(entries []*AnnotationEntry) string {
	for _, e := range entries {
		if e.Key != "overflow" || e.Value == nil {
			continue
		}
		switch v := e.Value.(type) {
		case *AnnotationIdentValue:
			return NormalizeOverflowMode(v.Value)
		case *AnnotationStringValue:
			return NormalizeOverflowMode(v.Value)
		}
	}
	return ""
}

// setStmtOverflowMode 將溢出模式寫入陳述節點的 OverflowMode 欄位（ForStatement / ExpressionStatement 需要），
// 使 HIR 重建後仍能還原模式（不依賴語意 side-table）。HIR 模式下 g.sem 為 nil，
// 若無此欄位，迴圈體 / if·match 臂體整數運算會退回預設 option 模式，產生 %option 後被 trunc 到窄型別而報錯。
// 行注解的下一條陳述可能是任意型別（let 綁定 / return / 表達式陳述 / for），
// 此處統一將溢出模式寫入其 OverflowMode 欄位，使 HIR 重建與本檢查
// （checker.stmtOverflowAnnotated 會讀取該欄位）都能還原模式（不依賴語意 side-table）。
// HIR 模式下 g.sem 為 nil，若無此欄位，相關整數運算會退回預設 option 模式，
// 產生 %option 後被 trunc 到窄型別而報錯。
func setStmtOverflowMode(s Statement, mode string) {
	if mode == "" {
		return
	}
	switch n := s.(type) {
	case *ForStatement:
		n.OverflowMode = mode
	case *ExpressionStatement:
		n.OverflowMode = mode
	case *LetStatement:
		n.OverflowMode = mode
	case *ReturnStatement:
		n.OverflowMode = mode
	case *MultiAssignStatement:
		n.OverflowMode = mode
	}
}

// mergeAnnotations 將 entries 合併（無則直接設定）到節點 n 的註解副表。
//
// 去重按 key 進行（而非 key+value）：同一 key 只保留最後一次出現的條目，
// 後到的 value 覆寫先前的——例如 `#{index-out=0, index-out=1}` 會收斂成
// `#{index-out=1}`（後面的替換前面的）。這同時涵蓋區塊級 overflow 傳播與語句
// 自帶同名註解疊加成 [wrap, wrap] 的場景（同 key 同 value 自然去重，不會讓
// `no fmt` 非冪等：每格式化一輪多印一次）。
func (p *Parser) mergeAnnotations(n Node, entries []*AnnotationEntry) {
	if isNil(n) || len(entries) == 0 {
		return
	}
	// 解析期 Annotations 尚未由 ResolveProgram 從 RawAnnotations 拷貝，若只讀
	// Annotations 會得到空切片 → 走 else 分支直接覆寫，把語句原有的非 overflow
	// 註解（如 #{intrinsic}）連同既有 RawAnnotations 一起丟掉。故回退讀 RawAnnotations。
	existing := p.sem.AnnotationsOf(n)
	if len(existing) == 0 {
		existing = p.sem.RawAnnotationsOf(n)
	}
	if len(existing) > 0 {
		// key -> 在 merged 中的位置（同 key 後到者就地覆寫）。
		pos := make(map[string]int, len(existing))
		merged := make([]*AnnotationEntry, 0, len(existing)+len(entries))
		for _, e := range existing {
			k := e.Key
			if i, ok := pos[k]; ok {
				merged[i] = e // 同 key：existing 內部亦取最後一次
				continue
			}
			pos[k] = len(merged)
			merged = append(merged, e)
		}
		for _, e := range entries {
			k := e.Key
			if i, ok := pos[k]; ok {
				merged[i] = e // 後到的 entries 覆寫 existing（最後一次獲勝）
				continue
			}
			pos[k] = len(merged)
			merged = append(merged, e)
		}
		p.sem.SetRawAnnotations(n, merged)
	} else {
		p.sem.SetRawAnnotations(n, entries)
	}
}

// mergeIndexOutAnnotations 把上一行獨立 `#{index-out=DEF}` 的條目複製進目標陳述的
// side-table，供 desugar（maybeIndexOutAssign / maybeIndexOutReturn）辨識。副本一律
// 標記 Propagated=true，讓 formatter 的 attachedAnnotations 跳過它們——否則同一行
// index-out 會被同時印到每個受影響的陳述（for 迴圈體每條、或獨立節點的下一條）上方，
// 造成雙印且非冪等。克隆條目避免污染來源節點自身的 Entries（同為 index-out 顯示用）。
func (p *Parser) mergeIndexOutAnnotations(n Node, entries []*AnnotationEntry) {
	if isNil(n) || len(entries) == 0 {
		return
	}
	copies := make([]*AnnotationEntry, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		c := *e
		c.Propagated = true
		copies = append(copies, &c)
	}
	p.mergeAnnotations(n, copies)
}

// dedupeAnnotationEntries 依 key 去重，同名鍵只保留最後一次出現的條目
// （後到的 value 覆寫先前的）。例如 [index-out=0, index-out=1] 收斂成
// [index-out=1]。這避免同一陳述上寫多個同名註解時輸出
// `#{index-out=0, index-out=1}`，並讓 desugar 取用正確（最後一個）的預設值。
func dedupeAnnotationEntries(entries []*AnnotationEntry) []*AnnotationEntry {
	if len(entries) <= 1 {
		return entries
	}
	pos := make(map[string]int, len(entries))
	out := make([]*AnnotationEntry, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		if i, ok := pos[e.Key]; ok {
			out[i] = e // 後到的覆寫先前的（同 key 取最後一次）
			continue
		}
		pos[e.Key] = len(out)
		out = append(out, e)
	}
	return out
}

// NormalizeOverflowMode 將 #{overflow = ...} 的值正規化為內部模式名：
//   - 通用：wrap / clamp0 / min / max / saturate。
//   - 型別前綴形式（如 i8-min、u8-max、i16-saturate）：擷取方向尾碼
//     （min/max/saturate），忽略型別前綴——箝位邊界由運算結果的實際型別決定。
//
// 無法辨識的值回傳空字串（非溢出註解，忽略）。
func NormalizeOverflowMode(s string) string {
	switch s {
	case "wrap", "clamp0", "min", "max", "saturate":
		return s
	}
	// 型別前綴：<u?i\d+>-(min|max|saturate)
	if len(s) > 4 && s[0] == 'i' || (len(s) > 4 && s[0] == 'u') {
		// 找尋最後一個 '-'
		idx := strings.LastIndex(s, "-")
		if idx > 0 && idx < len(s)-1 {
			typ := s[:idx]
			dir := s[idx+1:]
			if isIntTypeName(typ) && (dir == "min" || dir == "max" || dir == "saturate") {
				return dir
			}
		}
	}
	return ""
}

// isIntTypeName 報告名稱是否為整數型別名（i8..i128 / u8..u128）。
func isIntTypeName(s string) bool {
	switch s {
	case "i8", "i16", "i32", "i64", "i128", "u8", "u16", "u32", "u64", "u128":
		return true
	}
	return false
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
			Value: true,
		}
		p.nextToken()
		return val
	case lexer.FALSE:
		// 顯式布爾值 `#{inline=false}`：真假必須保留（見 AnnotationBoolValue 的註解）。
		// 獨立布爾鍵 `#{debug}` 走的是另一條路徑——parseAnnotationBody 在沒有 `=` 時
		// 直接建 Value 為 nil 的條目，鍵存在即 true。
		val := &AnnotationBoolValue{
			Token: p.currentToken,
			Value: false,
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
