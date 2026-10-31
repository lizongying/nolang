package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	pkg "github.com/lizongying/nolang/package"
	"github.com/lizongying/nolang/parser"
)

// ─── .noml 直接引入 ──────────────────────────────────────────────────────────
//
// nolang 源碼可以 `# /cfg/server` 直接引入 cfg/server.noml（解析順序：先找
// server.no，找不到再找 server.noml）。引入後：
//
//   - 生成一個結構體，結構體名 = 文件名（server.noml → server）；
//   - 各成員成為該結構體的欄位，欄位型別取成員的 `#{type = '...'}` 註解，
//     未標註則由值推斷（字串 → str、整數 → i64、小數 → f64、布林 → bool、
//     物件 → 自動生成的巢狀結構體、陣列 → []元素型別）；
//   - 生成一個與文件同名的全局值（`server = server { ... }`），因此導入方
//     以 `server.port` 讀取配置，`as` 別名可改寫值名（結構體名不變）。
//
// 為什麼要型別註解：noml 值本身無型別（`0` / `'0'` / `false` / `'false'`
// 都是合法值），空容器 `[]` / `{}` 更是無法推斷。要從 noml **重建**強型別
// 結構，元素型別必須由 `#{type = '...'}` 攜帶（見 package/noml.go）。
//
// 實現方式是「生成 nolang 源碼再解析」：生成物是普通的結構體定義 + 結構體
// 字面量，後續的模組合併、簽名預載入、常量傳播、型別檢查與 codegen 全部
// 沿用既有管線，不需要在編譯器裡新增第二套型別系統。

// nomlStem 回傳 .noml 檔案對應的 nolang 名稱（結構體名與值名）：server.noml → server。
func nomlStem(nomlPath string) string {
	return nomlValueName(strings.TrimSuffix(filepath.Base(nomlPath), filepath.Ext(nomlPath)))
}

// NomlImportSource 把 .noml 文件重建為 nolang 源碼：文件名即結構體名，
// 同名全局值承載配置內容（`server = server { ... }`）。
//
// 別名不由本函式處理：生成的源碼只依檔案內容（因此可按路徑快取），
// 合併階段再把頂層值綁定改名為 use.Alias。
func NomlImportSource(nomlPath string, data []byte) (string, error) {
	doc, err := pkg.ParseNomlDoc(data)
	if err != nil {
		return "", err
	}
	if doc.Root == nil {
		return "", fmt.Errorf("top level must be an object")
	}
	stem := nomlStem(nomlPath)
	g := &nomlStructGen{doc: doc, stem: stem, typeNames: make(map[string]string)}
	if err := g.collect("", stem, doc.Root); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, def := range g.defs {
		sb.WriteString(def)
	}
	lit, err := g.valueLiteral("", doc.Root, 0)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&sb, "%s = %s\n", stem, lit)
	return sb.String(), nil
}

// nomlStructGen 把 noml 值樹重建為結構體定義 + 結構體字面量。
type nomlStructGen struct {
	doc *pkg.NomlDoc
	// stem 是根結構體名（文件名）；巢狀結構體名以 stem 為前綴。
	stem string
	// defs 是已生成的結構體定義（後序：巢狀型別先於宿主型別）。
	defs []string
	// typeNames 記錄物件路徑 → 結構體名（根路徑為 ""）。
	typeNames map[string]string
}

// collect 為 path 處的物件生成結構體定義。typeName 為該物件使用的型別名；
// 巢狀物件先遞迴（後序輸出），保證定義順序不需要前向引用。
func (g *nomlStructGen) collect(path, typeName string, obj map[string]any) error {
	g.typeNames[path] = typeName
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]string, 0, len(keys))
	seen := make(map[string]string, len(keys))
	for _, k := range keys {
		mpath := nomlMemberPath(path, k)
		field := nomlFieldName(k)
		if prev, ok := seen[field]; ok {
			return fmt.Errorf("keys %q and %q map to the same nolang field %q", prev, k, field)
		}
		seen[field] = k
		ftype, err := g.fieldType(mpath, obj[k])
		if err != nil {
			return err
		}
		fields = append(fields, "\t"+field+" "+ftype+"\n")
	}
	var sb strings.Builder
	sb.WriteString(typeName)
	sb.WriteString(" {\n")
	for _, f := range fields {
		sb.WriteString(f)
	}
	sb.WriteString("}\n")
	g.defs = append(g.defs, sb.String())
	return nil
}

