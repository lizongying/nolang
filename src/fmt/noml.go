package fmt

import "strings"

// noml 配置檔的格式化器。
//
// `no fmt` 原本只管 `.no` 源碼（與 Markdown 裡的 Nolang 片段）；`.noml` 是
// 工具鏈的配置格式（package.noml / workspace.noml / nolang.lock.noml /
// locales/*.noml / glossary/data/*.noml），同樣是手寫、同樣需要一個排版權威，
// 否則縮進與空白會各寫各的。
//
// 與 `.no` 的格式化器不同，這裡刻意**不做語法重排**：noml 檔的可讀性大半來自
// 作者自己安排的註釋段落、空行分組、單行/多行容器選擇（`[]` 與跨行列表混用），
// 重排會把這些結構打散，且註釋該掛到誰身上並沒有客觀答案。因此本檔只做「排版
// 規範化」這五件事：
//
//  1. 資料行（成員、容器定界符、陣列元素）的縮進統一為 2 空格一級，按容器
//     嵌套深度計算；
//  2. 成員行 `key:` 之後恰好一個空格（`key:   'v'` → `key: 'v'`）；
//  3. 行尾空白清除；
//  4. 連續空行壓成一個，檔首/檔尾空行丟棄；
//  5. 檔尾恰好一個換行。
//
// 四條不可妥協的邊界：
//
//   - **原始字串內容逐位元組保留**。`` ` `` 開的多行原始串裡是散文、程式碼、
//     markdown，重縮進等於改內容（縮進在原始串裡是資料）。
//   - **塊註釋 `;;` … `;;` 的內容逐位元組保留**，理由同上。
//   - **註釋行的縮進原樣保留**（`;` 行註釋與 `;;` 定界符都不跟著成員重縮進）。
//     noml 檔的註釋常是「頂格散文 + 縮進資料」的版面（locales/*.noml 的說明段、
//     glossary 的釋義），拉到成員層級會把整段打散，而註釋縮進不影響語義。
//   - **不改成員順序、不改引號形式、不動單行/多行選擇**。鍵的順序在 noml 裡
//     就是檔案的書寫順序，重排會製造無謂的 diff。
//
// 未閉合的原始串或塊註釋不做猜測：整份輸出退回原文（等於不格式化），並回報
// 問題行號，由呼叫端提示作者。

// NomlProblem 記錄一處讓格式化器放棄修改的原因。
type NomlProblem struct {
	// Line 是問題所在的行號（1-based）。
	Line int
	// Reason 是英文短句，供呼叫端套進 i18n 訊息。
	Reason string
}

// nomlFmtState 是逐行狀態機的狀態：普通行之外的兩種「內容不可改」區域。
type nomlFmtState int

const (
	nomlStateNormal nomlFmtState = iota
	// nomlStateRaw 在反引號原始字串內部，直到「只有空白 + `」的行。
	nomlStateRaw
	// nomlStateBlock 在 `;;` … `;;` 塊註釋內部。
	nomlStateBlock
)

// IsNomlPath 報告路徑是否是 noml 配置檔（`.noml`，大小寫不敏感）。
//
// noml 不是 Nolang 源碼，檔名不受源碼命名規範（小寫 + 中劃線）約束，上層
// 據此豁免檔名檢查——與 Markdown 同口徑。
func IsNomlPath(path string) bool {
	return strings.ToLower(pathExt(path)) == ".noml"
}

