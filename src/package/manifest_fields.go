package pkg

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// package.noml 的「完整欄位集」。
//
// 唯一真相是 Package / CompilerOptions 結構體的 json tag（向後相容的 decode
// 也走同一張表）：所有可識別欄位都必須出現在檔案裡，已廢棄的 workspace 除外。
// no fmt 據此補上缺的欄位、移除多的欄位；LoadPackage 據此報 warning。
// 編譯器已移除的 main / alias / version 不再是結構體欄位，因此視為未知欄位。
//
// 這裡刻意不用 MarshalNoml 重建文件——那會排序鍵、丟註解、改單/多行版面。
// 本檔的欄位增刪是純文字手術，保留鍵序、註解、#{...} 註解與引號形式。

// ManifestField 描述一個規範欄位與它在 no fmt 補全時寫入的預設值。
type ManifestField struct {
	// Key 是 noml 成員名（與結構體 json tag 一致）。
	Key string
	// Default 是補全時寫入的 noml 字面值，例如 '' / [] / {} / true / 0。
	Default string
}

var (
	manifestFieldsOnce  sync.Once
	manifestFieldsCache []ManifestField
	compilerFieldsOnce  sync.Once
	compilerFieldsCache []ManifestField
)

// ManifestFields 回傳 package.noml 頂層的全部規範欄位（順序即補全順序）。
func ManifestFields() []ManifestField {
	manifestFieldsOnce.Do(func() {
		manifestFieldsCache = manifestFieldsFromStruct(reflect.TypeOf(Package{}), map[string]bool{"workspace": true})
	})
	return manifestFieldsCache
}

// CompilerManifestFields 回傳 compiler 子物件的全部**可識別**欄位。
func CompilerManifestFields() []ManifestField {
	compilerFieldsOnce.Do(func() {
		compilerFieldsCache = manifestFieldsFromStruct(reflect.TypeOf(CompilerOptions{}), nil)
	})
	return compilerFieldsCache
}

// nonRequiredManifestFields 是可識別但不要求寫在 package.noml 的欄位
// （路徑用點號，頂層直接寫鍵名）。no fmt 不會補它們，缺席也不 warning；
// 出現時照常解碼保留。
var nonRequiredManifestFields = map[string]bool{
	"dev-dependencies": true,
	"publish":          true,
	"mirrors":          true,
	"output":           true,
	"ignore":           true,

	"compiler.anonymous-fn-type":               true,
	"compiler.emit":                            true,
	"compiler.link-libs":                       true,
	"compiler.esm-cdn":                         true,
	"compiler.lang":                            true,
	"compiler.option-payload-inline-threshold": true,
}

// IsNonRequiredManifestField 報告 path 是否為可識別但不強制的欄位。
func IsNonRequiredManifestField(path string) bool {
	return nonRequiredManifestFields[path]
}

// RequiredManifestFields 回傳 package.noml 頂層必寫的欄位。
func RequiredManifestFields() []ManifestField {
	all := ManifestFields()
	out := make([]ManifestField, 0, len(all))
	for _, f := range all {
		if !IsNonRequiredManifestField(f.Key) {
			out = append(out, f)
		}
	}
	return out
}

// RequiredCompilerManifestFields 回傳 compiler 子物件中必寫的欄位。
func RequiredCompilerManifestFields() []ManifestField {
	all := CompilerManifestFields()
	out := make([]ManifestField, 0, len(all))
	for _, f := range all {
		if !IsNonRequiredManifestField("compiler." + f.Key) {
			out = append(out, f)
		}
	}
	return out
}

func manifestFieldsFromStruct(t reflect.Type, skip map[string]bool) []ManifestField {
	out := make([]ManifestField, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" || skip[tag] {
			continue
		}
		out = append(out, ManifestField{Key: tag, Default: manifestDefaultValue(tag, f.Type)})
	}
	return out
}

func manifestDefaultValue(key string, t reflect.Type) string {
	// dependencies / dev-dependencies 在 package.noml 裡一律是 vec（'key:version'
	// 字串陣列），即使 Go 側為方便查詢解成 map[string]string 也一樣。
	if key == "dependencies" || key == "dev-dependencies" {
		return "[]"
	}
	switch t.Kind() {
	case reflect.String:
		return "''"
	case reflect.Bool:
		if key == "publish" {
			return "true"
		}
		return "false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "0"
	case reflect.Map, reflect.Struct:
		return "{}"
	case reflect.Slice, reflect.Array:
		return "[]"
	default:
		return "''"
	}
}

