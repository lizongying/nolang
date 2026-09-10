package lsp

import (
	"fmt"
	"strings"
)

// keywordDoc 為 LSP hover 提供新式/舊式關鍵字文檔
var keywordDoc = map[string]string{
	"!!":    "**舊式語法（已廢棄）** — `!! { }` 無限迴圈。請改用新式 `{ } (true)`。\n\n```nolang\n{\n    *     // break\n    **    // continue\n} (true)\n```",
	"!":     "**舊式語法（已廢棄）** — `! { }` 無限迴圈。請改用新式 `{ } (true)`。\n\n```nolang\n{\n    *     // break\n    **    // continue\n} (true)\n```",
	"*":     "**新式語法** — 跳出當前迴圈（break）。",
	"**":    "**新式語法** — 跳過當前迴圈迭代（continue）。",
	"...":   "**新式語法** — 終止當前語句序列並回傳值，類似舊式 `return` 後接值。",
	"if":    "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"elif":  "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"else":  "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"for":   "**舊式語法（已廢棄）** — 請改用新式 `{ } (cond)` 條件迴圈、`{ } (true)` 無限迴圈、`{ } * N` 計次迴圈或 `i <- [a..b]: { }` 範圍迴圈。",
	"match": "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
}

// builtinTypeDoc 為 LSP hover 提供內建型別的範圍資訊
var builtinTypeDoc = map[string]string{
	"i8":   "**型別**: `i8` (8-bit signed integer)\n\n- **Range(dec)**: `-128` .. `127`\n- **Range(hex)**: `0x80` .. `0x7f`",
	"i16":  "**型別**: `i16` (16-bit signed integer)\n\n- **Range(dec)**: `-32768` .. `32767`\n- **Range(hex)**: `0x8000` .. `0x7fff`",
	"i32":  "**型別**: `i32` (32-bit signed integer)\n\n- **Range(dec)**: `-2147483648` .. `2147483647`\n- **Range(hex)**: `0x80000000` .. `0x7fffffff`",
	"i64":  "**型別**: `i64` (64-bit signed integer)\n\n- **Range(dec)**: `-9223372036854775808` .. `9223372036854775807`\n- **Range(hex)**: `0x8000000000000000` .. `0x7fffffffffffffff`",
	"i128": "**型別**: `i128` (128-bit signed integer)\n\n- **Range(dec)**: `-170141183460469231731687303715884105728` .. `170141183460469231731687303715884105727`\n- **Range(hex)**: `0x80000000000000000000000000000000` .. `0x7fffffffffffffffffffffffffffffff`",
	"u8":   "**型別**: `u8` (8-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `255`\n- **Range(hex)**: `0x00` .. `0xff`",
	"u16":  "**型別**: `u16` (16-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `65535`\n- **Range(hex)**: `0x0000` .. `0xffff`",
	"u32":  "**型別**: `u32` (32-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `4294967295`\n- **Range(hex)**: `0x00000000` .. `0xffffffff`",
	"u64":  "**型別**: `u64` (64-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `18446744073709551615`\n- **Range(hex)**: `0x0000000000000000` .. `0xffffffffffffffff`",
	"u128": "**型別**: `u128` (128-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `340282366920938463463374607431768211455`\n- **Range(hex)**: `0x00000000000000000000000000000000` .. `0xffffffffffffffffffffffffffffffff`",
	"byte": "**型別**: `byte` (8-bit unsigned, alias of u8)\n\n- **Range(dec)**: `0` .. `255`\n- **Range(hex)**: `0x00` .. `0xff`",
	"f32":  "**型別**: `f32` (32-bit float)\n\n- **Range(dec)**: `-3.4028234663852886e+38` .. `3.4028234663852886e+38`",
	"f64":  "**型別**: `f64` (64-bit float)\n\n- **Range(dec)**: `-1.7976931348623157e+308` .. `1.7976931348623157e+308`",
	"bool": "**型別**: `bool` (boolean)\n\n- **Values**: `true` | `false`",
	"str":  "**型別**: `str` (string, immutable byte sequence)",
	"txt":  "**型別**: `txt` (fixed 256-byte string, max 255 bytes data)\n\n- **Layout**: `{ [255]byte data, byte len }`\n- **Total size**: 256 bytes\n- **Max content**: 255 bytes\n- **Usage**: `t txt = 'hello'`",
	"char": "**型別**: `char` (single Unicode code point, stored as i64)",
	"fd":   "**型別**: `fd` (file descriptor, stored as i64)",
}

