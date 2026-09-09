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