// ManifestFieldIssues 是一次欄位集比對的結果。四個清單都已排序，方便輸出與測試。
type ManifestFieldIssues struct {
	Missing         []string
	Extra           []string
	CompilerMissing []string // 點號前綴 compiler.
	CompilerExtra   []string // 點號前綴 compiler.
}

// Empty 報告欄位集是否完整。
func (i ManifestFieldIssues) Empty() bool {
	return len(i.Missing) == 0 && len(i.Extra) == 0 && len(i.CompilerMissing) == 0 && len(i.CompilerExtra) == 0
}

// Describe 把差異整理成一句英文（呼叫端自行加檔名與 i18n 前綴）。
func (i ManifestFieldIssues) Describe() string {
	parts := make([]string, 0, 4)
	if len(i.Missing) > 0 {
		parts = append(parts, "missing fields: "+strings.Join(i.Missing, ", "))
	}
	if len(i.Extra) > 0 {
		parts = append(parts, "unknown fields: "+strings.Join(i.Extra, ", "))
	}
	if len(i.CompilerMissing) > 0 {
		parts = append(parts, "missing compiler fields: "+strings.Join(i.CompilerMissing, ", "))
	}
	if len(i.CompilerExtra) > 0 {
		parts = append(parts, "unknown compiler fields: "+strings.Join(i.CompilerExtra, ", "))
	}
	return strings.Join(parts, "; ")
}

// CheckManifestFields 比對一份 package.noml 文字與規範欄位集。
//
// 走純文字掃描而非 ParseNomlMap：一來這是 no fmt 的同一套看法（兩條路徑不能
// 分歧），二來語法小瑕疵（例如舊檔的雙引號）不該讓 warning 整個消失。
// 結構讀不出來時回傳空差異，交給既有的 parse error 路徑處理。
func CheckManifestFields(data []byte) ManifestFieldIssues {
	return scanManifest(string(data)).issues()
}

// NormalizeManifestFields 對 package.noml 做欄位集手術：補上缺少的欄位、移除
// 未知的欄位（連同其上方的 #{...} 註解與多行值），其餘位元組原樣保留。
//
// 回傳的第二項是「手術前」的差異，供呼叫端發 warning；排版仍交給
// fmt.FormatNomlFile，因此這裡插入的行用固定 2/4 空格即可。
func NormalizeManifestFields(src string) (string, ManifestFieldIssues, error) {
	s := scanManifest(src)
	issues := s.issues()
	if s.rootOpen < 0 || s.rootClose < 0 {
		return src, issues, fmt.Errorf("cannot locate the top-level object in package.noml")
	}

	topCanonical := make(map[string]bool, len(ManifestFields()))
	for _, f := range ManifestFields() {
		topCanonical[f.Key] = true
	}
	compilerCanonical := make(map[string]bool, len(CompilerManifestFields()))
	for _, f := range CompilerManifestFields() {
		compilerCanonical[f.Key] = true
	}

	removed := make(map[int]bool)
	for key, m := range s.topMembers {
		if !topCanonical[key] {
			for line := m.start; line <= m.end; line++ {
				removed[line] = true
			}
		}
	}
	for key, m := range s.compilerMembers {
		if !compilerCanonical[key] {
			for line := m.start; line <= m.end; line++ {
				removed[line] = true
			}
		}
	}

	// compiler 本身缺場時，補的是整個區塊（含全部子欄位），不再逐子欄位插入。
	_, hasCompiler := s.topMembers["compiler"]
	var missingTop []ManifestField
	for _, f := range RequiredManifestFields() {
		if _, ok := s.topMembers[f.Key]; !ok {
			missingTop = append(missingTop, f)
		}
	}
	var missingCompiler []ManifestField
	if hasCompiler {
		for _, f := range RequiredCompilerManifestFields() {
			if _, ok := s.compilerMembers[f.Key]; !ok {
				missingCompiler = append(missingCompiler, f)
			}
		}
	}

	emitCompiler := func(indent string) []string {
		out := []string{indent + "compiler: {"}
		for _, f := range RequiredCompilerManifestFields() {
			out = append(out, indent+"  "+f.Key+": "+f.Default)
		}
		return append(out, indent+"}")
	}
	emitField := func(indent string, f ManifestField) []string {
		if f.Key == "compiler" && f.Default == "{}" {
			return emitCompiler(indent)
		}
		return []string{indent + f.Key + ": " + f.Default}
	}

	out := make([]string, 0, len(s.lines)+len(missingTop)+len(missingCompiler))
	for i, line := range s.lines {
		if removed[i] {
			continue
		}
		// 單行空 compiler: { } 攤成多行才能補子欄位。
		if i == s.compilerLine && s.compilerInline && len(missingCompiler) > 0 {
			out = append(out, "  compiler: {")
			for _, f := range missingCompiler {
				out = append(out, "    "+f.Key+": "+f.Default)
			}
			out = append(out, "  }")
			continue
		}
		if i == s.compilerClose && !s.compilerInline {
			for _, f := range missingCompiler {
				out = append(out, "    "+f.Key+": "+f.Default)
			}
		}
		if i == s.rootClose {
			for _, f := range missingTop {
				out = append(out, emitField("  ", f)...)
			}
		}
		out = append(out, line)
	}

	res := strings.Join(out, "\n")
	if res != "" {
		res += "\n"
	}
	return res, issues, nil
}