// overflowKeyDoc 為 LSP hover 提供 `#{overflow = ...}` 註解鍵（overflow）的文檔，
// 解釋什麼是整數溢出以及 nolang 預設/註解驅動的處理策略。
var overflowKeyDoc = "**整數溢出註解 `#{overflow = ...}`**\n\n" +
	"整數運算（加減乘，以及有符號除法 `INT_MIN / -1`）的結果可能超出該型別可表示的範圍，稱為**溢出**。\n\n" +
	"nolang 的預設策略是：這些運算回傳 `option<int>`——溢出時得到 `err`，**永不 panic**，把溢出變成可處理的錯誤而非崩潰。\n\n" +
	"若你希望改用其他溢出處理策略（例如底層演算法需要靜默按位回繞），可用註解改寫：\n\n" +
	"```nolang\n" +
	"#{overflow = wrap}      // 靜默 two's complement 回繞\n" +
	"#{overflow = clamp0}    // 下溢歸零\n" +
	"#{overflow = min}       // 下溢箝位到型別最小值\n" +
	"#{overflow = max}       // 上溢箝位到型別最大值\n" +
	"#{overflow = saturate}  // 上下溢皆飽和箝位\n" +
	"```\n\n" +
	"- **作用域**：寫在語句上方時僅作用於該語句；寫在函式/區塊頂部時，會沿用到其後所有語句，直到被下一個 `#{overflow = ...}` 覆寫。std 函式常以「區塊前置一次」覆蓋整塊。\n" +
	"- 註解驅動的運算不再回傳 `option`，結果直接是普通 `int`。"

// overflowModeDoc 為 LSP hover 提供 `#{overflow = <mode>}` 各模式值的詳細文檔（含示例）。
var overflowModeDoc = map[string]string{
	"wrap": "**`wrap` — 靜默按位回繞（two's complement wrap-around）**\n\n" +
		"溢出時按二進位補碼截斷，結果環繞到型別區間的另一端。這是 C/C++、Rust（release）、Go（無符號）等的預設整數溢出行為，也是雜湊、校驗和、加密、亂數等底層運算的常見選擇。\n\n" +
		"- 上溢：`MAX + 1 → MIN`\n" +
		"- 下溢：`MIN - 1 → MAX`\n\n" +
		"```nolang\n" +
		"x i8 = 120\n" +
		"#{overflow = wrap}\n" +
		"x = x + 10        // 130 超出 i8 範圍 → 回繞為 -126\n" +
		"```\n\n" +
		"```nolang\n" +
		"c u8 = 255\n" +
		"#{overflow = wrap}\n" +
		"c = c + 1         // 256 超出 u8 範圍 → 回繞為 0\n" +
		"```\n\n" +
		"- 與預設 `option<int>` 模式不同，`wrap` 不回傳 `option`，結果直接是普通 `int`。\n" +
		"- 型別前綴形式 `i8-wrap` 等會被歸一化為 `wrap`（邊界由運算結果的實際型別決定）。",
	"clamp0": "**`clamp0` — 下溢歸零（saturate to 0）**\n\n" +
		"運算結果下溢（小於型別最小值）時，結果被箝位為 `0`；上溢時仍按預設行為處理。適用於「計數/累加不允許為負」的場景。\n\n" +
		"```nolang\n" +
		"n i32 = -5\n" +
		"#{overflow = clamp0}\n" +
		"n = n - 10        // -15 下溢 → 箝位為 0\n" +
		"```",
	"min": "**`min` — 下溢箝位到型別最小值**\n\n" +
		"運算結果下溢時，結果被箝位為該型別的最小值（如 `i8 → -128`、`u8 → 0`）；上溢時仍按預設行為處理。\n\n" +
		"```nolang\n" +
		"x i8 = -120\n" +
		"#{overflow = min}\n" +
		"x = x - 20        // -140 下溢 → 箝位為 -128\n" +
		"```",
	"max": "**`max` — 上溢箝位到型別最大值**\n\n" +
		"運算結果上溢時，結果被箝位為該型別的最大值（如 `i8 → 127`、`u8 → 255`）；下溢時仍按預設行為處理。\n\n" +
		"```nolang\n" +
		"x u8 = 250\n" +
		"#{overflow = max}\n" +
		"x = x + 10        // 260 上溢 → 箝位為 255\n" +
		"```",
	"saturate": "**`saturate` — 上下溢皆飽和箝位（saturating arithmetic）**\n\n" +
		"運算結果上溢時箝位到型別最大值、下溢時箝位到型別最小值，結果始終落在型別區間內。這是影像/音訊處理、物理量積分等「數值不應越界」場景的標準做法。\n\n" +
		"```nolang\n" +
		"x i8 = 120\n" +
		"#{overflow = saturate}\n" +
		"x = x + 20        // 140 上溢 → 箝位為 127\n" +
		"x = x - 300       // -173 下溢 → 箝位為 -128\n" +
		"```",
}

