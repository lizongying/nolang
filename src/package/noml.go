package pkg

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
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
//
// 解析產出 Go 原生型別：map[string]any / []any / string / float64 / bool / nil，
// 再以 DecodeInto（反射，沿用 struct 的 json tag 欄位名）落地到強型別結構。
// dependencies 等 map[string]string 欄位在 noml 中是字串陣列，元素形如
// 'key:version'（version 可省略，預設 '*'）。

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

type nomlParser struct {
	data []byte
	pos  int
	line int
	col  int
}

// ParseNoml 解析一份 noml 設定檔，回傳 Go 原生值樹
// （map[string]any / []any / string / float64 / bool / nil）。
func ParseNoml(data []byte) (any, error) {
	p := &nomlParser{data: data, line: 1, col: 1}
	p.skipTrivia()
	if p.pos >= len(p.data) {
		return nil, p.errf("empty input")
	}
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipTrivia()
	if p.pos < len(p.data) {
		return nil, p.errf("unexpected character %q after value", p.data[p.pos])
	}
	return v, nil
}

// ParseNomlMap 解析 noml 頂層物件。
func ParseNomlMap(data []byte) (map[string]any, error) {
	v, err := ParseNoml(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("noml: expected object, got %T", v)
	}
	return m, nil
}

func (p *nomlParser) parseValue() (any, error) {
	p.skipTrivia()
	if p.pos >= len(p.data) {
		return nil, p.errf("unexpected end of input")
	}
	c := p.data[p.pos]
	switch {
	case c == '{':
		return p.parseObject()
	case c == '[':
		return p.parseArray()
	case c == '\'':
		return p.parseString()
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

// parseObject 解析 { 成員... }，成員以換行分隔（無逗號）。
func (p *nomlParser) parseObject() (map[string]any, error) {
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
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		result[key] = val
		if err := p.requireMemberEnd(key); err != nil {
			return nil, err
		}
	}
}

// parseArray 解析 [ 元素, ... ]，元素以逗號分隔，支援尾隨逗號。
func (p *nomlParser) parseArray() ([]any, error) {
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
		val, err := p.parseValue()
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
	return fmt.Errorf("noml: line %d col %d: %s", p.line, p.col, fmt.Sprintf(format, args...))
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
				return nil, fmt.Errorf("noml: map key must be string: %T", v)
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
			return nil, fmt.Errorf("noml: unsupported value type %T", v)
		}
	}
}

func nomlWrite(sb *strings.Builder, v any, indent int) error {
	n, err := nomlNormalize(v)
	if err != nil {
		return err
	}
	return nomlWriteNormalized(sb, n, indent)
}

func nomlWriteNormalized(sb *strings.Builder, v any, indent int) error {
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
			sb.WriteString(pad)
			sb.WriteString("  ")
			sb.WriteString(nomlFormatKey(k))
			sb.WriteString(": ")
			if err := nomlWriteNormalized(sb, t[k], indent+1); err != nil {
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
			if err := nomlWriteNormalized(sb, elem, indent+1); err != nil {
				return err
			}
			sb.WriteString(",\n") // 尾隨逗號：陣列元素一律帶逗號結尾
		}
		sb.WriteString(pad)
		sb.WriteString("]")
	default:
		return fmt.Errorf("noml: unsupported value type %T", v)
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
		return fmt.Errorf("noml: DecodeInto requires non-nil pointer, got %T", dst)
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
			return fmt.Errorf("noml: field %s: expected string, got %T", field, v)
		}
		dst.SetString(s)
		return nil
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return fmt.Errorf("noml: field %s: expected bool, got %T", field, v)
		}
		dst.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f, ok := v.(float64)
		if !ok {
			return fmt.Errorf("noml: field %s: expected number, got %T", field, v)
		}
		dst.SetInt(int64(f))
		return nil
	case reflect.Float32, reflect.Float64:
		f, ok := v.(float64)
		if !ok {
			return fmt.Errorf("noml: field %s: expected number, got %T", field, v)
		}
		dst.SetFloat(f)
		return nil
	case reflect.Slice:
		arr, ok := v.([]any)
		if !ok {
			return fmt.Errorf("noml: field %s: expected array, got %T", field, v)
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
			return fmt.Errorf("noml: field %s: expected object, got %T", field, v)
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
			return fmt.Errorf("noml: field %s: expected object, got %T", field, v)
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
		return fmt.Errorf("noml: field %s: unsupported target kind %s", field, dst.Kind())
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
			return fmt.Errorf("noml: field %s[%d]: dependency element must be a string, got %T", field, i, elem)
		}
		key, version := SplitDepSpec(s)
		dst.SetMapIndex(reflect.ValueOf(key), reflect.ValueOf(version))
	}
	return nil
}
