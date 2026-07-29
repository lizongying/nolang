package parser

import (
	"fmt"
	"strconv"
	"unicode"
)

// FormatField 表示一个替换字段 {name[:spec]}
type FormatField struct {
	Name   string      // 变量名称
	Spec   string      // 原始规格字符串（无规格时为空）
	Parsed *FormatSpec // 解析后的规格（无规格时为 nil）
	Pos    int         // 在原始字符串中的位置（用于错误报告）
}

// FormatSegment 是字面文字段落或替换字段
type FormatSegment struct {
	Literal string       // 非空时表示字面文字段落
	Field   *FormatField // 非 nil 时表示字段段落
}

// FormatSpec 表示解析后的格式规格
type FormatSpec struct {
	Fill      byte  // 填充字符（0 表示未设置）
	Align     byte  // '<', '>', '=', '^'（0 表示默认）
	Sign      byte  // '+', '-', ' '（0 表示默认 '-'）
	AltForm   bool  // '#' 标志
	ZeroPad   bool  // '0' 标志
	Width     int   // 最小字段宽度（0 表示无宽度）
	Grouping  byte  // '_' 或 ','（0 表示无分组）
	Precision int   // 精度（-1 表示无精度）
	Type      byte  // 类型字符（0 表示默认）
}

// ParseFormatString 解析 Python 风格的具名格式字符串。
// 支持 {name} 与 {name:spec} 替换字段，以及 {{ 与 }} 转义序列。
func ParseFormatString(s string) ([]FormatSegment, error) {
	var segments []FormatSegment
	var buf []byte // 当前字面文字缓冲区

	// flush 将缓冲区内容作为字面段落输出（即使为空），然后清空缓冲区
	flush := func() {
		segments = append(segments, FormatSegment{Literal: string(buf)})
		buf = nil
	}

	i := 0
	n := len(s)
	for i < n {
		c := s[i]
		switch c {
		case '{':
			// 检查是否为 {{ 转义
			if i+1 < n && s[i+1] == '{' {
				buf = append(buf, '{')
				i += 2
				continue
			}
			// 替换字段开始：先输出已累积的字面文字（即使为空）
			flush()
			// 检查后续是否有匹配的 '}'；若无则视为普通字面 '{'
			hasClose := false
			for j := i + 1; j < n; j++ {
				if s[j] == '}' {
					hasClose = true
					break
				}
			}
			if !hasClose {
				segments = append(segments, FormatSegment{Literal: "{"})
				i++
				continue
			}
			field, next, err := parseField(s, i)
			if err != nil {
				return nil, err
			}
			segments = append(segments, FormatSegment{Field: field})
			i = next
			continue
		case '}':
			// 检查是否为 }} 转义
			if i+1 < n && s[i+1] == '}' {
				buf = append(buf, '}')
				i += 2
				continue
			}
			// 未匹配的 '}'：视为普通字面字符，不报错
			buf = append(buf, '}')
			i++
		default:
			buf = append(buf, c)
			i++
		}
	}
	// 输出剩余的字面文字（即使为空）
	flush()

	// 合并相邻字面段落
	segments = mergeAdjacentLiterals(segments)

	return segments, nil
}

// parseField 从 s[start]（应为 '{'）开始解析一个替换字段。
// 返回字段、'}' 之后的下一个位置以及可能的错误。
func parseField(s string, start int) (*FormatField, int, error) {
	// s[start] == '{'，从 start+1 开始寻找匹配的 '}'
	i := start + 1
	n := len(s)
	var name, spec []byte
	hasSpec := false

	for i < n {
		c := s[i]
		if c == '}' {
			break
		}
		if c == ':' && !hasSpec {
			hasSpec = true
			i++
			continue
		}
		if hasSpec {
			spec = append(spec, c)
		} else {
			name = append(name, c)
		}
		i++
	}

	if i >= n {
		// 没有找到匹配的 '}'
		return nil, 0, fmt.Errorf("格式字符串位置 %d: 未匹配的 '{'", start)
	}

	nameStr := string(name)
	specStr := string(spec)

	// 校验名称
	if len(nameStr) == 0 {
		return nil, 0, fmt.Errorf("格式字符串位置 %d: 空的字段名称", start)
	}
	if !isValidIdentifier(nameStr) {
		return nil, 0, fmt.Errorf("格式字符串位置 %d: 无效的字段名称 %q", start, nameStr)
	}

	field := &FormatField{
		Name: nameStr,
		Spec: specStr,
		Pos:  start,
	}

	// 若存在规格则解析之
	if hasSpec {
		parsed, err := ParseFormatSpec(specStr)
		if err != nil {
			return nil, 0, fmt.Errorf("格式字符串位置 %d: 无效的规格 %q: %w", start, specStr, err)
		}
		field.Parsed = parsed
	}

	// i 指向 '}'，返回其后的位置
	return field, i + 1, nil
}