// matchOptionPatternDoc 為 match arm 中的 option 模式（`ok` / `nil` / `err`）提供
// hover 文檔主體，說明該模式在 option 三態（有值 / 無值 / 錯誤）中的語義。
// 型別行與被匹配表達式由呼叫端依原始碼上下文補上（見 getMatchOptionPatternHover）。
var matchOptionPatternDoc = map[string]string{
	"ok": "**`ok` — option 模式：有值（成功）**\n\n" +
		"被匹配的表達式帶有有效值時命中此臂。\n\n" +
		"- 臂內可用 `it` 取得解箱後的內層值\n" +
		"- 也可寫成 `ok(cond) -> ...`：在「有值」之外額外要求 `cond` 為真\n\n" +
		"```nolang\n" +
		"size ?= stat-size(p)\n" +
		"size: {\n" +
		"    ok -> n = it      // it 是解箱後的內層值\n" +
		"    nil -> n = 0\n" +
		"    err -> n = 0\n" +
		"}\n" +
		"```",
	"nil": "**`nil` — option 模式：無值（空）**\n\n" +
		"被匹配的表達式「沒有值」時命中此臂——注意它**不是**錯誤，只是空。\n\n" +
		"- 典型來源：查不到鍵、讀到 EOF、函式回傳空結果\n" +
		"- 臂內**不要**解引用 `it`（此時沒有值可解）\n\n" +
		"```nolang\n" +
		"v = m.get(k)\n" +
		"v: {\n" +
		"    ok -> io.outln(it)\n" +
		"    nil -> io.outln('not found')   // 空，不是錯誤\n" +
		"    err -> io.outln('failed')\n" +
		"}\n" +
		"```",
	"err": "**`err` — option 模式：錯誤**\n\n" +
		"被匹配的表達式代表一次**失敗的運算**時命中此臂。\n\n" +
		"- 典型來源：整數溢出（nolang 預設策略）、I/O 失敗、呼叫失敗\n" +
		"- 與 `nil` 的差別：`err` 是「出錯了」，`nil` 只是「沒有值」\n" +
		"- 臂內**不要**解引用 `it`\n\n" +
		"```nolang\n" +
		"x i8 = 120\n" +
		"(x + 10): {\n" +
		"    ok  -> io.outln(it)\n" +
		"    nil -> io.outln('no value')\n" +
		"    err -> io.outln('overflow')    // 130 超出 i8 範圍\n" +
		"}\n" +
		"```",
}

