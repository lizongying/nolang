package pkg

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/lizongying/nolang/i18n"
)

// ─── noml：Nolang Markup Language ────────────────────────────────────────────
//
// noml 是 Nolang 工具链配置檔（package.noml / workspace.noml /
// nolang.lock.noml / sum.noml）的資料交換格式，取代舊的 JSONC。語法：
//
//   - 頂層為物件：{ 與 } 各独占一行，成員一行一個，換行即分隔，不用逗號。
//   - 鍵：預設裸識別符，規則同 nolang 變數名——下劃線開頭、或開頭為小寫
//     字母／數字；中間可含小寫字母、數字、中劃線；不能以數字開頭
//     （下劃線開頭除外）。不符合規則的鍵寫成單引號字串鍵。
//   - 值：單引號字串（\ 轉義：\' \\ \n \t \r）、true/false（全小寫）、
//     null、裸數字（整數/小數/指數）、物件 {…}、陣列 […]。
//   - 陣列：元素用逗號分隔，支援尾隨逗號；[ 、元素、,、] 皆可跨行。
//   - 註解：; 到行尾（字串內的 ; 除外）。縮排無意義。
//   - 成員註解：`#{...}` 與 nolang 註解語法完全一致（`key = value`，逗號分隔），
//     只允許獨立成行寫在成員**上方**，或寫在成員同一行的**尾隨**位置。
//     寫在成員前方同一行（`#{...} name: 'x'`）是語法錯誤，判定規則與 nolang
//     的 `#{...}` 位置規則同源。
//   - 文件級註解：寫在頂層值**上方**（或頂層值同一行尾隨）的 `#{...}` 屬於整份
//     文件，保存在 Annotations 的空路徑 `""`。`#{name = 'server-config'}`（或
//     `#{type = '...'}`）指定重建後的結構體名，預設用檔名。
//
// 成員註解目前用於攜帶「元素型別」（`#{type = '[]str'}`）：noml 值本身無型別
// （`[]`、`0`、`'0'` 都合法），要由 noml 重建強型別結構（例如 nolang 直接
// 引入 .noml 生成結構體）就必須知道每個成員的型別；空容器無法推斷，必須
// 顯式標註。
//
// 解析產出 Go 原生型別：map[string]any / []any / string / float64 / bool / nil，
// 再以 DecodeInto（反射，沿用 struct 的 json tag 欄位名）落地到強型別結構。
// dependencies 等 map[string]string 欄位在 noml 中是字串陣列，元素形如
// 'key:version'（version 可省略，預設 '*'）。
//
// 兩套入口：ParseNoml/ParseNomlMap 回傳純值樹（註解被解析但不保留）；
// ParseNomlDoc 額外保留每個成員的註解（路徑 → 條目），配合 MarshalNomlDoc
// 可原樣往返（重建）。

// NomlFileNameRule 描述三類配置檔的檔名（硬切 .jsonc → .noml）。
const (
	PackageManifestName   = "package.noml"
	WorkspaceFileName     = "workspace.noml"
	WorkspacePrivateName  = ".workspace.noml"
	LockFileName          = "nolang.lock.noml"
	SumFileName           = "sum.noml"
	defaultDependencySpec = "*"
)

// ─── 詞法輔助 ────────────────────────────────────────────────────────────────

// isNomlIdentStart 報告 c 是否可作 noml 裸鍵的首字元：下劃線或小寫字母
// （不能以數字開頭，規則同 nolang 變數名）。
func isNomlIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z')
}

// isNomlIdentChar 報告 c 是否可作 noml 裸鍵的後續字元：小寫字母、數字、
// 下劃線、中劃線（中間只能是中劃線）。
func isNomlIdentChar(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// IsNomlIdentifierKey 報告 s 是否可寫成 noml 裸鍵。
func IsNomlIdentifierKey(s string) bool {
	if s == "" || !isNomlIdentStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isNomlIdentChar(s[i]) {
			return false
		}
	}
	return true
}

// ─── 解析器 ──────────────────────────────────────────────────────────────────

// NomlAnnotation 是寫在 noml 物件成員上的 `#{key = value}` 註解條目。
//
// 語法與 nolang 的 `#{...}` 一致：多個條目以逗號分隔；值支援單引號字串、
// 數字、true/false/null、裸識別字與陣列；裸鍵（`#{debug}`）的 Value 為 nil。
// 位置規則也一致：只允許獨立成行寫在成員上方，或寫在成員同一行的尾隨位置。
//
// 有語意的鍵是 `type`（值為 nolang 型別表達式字串，如 `'[]str'`、`'i64'`、
// `'compiler-options'`），其餘鍵作為元數據原樣保留。成員型別只由該成員的註解
// 攜帶——這是「從 noml 重建強型別結構」的前提。文件級註解（空路徑 `""`）另
// 支援 `name`，指定重建後的結構體名。
//
// 註解不進值樹，而是保存在 NomlDoc.Annotations（成員路徑 → 條目）。
type NomlAnnotation struct {
	Key   string
	Value any // string / float64 / bool / nil / []any
	Line  int
}

// NomlDoc 是帶註解的 noml 文件。Root 是物件值樹（頂層非物件時為 nil），
// Annotations 以成員路徑（`compiler.version`、`dependencies`）為 key 保存
// 該成員的註解條目。路徑段使用原始鍵名；陣列元素不携带獨立註解。
// 空路徑 `""` 是文件級註解（寫在頂層值上方或同一行尾隨）。
type NomlDoc struct {
	Root        map[string]any
	Value       any
	Annotations map[string][]NomlAnnotation
}