// FormatNomlFile 按 noml 排版規範格式化一份文本。
//
// 返回值第二項非空表示遇到了無法安全格式化的結構（未閉合的原始串或塊註釋），
// 此時第一個返回值是原文，呼叫端應把問題報給作者而不是寫回檔案。
func FormatNomlFile(src string) (string, []NomlProblem) {
	lines := strings.Split(src, "\n")
	// Split 在「檔尾有換行」時會多出一個空元素，去掉它；檔尾換行由輸出統一補。
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	var (
		out          []string
		problems     []NomlProblem
		indent       int
		state        = nomlStateNormal
		pendingBlank bool
		// rawStart / blockStart 記錄當前「不可改內容區」開始的行號，供未閉合回報。
		rawStart, blockStart int
	)
	pad := func() string { return strings.Repeat("  ", indent) }

	for idx, raw := range lines {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimLeft(line, " \t")

		switch state {
		case nomlStateRaw:
			// 原始字串內容：逐位元組保留。只有「去掉前導空白後以 ` 開頭」的行
			// 才是結束定界符——與 src/package 的 parseRawString 判定一致。
			// 結束定界符同樣原樣保留（它是內容區的邊界，作者怎麼縮進就怎麼留）。
			if strings.HasPrefix(trimmed, "`") {
				out = append(out, line)
				state = nomlStateNormal
				continue
			}
			// 只去掉 CR：CRLF 檔案的內容行不該把 \r 帶進 LF 輸出。
			out = append(out, strings.TrimRight(raw, "\r"))
			continue
		case nomlStateBlock:
			// 塊註釋內容：逐位元組保留；結束定界符原樣保留。
			if trimmed == ";;" {
				out = append(out, line)
				state = nomlStateNormal
				continue
			}
			out = append(out, strings.TrimRight(raw, "\r"))
			continue
		}

		if trimmed == "" {
			if len(out) > 0 {
				pendingBlank = true
			}
			continue
		}
		if pendingBlank {
			out = append(out, "")
			pendingBlank = false
		}

		// 塊註釋開始：`;;` 之後到行尾只有空白。
		if trimmed == ";;" {
			out = append(out, line)
			state = nomlStateBlock
			blockStart = idx + 1
			continue
		}
		// 行註釋（含 `; note` 與單個 `;` 的空註釋行）：整行原樣保留。
		//
		// 註釋不跟著成員重縮進是刻意的：noml 檔裡的註釋塊常是作者按「頂格散文 +
		// 縮進資料」排的（locales/*.noml 的說明段、glossary 的釋義全是這種寫法），
		// 把註釋拉到成員層級會把整段版面打散，而註釋的縮進不影響任何語義。
		if strings.HasPrefix(trimmed, ";") {
			out = append(out, line)
			continue
		}

		net, openRaw, openBlock := nomlScanLine(trimmed)
		// 收縮：行首是閉合符號時先退回一級。行內的閉合由 net 的平衡量處理，
		// 這裡把「已經被縮進抵消掉的那一個」從平衡量裡扣除，避免重複計算。
		delta := nomlNetBalance(net)
		if strings.HasPrefix(net, "}") || strings.HasPrefix(net, "]") {
			if indent > 0 {
				indent--
			}
			delta++
		}

		out = append(out, pad()+nomlNormalizeMember(trimmed))

		if openRaw {
			state = nomlStateRaw
			rawStart = idx + 1
			continue
		}
		if openBlock {
			state = nomlStateBlock
			blockStart = idx + 1
			continue
		}
		indent += delta
		if indent < 0 {
			indent = 0
		}
	}

	switch state {
	case nomlStateRaw:
		problems = append(problems, NomlProblem{Line: rawStart, Reason: "unterminated raw string (the closing ` must be alone on its own line)"})
	case nomlStateBlock:
		problems = append(problems, NomlProblem{Line: blockStart, Reason: "unterminated block comment (missing closing ';;')"})
	}
	if len(problems) > 0 {
		return src, problems
	}

	res := strings.Join(out, "\n")
	if res != "" {
		res += "\n"
	}
	return res, nil
}

// nomlScanLine 掃描一行（已去掉行首縮進），回傳三件東西：
//
//	net        —— 去掉字串內容與註釋後的「結構淨文本」，用來算容器平衡；
//	openRaw    —— 該行是否開啟反引號原始字串（`` ` `` 之後到行尾只有空白）；
//	openBlock  —— 該行是否開啟塊註釋（`;;` 之後到行尾只有空白）。
//
// 字串裡的 `{` `}` `[` `]` `;` `:` 都不是語法符號，必須先跳過，否則
// `title: '已知坑 {x}'` 會被算成一個未閉合物件。
func nomlScanLine(line string) (net string, openRaw, openBlock bool) {
	var sb strings.Builder
	i := 0
	for i < len(line) {
		c := line[i]
		switch c {
		case '\'':
			i++
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '\'' {
					i++
					break
				}
				i++
			}
		case '`':
			// 反引號原始字串的開頭必須在行尾（其後只有空白）。
			if strings.TrimSpace(line[i+1:]) == "" {
				return sb.String(), true, false
			}
			sb.WriteByte(c)
			i++
		case ';':
			if strings.HasPrefix(line[i:], ";;") && strings.TrimSpace(line[i+2:]) == "" {
				return sb.String(), false, true
			}
			return sb.String(), false, false
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String(), false, false
}

// nomlNetBalance 計算淨文本裡 `{` `[` 與 `}` `]` 的淨增量。
func nomlNetBalance(net string) int {
	d := 0
	for i := 0; i < len(net); i++ {
		switch net[i] {
		case '{', '[':
			d++
		case '}', ']':
			d--
		}
	}
	return d
}

// nomlNormalizeMember 規範成員行的冒號空白：`key :  value` → `key: value`。
//
// 只動**頂層**的冒號（跳過引號內容），且只動冒號兩側的空白——冒號右邊整段
// 原樣保留（值內部、行尾 `;` 註釋都不重排）。陣列元素行（`'a',`）沒有頂層
// 冒號，原樣返回。
func nomlNormalizeMember(line string) string {
	i := 0
	for i < len(line) {
		c := line[i]
		switch c {
		case '\'':
			i++
			for i < len(line) {
				if line[i] == '\\' {
					i += 2
					continue
				}
				if line[i] == '\'' {
					i++
					break
				}
				i++
			}
		case '`', ';':
			return line
		case ':':
			key := strings.TrimRight(line[:i], " \t")
			rest := strings.TrimLeft(line[i+1:], " \t")
			if rest == "" {
				return key + ":"
			}
			return key + ": " + rest
		default:
			i++
		}
	}
	return line
}