// matchOptionPatternFooter 接在每個 option 模式文檔之後，解釋 `?T` 三態與
// 「三臂齊全」的窮盡性要求。
const matchOptionPatternFooter = "\n\n---\n\n" +
	"`?T` 是 **option 型別**：要嘛是 `T` 的值（`ok`），要嘛是空（`nil`），要嘛是錯誤（`err`）。\n\n" +
	"nolang 的 match 要求 `ok` / `nil` / `err` 三臂齊全（或以 `->` 通配臂收尾），" +
	"避免漏處理失敗路徑——這是把錯誤當成值處理、而非抛異常的核心機制。"

type HoverProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewHoverProvider(doc *TextDocument, index *SymbolIndex) *HoverProvider {
	return &HoverProvider{
		index: index,
		doc:   doc,
	}
}

func (hp *HoverProvider) GetHover(position Position) (*Hover, bool) {
	word := getTokenAtPosition(hp.doc.Text, position)
	if word == "" {
		return nil, false
	}

	// 關鍵字 hover（新舊語法）
	if doc, ok := keywordDoc[word]; ok {
		return &Hover{
			Contents: MarkupContent{
				Kind:  MarkupKindMarkdown,
				Value: doc,
			},
		}, true
	}

	// 內建型別 hover（i64, u8, byte, bool 等）
	if doc, ok := builtinTypeDoc[word]; ok {
		return &Hover{
			Contents: MarkupContent{
				Kind:  MarkupKindMarkdown,
				Value: doc,
			},
		}, true
	}

	// match arm 的 option 模式 hover（`ok` / `nil` / `err`）：僅當游標位於
	// `->` 之前的 pattern 區段時觸發，避免誤傷同名變數（如結果參數 `ok ?bool`）。
	if doc, ok := hp.getMatchOptionPatternHover(position, word); ok {
		return &Hover{
			Contents: MarkupContent{
				Kind:  MarkupKindMarkdown,
				Value: doc,
			},
		}, true
	}

	// overflow 註解 hover：僅當游標位於 `#{overflow = ...}` 註解行時觸發，
	// 避免誤傷同名識別符（如變數/函式名 wrap、min、max）。
	if doc, ok := hp.getOverflowAnnotationHover(position, word); ok {
		return &Hover{
			Contents: MarkupContent{
				Kind:  MarkupKindMarkdown,
				Value: doc,
			},
		}, true
	}

	if hp.index == nil {
		return nil, false
	}

	entry, ok := hp.index.LookupAtPosition(word, position)
	if !ok {
		entry, ok = hp.index.GetDefinition(word)
		if !ok {
			return nil, false
		}
	}

	contents := hp.formatHoverContent(entry)
	return &Hover{
		Contents: contents,
	}, true
}

// getOverflowAnnotationHover 在游標位於 `#{overflow = ...}` 註解行時，為
// `overflow` 鍵或各模式值（wrap/clamp0/min/max/saturate）提供 hover 文檔。
// 透過「僅在含 `#{` 與 `overflow` 的註解行觸發」來避免誤傷同名識別符。
func (hp *HoverProvider) getOverflowAnnotationHover(position Position, word string) (string, bool) {
	lines := getLines(hp.doc.Text)
	if int(position.Line) >= len(lines) {
		return "", false
	}
	line := lines[position.Line]
	if !strings.Contains(line, "#{") || !strings.Contains(line, "overflow") {
		return "", false
	}
	if word == "overflow" {
		return overflowKeyDoc, true
	}
	if doc, ok := overflowModeDoc[word]; ok {
		return doc, true
	}
	return "", false
}

// isOptionPatternName 判斷 word 是否為 option match 模式（`ok` / `nil` / `err`）。
func isOptionPatternName(word string) bool {
	switch word {
	case "ok", "nil", "err":
		return true
	}
	return false
}