// TypeOf 回傳某成員 `#{type = '...'}` 標註的型別字串（未標註回傳 ""）。
// 傳空路徑即回傳文件級 `#{type = '...'}`（整個文檔的型別 = 結構體名）。
func (d *NomlDoc) TypeOf(path string) string {
	for _, a := range d.Annotations[path] {
		if a.Key != "type" {
			continue
		}
		if s, ok := a.Value.(string); ok {
			return s
		}
	}
	return ""
}

// RootName 回傳文件級註解指定的結構體名：`#{name = '...'}` 優先，其次
// `#{type = '...'}`（頂部註解描述整份文件，其 type 就是重建後的結構體名）。
// 未標註回傳 ""，呼叫方退回預設規則（檔名）。名字的正規化與合法性由導入方
// 按 nolang 識別符規則處理，本函式只取註解原文。
func (d *NomlDoc) RootName() string {
	if d == nil {
		return ""
	}
	name := ""
	for _, a := range d.Annotations[""] {
		s, ok := a.Value.(string)
		if !ok || s == "" {
			continue
		}
		if a.Key == "name" {
			return s
		}
		if a.Key == "type" && name == "" {
			name = s
		}
	}
	return name
}

type nomlParser struct {
	data []byte
	pos  int
	line int
	col  int
	// anns 收集成員註解（路徑 → 條目），成員路徑由 parseValue 的 path 參數累積。
	anns map[string][]NomlAnnotation
}

// ParseNoml 解析一份 noml 設定檔，回傳 Go 原生值樹
// （map[string]any / []any / string / float64 / bool / nil）。成員註解會
// 一併解析（語法錯誤仍會報錯），但不在回傳值中保留；需要註解時用 ParseNomlDoc。
func ParseNoml(data []byte) (any, error) {
	doc, err := ParseNomlDoc(data)
	if err != nil {
		return nil, err
	}
	return doc.Value, nil
}

// ParseNomlDoc 解析 noml 並保留成員註解（路徑 → `#{...}` 條目）。
func ParseNomlDoc(data []byte) (*NomlDoc, error) {
	p := &nomlParser{data: data, line: 1, col: 1, anns: make(map[string][]NomlAnnotation)}
	p.skipTrivia()
	if p.pos >= len(p.data) {
		return nil, p.errf("empty input")
	}
	// 文件級註解：寫在頂層值上方的 `#{...}`（可多個群組，各自獨立成行）。
	var rootAnns []NomlAnnotation
	for {
		group, err := p.parseLeadingAnnotations()
		if err != nil {
			return nil, err
		}
		if len(group) == 0 {
			break
		}
		rootAnns = append(rootAnns, group...)
		p.skipTrivia()
		if p.pos >= len(p.data) {
			return nil, p.errf("annotation without a value to attach to")
		}
	}
	v, err := p.parseValue("")
	if err != nil {
		return nil, err
	}
	// 頂層值同一行尾隨的註解同樣屬於整份文件。
	trail, err := p.parseTrailingAnnotations()
	if err != nil {
		return nil, err
	}
	if all := dedupeNomlAnnotations(append(rootAnns, trail...)); len(all) > 0 {
		p.anns[""] = all
	}
	p.skipTrivia()
	if p.pos < len(p.data) {
		return nil, p.errf("unexpected character %q after value", p.data[p.pos])
	}
	doc := &NomlDoc{Value: v, Annotations: p.anns}
	if m, ok := v.(map[string]any); ok {
		doc.Root = m
	}
	return doc, nil
}

// ParseNomlMap 解析 noml 頂層物件。
func ParseNomlMap(data []byte) (map[string]any, error) {
	v, err := ParseNoml(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, i18n.Errorf("97nc8yti", "noml: expected object, got %T", v)
	}
	return m, nil
}

// parseValue 解析一個值。path 是當前成員路徑（如 "compiler.version"），
// 僅用於把下層成員的註解歸到正確路徑。
func (p *nomlParser) parseValue(path string) (any, error) {
	p.skipTrivia()
	if p.pos >= len(p.data) {
		return nil, p.errf("unexpected end of input")
	}
	c := p.data[p.pos]
	// 註解寫在值的位臵（`key: #{type = 'x'} 1`）是位置規則錯誤，而不是
	// 非法值；單獨報出來，提示使用者放到成員上方或同一行尾隨。
	if c == '#' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '{' {
		return nil, p.errf("`#{...}` annotation must be on its own line above the member, or trailing on the member's line (not before the member's value)")
	}
	switch {
	case c == '{':
		return p.parseObject(path)
	case c == '[':
		return p.parseArray(path)
	case c == '\'':
		return p.parseString()
	case c == '`':
		return p.parseRawString()
	case c == 't' || c == 'f':
		return p.parseBool()
	case c == 'n':
		return p.parseNull()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return nil, p.errf("unexpected character %q in value", c)
	}
}