// fieldType 解析成員型別：優先取 `#{type = '...'}`，否則由值推斷。
// 物件（含陣列元素物件）會遞迴生成巢狀結構體並登記到 typeNames。
func (g *nomlStructGen) fieldType(path string, v any) (string, error) {
	if t := g.doc.TypeOf(path); t != "" {
		return t, nil
	}
	switch val := v.(type) {
	case string:
		return "str", nil
	case bool:
		return "bool", nil
	case float64:
		if val == float64(int64(val)) {
			return "i64", nil
		}
		return "f64", nil
	case map[string]any:
		if len(val) == 0 {
			return "", fmt.Errorf("member %q is an empty object: annotate it with #{type = '<struct>'}", path)
		}
		name := g.nestedTypeName(path)
		if err := g.collect(path, name, val); err != nil {
			return "", err
		}
		return name, nil
	case []any:
		if len(val) == 0 {
			return "", fmt.Errorf("member %q is an empty array: annotate it with #{type = '[]<elem>'}", path)
		}
		elem, err := g.fieldType(path, val[0])
		if err != nil {
			return "", err
		}
		// 陣列元素共用同一個元素型別：後續元素必須同形（物件需同一組鍵）。
		first, isObj := val[0].(map[string]any)
		if isObj {
			want := objKeySet(first)
			for i := 1; i < len(val); i++ {
				cur, ok := val[i].(map[string]any)
				if !ok {
					return "", fmt.Errorf("member %q[%d]: array mixes objects and scalars; annotate it with #{type = '[]<elem>'}", path, i)
				}
				if got := objKeySet(cur); got != want {
					return "", fmt.Errorf("member %q[%d]: object keys %s differ from %q's %s; each element needs the same shape", path, i, got, path, want)
				}
			}
		}
		return "[]" + elem, nil
	default:
		return "", fmt.Errorf("member %q has unsupported value type %T (annotate it with #{type = '...'})", path, v)
	}
}

// nestedTypeName 由成員路徑生成巢狀結構體名：server + compiler + link = server-compiler-link。
func (g *nomlStructGen) nestedTypeName(path string) string {
	parts := strings.Split(path, ".")
	for i, p := range parts {
		parts[i] = nomlValueName(p)
	}
	return strings.Join(append([]string{g.stem}, parts...), "-")
}

// valueLiteral 輸出成員值的 nolang 字面量。
func (g *nomlStructGen) valueLiteral(path string, v any, indent int) (string, error) {
	switch val := v.(type) {
	case string:
		return pkg.NomlQuote(val), nil
	case bool:
		if val {
			return "true", nil
		}
		return "false", nil
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10), nil
		}
		return strconv.FormatFloat(val, 'g', -1, 64), nil
	case map[string]any:
		typeName := g.typeNames[path]
		if typeName == "" {
			typeName = g.doc.TypeOf(path)
		}
		if typeName == "" {
			return "", fmt.Errorf("member %q: missing type for object literal (annotate it with #{type = '<struct>'})", path)
		}
		return g.objectLiteral(path, typeName, val, indent)
	case []any:
		parts := make([]string, 0, len(val))
		for _, elem := range val {
			lit, err := g.valueLiteral(path, elem, indent+1)
			if err != nil {
				return "", err
			}
			parts = append(parts, lit)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	default:
		return "", fmt.Errorf("member %q has unsupported value type %T", path, v)
	}
}

// objectLiteral 輸出多行結構體字面量（nolang 要求結構體字面量多行）。
func (g *nomlStructGen) objectLiteral(path, typeName string, obj map[string]any, indent int) (string, error) {
	pad := strings.Repeat("\t", indent)
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(typeName)
	sb.WriteString(" {\n")
	for _, k := range keys {
		mpath := nomlMemberPath(path, k)
		lit, err := g.valueLiteral(mpath, obj[k], indent+1)
		if err != nil {
			return "", err
		}
		sb.WriteString(pad)
		sb.WriteString("\t")
		sb.WriteString(nomlFieldName(k))
		sb.WriteString(": ")
		sb.WriteString(lit)
		sb.WriteString("\n")
	}
	sb.WriteString(pad)
	sb.WriteString("}")
	return sb.String(), nil
}

// nomlKeywords 是 nolang 中不能直接用作欄位/變數名的保留字。
// 命中時加前綴 `x-`（noml 鍵可以任意，nolang 識別符不行）。
var nomlKeywords = map[string]bool{
	"and": true, "as": true, "break": true, "continue": true, "else": true,
	"false": true, "for": true, "fn": true, "if": true, "in": true, "it": true,
	"match": true, "new": true, "nil": true, "not": true, "or": true,
	"return": true, "run": true, "self": true, "true": true, "type": true,
	"use": true, "while": true,
}

// nomlFieldName 把 noml 鍵轉成合法的 nolang 欄位名（kebab-case）。
func nomlFieldName(k string) string {
	s := strings.ToLower(k)
	var sb strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			sb.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			sb.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(sb.String(), "-")
	if out == "" {
		out = "field"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "f-" + out
	}
	if nomlKeywords[out] {
		out = "x-" + out
	}
	return out
}

// nomlValueName 把文件名/別名轉成合法的 nolang 值名。
func nomlValueName(s string) string { return nomlFieldName(s) }

