package lsp

import (
	"testing"
)

// decodeSemanticTokens 將 LSP delta 編碼的 semantic tokens 解碼為 (line, col, length, type) 元組。
func decodeSemanticTokens(data []uint32) [][4]uint32 {
	var result [][4]uint32
	prevLine := uint32(0)
	prevChar := uint32(0)
	for i := 0; i+4 < len(data); i += 5 {
		deltaLine := data[i]
		deltaChar := data[i+1]
		length := data[i+2]
		tokenType := data[i+3]
		var line, col uint32
		if deltaLine == 0 {
			line = prevLine
			col = prevChar + deltaChar
		} else {
			line = prevLine + deltaLine
			col = deltaChar
		}
		result = append(result, [4]uint32{line, col, length, tokenType})
		prevLine = line
		prevChar = col
	}
	return result
}

// findTokenAt 在解碼後的 tokens 中尋找指定 (line, col) 的 token，返回其 (length, type)。
func findTokenAt(tokens [][4]uint32, line, col uint32) (uint32, uint32, bool) {
	for _, t := range tokens {
		if t[0] == line && t[1] == col {
			return t[2], t[3], true
		}
	}
	return 0, 0, false
}

func TestFormatStringVariableHighlight(t *testing.T) {
	// print('pi = {pi:.2f}')
	//           123456789012345
	// 變數 pi 在字串內容偏移 6 處，源碼 column 13（0-based）
	src := "print('pi = {pi:.2f}')\n"
	doc := &TextDocument{Text: src}
	sp := NewSemanticTokensProvider(doc)
	tokens := decodeSemanticTokens(sp.GetSemanticTokens().Data)

	// 驗證變數 pi 被標記為 variable (type 8)
	// 字串開引號在 col 6（0-based），內容首字元在 col 7，pi 在偏移 6 → col 13
	length, tokenType, ok := findTokenAt(tokens, 0, 13)
	if !ok {
		t.Fatalf("未找到 col 13 處的 token，所有 tokens: %v", tokens)
	}
	if tokenType != SemTokenTypeVariable {
		t.Errorf("col 13 的 token 類型應為 variable(%d)，得到 %d", SemTokenTypeVariable, tokenType)
	}
	if length != 2 {
		t.Errorf("變數 pi 的長度應為 2，得到 %d", length)
	}
}

func TestFormatStringNoFieldNotSplit(t *testing.T) {
	// 無欄位的普通字串應作為整體 string token 輸出
	src := "s = 'hello world'\n"
	doc := &TextDocument{Text: src}
	sp := NewSemanticTokensProvider(doc)
	tokens := decodeSemanticTokens(sp.GetSemanticTokens().Data)

	// 尋找 string 類型 token (type 18)
	var strToken [4]uint32
	found := false
	for _, t := range tokens {
		if t[3] == SemTokenTypeString {
			strToken = t
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("未找到 string token，所有 tokens: %v", tokens)
	}
	// 'hello world' 內容長度 11，應作為單一 token
	if strToken[2] != 11 {
		t.Errorf("普通字串應作為整體 token（長度 11），得到長度 %d", strToken[2])
	}
}

func TestFormatStringMultipleFields(t *testing.T) {
	// '{name} = {value}' 兩個欄位
	src := "s = '{name} = {value}'\n"
	doc := &TextDocument{Text: src}
	sp := NewSemanticTokensProvider(doc)
	tokens := decodeSemanticTokens(sp.GetSemanticTokens().Data)

	// 驗證兩個 variable token
	variableCount := 0
	for _, t := range tokens {
		if t[3] == SemTokenTypeVariable {
			variableCount++
		}
	}
	if variableCount != 2 {
		t.Errorf("應有 2 個 variable token，得到 %d", variableCount)
	}
}

func TestFormatStringEscapedBraceNotSplit(t *testing.T) {
	// '{{literal}}' 應被解析為字面段落，不產生 variable token
	src := "s = '{{literal}}'\n"
	doc := &TextDocument{Text: src}
	sp := NewSemanticTokensProvider(doc)
	tokens := decodeSemanticTokens(sp.GetSemanticTokens().Data)

	for _, tok := range tokens {
		if tok[3] == SemTokenTypeVariable {
			t.Errorf("轉義大括號不應產生 variable token，tokens: %v", tokens)
		}
	}
}