// memberPath 拼接成員路徑（頂層成員就是鍵本身）。
func memberPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// parseObject 解析 { 成員... }，成員以換行分隔（無逗號）。成員上方或
// 同一行尾隨的 `#{...}` 會收集到 p.anns[成員路徑]。
func (p *nomlParser) parseObject(path string) (map[string]any, error) {
	p.advance() // skip {
	result := make(map[string]any)
	seen := make(map[string]bool)
	for {
		p.skipTrivia()
		if p.pos >= len(p.data) {
			return nil, p.errf("unexpected end of input in object")
		}
		if p.data[p.pos] == '}' {
			p.advance()
			return result, nil
		}
		// 成員上方的 `#{...}`（獨立成行）。
		anns, err := p.parseLeadingAnnotations()
		if err != nil {
			return nil, err
		}
		if len(anns) > 0 {
			p.skipTrivia()
			if p.pos >= len(p.data) || p.data[p.pos] == '}' {
				return nil, p.errf("annotation without a member to attach to")
			}
		}
		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, p.errf("duplicate key %q", key)
		}
		seen[key] = true
		p.skipInlineSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, p.errf("expected ':' after key %q", key)
		}
		p.advance() // skip :
		mpath := memberPath(path, key)
		val, err := p.parseValue(mpath)
		if err != nil {
			return nil, err
		}
		result[key] = val
		// 成員同一行的尾隨 `#{...}`。
		trail, err := p.parseTrailingAnnotations()
		if err != nil {
			return nil, err
		}
		if all := dedupeNomlAnnotations(append(anns, trail...)); len(all) > 0 {
			p.anns[mpath] = all
		}
		if err := p.requireMemberEnd(key); err != nil {
			return nil, err
		}
	}
}

// parseArray 解析 [ 元素, ... ]，元素以逗號分隔，支援尾隨逗號。
func (p *nomlParser) parseArray(path string) ([]any, error) {
	p.advance() // skip [
	var result []any
	for {
		p.skipTrivia()
		if p.pos >= len(p.data) {
			return nil, p.errf("unexpected end of input in array")
		}
		if p.data[p.pos] == ']' {
			p.advance()
			return result, nil
		}
		val, err := p.parseValue(path)
		if err != nil {
			return nil, err
		}
		result = append(result, val)
		p.skipTrivia()
		if p.pos >= len(p.data) {
			return nil, p.errf("unexpected end of input in array")
		}
		switch p.data[p.pos] {
		case ',':
			p.advance()
		case ']':
			p.advance()
			return result, nil
		default:
			return nil, p.errf("expected ',' or ']' in array, got %q", p.data[p.pos])
		}
	}
}

// ─── 成員註解 #{...} ────────────────────────────────────────────────────────

// parseLeadingAnnotations 解析寫在成員上方的註解群組（可連續多個）。
// 位置規則與 nolang 一致：註解必須獨立成行（行首只有空白）；若 `}` 之後
// 同一行還有程式碼（前綴寫法 `#{...} name: 'x'`），報錯。
func (p *nomlParser) parseLeadingAnnotations() ([]NomlAnnotation, error) {
	var out []NomlAnnotation
	for {
		p.skipInlineBlanks()
		if p.pos >= len(p.data) || p.data[p.pos] != '#' {
			return out, nil
		}
		if !p.atLineStart() {
			return nil, p.errf("`#{...}` annotation must be on its own line above the member, or trailing on the member's line (not before it on the same line)")
		}
		group, err := p.parseAnnotationGroup()
		if err != nil {
			return nil, err
		}
		out = append(out, group...)
		// 註解群組後面同一行不得再有程式碼（只能換行、; 註解或下一個註解群組）。
		p.skipInlineBlanks()
		if p.pos < len(p.data) && p.data[p.pos] == '#' {
			continue
		}
		if p.pos < len(p.data) && p.data[p.pos] != '\n' && p.data[p.pos] != ';' && p.data[p.pos] != '\r' {
			return nil, p.errf("`#{...}` annotation must not share its line with the member; put it on the line above or after the member's value")
		}
		return out, nil
	}
}

// parseTrailingAnnotations 解析成員值同一行尾隨的註解群組。
// 不跨越換行——換行後的 `#{...}` 屬於下一個成員。
func (p *nomlParser) parseTrailingAnnotations() ([]NomlAnnotation, error) {
	var out []NomlAnnotation
	for {
		p.skipInlineBlanks()
		if p.pos >= len(p.data) || p.data[p.pos] != '#' {
			return out, nil
		}
		group, err := p.parseAnnotationGroup()
		if err != nil {
			return nil, err
		}
		out = append(out, group...)
	}
}

// parseAnnotationGroup 解析 `#{key = value, ...}`。裸鍵（`#{debug}`）的
// Value 為 nil，與 nolang 註解語意一致。
func (p *nomlParser) parseAnnotationGroup() ([]NomlAnnotation, error) {
	line := p.line
	if p.pos+1 >= len(p.data) || p.data[p.pos] != '#' || p.data[p.pos+1] != '{' {
		return nil, p.errf("expected '#{'")
	}
	p.advance() // skip #
	p.advance() // skip {
	var out []NomlAnnotation
	for {
		p.skipAnnotationSpace()
		if p.pos >= len(p.data) {
			return nil, p.errf("unterminated annotation (missing '}')")
		}
		if p.data[p.pos] == '}' {
			p.advance()
			return out, nil
		}
		key, err := p.parseAnnotationKey()
		if err != nil {
			return nil, err
		}
		p.skipInlineBlanks()
		var val any
		if p.pos < len(p.data) && p.data[p.pos] == '=' {
			p.advance() // skip =
			p.skipInlineBlanks()
			val, err = p.parseAnnotationValue()
			if err != nil {
				return nil, err
			}
		}
		out = append(out, NomlAnnotation{Key: key, Value: val, Line: line})
		p.skipInlineBlanks()
		if p.pos < len(p.data) && p.data[p.pos] == ',' {
			p.advance()
			continue
		}
		if p.pos < len(p.data) && p.data[p.pos] == '}' {
			p.advance()
			return out, nil
		}
		return nil, p.errf("expected ',' or '}' in annotation, got %q", p.data[p.pos])
	}
}

