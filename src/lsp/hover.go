package lsp

import (
	"fmt"
	"strings"
)

// keywordDoc 為 LSP hover 提供新式/舊式關鍵字文檔
var keywordDoc = map[string]string{
	"!":     "**舊式語法（已廢棄）** — `! { }` / `!! { }` 無限迴圈。請改用新式 `{ } ()`。\n\n```nolang\n{\n    *     // break\n    **    // continue\n} ()\n```",
	"*":     "**新式語法** — 跳出當前迴圈（break）。",
	"**":    "**新式語法** — 跳過當前迴圈迭代（continue）。",
	"...":   "**新式語法** — 終止當前語句序列並回傳值，類似舊式 `return` 後接值。",
	"if":    "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"elif":  "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"else":  "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
	"for":   "**舊式語法（已廢棄）** — 請改用新式 `{ } (cond)` 條件迴圈、`{ } ()` 無限迴圈、`{ } * N` 計次迴圈或 `i <- [a..b]: { }` 範圍迴圈。",
	"match": "**舊式語法（已廢棄）** — 請改用新式 `{ cond -> body }` 裸 match 表達式。",
}

// builtinTypeDoc 為 LSP hover 提供內建型別的範圍資訊
var builtinTypeDoc = map[string]string{
	"i8":   "**型別**: `i8` (8-bit signed integer)\n\n- **Range(dec)**: `-128` .. `127`\n- **Range(hex)**: `0x80` .. `0x7f`",
	"i16":  "**型別**: `i16` (16-bit signed integer)\n\n- **Range(dec)**: `-32768` .. `32767`\n- **Range(hex)**: `0x8000` .. `0x7fff`",
	"i32":  "**型別**: `i32` (32-bit signed integer)\n\n- **Range(dec)**: `-2147483648` .. `2147483647`\n- **Range(hex)**: `0x80000000` .. `0x7fffffff`",
	"i64":  "**型別**: `i64` (64-bit signed integer)\n\n- **Range(dec)**: `-9223372036854775808` .. `9223372036854775807`\n- **Range(hex)**: `0x8000000000000000` .. `0x7fffffffffffffff`",
	"u8":   "**型別**: `u8` (8-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `255`\n- **Range(hex)**: `0x00` .. `0xff`",
	"u16":  "**型別**: `u16` (16-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `65535`\n- **Range(hex)**: `0x0000` .. `0xffff`",
	"u32":  "**型別**: `u32` (32-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `4294967295`\n- **Range(hex)**: `0x00000000` .. `0xffffffff`",
	"u64":  "**型別**: `u64` (64-bit unsigned integer)\n\n- **Range(dec)**: `0` .. `18446744073709551615`\n- **Range(hex)**: `0x0000000000000000` .. `0xffffffffffffffff`",
	"byte": "**型別**: `byte` (8-bit unsigned, alias of u8)\n\n- **Range(dec)**: `0` .. `255`\n- **Range(hex)**: `0x00` .. `0xff`",
	"f32":  "**型別**: `f32` (32-bit float)\n\n- **Range(dec)**: `-3.4028234663852886e+38` .. `3.4028234663852886e+38`",
	"f64":  "**型別**: `f64` (64-bit float)\n\n- **Range(dec)**: `-1.7976931348623157e+308` .. `1.7976931348623157e+308`",
	"bool": "**型別**: `bool` (boolean)\n\n- **Values**: `true` | `false`",
	"str":  "**型別**: `str` (string, immutable byte sequence)",
	"char": "**型別**: `char` (single Unicode code point, stored as i64)",
	"fd":   "**型別**: `fd` (file descriptor, stored as i64)",
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

func (hp *HoverProvider) formatHoverContent(entry *IndexEntry) interface{} {
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
	case "u8":
		return "- **Range(dec)**: `0` .. `255`\n- **Range(hex)**: `0x00` .. `0xff`\n"
	case "u16":
		return "- **Range(dec)**: `0` .. `65535`\n- **Range(hex)**: `0x0000` .. `0xffff`\n"
	case "u32":
		return "- **Range(dec)**: `0` .. `4294967295`\n- **Range(hex)**: `0x00000000` .. `0xffffffff`\n"
	case "u64":
		return "- **Range(dec)**: `0` .. `18446744073709551615`\n- **Range(hex)**: `0x0000000000000000` .. `0xffffffffffffffff`\n"
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