// isOptionPatternList 判斷 s（match arm 的 `->` 之前區段）是否只由 option 模式組成，
// 例如 `ok`、`err `、`nil || err`、`ok(n > 0)`。含有其他識別符（如裸 match 的
// 一般條件 `foo`）時回傳 false，避免把同名變數誤判為模式。
func isOptionPatternList(s string) bool {
	for _, part := range strings.Split(s, "||") {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		name := part
		if i := strings.Index(part, "("); i >= 0 {
			name = strings.TrimSpace(part[:i])
		}
		if !isOptionPatternName(name) {
			return false
		}
	}
	return true
}

// stripLineComment 去掉行內 `;` 註解（`;` 是 nolang 的單行註解符號）。
func stripLineComment(s string) string {
	if i := strings.Index(s, ";"); i >= 0 {
		return s[:i]
	}
	return s
}

// indentWidth 回傳行的前導空白寬度（tab 視為 4 欄）。
func indentWidth(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

// matchSubjectFromHeader 從 match 的 header 行（`size: {`、`f: {` 等）取出被匹配的
// 表達式文字；裸 match（`{ ... }`）沒有 subject，回傳 ""。
func matchSubjectFromHeader(line string) string {
	s := strings.TrimSpace(stripLineComment(line))
	s = strings.TrimSpace(strings.TrimSuffix(s, "{"))
	if !strings.HasSuffix(s, ":") {
		return "" // 裸 match，無 subject
	}
	return strings.TrimSpace(strings.TrimSuffix(s, ":"))
}

// findMatchSubject 向上尋找包住第 lineIdx 行 arm 的 match header，回傳被匹配表達式文字。
// 同行寫法（`size: { ok -> ... }`）直接在當行取；否則向上一行行掃描，
// 直到遇到縮排更淺且以 `{` 結尾的 header 行為止。
func findMatchSubject(lines []string, lineIdx int, line string, arrowIdx int) string {
	// 同行情形：`size: { ok -> ... }`
	if brace := strings.LastIndex(line[:arrowIdx], "{"); brace >= 0 {
		return matchSubjectFromHeader(line[:brace])
	}
	indent := indentWidth(line)
	for i := lineIdx - 1; i >= 0; i-- {
		cur := lines[i]
		if strings.TrimSpace(stripLineComment(cur)) == "" {
			continue // 空行 / 純註解行
		}
		if indentWidth(cur) >= indent {
			continue // 縮排不比 arm 淺 → 不是 header
		}
		trimmed := strings.TrimSpace(stripLineComment(cur))
		if !strings.HasSuffix(trimmed, "{") {
			return "" // 縮排更淺但不是 block 起始 → 放棄
		}
		return matchSubjectFromHeader(cur)
	}
	return ""
}

// resolveOptionType 依被匹配表達式文字推導 option 型別與其內層型別。
// 運算式為純識別符時查符號表（`size` → i64 → `?i64` / i64）；
// 為呼叫時查函式回傳型別。無法判定時回傳空字串。
func (hp *HoverProvider) resolveOptionType(subject string) (optType string, innerType string) {
	if hp.index == nil || subject == "" {
		return "", ""
	}
	base := subject
	if i := strings.Index(base, "("); i >= 0 {
		// 呼叫形式：`stat-size(p)` → 查函式回傳型別
		fn := strings.TrimSpace(base[:i])
		if entry, ok := hp.index.functions[fn]; ok && len(entry.ResultParams) > 0 {
			t := entry.ResultParams[0].Type
			if t == "" {
				return "", ""
			}
			if strings.HasPrefix(t, "?") {
				return t, strings.TrimPrefix(t, "?")
			}
			return "?" + t, t
		}
		return "", ""
	}
	// 純識別符（含 `.fd` 之類的接收者欄位則查不到，回傳空）
	if entry, ok := hp.index.symbols[base]; ok && entry.Type != "" && !strings.HasPrefix(entry.Type, "call ") {
		t := entry.Type
		if strings.HasPrefix(t, "?") {
			return t, strings.TrimPrefix(t, "?")
		}
		return "?" + t, t
	}
	return "", ""
}

// getMatchOptionPatternHover 在游標位於 match arm 的 option 模式
// （`ok` / `nil` / `err`）上時，提供「option 型別 + 該模式語義」的 hover 文檔。
// 透過「僅在 `->` 之前且該區段全是 option 模式時觸發」來避免誤傷同名變數
// （如結果參數 `ok ?bool`、或 `err -> ok = err('...')` 右側的 `ok`）。
func (hp *HoverProvider) getMatchOptionPatternHover(position Position, word string) (string, bool) {
	if !isOptionPatternName(word) {
		return "", false
	}
	lines := getLines(hp.doc.Text)
	if int(position.Line) >= len(lines) {
		return "", false
	}
	line := lines[position.Line]
	arrowIdx := strings.Index(line, "->")
	if arrowIdx < 0 || int(position.Character) >= arrowIdx {
		return "", false // 游標不在 `->` 之前的 pattern 區段
	}
	// 確認游標所在的單字確實完整落在 pattern 區段內
	start, end := wordBoundsAt(line, position.Character)
	if start < 0 || end > arrowIdx || line[start:end] != word {
		return "", false
	}
	prefix := line[:arrowIdx]
	if brace := strings.LastIndex(prefix, "{"); brace >= 0 {
		prefix = prefix[brace+1:]
	}
	if !isOptionPatternList(prefix) {
		return "", false
	}

	doc, ok := matchOptionPatternDoc[word]
	if !ok {
		return "", false
	}

	subject := findMatchSubject(lines, int(position.Line), line, arrowIdx)
	optType, innerType := hp.resolveOptionType(subject)

	// 各模式的「自身型別」：`ok` 是解箱後的內層值型別，`nil` / `err` 是各自的哨兵型別。
	// 這比一律顯示 option 型別更精確——懸停 `nil` 時使用者想知道的是「這裡是 nil」。
	selfType := ""
	switch word {
	case "ok":
		selfType = innerType
	case "nil", "err":
		selfType = word
	}

	var b strings.Builder
	b.WriteString(doc)
	if optType != "" || selfType != "" {
		b.WriteString("\n\n")
		if subject != "" {
			b.WriteString(fmt.Sprintf("- **Matched**: `%s`\n", subject))
		}
		if optType != "" {
			b.WriteString(fmt.Sprintf("- **Option type**: `%s`\n", optType))
		}
		if selfType != "" {
			if word == "ok" {
				b.WriteString(fmt.Sprintf("- **Type**: `%s`（解箱後的內層值型別，可經 `ok(name)` 改名）\n", selfType))
			} else {
				b.WriteString(fmt.Sprintf("- **Type**: `%s`\n", selfType))
			}
		}
	}
	b.WriteString(matchOptionPatternFooter)
	return b.String(), true
}

// wordBoundsAt 回傳行內第 col 欄所在單字的 [start, end) 區間；col 不在單字上時回傳 (-1, -1)。
func wordBoundsAt(line string, col uint32) (int, int) {
	c := int(col)
	if c < 0 || c > len(line) {
		return -1, -1
	}
	start, end := c, c
	for start > 0 && isWordChar(line[start-1]) {
		start--
	}
	for end < len(line) && isWordChar(line[end]) {
		end++
	}
	if start == end {
		return -1, -1
	}
	return start, end
}

func (hp *HoverProvider) formatHoverContent(entry *IndexEntry) any {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("**%s**\n\n", entry.Name))

	if entry.Doc != "" {
		builder.WriteString(fmt.Sprintf("%s\n\n", entry.Doc))
	}

	if entry.Type != "" {
		builder.WriteString(fmt.Sprintf("- **Type**: `%s`\n", entry.Type))
		if rangeInfo := typeRangeInfo(entry.Type); rangeInfo != "" {
			builder.WriteString(rangeInfo)
		}
	}

	if entry.Location.URI != "" {
		line := entry.Location.Range.Start.Line + 1
		col := entry.Location.Range.Start.Character + 1
		builder.WriteString(fmt.Sprintf("- **Declared at**: line %d, column %d\n", line, col))
	}

	if entry.Value != "" {
		builder.WriteString(fmt.Sprintf("- **Value**: %s\n", entry.Value))
	}

	if entry.Type == "enum" && entry.Value != "" {
		builder.WriteString(fmt.Sprintf("- **Variants**: %s\n", entry.Value))
	}

	if entry.Kind == SymbolKindEnumMember && entry.Type != "" {
		builder.WriteString(fmt.Sprintf("- **Enum**: `%s`\n", entry.Type))
	}

	if len(entry.Params) > 0 {
		builder.WriteString("- **Parameters**:\n")
		for _, p := range entry.Params {
			if p.Type != "" {
				if p.DefaultValue != "" {
					builder.WriteString(fmt.Sprintf("  - `%s: %s = %s`\n", p.Name, p.Type, p.DefaultValue))
				} else {
					builder.WriteString(fmt.Sprintf("  - `%s: %s`\n", p.Name, p.Type))
				}
			} else {
				builder.WriteString(fmt.Sprintf("  - `%s`\n", p.Name))
			}
		}
	}

	if len(entry.ResultParams) > 0 {
		builder.WriteString("- **Returns**:\n")
		for _, r := range entry.ResultParams {
			if r.Type != "" {
				builder.WriteString(fmt.Sprintf("  - `%s: %s`\n", r.Name, r.Type))
			} else {
				builder.WriteString(fmt.Sprintf("  - `%s`\n", r.Name))
			}
		}
	}

	return MarkupContent{
		Kind:  MarkupKindMarkdown,
		Value: builder.String(),
	}
}