// ─── 純文字掃描 ────────────────────────────────────────────────────────────────

// manifestMember 記錄一個物件的直接成員与其橫跨的行範圍。
type manifestMember struct {
	key       string
	line      int
	start     int // 含上方連續的 #{...} 註解
	end       int // 含多行值／原始串的結尾行
	baseDepth int
}

type manifestScan struct {
	lines           []string
	rootOpen        int
	rootClose       int
	topMembers      map[string]*manifestMember
	topOrder        []string
	compilerLine    int
	compilerClose   int
	compilerInline  bool
	compilerMembers map[string]*manifestMember
	compilerOrder   []string
}

func (s *manifestScan) issues() ManifestFieldIssues {
	var out ManifestFieldIssues
	if s.rootOpen < 0 || s.rootClose < 0 {
		return out
	}
	top := make(map[string]bool, len(s.topOrder))
	for _, k := range s.topOrder {
		top[k] = true
	}
	for _, f := range RequiredManifestFields() {
		if !top[f.Key] {
			out.Missing = append(out.Missing, f.Key)
		}
	}
	for _, k := range s.topOrder {
		if k == "compiler" {
			continue
		}
		found := false
		for _, f := range ManifestFields() {
			if f.Key == k {
				found = true
				break
			}
		}
		if !found {
			out.Extra = append(out.Extra, k)
		}
	}
	// compiler 缺席時只報 compiler 本身，不再逐子欄位列舉。
	if top["compiler"] {
		sub := make(map[string]bool, len(s.compilerOrder))
		for _, k := range s.compilerOrder {
			sub[k] = true
		}
		for _, f := range RequiredCompilerManifestFields() {
			if !sub[f.Key] {
				out.CompilerMissing = append(out.CompilerMissing, "compiler."+f.Key)
			}
		}
		for _, k := range s.compilerOrder {
			found := false
			for _, f := range CompilerManifestFields() {
				if f.Key == k {
					found = true
					break
				}
			}
			if !found {
				out.CompilerExtra = append(out.CompilerExtra, "compiler."+k)
			}
		}
	}
	sort.Strings(out.Missing)
	sort.Strings(out.Extra)
	sort.Strings(out.CompilerMissing)
	sort.Strings(out.CompilerExtra)
	return out
}

