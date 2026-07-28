package parser

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/lexer"
)

// ring buffer 相關實現。
//
// 設計目標（滿足「lexer 只產 token 流，parser 用定長 ring 做前瞻，而非回寫 lexer 狀態」）：
//   - lexer 僅透過 TokenAt(i) 以「唯讀」方式提供 token（其預掃描的 allTokens 陣列），
//     parser 永不呼叫 lexer.SaveState/RestoreState/LookAhead，即不向 lexer 回寫任何狀態。
//   - parser 維護一個定長 ring（容量 tokenBufferSize）作為「近期 token 視窗」：
//     已消費的 token 駐留 ring，回溯（saveState/restoreState）後重新推進時從 ring 重放，
//     並重新收集其間的註釋，因此註釋不會因回溯而遺失。
//   - 前瞻 look(k) 從 ring 讀取近期 token；若所需 token 超出 ring 視窗（例如配對大括號的
//     深層前向掃描，距離可能達數百 token），則直接從 lexer.TokenAt 隨機讀取（唯讀、不消耗流）。
//     因此即便 ring 容量有限，也無需為無界前瞻而擴大 ring——容量只影響回溯重放的快取命中率。
//
// tokenBufferSize：native 256 / wasm 32。

// tokAt 返回絕對 token 索引 idx（註釋包含）處的 token：
//   - 在 ring 視窗內 → 直接回 ring（O(1)，支援回溯重放）
//   - 超出 ring 視窗（深層前向掃描或深層回溯）→ 從 lexer.TokenAt 隨機讀取（唯讀、索引正確）
func (p *Parser) tokAt(idx int) lexer.Token {
	if idx < 0 {
		return lexer.Token{}
	}
	if idx < p.tkFilled && idx >= p.tkFilled-tokenBufferSize {
		return p.tk[idx%tokenBufferSize]
	}
	return p.lexer.TokenAt(idx)
}

// fill 確保 ring 已駐留絕對索引 idx（含）。從 lexer.TokenAt 按索引複製（索引正確，
// 即使處於回溯後也能正確重放）。僅在推進（nextToken）時對近期 idx 呼叫。
func (p *Parser) fill(idx int) {
	for p.tkFilled <= idx {
		t := p.lexer.TokenAt(p.tkFilled)
		p.tk[p.tkFilled%tokenBufferSize] = t
		p.tkFilled++
	}
}

// look 返回 currentToken 之後第 (k+2) 個非註釋 token，
// 語意等價於舊 lexer.LookAhead(k)：跳過註釋但不收集。
func (p *Parser) look(k int) lexer.Token {
	idx := p.cur
	cnt := 0
	for {
		idx++
		t := p.tokAt(idx)
		if t.Type == lexer.COMMENT {
			continue
		}
		cnt++
		if cnt == k+2 {
			return t
		}
	}
}

// advanceCollect 從 from 之後找到下一個非註釋 token，並將其間的註釋收集進 p.comments。
// 用於 nextToken 推進，註釋收集時機與舊實作一致；回溯後重新推進會從 ring 重放並重收註釋。
func (p *Parser) advanceCollect(from int) (int, lexer.Token) {
	idx := from
	for {
		idx++
		p.fill(idx)
		t := p.tokAt(idx)
		if t.Type == lexer.COMMENT {
			p.comments = append(p.comments, t)
			p.warnSemiSwallow(idx, t)
			continue
		}
		return idx, t
	}
}

// warnSemiSwallow 對「單個 ; 行尾註釋疑似吞掉代碼」發出警告。
// nolang 中單個 `;` 是行註釋標記（註釋到行尾）。若某行代碼中間出現 `;`，
// 其後的代碼（含 `}` 等）會被整體當成註釋吞掉，造成大括號失衡、
// 後續定義被錯誤嵌套等隱蔽錯誤（例如 { a = 1; b = 2 } 中 `; b = 2 }` 被吞）。
// 啟發式條件（全部滿足才警告）：
//  1. Marker == ";"（單分號行註釋，非 ;;）
//  2. 註釋前同一行存在代碼 token（即行尾註釋，非行首整行註釋）
//  3. 註釋內容包含 `{` 或 `}`（疑似吞掉了語句與右大括號）
func (p *Parser) warnSemiSwallow(idx int, t lexer.Token) {
	if t.Marker != ";" || p.warnedSemiEat[idx] {
		return
	}
	if !strings.ContainsAny(t.Literal, "{}") {
		return
	}
	// 檢查前一個 token 是否為同一行的代碼 token（跳過緊鄰的註釋）
	prevIdx := idx - 1
	for prevIdx >= 0 {
		pt := p.tokAt(prevIdx)
		if pt.Type == lexer.COMMENT {
			prevIdx--
			continue
		}
		if pt.Type == lexer.NEWLINE || pt.Line != t.Line {
			return // 行首註釋，不警告
		}
		break
	}
	if prevIdx < 0 {
		return
	}
	p.warnedSemiEat[idx] = true
	p.diags = append(p.diags, Diagnostic{
		Filename: p.Filename,
		Pos:      lexer.Position{Line: t.Line, Column: t.Column},
		Severity: SeverityWarning,
		Code:     WarnSemiSwallow,
		Message: fmt.Sprintf(
			"single ';' starts a line comment; the rest of this line (%q) is ignored — if you meant to separate statements, use a newline instead",
			";"+t.Literal),
	})
}