// parseAnnotationKey 解析註解鍵（nolang 標識符規則）。
func (p *nomlParser) parseAnnotationKey() (string, error) {
	start := p.pos
	if p.pos >= len(p.data) || !isNomlIdentStart(p.data[p.pos]) {
		return "", p.errf("expected annotation key")
	}
	p.advance()
	for p.pos < len(p.data) && isNomlIdentChar(p.data[p.pos]) {
		p.advance()
	}
	return string(p.data[start:p.pos]), nil
}

// parseAnnotationValue 解析註解值：單引號字串、數字、true/false/null、
// 裸識別字（含 `/` `.` 等路徑字元）與陣列。
func (p *nomlParser) parseAnnotationValue() (any, error) {
	if p.pos >= len(p.data) {
		return nil, p.errf("expected annotation value")
	}
	c := p.data[p.pos]
	switch {
	case c == '\'':
		return p.parseString()
	case c == '`':
		// 多行說明文字（模組 / 方法 / 函式的 doc 註解）走原始字串，
		// 這樣翻譯索引對應的文字可以保持原本的換行與縮排。
		return p.parseRawString()
	case c == '[':
		p.advance() // skip [
		var arr []any
		for {
			p.skipAnnotationSpace()
			if p.pos >= len(p.data) {
				return nil, p.errf("unterminated annotation array")
			}
			if p.data[p.pos] == ']' {
				p.advance()
				return arr, nil
			}
			v, err := p.parseAnnotationValue()
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
			p.skipAnnotationSpace()
			if p.pos < len(p.data) && p.data[p.pos] == ',' {
				p.advance()
			}
		}
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	case c == 't' || c == 'f' || c == 'n':
		if p.matchKeyword("true") {
			return true, nil
		}
		if p.matchKeyword("false") {
			return false, nil
		}
		if p.matchKeyword("null") {
			return nil, nil
		}
		return p.parseAnnotationIdent()
	default:
		return p.parseAnnotationIdent()
	}
}

// parseAnnotationIdent 解析裸識別字值（型別名、封閉取值、路徑等）。
func (p *nomlParser) parseAnnotationIdent() (string, error) {
	start := p.pos
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ',' || c == '}' || c == ']' {
			break
		}
		p.advance()
	}
	if p.pos == start {
		return "", p.errf("expected annotation value")
	}
	return string(p.data[start:p.pos]), nil
}

// dedupeNomlAnnotations 依鍵去重，保留最後一次（與 nolang 註解去重語意一致）。
func dedupeNomlAnnotations(anns []NomlAnnotation) []NomlAnnotation {
	if len(anns) <= 1 {
		return anns
	}
	out := make([]NomlAnnotation, 0, len(anns))
	index := make(map[string]int, len(anns))
	for _, a := range anns {
		if i, ok := index[a.Key]; ok {
			out[i] = a
			continue
		}
		index[a.Key] = len(out)
		out = append(out, a)
	}
	return out
}

// atLineStart 報告當前位置是否為行首（其前只有空白）。
func (p *nomlParser) atLineStart() bool {
	for i := p.pos - 1; i >= 0; i-- {
		c := p.data[i]
		if c == '\n' {
			return true
		}
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}

// skipInlineBlanks 只跳過行內空白與 ; 行註解，不跨越換行。
func (p *nomlParser) skipInlineBlanks() {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ' ' || c == '\t' {
			p.advance()
			continue
		}
		return
	}
}

// skipAnnotationSpace 跳過註解內部的空白與換行（註解可跨行書寫）。
func (p *nomlParser) skipAnnotationSpace() {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			p.advance()
			continue
		}
		return
	}
}

// parseKey 解析裸識別符鍵或單引號字串鍵。
func (p *nomlParser) parseKey() (string, error) {
	if p.pos >= len(p.data) {
		return "", p.errf("unexpected end of input, expected key")
	}
	if p.data[p.pos] == '\'' {
		return p.parseString()
	}
	start := p.pos
	if !isNomlIdentStart(p.data[p.pos]) {
		return "", p.errf("expected key, got %q (non-identifier keys must be single-quoted)", p.data[p.pos])
	}
	p.advance()
	for p.pos < len(p.data) && isNomlIdentChar(p.data[p.pos]) {
		p.advance()
	}
	return string(p.data[start:p.pos]), nil
}

