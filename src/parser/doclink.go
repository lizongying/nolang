// doclink.go — 文檔註解的 Markdown 連結標記。
//
// 一個 `;` 註解群組的**最後一行**可以是一條獨立的 Markdown 連結，用來把整段
// 註解指向翻譯目錄中的一個條目：
//
//	; get: 獲取第 i 個參數
//	; 委派至 os.arg(i) builtin（讀取 main 的 argv[i]）；越界回傳 nil
//	; [get](#args.get)
//	get = (i i64) (val ?str) {
//	    val = os.arg(i)
//	}
//
// 為什麼是「獨立成行的連結」而不是行內標記：連結的目標（`#args.get`）本身就是
// 鍵，作者寫得出來、譯者看得懂、兩端都只是**讀**它，不需要任何派生。這解決了
// 一個內容尋址鍵（ID 由文字雜湊派生）繞不過去的問題——產生端掃的是原始碼文字，
// 查詢端讀的是 parser 掛在節點上的 Doc 群組，只要兩邊對「一段文字從哪開始到哪
// 結束」有一點分歧，鍵就對不上，而症狀是「翻譯靜默失效」。改成由作者指定鍵之後，
// 鍵永遠一致，最壞情況只是種子文字（en 側首次寫入的內容）不完美。
//
// 為什麼要求 `#` 前綴：`[docs](https://example.com)` 是作者寫給人看的普通連結，
// 絕不能被當成翻譯標記。`#fragment` 是「同一份文件內的錨點」，語意正好對應
// 「本目錄內的一個條目」，也讓外鏈自然免疫。
//
// 為什麼標籤（`[get]` 的 `get`）不參與翻譯：它只是給讀原始碼的人看的提示，
// 顯示時整行都會被剝掉。允許空標籤 `[](#args.get)`。
package parser

import "strings"

// isDocAnchorChar 是錨點允許的字元集合：字母、數字、`.`、`_`、`-`。
// 刻意不含 `#`、`(`、`)`、空白——那些會讓 Markdown 連結的邊界變得模糊，
// 而「這一行是不是連結」必須一眼可判，否則產生端與查詢端又要各自猜一次。
func isDocAnchorChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '.' || c == '_' || c == '-':
		return true
	}
	return false
}

// ValidDocAnchor 判斷 s 是否是合法的文檔錨點。
func ValidDocAnchor(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isDocAnchorChar(s[i]) {
			return false
		}
	}
	return true
}

// ParseDocLinkLine 判斷一行註解（已去掉 `;` 前綴）是否**整行**就是一條指向
// 本目錄條目的 Markdown 連結，回傳標籤與錨點。
//
// 接受的形式：`[label](#anchor)`、`[](#anchor)`、`[label](#anchor) ` （允許前後
// 空白）。其餘一律不算——包括 `[label](anchor)`（沒有 `#`，可能是外鏈）、
// `[label](#)`（空錨點）、`see [label](#x) for details`（不是獨立成行）、
// `[label](#a b)`（錨點含空白）。
func ParseDocLinkLine(line string) (label, anchor string, ok bool) {
	s := strings.TrimSpace(line)
	if len(s) < 5 || s[0] != '[' || s[len(s)-1] != ')' {
		return "", "", false
	}
	// 標籤與目標的分界是 `](`。標籤本身不允許 `[`/`]`，避免 `[a][b](#c)` 這種
	// 引用式連結被誤認。
	sep := strings.Index(s, "](")
	if sep < 0 {
		return "", "", false
	}
	label = s[1:sep]
	if strings.ContainsAny(label, "[]") {
		return "", "", false
	}
	target := s[sep+2 : len(s)-1]
	if len(target) < 2 || target[0] != '#' {
		return "", "", false
	}
	anchor = target[1:]
	if !ValidDocAnchor(anchor) {
		return "", "", false
	}
	return label, anchor, true
}

// SplitDocBlock 把一段註解文字（每個元素是一行，已去掉 `;` 前綴）切成
// 「正文 + 錨點」。只有當**最後一行**是合法的文檔連結時才算帶標記。
//
// 回傳的 block 已 TrimSpace 並去掉尾部空行；anchor 在 hasLink 為 false 時為空。
// 正文允許為空（一條只有連結、沒有說明的註解），呼叫端自行決定要不要記錄它。
//
// 之所以要求「最後一行」：註解群組在原始碼裡是緊貼宣告上方的一段連續 `;` 行，
// 「最後一行」是這段區間唯一與宣告相鄰的端點，兩端都能無歧義地定位它。
// 允許寫在中間就必須回答「連結之後的行算不算正文」，而那個答案兩端會不一致。
func SplitDocBlock(texts []string) (block, anchor string, hasLink bool) {
	if len(texts) == 0 {
		return "", "", false
	}
	_, anchor, hasLink = ParseDocLinkLine(texts[len(texts)-1])
	if !hasLink {
		return strings.Join(trimDocLines(texts), "\n"), "", false
	}
	return strings.Join(trimDocLines(texts[:len(texts)-1]), "\n"), anchor, true
}

// trimDocLines 逐行 TrimSpace 並去掉尾部空行。
//
// 逐行而不是整段：`;;block` 註解的內文行在原始碼裡沒有 `;` 前綴，前導空白是
// 作者排版用的縮排，顯示時不該保留；尾部空行則來自註解區塊的收尾空行。
func trimDocLines(texts []string) []string {
	out := make([]string, 0, len(texts))
	for _, t := range texts {
		out = append(out, strings.TrimSpace(t))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// CommentCarriesDocLink 判斷一行註解是否可以參與文檔連結。
//
// 只有普通的 `;` 單行註解算數。`;;` 與 `;;block` 一律不算：產生端掃的是原始碼
// 文字（看到 `;` 就取行尾），`;;block` 的內文行在原始碼裡根本沒有 `;` 前綴，
// 兩端會得出不同的集合。與其寫一個「區塊註解狀態機」去追平，不如把規則縮到
// 一端一眼可判的範圍。
//
// `//`（Marker 為空）同樣不算：本倉的 .no 語料一行都沒有 `//` 註解。
func CommentCarriesDocLink(marker string) bool {
	return marker == ";"
}