// nomlMemberPath 拼接 noml 成員路徑（與 package/noml.go 的註解鍵一致）：
// 頂層 "port"、巢狀 "compiler.version"。
func nomlMemberPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// findNomlImport 檢查 .no 路徑對應的 .noml 是否存在，返回其路徑。
func findNomlImport(noPath string) string {
	if noPath == "" {
		return ""
	}
	nomlPath := strings.TrimSuffix(noPath, filepath.Ext(noPath)) + ".noml"
	if _, err := os.Stat(nomlPath); err == nil {
		return nomlPath
	}
	return ""
}

// nomlImportPath 回傳導入路徑應走的 .noml 檔案：.no 存在時優先 .no
// （回傳 ""，呼叫方走原本的 .no 解析），否則回退到同名 .noml。
func nomlImportPath(noPath string) string {
	if noPath == "" {
		return ""
	}
	if _, err := os.Stat(noPath); err == nil {
		return ""
	}
	return findNomlImport(noPath)
}

// objKeySet 返回物件鍵的排序逗號串，用於比較陣列元素是否同形。
func objKeySet(obj map[string]any) string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// nomlImportForUse 回傳 use 指向的本地 .noml（同名 .no 不存在時）。
// std/、js/、npm/ 與 URL 風格的遠端包不參與（它們不是本地工作區檔案）。
func (t *Transpiler) nomlImportForUse(use *parser.UseStatement) string {
	p := use.Path
	if !strings.HasPrefix(p, "/") {
		if strings.HasPrefix(p, "std/") || p == "std" ||
			strings.HasPrefix(p, "js/") || p == "js" ||
			strings.HasPrefix(p, "npm/") || p == "npm" {
			return ""
		}
		if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") {
			return ""
		}
	}
	rel := strings.TrimPrefix(p, "/")
	noPath := pkg.ResolveToWorkspaceRoot(t.workspaceRoot(), rel)
	if !strings.HasSuffix(noPath, ".no") {
		noPath += ".no"
	}
	return nomlImportPath(filepath.Clean(noPath))
}

// resolveNomlFile 讀取 .noml、重建為 nolang 源碼並解析為 Program。
// 生成的源碼只依檔案內容（別名在合併階段處理），故可按路徑命中 fileCache。
func (t *Transpiler) resolveNomlFile(nomlPath string) (*parser.Program, error) {
	data, err := os.ReadFile(nomlPath)
	if err != nil {
		return nil, err
	}
	src, err := NomlImportSource(nomlPath, data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", nomlPath, err)
	}
	return t.parseEmbeddedProgram(nomlPath, []byte(src))
}

// mergeNomlModule 把 .noml 重建出的語句併入 merged。
//
// 與普通模組不同，noml 導入不參與模組前綴：
//   - 結構體名就是文件名（server.noml → server），導入方以 server.port 讀取；
//   - 型別不登記 typeOwner，故 rewriteTypeRefs / prefixMethodNames 不會改寫它；
//   - use.Alias 只改寫頂層值的綁定名（結構體名不變），因此 alias 導入也要
//     合併型別定義——這與普通模組「alias 只導入指定函數」的語意不同。
func (a *compileAssembly) mergeNomlModule(use *parser.UseStatement, nomlPath string) error {
	t := a.t
	modProg, err := t.resolveUse(use)
	if err != nil {
		return fmt.Errorf("loading module %s: %w", use.Path, err)
	}
	modProg = parser.CloneProgram(modProg)
	a.merged.Sem.Merge(modProg.Sem)
	a.merged.FmtTrivia.Merge(modProg.FmtTrivia)
	if use.Alias == "" {
		a.loadedUserModules[use.Path] = true
	}
	stem := nomlStem(nomlPath)
	alias := ""
	if use.Alias != "" {
		alias = nomlValueName(use.Alias)
	}
	for _, ms := range modProg.Statements {
		if _, ok := ms.(*parser.UseStatement); ok {
			continue
		}
		// 記錄 noml 本地型別 / 語句：prefixMethodsAndTypes 據此把它們與主
		// 程序本地型別同等對待（不參與模組前綴、不被同名模組型別改寫）。
		if name := getTypeDefName(ms); name != "" {
			if a.nomlTypes == nil {
				a.nomlTypes = make(map[string]bool)
			}
			a.nomlTypes[name] = true
		}
		if a.nomlStmts == nil {
			a.nomlStmts = make(map[parser.Statement]bool)
		}
		a.nomlStmts[ms] = true
		if ls, ok := ms.(*parser.LetStatement); ok && ls.Name != nil {
			if alias != "" && ls.Name.Value == stem {
				ls.Name.Value = alias
			}
			if a.mainVarNames[ls.Name.Value] {
				return fmt.Errorf("import %s: %q is already declared in the main program", use.Path, ls.Name.Value)
			}
			a.merged.Statements = append(a.merged.Statements, ls)
			a.merged.Sem.SetSourceFileDeep(ls, nomlPath)
			continue
		}
		a.merged.Statements = append(a.merged.Statements, ms)
		a.merged.Sem.SetSourceFileOf(ms, nomlPath)
	}
	return nil
}