// parseString 解析單引號字串（含 \ 轉義）。
func (p *nomlParser) parseString() (string, error) {
	if p.data[p.pos] != '\'' {
		return "", p.errf("expected \"'\"")
	}
	p.advance()
	var sb strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == '\'' {
			p.advance()
			return sb.String(), nil
		}
		if c == '\n' {
			return "", p.errf("unterminated string")
		}
		if c == '\\' {
			p.advance()
			if p.pos >= len(p.data) {
				return "", p.errf("unexpected end of input in string escape")
			}
			esc := p.data[p.pos]
			p.advance()
			switch esc {
			case '\'', '\\', '/':
				sb.WriteByte(esc)
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case '0':
				sb.WriteByte(0)
			case 'u':
				if p.pos+4 > len(p.data) {
					return "", p.errf("incomplete unicode escape")
				}
				hexStr := string(p.data[p.pos : p.pos+4])
				code, err := strconv.ParseInt(hexStr, 16, 32)
				if err != nil {
					return "", p.errf("invalid unicode escape \\u%s", hexStr)
				}
				p.pos += 4
				p.col += 4
				sb.WriteRune(rune(code))
			default:
				return "", p.errf("invalid escape \\%c", esc)
			}
			continue
		}
		sb.WriteByte(c)
		p.advance()
	}
	return "", p.errf("unterminated string")
}

// parseRawString 解析反引號原始字串，語法與 nolang 原始碼的反引號字面量完全一致：
//
//   - 開頭 ` 後必須緊跟一個換行（LF，或 Windows 檔案的 CRLF）
//   - 結尾 ` 必須單獨佔一行（該行只允許空白 + `）
//   - 首尾兩行不計入內容
//   - 內部除 CR 外所有字元原樣保留，**不做任何轉義解析**（所以 `\n` 就是反斜線加 n）
//     CR 一律丟棄，故 CRLF 檔案與 LF 檔案得到逐位元組相同的值
//
// 存在的理由：單引號字串禁止跨行，長文案（診斷訊息、模組/函式說明）只能寫成
// `\n` 轉義，既不可讀也容易被翻譯者改壞。原始字串讓多行文案保持原樣。
func (p *nomlParser) parseRawString() (string, error) {
	if p.pos >= len(p.data) || p.data[p.pos] != '`' {
		return "", p.errf("expected \"`\"")
	}
	p.advance() // 跳過開頭 `

	// 開頭 ` 後必須緊跟換行：LF，或 CRLF（Windows 檔案）
	if p.pos+1 < len(p.data) && p.data[p.pos] == '\r' && p.data[p.pos+1] == '\n' {
		p.advance() // 吃掉 CR，下面只認 LF
	}
	if p.pos >= len(p.data) || p.data[p.pos] != '\n' {
		return "", p.errf("raw string: the opening ` must be followed by a newline")
	}
	p.advance() // 跳過開頭換行

	var sb strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]

		// 結尾偵測：換行之後的一行若只有空白 + ` 就是結束標記。
		if c == '\n' {
			j := p.pos + 1
			for j < len(p.data) && (p.data[j] == ' ' || p.data[j] == '\t' || p.data[j] == '\r') {
				j++
			}
			if j < len(p.data) && p.data[j] == '`' {
				for p.pos <= j {
					p.advance()
				}
				return sb.String(), nil
			}
			sb.WriteByte('\n')
			p.advance()
			continue
		}
		if c == '\r' {
			// CR 一律丟棄（與原始碼 lexer 同口徑）：CRLF 與 LF 檔案得到相同的值。
			p.advance()
			continue
		}
		sb.WriteByte(c)
		p.advance()
	}
	return "", p.errf("unterminated raw string (the closing ` must be alone on its own line)")
}

func (p *nomlParser) parseBool() (bool, error) {
	if p.matchKeyword("true") {
		return true, nil
	}
	if p.matchKeyword("false") {
		return false, nil
	}
	return false, p.errf("invalid boolean literal")
}
func (p *nomlParser) parseNull() (any, error) {
	if p.matchKeyword("null") {
		return nil, nil
	}
	return nil, p.errf("invalid null literal")
}

func (p *nomlParser) parseNumber() (float64, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.advance()
	}
	for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
		p.advance()
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.advance()
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.advance()
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		save := p.pos
		p.advance()
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.advance()
		}
		digits := 0
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.advance()
			digits++
		}
		if digits == 0 {
			// 不是指數（例如 '2' 後接 'entity'）：回退。
			p.pos = save
		}
	}
	numStr := string(p.data[start:p.pos])
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, p.errf("invalid number %q", numStr)
	}
	return num, nil
}

// requireMemberEnd 校驗物件成員結束後必須是行內空白、換行、或容器閉合。
// 規則：成員之间以換行分隔；同一行上僅允許行內空白（; 註解必須以換行收尾，
// 註解本身由 skipTrivia 在下一輪處理）。
func (p *nomlParser) requireMemberEnd(key string) error {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ' ' || c == '\t' || c == '\r' {
			p.advance()
			continue
		}
		if c == '\n' {
			p.advance()
			return nil
		}
		if c == '}' {
			return nil // 交由外層迴圈閉合容器
		}
		if c == ';' {
			// 行尾註解：吃到換行；換行即成員分隔。
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.advance()
			}
			if p.pos < len(p.data) {
				p.advance() // skip '\n'
			}
			return nil
		}
		return p.errf("expected newline after member %q, got %q (object members are newline-separated)", key, c)
	}
	return p.errf("unexpected end of input in object (after key %q)", key)
}

// skipTrivia 跳過空白（含換行）與 ; 行註解。
func (p *nomlParser) skipTrivia() {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.advance()
		case c == ';':
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.advance()
			}
		default:
			return
		}
	}
}