// typeRangeInfo returns human-readable range lines for builtin numeric
// types, byte, and bool. Returns "" for non-builtin or non-numeric types.
// Each returned string ends with a newline.
func typeRangeInfo(typeName string) string {
	switch typeName {
	case "i8":
		return "- **Range(dec)**: `-128` .. `127`\n- **Range(hex)**: `0x80` .. `0x7f`\n"
	case "i16":
		return "- **Range(dec)**: `-32768` .. `32767`\n- **Range(hex)**: `0x8000` .. `0x7fff`\n"
	case "i32":
		return "- **Range(dec)**: `-2147483648` .. `2147483647`\n- **Range(hex)**: `0x80000000` .. `0x7fffffff`\n"
	case "i64":
		return "- **Range(dec)**: `-9223372036854775808` .. `9223372036854775807`\n- **Range(hex)**: `0x8000000000000000` .. `0x7fffffffffffffff`\n"
	case "i128":
		return "- **Range(dec)**: `-170141183460469231731687303715884105728` .. `170141183460469231731687303715884105727`\n- **Range(hex)**: `0x80000000000000000000000000000000` .. `0x7fffffffffffffffffffffffffffffff`\n"
	case "u8":
		return "- **Range(dec)**: `0` .. `255`\n- **Range(hex)**: `0x00` .. `0xff`\n"
	case "u16":
		return "- **Range(dec)**: `0` .. `65535`\n- **Range(hex)**: `0x0000` .. `0xffff`\n"
	case "u32":
		return "- **Range(dec)**: `0` .. `4294967295`\n- **Range(hex)**: `0x00000000` .. `0xffffffff`\n"
	case "u64":
		return "- **Range(dec)**: `0` .. `18446744073709551615`\n- **Range(hex)**: `0x0000000000000000` .. `0xffffffffffffffff`\n"
	case "u128":
		return "- **Range(dec)**: `0` .. `340282366920938463463374607431768211455`\n- **Range(hex)**: `0x00000000000000000000000000000000` .. `0xffffffffffffffffffffffffffffffff`\n"
	case "byte":
		return "- **Range(dec)**: `0` .. `255`\n- **Range(hex)**: `0x00` .. `0xff`\n"
	case "f32":
		return "- **Range(dec)**: `-3.4028234663852886e+38` .. `3.4028234663852886e+38`\n"
	case "f64":
		return "- **Range(dec)**: `-1.7976931348623157e+308` .. `1.7976931348623157e+308`\n"
	case "bool":
		return "- **Values**: `true` | `false`\n"
	}
	return ""
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}
