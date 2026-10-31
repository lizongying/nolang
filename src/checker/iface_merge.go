package checker

import (
	"github.com/lizongying/nolang/i18n"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// 接口合併（interface merging）的唯一真相源。
//
// 語法：合併項寫在接口體內，每行一個父接口名（可帶模組前綴）：
//
//	json { to-json() }
//	noml { to-noml() }
//	serialize {
//	    json
//	    noml
//	}
//
// AST 上它們落在 parser.InterfaceDefinition.Implements（複用既有欄位，不新增
// 欄位，因此 `no ast` dump、HIR、模組前綴改寫、gob/`std_ast.bin` 全部零改動）。
// 本檔案是唯一把 Implements 解釋成「方法集」的地方：checker 的簽名校驗、
// CollectDefinedVars 的未定義抑制、build 的去虛擬化都從這裡取。
//
// 語義（類型層完整合併）：
//   - 遞迴傳遞閉包：`a { b }` + `b { c }` ⇒ a 擁有 c 的方法；
//   - 同名同簽名去重（先父、後自身；保留首個 ⇒ 自身方法在父之後才進入，
//     故「自身優先」只能靠順序保證：父方法先到，自身同名同簽名者被丟棄，
//     簽名不同則報衝突）；
//   - 同名異簽名 / 循環 / 未知父接口 → 診斷。
//
// 名稱歸一：模組前綴改寫（build/module_prefix.go）會把 Implements 條目與
// InterfaceDefinition.Name 同時加上模組前綴，因此先按完整名匹配，唯一時才按
// ifaceBaseName 回退。

// InterfaceMergeTraceID 是接口合併系列診斷（lint 步 `interface-merge`）的穩定
// TraceID。未知父接口 / 循環 / 簽名衝突共用它：三者同屬一個語言特性，且都
// 由同一個函式產生。
const InterfaceMergeTraceID = "ifmrg001"

// MergedInterface 是一個接口的合併結果：自身方法與繼承來的父方法按穩定順序
// 去重後的並集。
type MergedInterface struct {
	Name    string // 原樣名（可能帶模組前綴）
	Decl    *parser.InterfaceDefinition
	Methods []*parser.InterfaceMethod // 自身 + 繼承，按穩定順序去重
	Parents []string                  // 解析到的父接口（完整名）；未知父接口不在內
}

// InterfaceSet 是一個 Program 內所有接口的合併結果。
type InterfaceSet struct {
	byName map[string]*MergedInterface // 完整名 → 合併結果
	byBase map[string][]string         // 基名 → 完整名（跨模組可能多個）
	order  []string                    // 宣告順序（穩定遍歷用）
}

// ResolveInterfaces 解析 program 內全部接口定義並做合併。
// 第二個回傳值是診斷（未知父接口 / 循環 / 簽名衝突），位置取接口名 token
// 自身的位置，因此可直接歸因到宣告處。
func ResolveInterfaces(program *parser.Program) (*InterfaceSet, []ValidateResult) {
	set := &InterfaceSet{
		byName: map[string]*MergedInterface{},
		byBase: map[string][]string{},
	}
	if program == nil {
		return set, nil
	}
	decls := map[string]*parser.InterfaceDefinition{}
	for _, stmt := range program.Statements {
		id, ok := stmt.(*parser.InterfaceDefinition)
		if !ok || id.Name == "" {
			continue
		}
		if _, dup := decls[id.Name]; dup {
			continue
		}
		decls[id.Name] = id
		set.order = append(set.order, id.Name)
		base := ifaceBaseName(id.Name)
		set.byBase[base] = append(set.byBase[base], id.Name)
	}

	r := &ifaceResolver{
		program: program,
		decls:   decls,
		byBase:  set.byBase,
		set:     set,
		state:   map[string]int{}, // 0=未访问 1=访问中 2=完成
	}
	for _, name := range set.order {
		r.resolve(name)
	}
	return set, r.results
}

type ifaceResolver struct {
	program *parser.Program
	decls   map[string]*parser.InterfaceDefinition
	byBase  map[string][]string
	set     *InterfaceSet
	state   map[string]int
	stack   []string
	results []ValidateResult
}

// resolve 對 name 做 DFS + memo 合併。返回 nil 表示該名無法解析（未知或循環），
// 呼叫端據此跳過該父接口。
func (r *ifaceResolver) resolve(name string) *MergedInterface {
	if m, ok := r.set.byName[name]; ok {
		return m
	}
	switch r.state[name] {
	case 1:
		r.reportCycle(name)
		return nil
	case 2:
		// 已判定不可解析（身處循環中或父接口未知），不重複報。
		return nil
	}
	decl := r.decls[name]
	if decl == nil {
		return nil
	}
	r.state[name] = 1
	r.stack = append(r.stack, name)

	m := &MergedInterface{Name: name, Decl: decl}
	ownerByName := map[string]*parser.InterfaceMethod{}
	ownerOfMethod := map[string]string{}
	keep := func(mth *parser.InterfaceMethod, owner string) {
		if mth == nil {
			return
		}
		prev, seen := ownerByName[mth.Name]
		if !seen {
			ownerByName[mth.Name] = mth
			ownerOfMethod[mth.Name] = owner
			m.Methods = append(m.Methods, mth)
			return
		}
		if canonicalMethodSig(prev) == canonicalMethodSig(mth) {
			return // 同名同簽名 → 去重，保留先到者
		}
		r.reportConflict(name, mth, owner, ownerOfMethod[mth.Name], prev)
	}

	// 先父（按宣告順序，遞迴），後自身方法。
	for _, parent := range decl.Implements {
		full, ok := r.lookup(parent)
		if !ok {
			r.reportUnknownParent(name, decl, parent)
			continue
		}
		pm := r.resolve(full)
		if pm == nil {
			continue
		}
		m.Parents = append(m.Parents, full)
		for _, mth := range pm.Methods {
			keep(mth, full)
		}
	}
	for _, mth := range decl.Methods {
		keep(mth, name)
	}

	r.stack = r.stack[:len(r.stack)-1]
	r.state[name] = 2
	r.set.byName[name] = m
	return m
}

// lookup 把 Implements 裡的寫法歸一到已宣告接口的完整名。
// 先按完整名匹配；未命中且基名唯一時按基名回退（模組前綴改寫後兩側形態
// 可能不一致——一方是 `sql.db`、另一方是 `db`）。
func (r *ifaceResolver) lookup(ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	if _, ok := r.decls[ref]; ok {
		return ref, true
	}
	if cands := r.byBase[ifaceBaseName(ref)]; len(cands) == 1 {
		return cands[0], true
	}
	return "", false
}

func (r *ifaceResolver) fileOf(decl *parser.InterfaceDefinition) string {
	if r.program == nil || r.program.Sem == nil || decl == nil {
		return ""
	}
	return r.program.Sem.SourceFileOf(decl)
}

func (r *ifaceResolver) reportUnknownParent(name string, decl *parser.InterfaceDefinition, parent string) {
	r.results = append(r.results, ValidateResult{
		TraceID:   "wekxh2lc",
		File:      r.fileOf(decl),
		Line:      decl.Token.Line,
		Column:    decl.Token.Column,
		EndColumn: decl.Token.Column + len(decl.Name),
		Message:   i18n.T("wekxh2lc", "interface '%s': unknown parent interface '%s'", name, parent),
	})
}

func (r *ifaceResolver) reportCycle(name string) {
	start := 0
	for i, n := range r.stack {
		if n == name {
			start = i
			break
		}
	}
	path := append([]string{}, r.stack[start:]...)
	path = append(path, name)
	names := make([]string, 0, len(path))
	for _, n := range path {
		names = append(names, n)
	}
	decl := r.decls[name]
	line, col := 0, 0
	if decl != nil {
		line, col = decl.Token.Line, decl.Token.Column
	}
	r.results = append(r.results, ValidateResult{
		TraceID:   "8zompytc",
		File:      r.fileOf(decl),
		Line:      line,
		Column:    col,
		EndColumn: col + len(name),
		Message:   i18n.T("8zompytc", "interface '%s': merge cycle %s", name, strings.Join(names, " -> ")),
	})
}

func (r *ifaceResolver) reportConflict(name string, mth *parser.InterfaceMethod, owner, prevOwner string, prev *parser.InterfaceMethod) {
	decl := r.decls[name]
	line, col := mth.Token.Line, mth.Token.Column
	if line == 0 && decl != nil {
		line, col = decl.Token.Line, decl.Token.Column
	}
	r.results = append(r.results, ValidateResult{
		TraceID:   "ifmrg001",
		File:      r.fileOf(decl),
		Line:      line,
		Column:    col,
		EndColumn: col + len(mth.Name),
		Message: i18n.T("ifmrg001", "interface '%s': merged method '%s' has conflicting signatures ('%s.%s', '%s.%s')",
			name, mth.Name, prevOwner, prev.Name, owner, mth.Name),
	})
}

// canonicalMethodSig 產生跨接口可比較的規範簽名：
// `name|paramTypes|resultTypes`，其中方法自身的 Receiver 識別符（t / u / fs）
// 統一替換為 $recv 佔位符，避免 `t.gt` 與 `u.gt` 被誤判為簽名衝突。
func canonicalMethodSig(m *parser.InterfaceMethod) string {
	if m == nil {
		return ""
	}
	recv := ""
	if m.IsGenericReceiver {
		recv = m.Receiver
	}
	norm := func(p *parser.Parameter) string {
		if p == nil || p.Type == nil {
			return ""
		}
		t := p.Type.String()
		if recv != "" {
			t = replaceReceiverIdent(t, recv)
		}
		return t
	}
	parts := make([]string, 0, len(m.Parameters))
	for _, p := range m.Parameters {
		parts = append(parts, norm(p))
	}
	res := make([]string, 0, len(m.Results))
	for _, p := range m.Results {
		res = append(res, norm(p))
	}
	return m.Name + "|" + strings.Join(parts, ",") + "|" + strings.Join(res, ",")
}

// replaceReceiverIdent 把「完整識別符」形態的接收者名換成 $recv 佔位符：
// `t` → `$recv`、`[]t` → `[]$recv`、`?t` → `?$recv`，而 `txt` 不受影響。
func replaceReceiverIdent(s, recv string) string { return replaceIdentWord(s, recv, "$recv") }

// replaceIdentWord 只替換「完整識別符」——不能退化成子串替換：
// 舊碼的 strings.ReplaceAll("str", "t", "user") 會把 `t.method() (s str)` 的預期
// （型別 `str` 中的字母 t）誤改成 `sur`，任何泛型接收者方法的 str 型別參數／回傳
// 都因此被誤報。
func replaceIdentWord(s, word, repl string) string {
	if word == "" || s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if isIdentStartByte(s[i]) {
			j := i + 1
			for j < len(s) && isIdentPartByte(s[j]) {
				j++
			}
			if s[i:j] == word {
				b.WriteString(repl)
			} else {
				b.WriteString(s[i:j])
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isIdentStartByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPartByte(c byte) bool {
	return isIdentStartByte(c) || (c >= '0' && c <= '9') || c == '-'
}

// MethodsOf 返回 name 對應接口的合併方法集。先按完整名匹配，唯一時按基名回退。
func (s *InterfaceSet) MethodsOf(name string) []*parser.InterfaceMethod {
	if s == nil || name == "" {
		return nil
	}
	if m, ok := s.byName[name]; ok {
		return m.Methods
	}
	if cands := s.byBase[ifaceBaseName(name)]; len(cands) == 1 {
		return s.byName[cands[0]].Methods
	}
	return nil
}

// Closure 返回「自身 + 所有祖先」，以完整名去重、按宣告順序穩定排序。
// 未知父接口不計入（ResolveInterfaces 已報過診斷）。
func (s *InterfaceSet) Closure(name string) []string {
	if s == nil || name == "" {
		return nil
	}
	full, ok := s.lookupFull(name)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(string)
	walk = func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		out = append(out, n)
		if m, ok := s.byName[n]; ok {
			for _, p := range m.Parents {
				walk(p)
			}
		}
	}
	walk(full)
	return out
}

// lookupFull 把任意寫法歸一到完整名（與 ifaceResolver.lookup 同規則）。
func (s *InterfaceSet) lookupFull(ref string) (string, bool) {
	if _, ok := s.byName[ref]; ok {
		return ref, true
	}
	if cands := s.byBase[ifaceBaseName(ref)]; len(cands) == 1 {
		return cands[0], true
	}
	return "", false
}

// Has 報告 name 是否是一個已宣告的接口（任一寫法）。
func (s *InterfaceSet) Has(name string) bool {
	_, ok := s.lookupFull(name)
	return ok
}

// All 按宣告順序返回全部合併結果。
func (s *InterfaceSet) All() []*MergedInterface {
	if s == nil {
		return nil
	}
	out := make([]*MergedInterface, 0, len(s.order))
	for _, n := range s.order {
		if m, ok := s.byName[n]; ok {
			out = append(out, m)
		}
	}
	return out
}