// ParseFormatSpec 解析格式规格字符串。
// 规格语法: [[fill]align][sign]["#"]["0"][width][grouping_option]["." precision][type]
func ParseFormatSpec(spec string) (*FormatSpec, error) {
	if len(spec) == 0 {
		// 空规格返回默认值（Precision 默认为 -1 表示无精度）
		return &FormatSpec{Precision: -1}, nil
	}

	fs := &FormatSpec{Precision: -1}

	i := 0
	n := len(spec)

	// [[fill]align]
	// 特殊处理：'0' 后接对齐字符时，'0' 视为零填充标志而非填充字符
	if n >= 2 && spec[0] == '0' && isAlignChar(spec[1]) {
		fs.ZeroPad = true
		fs.Align = spec[1]
		i = 2
	} else if n >= 2 && isAlignChar(spec[1]) {
		// fill 字符 + align
		fs.Fill = spec[0]
		fs.Align = spec[1]
		i = 2
	} else if n >= 1 && isAlignChar(spec[0]) {
		// 仅 align
		fs.Align = spec[0]
		i = 1
	}

	// [sign]
	if i < n {
		c := spec[i]
		if c == '+' || c == '-' || c == ' ' {
			fs.Sign = c
			i++
		}
	}

	// ["#"] 替代形式
	if i < n && spec[i] == '#' {
		fs.AltForm = true
		i++
	}

	// ["0"] 零填充
	if i < n && spec[i] == '0' {
		fs.ZeroPad = true
		if fs.Align == 0 {
			fs.Align = '='
		}
		if fs.Fill == 0 {
			fs.Fill = '0'
		}
		i++
	}

	// [width]
	widthStart := i
	for i < n && spec[i] >= '0' && spec[i] <= '9' {
		i++
	}
	if i > widthStart {
		w, err := strconv.Atoi(spec[widthStart:i])
		if err != nil {
			return nil, fmt.Errorf("无效的宽度: %q", spec[widthStart:i])
		}
		fs.Width = w
	}

	// [grouping_option]
	if i < n && (spec[i] == '_' || spec[i] == ',') {
		fs.Grouping = spec[i]
		i++
	}

	// ["." precision]
	if i < n && spec[i] == '.' {
		i++
		precStart := i
		for i < n && spec[i] >= '0' && spec[i] <= '9' {
			i++
		}
		if i == precStart {
			return nil, fmt.Errorf("精度缺少数字")
		}
		p, err := strconv.Atoi(spec[precStart:i])
		if err != nil {
			return nil, fmt.Errorf("无效的精度: %q", spec[precStart:i])
		}
		fs.Precision = p
	}

	// [type] 必须是最后一个字符
	if i < n {
		if n-i != 1 {
			return nil, fmt.Errorf("无效的格式规格: %q", spec)
		}
		c := spec[i]
		if !isValidTypeChar(c) {
			return nil, fmt.Errorf("无效的类型字符: %q", c)
		}
		fs.Type = c
		i++
	}

	if i != n {
		return nil, fmt.Errorf("无效的格式规格: %q", spec)
	}

	return fs, nil
}

// isValidIdentifier 校验 Nolang 标识符：
// 首字符为字母（含 Unicode 字母）或下划线，后续字符为字母、数字、下划线、连字符或点号。
// 点号用于格式字串中的點表達式欄位名（如 {content.len}）。
func isValidIdentifier(name string) bool {
	if len(name) == 0 {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
		} else {
			if r != '_' && r != '-' && r != '.' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
		}
	}
	return true
}

// isAlignChar 判断是否为对齐字符 < > = ^
func isAlignChar(c byte) bool {
	return c == '<' || c == '>' || c == '=' || c == '^'
}

// isValidTypeChar 判断是否为有效的类型字符
func isValidTypeChar(c byte) bool {
	switch c {
	case 'b', 'c', 'd', 'e', 'E', 'f', 'F', 'g', 'G', 'o', 's', 'x', 'X', '%':
		return true
	}
	return false
}

// mergeAdjacentLiterals 合并相邻的字面段落
func mergeAdjacentLiterals(segments []FormatSegment) []FormatSegment {
	if len(segments) <= 1 {
		return segments
	}
	result := make([]FormatSegment, 0, len(segments))
	for _, seg := range segments {
		if seg.Field == nil && len(result) > 0 && result[len(result)-1].Field == nil {
			// 相邻字面段落，合并
			result[len(result)-1].Literal += seg.Literal
		} else {
			result = append(result, seg)
		}
	}
	return result
}