func scanManifest(src string) *manifestScan {
	lines := strings.Split(src, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	s := &manifestScan{
		lines:           lines,
		rootOpen:        -1,
		rootClose:       -1,
		compilerLine:    -1,
		compilerClose:   -1,
		topMembers:      make(map[string]*manifestMember),
		compilerMembers: make(map[string]*manifestMember),
	}

	depth := 0
	inRaw, inBlock := false, false
	rawExtend, blockExtend := -1, -1
	pendingAnn := make([]int, 0, 2)
	openMembers := make([]*manifestMember, 0, 4)

	closeMember := func(end int, m *manifestMember) {
		m.end = end
		for i, om := range openMembers {
			if om == m {
				openMembers = append(openMembers[:i], openMembers[i+1:]...)
				break
			}
		}
	}

	for i, raw := range lines {
		line := strings.TrimRight(raw, " \t\r")
		trimmed := strings.TrimLeft(line, " \t")

		if inRaw {
			if strings.HasPrefix(trimmed, "`") {
				inRaw = false
				if rawExtend >= 0 {
					for _, m := range openMembers {
						if m.line == rawExtend {
							closeMember(i, m)
						}
					}
					rawExtend = -1
				}
			}
			continue
		}
		if inBlock {
			if trimmed == ";;" {
				inBlock = false
				if blockExtend >= 0 {
					for _, m := range openMembers {
						if m.line == blockExtend {
							closeMember(i, m)
						}
					}
					blockExtend = -1
				}
			}
			continue
		}
		if trimmed == "" {
			pendingAnn = pendingAnn[:0]
			continue
		}
		if strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "#{") {
			pendingAnn = append(pendingAnn, i)
			continue
		}

		net, openRaw, openBlock := manifestScanLine(trimmed)
		closesAtStart := strings.HasPrefix(net, "}") || strings.HasPrefix(net, "]")
		before := depth
		newMembers := make([]*manifestMember, 0, 2)
		if closesAtStart && before > 0 {
			depth--
		}

		if s.rootOpen < 0 && !closesAtStart && strings.Contains(net, "{") {
			s.rootOpen = i
		}

		if s.rootOpen >= 0 && i > s.rootOpen && before == 1 && !closesAtStart {
			if key, ok := manifestMemberKey(net); ok {
				start := i
				if len(pendingAnn) > 0 && pendingAnn[len(pendingAnn)-1] == i-1 {
					start = pendingAnn[0]
				}
				m := &manifestMember{key: key, line: i, start: start, end: i, baseDepth: before}
				s.topMembers[key] = m
				s.topOrder = append(s.topOrder, key)
				if key == "compiler" {
					s.compilerLine = i
					s.compilerInline = strings.Contains(net, "{") && strings.Contains(net, "}")
				}
				newMembers = append(newMembers, m)
			}
		}
		if s.compilerLine >= 0 && i > s.compilerLine && before == 2 && !closesAtStart {
			if key, ok := manifestMemberKey(net); ok {
				start := i
				if len(pendingAnn) > 0 && pendingAnn[len(pendingAnn)-1] == i-1 {
					start = pendingAnn[0]
				}
				m := &manifestMember{key: key, line: i, start: start, end: i, baseDepth: before}
				s.compilerMembers[key] = m
				s.compilerOrder = append(s.compilerOrder, key)
				newMembers = append(newMembers, m)
			}
		}
		pendingAnn = pendingAnn[:0]

		delta := manifestNetBalance(net)
		if closesAtStart {
			delta++
		}
		depth += delta
		if depth < 0 {
			depth = 0
		}

		if s.rootClose < 0 && s.rootOpen >= 0 && i > s.rootOpen && before == 1 && closesAtStart && strings.HasPrefix(net, "}") {
			s.rootClose = i
		}
		if s.compilerClose < 0 && s.compilerLine >= 0 && i > s.compilerLine && before == 2 && closesAtStart && strings.HasPrefix(net, "}") {
			s.compilerClose = i
		}
		if s.compilerInline {
			s.compilerClose = s.compilerLine
		}

		// 只有真正跨行的值（容器／原始串／塊註釋）才留在 openMembers，
		// 否則單行成員的 end 會被下一行「補」掉，誤刪緊鄰的欄位。
		for _, m := range newMembers {
			if depth > m.baseDepth || openRaw || openBlock {
				openMembers = append(openMembers, m)
				if openRaw {
					rawExtend = m.line
				}
				if openBlock {
					blockExtend = m.line
				}
			}
		}
		// 多行值的成员在深度回到原層時才算結束。
		for j := 0; j < len(openMembers); {
			m := openMembers[j]
			if depth <= m.baseDepth && i > m.line {
				closeMember(i, m)
				continue
			}
			j++
		}

		if openRaw {
			inRaw = true
		}
		if openBlock {
			inBlock = true
		}
	}
	if s.compilerInline {
		s.compilerClose = s.compilerLine
	}
	return s
}

// manifestScanLine 去掉引號內容與註解，留下結構文本；同時報告該行是否開啟
// 原始串／塊註釋（與 src/fmt 的 nomlScanLine 同口徑）。
func manifestScanLine(line string) (net string, openRaw, openBlock bool) {
	var sb strings.Builder
	for i := 0; i < len(line); {
		switch line[i] {
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
			if strings.TrimSpace(line[i+1:]) == "" {
				return sb.String(), true, false
			}
			sb.WriteByte(line[i])
			i++
		case ';', '#':
			return sb.String(), false, false
		default:
			sb.WriteByte(line[i])
			i++
		}
	}
	return sb.String(), false, false
}

func manifestNetBalance(net string) int {
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

// manifestMemberKey 從結構文本取出 `key:` 的左側；沒有頂層冒號就不是成員。
func manifestMemberKey(net string) (string, bool) {
	idx := strings.IndexByte(net, ':')
	if idx <= 0 {
		return "", false
	}
	key := strings.TrimSpace(net[:idx])
	if len(key) >= 2 && key[0] == '\'' && key[len(key)-1] == '\'' {
		key = key[1 : len(key)-1]
	}
	if key == "" {
		return "", false
	}
	return key, true
}