// skipInlineSpace 只跳行內空白與 ; 註解，不跨越換行。
func (p *nomlParser) skipInlineSpace() {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c == ' ' || c == '\t' {
			p.advance()
			continue
		}
		if c == ';' {
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.advance()
			}
			continue
		}
		return
	}
}

func (p *nomlParser) matchKeyword(kw string) bool {
	if p.pos+len(kw) > len(p.data) {
		return false
	}
	if string(p.data[p.pos:p.pos+len(kw)]) != kw {
		return false
	}
	// 關鍵字後不能緊接識別符字元。
	if p.pos+len(kw) < len(p.data) {
		c := p.data[p.pos+len(kw)]
		if isNomlIdentChar(c) {
			return false
		}
	}
	p.pos += len(kw)
	p.col += len(kw)
	return true
}

func (p *nomlParser) advance() {
	if p.pos < len(p.data) {
		if p.data[p.pos] == '\n' {
			p.line++
			p.col = 1
		} else {
			p.col++
		}
		p.pos++
	}
}

func (p *nomlParser) errf(format string, args ...any) error {
	return i18n.Errorf("m8blb8q2", "noml: line %d col %d: %s", p.line, p.col, fmt.Sprintf(format, args...))
}

// ─── 序列化 ──────────────────────────────────────────────────────────────────

// NomlQuote 產生單引號 noml 字串面值。
func NomlQuote(s string) string {
	var sb strings.Builder
	sb.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\'':
			sb.WriteString(`\'`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if c < 0x20 {
				sb.WriteString(fmt.Sprintf(`\u%04x`, c))
			} else {
				sb.WriteByte(c)
			}
		}
	}
	sb.WriteByte('\'')
	return sb.String()
}

// NomlQuoteValue 產生 noml 值面值。含換行的字串用反引號原始字串輸出（見
// parseRawString），單行字串仍用單引號——這樣長文案在檔案裡保持可讀，而不是
// 變成一長串 `\n` 轉義。
//
// 只有當內容能安全往返時才用原始形式：任何一行若去掉前導空白後恰好是 `，就會
// 被解析器當成結束標記，這種字串退回單引號形式（`\n` 轉義仍然正確）。
func NomlQuoteValue(s string) string {
	if strings.ContainsRune(s, '\n') && rawStringSafe(s) {
		var sb strings.Builder
		sb.WriteString("`\n")
		sb.WriteString(s)
		sb.WriteString("\n`")
		return sb.String()
	}
	return NomlQuote(s)
}

// rawStringSafe 判斷 s 放進反引號區塊後能否原樣取回。
//
// 判據必須與 parseRawString 的結束偵測完全一致：那裡是「換行之後的一行若只有
// 空白 + ` 就是結束標記」，即只要某行去掉前導空白後以 ` 開頭就終止區塊。所以
// 用 HasPrefix 而不是 == —— 一行以 ` 開頭但後面還有內容（例如 markdown 行內
// 程式碼 `#{overflow=wrap}`；u128 的註解就是這樣）同樣會提前收尾，寫出去的
// 檔案再讀回來就報 "expected newline after member"。
func rawStringSafe(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " \t\r"), "`") {
			return false
		}
	}
	return true
}

// nomlFormatKey 依裸鍵規則決定是否加引號。
func nomlFormatKey(k string) string {
	if IsNomlIdentifierKey(k) {
		return k
	}
	return NomlQuote(k)
}

// nomlFormatNumber 輸出整數面值（float64 且為整值時不帶小數點）。
func nomlFormatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// MarshalNoml 把值樹（map[string]any / []any / string / float64 / bool / nil，
// 亦接受任意 map[string]string）序列化為 noml 文字。物件鍵排序輸出以求確定性。
func MarshalNoml(v any) (string, error) {
	var sb strings.Builder
	if err := nomlWrite(&sb, v, 0); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// MarshalNomlDoc 序列化帶註解的 noml 文件：文件級註解（空路徑）輸出在頂層值
// 上方的獨立一行，成員的 `#{...}` 註解輸出在成員上方的獨立一行（與解析端接受
// 的位置一致），因此 ParseNomlDoc → MarshalNomlDoc 可完整往返（重建）。
func MarshalNomlDoc(doc *NomlDoc) (string, error) {
	if doc == nil {
		return "", nil
	}
	var sb strings.Builder
	if entries := doc.Annotations[""]; len(entries) > 0 {
		// 文件級註解輸出在頂層值上方的獨立一行（與解析端接受的位置一致）。
		sb.WriteString("#{")
		out := make([]string, 0, len(entries))
		for _, a := range entries {
			out = append(out, FormatNomlAnnotation(a))
		}
		sb.WriteString(strings.Join(out, ", "))
		sb.WriteString("}\n")
	}
	if err := nomlWriteAnnotated(&sb, doc.Value, 0, "", doc.Annotations); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// FormatNomlAnnotation 輸出單個註解條目（`key = value` 或裸鍵 `key`）。
func FormatNomlAnnotation(a NomlAnnotation) string {
	if a.Value == nil {
		return a.Key
	}
	return a.Key + " = " + nomlAnnotationValue(a.Value)
}

// nomlAnnotationValue 輸出註解值面值（與解析端接受的語法一致）。
func nomlAnnotationValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return NomlQuote(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return nomlFormatNumber(t)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, nomlAnnotationValue(e))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return NomlQuote(fmt.Sprint(t))
	}
}

// nomlNormalize 把 Go 原生容器（map[string]string 等）轉成值樹形态。
func nomlNormalize(v any) (any, error) {
	switch t := v.(type) {
	case map[string]string:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = val
		}
		return m, nil
	case []string:
		out := make([]any, 0, len(t))
		for _, s := range t {
			out = append(out, s)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			n, err := nomlNormalize(val)
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(t))
		for _, val := range t {
			n, err := nomlNormalize(val)
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		}
		return out, nil
	default:
		rv := reflect.ValueOf(v)
		if !rv.IsValid() {
			return nil, nil
		}
		switch rv.Kind() {
		case reflect.Map:
			if rv.Type().Key().Kind() != reflect.String {
				return nil, i18n.Errorf("89pwo0vg", "noml: map key must be string: %T", v)
			}
			m := make(map[string]any, rv.Len())
			iter := rv.MapRange()
			for iter.Next() {
				n, err := nomlNormalize(iter.Value().Interface())
				if err != nil {
					return nil, err
				}
				m[iter.Key().String()] = n
			}
			return m, nil
		case reflect.Slice, reflect.Array:
			out := make([]any, 0, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				n, err := nomlNormalize(rv.Index(i).Interface())
				if err != nil {
					return nil, err
				}
				out = append(out, n)
			}
			return out, nil
		case reflect.String:
			return rv.String(), nil
		case reflect.Bool:
			return rv.Bool(), nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return float64(rv.Int()), nil
		case reflect.Float32, reflect.Float64:
			return rv.Float(), nil
		default:
			return nil, i18n.Errorf("8sf1ugcv", "noml: unsupported value type %T", v)
		}
	}
}

func nomlWrite(sb *strings.Builder, v any, indent int) error {
	n, err := nomlNormalize(v)
	if err != nil {
		return err
	}
	return nomlWriteAnnotated(sb, n, indent, "", nil)
}

// nomlWriteAnnotated 寫出值樹；物件成員若有註解，先輸出註解行。
func nomlWriteAnnotated(sb *strings.Builder, v any, indent int, path string, anns map[string][]NomlAnnotation) error {
	pad := strings.Repeat("  ", indent)
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case string:
		sb.WriteString(NomlQuote(t))
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case float64:
		sb.WriteString(nomlFormatNumber(t))
	case map[string]any:
		sb.WriteString("{\n")
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			mpath := memberPath(path, k)
			if entries := anns[mpath]; len(entries) > 0 {
				sb.WriteString(pad)
				sb.WriteString("  ")
				sb.WriteString("#{")
				parts := make([]string, 0, len(entries))
				for _, a := range entries {
					parts = append(parts, FormatNomlAnnotation(a))
				}
				sb.WriteString(strings.Join(parts, ", "))
				sb.WriteString("}\n")
			}
			sb.WriteString(pad)
			sb.WriteString("  ")
			sb.WriteString(nomlFormatKey(k))
			sb.WriteString(": ")
			if err := nomlWriteAnnotated(sb, t[k], indent+1, mpath, anns); err != nil {
				return err
			}
			sb.WriteByte('\n')
		}
		sb.WriteString(pad)
		sb.WriteString("}")
	case []any:
		if len(t) == 0 {
			sb.WriteString("[]")
			return nil
		}
		sb.WriteString("[\n")
		for _, elem := range t {
			sb.WriteString(pad)
			sb.WriteString("  ")
			if err := nomlWriteAnnotated(sb, elem, indent+1, path, anns); err != nil {
				return err
			}
			sb.WriteString(",\n") // 尾隨逗號：陣列元素一律帶逗號結尾
		}
		sb.WriteString(pad)
		sb.WriteString("]")
	default:
		return i18n.Errorf("8sf1ugcv", "noml: unsupported value type %T", v)
	}
	return nil
}

// ─── 依賴元素 'key:version' ─────────────────────────────────────────────────

// SplitDepSpec 拆解 noml 依賴陣列元素為 (key, version)。
//
// 規則：以「最後一段路徑之後」的最右側 ':' 切分 key 與 version；
// 若切出的 version 形如版本約束（^ ~ >= <= = v X * 數字開頭），採用之，
// 否則整個元素都是 key（version 預設 '*'）。另外支援 nogit 內嵌版本形式
// 'nogit:v0.1.33@github.com/o/r'：此形式沒有獨立 version 段，
// 把內嵌版本拆出來，key 改寫成 'nogit@github.com/o/r'。
func SplitDepSpec(spec string) (string, string) {
	// 先剝離 nogit 內嵌版本（'nogit:v0.1.33@host/o/r' 不含獨立 version 段）。
	if strings.HasPrefix(spec, "nogit:") {
		rest := spec[len("nogit:"):]
		if at := strings.Index(rest, "@"); at > 0 {
			if embedded := rest[:at]; looksNomlVersion(embedded) {
				return "nogit" + rest[at:], embedded
			}
		}
	}

	lastSlash := strings.LastIndex(spec, "/")
	colon := strings.LastIndex(spec, ":")
	if colon > lastSlash && colon >= 0 {
		key, ver := spec[:colon], spec[colon+1:]
		if looksNomlVersion(ver) {
			return key, ver
		}
	}
	return spec, defaultDependencySpec
}

// looksNomlVersion 報告 s 是否像版本約束（^ ~ >= <= = 前綴、v 開頭、
// 純數字/星號開頭，或 '*'）。
func looksNomlVersion(s string) bool {
	if s == "" {
		return false
	}
	switch {
	case s == "*":
		return true
	case strings.HasPrefix(s, "^"), strings.HasPrefix(s, "~"),
		strings.HasPrefix(s, ">="), strings.HasPrefix(s, "<="),
		strings.HasPrefix(s, "="):
		return true
	case s[0] == 'v' || (s[0] >= '0' && s[0] <= '9'):
		return true
	}
	return false
}

// FormatDepSpec 合成 noml 依賴陣列元素；version 為空或 '*' 時省略。
func FormatDepSpec(key, version string) string {
	if version == "" || version == defaultDependencySpec {
		return key
	}
	return key + ":" + version
}

// ─── 反射落地（沿用 json tag 欄位名） ───────────────────────────────────────

// DecodeInto 把 ParseNoml 的值樹落地到 dst（指標）。struct 欄位名沿用既有
// json tag（第一個逗號前的名字）。noml 特例：目標為 map[string]string 且
// 來源是字串陣列時，按 SplitDepSpec 逐元素拆成 key→version。
func DecodeInto(v any, dst any) error {
	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return i18n.Errorf("ayjrpinp", "noml: DecodeInto requires non-nil pointer, got %T", dst)
	}
	return nomlDecode(v, rv.Elem(), "")
}

func nomlDecode(v any, dst reflect.Value, field string) error {
	if v == nil {
		return nil // null 等同未設定
	}
	// 先處理 noml 特有語法：map[string]string ← 字串陣列。
	if dst.Kind() == reflect.Map && isStringMap(dst.Type()) {
		if arr, ok := v.([]any); ok {
			return nomlDecodeDepArray(arr, dst, field)
		}
	}
	if dst.Kind() == reflect.Pointer {
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return nomlDecode(v, dst.Elem(), field)
	}
	switch dst.Kind() {
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return i18n.Errorf("qhkl8p9i", "noml: field %s: expected string, got %T", field, v)
		}
		dst.SetString(s)
		return nil
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return i18n.Errorf("j6hiyx4e", "noml: field %s: expected bool, got %T", field, v)
		}
		dst.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f, ok := v.(float64)
		if !ok {
			return i18n.Errorf("33exppv5", "noml: field %s: expected number, got %T", field, v)
		}
		dst.SetInt(int64(f))
		return nil
	case reflect.Float32, reflect.Float64:
		f, ok := v.(float64)
		if !ok {
			return i18n.Errorf("33exppv5", "noml: field %s: expected number, got %T", field, v)
		}
		dst.SetFloat(f)
		return nil
	case reflect.Slice:
		arr, ok := v.([]any)
		if !ok {
			return i18n.Errorf("qoykdxqy", "noml: field %s: expected array, got %T", field, v)
		}
		out := reflect.MakeSlice(dst.Type(), len(arr), len(arr))
		for i, elem := range arr {
			if err := nomlDecode(elem, out.Index(i), fmt.Sprintf("%s[%d]", field, i)); err != nil {
				return err
			}
		}
		dst.Set(out)
		return nil
	case reflect.Map:
		m, ok := v.(map[string]any)
		if !ok {
			return i18n.Errorf("5eo1kvb9", "noml: field %s: expected object, got %T", field, v)
		}
		if dst.IsNil() {
			dst.Set(reflect.MakeMap(dst.Type()))
		}
		for k, val := range m {
			ev := reflect.New(dst.Type().Elem()).Elem()
			if err := nomlDecode(val, ev, field+"."+k); err != nil {
				return err
			}
			dst.SetMapIndex(reflect.ValueOf(k).Convert(dst.Type().Key()), ev)
		}
		return nil
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return i18n.Errorf("5eo1kvb9", "noml: field %s: expected object, got %T", field, v)
		}
		dst_t := dst.Type()
		for i := 0; i < dst_t.NumField(); i++ {
			sf := dst_t.Field(i)
			if sf.PkgPath != "" {
				continue // 未導出欄位
			}
			name := nomlFieldName(sf)
			if name == "" || name == "-" {
				continue
			}
			val, exists := m[name]
			if !exists {
				continue
			}
			if err := nomlDecode(val, dst.Field(i), name); err != nil {
				return err
			}
		}
		// 未知鍵略過（同舊 json.Unmarshal 行為）：清單向前相容比嚴格更重要，
		// 寫回時未识别欄位由呼叫方自行保留或丟棄。
		return nil
	default:
		return i18n.Errorf("6y4tyjln", "noml: field %s: unsupported target kind %s", field, dst.Kind())
	}
}

func isStringMap(t reflect.Type) bool {
	return t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.String
}

func nomlFieldName(sf reflect.StructField) string {
	tag := sf.Tag.Get("json")
	if tag == "" {
		return ""
	}
	return strings.Split(tag, ",")[0]
}

func nomlDecodeDepArray(arr []any, dst reflect.Value, field string) error {
	if dst.IsNil() {
		dst.Set(reflect.MakeMap(dst.Type()))
	}
	for i, elem := range arr {
		s, ok := elem.(string)
		if !ok {
			return i18n.Errorf("5ckkrp9f", "noml: field %s[%d]: dependency element must be a string, got %T", field, i, elem)
		}
		key, version := SplitDepSpec(s)
		dst.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(version))
	}
	return nil
}
