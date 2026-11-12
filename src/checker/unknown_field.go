package checker

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// 本檔負責一件事：**接收者型別已知是 struct，卻存取了它既沒有欄位也沒有方法
// 的成員**時報錯。
//
// 為什麼需要它（2026-11-12）：這條路以前完全沒有診斷。`d.pool` 打錯成一個
// 不存在的欄位（std 的 `doc` 其實叫 `node-pool`）不會有任何抱怨，而是：
//
//   - 型別推斷落空 ⇒ `p = d.pool` 的 `p` 沒有型別 ⇒ `p.err.len()` 的接收者
//     解析失敗 ⇒ callee 退化成點分名字 ⇒ MIR 當成 `str-len` 內建呼叫而報
//     「builtin str-len: missing receiver」（1u2vckag）；
//   - 內聯成 `d.pool.nodes.len()` 時換一個面具：MIR 讀到一條不存在的欄位，
//     產出 NoVal 操作數 ⇒ 「MIR codegen could not handle … (bug #85)」；
//   - 若形狀剛好讓 codegen 閉嘴，就是**靜默的垃圾值**：`d.pool.strs.len()`
//     回 0、或運行期 `signal: bus error`，看起來像記憶體安全缺陷。
//
// 三種症狀（尤其第三種）在同一個「欄位名寫錯」上花掉整整一輪排查，卻沒有一行
// 診斷指出來。`glossary/data/traps.noml` 的 std-field-vec-read-breaks 就是這樣
// 被誤記成「[]str codegen 缺陷」的。
//
// 口徑（保守優先，寧可漏報也不誤報）：
//   - 只在**確知欄位集**時判定，且該 struct 至少登記到一個欄位；
//   - 型別名解析與 parser 的 structFieldsOf 同口徑：精確鍵 → 本模組限定 →
//     唯一後綴匹配。多個模組同名結構時**不猜**（`zip.writer` 與 `bufio.writer`
//     都叫 writer，猜錯就是把合法欄位報成不存在）；
//   - 成員名命中欄位、命中方法（本程式或內嵌 std 方法表）、或命中任何內建方法
//     名（`len` / `cap` / `push` …）一律放行；
//   - 方法呼叫位置（`recv.m(...)` 的 callee）不看欄位，只下鑽接收者；
//   - 接收者型別推不出來就放行。
func ValidateUnknownFields(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	c := &fieldAccessWalk{
		program:    program,
		tables:     collectStructTables(program),
		methods:    collectStructMethods(program),
		builtinSet: builtinMethodNames(),
		seen:       map[string]bool{},
	}
	for _, stmt := range program.Statements {
		// 每個頂層語句從空作用域開始：頂層 `x = 1` 與某函式內的 `x str` 不共享。
		// curModule 由該語句自己的來源檔決定（合併後一個 program 橫跨多個模組）。
		c.curModule = sourceModuleShort(semSourceFile(program, stmt))
		c.walkStmt(stmt, map[string]string{}, "")
	}
	return c.out
}

type fieldAccessWalk struct {
	program *parser.Program
	tables  *structTables
	methods map[string]map[string]bool
	// builtinSet 是所有內建方法名（`len` / `cap` / `push` / `to-str` …）。它們
	// 實作在 Go runtime 的 BuiltinMethod 表裡，既不是 struct 欄位也不在
	// `Type.method` 的定義集合中 —— 少了這層放行，`bigint.copy(x)` 這類寫法
	// 會被誤報成「沒有成員 copy」。
	builtinSet map[string]bool
	curModule  string
	out        []ValidateResult
	// seen 去重：同一 (行, 列, 成員) 只報一次 —— 合併後的程式會把同一段原始碼
	// 走兩遍（頂層 + 方法體），不去重會出現重複診斷。
	seen map[string]bool
}

// structTables 是「結構名 → 欄位集」的兩級索引。
//
// byKey 的鍵一律是**模組限定**的（`zip.writer` / `noml.doc`）或**裸名**（只在
// 該結構確實沒有模組資訊時使用）。bare 記錄「裸名 → 限定鍵列表」，讓型別名解析
// 能複現 parser.structFieldsOf 的唯一後綴匹配，而不必在每次查詢時掃全表。
type structTables struct {
	byKey map[string]map[string]string
	bare  map[string][]string
}

// collectStructTables 建表。使用者程式的結構名以來的來源檔推模組名（`.../zip.no`
// → `zip`）；內嵌 std 表本身就是限定鍵，直接併入。
func collectStructTables(program *parser.Program) *structTables {
	t := &structTables{byKey: map[string]map[string]string{}, bare: map[string][]string{}}
	add := func(key string, fields map[string]string) {
		if key == "" || fields == nil {
			return
		}
		if _, ok := t.byKey[key]; ok {
			return
		}
		t.byKey[key] = fields
		bare := key
		if i := strings.LastIndex(bare, "."); i >= 0 {
			bare = bare[i+1:]
		}
		t.bare[bare] = append(t.bare[bare], key)
	}
	for _, stmt := range program.Statements {
		sd, ok := stmt.(*parser.StructDefinition)
		if !ok || sd == nil || sd.Name == "" {
			continue
		}
		fields := make(map[string]string, len(sd.Fields))
		for _, f := range sd.Fields {
			if f == nil || f.Name == "" {
				continue
			}
			// 型別字串化失敗時記空串而不是丟掉：名字本身就是「這是合法成員」
			// 的證據，只要它在表裡，查詢端就不會誤報。
			fields[f.Name] = structFieldTypeString(f)
		}
		if mod := sourceModuleShort(semSourceFile(program, sd)); mod != "" {
			add(mod+"."+sd.Name, fields)
		} else {
			add(sd.Name, fields)
		}
	}
	for k, v := range embeddedStdStructFields {
		add(k, v)
	}
	return t
}

// resolve 複現 parser.structFieldsOf 的三段查表：精確 → 本模組限定 → 唯一後綴。
// 任何一段落空且存在多義時回 nil（呼叫端因此放行）。
func (t *structTables) resolve(typeName, curModule string) map[string]string {
	if typeName == "" || t == nil {
		return nil
	}
	if strings.Contains(typeName, ".") {
		return t.byKey[typeName]
	}
	if curModule != "" {
		if fields, ok := t.byKey[curModule+"."+typeName]; ok {
			return fields
		}
	}
	if fields, ok := t.byKey[typeName]; ok {
		return fields
	}
	keys := t.bare[typeName]
	if len(keys) != 1 {
		return nil
	}
	return t.byKey[keys[0]]
}

// semSourceFile 取節點的來源檔；Sem 缺席（單元測試的最小 program）時回空。
func semSourceFile(program *parser.Program, n parser.Node) string {
	if program == nil || program.Sem == nil || n == nil {
		return ""
	}
	return program.Sem.SourceFileOf(n)
}

// sourceModuleShort 由檔案路徑推模組短名（`.../std/zip.no` → `zip`，
// `f6.no` → `f6`）。std 優先走既有的 stdModuleForSourceFile，讓 `crypto/rand.no`
// 這類巢狀模組也對上簽名表的 ShortName。
func sourceModuleShort(file string) string {
	if file == "" {
		return ""
	}
	if m := stdModuleForSourceFile(file); m != "" {
		return m
	}
	base := filepath.ToSlash(file)
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ".no")
}

// builtinMethodNames 收集所有內建方法名（見 fieldAccessWalk.builtinSet）。
func builtinMethodNames() map[string]bool {
	set := make(map[string]bool, len(builtin.BuiltinMethodList))
	for _, m := range builtin.BuiltinMethodList {
		if m.MethodName != "" {
			set[m.MethodName] = true
		}
	}
	return set
}

// collectStructMethods 建「struct → 方法名」集合。
//
// 方法在 AST 裡是 `IsMethodDef` 的 FunctionDefinition，`Name` 已含型別前綴
// （`doc.pool`）。同時登記裸名與限定名：本模組寫 `a.get()` 時接收者型別解析成
// `本模組.a`，跨模組寫 `d.get()` 時是 `noml.doc`，兩邊都要命中。
func collectStructMethods(program *parser.Program) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	add := func(typeName, method string) {
		if typeName == "" || method == "" {
			return
		}
		set, ok := out[typeName]
		if !ok {
			set = map[string]bool{}
			out[typeName] = set
		}
		set[method] = true
	}
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok || fd == nil || !fd.IsMethodDef {
			continue
		}
		name := fd.Name
		i := strings.LastIndex(name, ".")
		if i <= 0 || i+1 >= len(name) {
			continue
		}
		typeName, method := name[:i], name[i+1:]
		add(typeName, method)
		if j := strings.LastIndex(typeName, "."); j >= 0 {
			add(typeName[j+1:], method)
		}
	}
	return out
}

// hasMember 回報型別 typeName 是否有名為 prop 的欄位或方法。
func (c *fieldAccessWalk) hasMember(typeName, prop string) bool {
	fields := c.tables.resolve(typeName, c.curModule)
	if _, has := fields[prop]; has {
		return true
	}
	names := []string{typeName}
	if i := strings.LastIndex(typeName, "."); i >= 0 {
		names = append(names, typeName[i+1:])
	}
	for _, n := range names {
		if c.methods[n][prop] {
			return true
		}
		if _, ok := embeddedStdMethodSigs[n+"."+prop]; ok {
			return true
		}
	}
	return false
}

func (c *fieldAccessWalk) report(line, col int, typeName, prop string) {
	key := fmt.Sprintf("%d:%d\x00%s\x00%s", line, col, typeName, prop)
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.out = append(c.out, ValidateResult{
		TraceID: "o4x247ya",
		Line:    line,
		Column:  col,
		Message: i18n.T("o4x247ya",
			"struct '%s' has no field or method '%s'", typeName, prop),
	})
}

// checkDot 是唯一產生診斷的入口。
func (c *fieldAccessWalk) checkDot(e *parser.DotExpression, varTypes map[string]string, selfType string) {
	if e == nil || e.Property == "" {
		return
	}
	// 內建方法名一律放行（見 builtinSet）。
	if c.builtinSet[e.Property] {
		return
	}
	recvT := ty.StripOptionRaw(inferExprType(e.Receiver, varTypes, nil, selfType))
	recvT = strings.TrimPrefix(recvT, "%")
	if recvT == "" || recvT == "any" || strings.Contains(recvT, "|") {
		return
	}
	// 欄位集必須非空：空的分不清「這個 struct 真的沒有欄位」與「欄位全沒登記」。
	fields := c.tables.resolve(recvT, c.curModule)
	if len(fields) == 0 {
		return
	}
	if c.hasMember(recvT, e.Property) {
		return
	}
	c.report(e.Token.Line, e.Token.Column, recvT, e.Property)
}

func (c *fieldAccessWalk) walkStmt(stmt parser.Statement, varTypes map[string]string, selfType string) {
	if stmt == nil {
		return
	}
	if rv := reflect.ValueOf(stmt); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if s.Value != nil {
			c.walkExpr(s.Value, varTypes, selfType)
		}
		if s.Name != nil {
			raw := ""
			if s.Type != nil {
				raw = declTypeOfRaw(nil, s, s.Type.String())
				if raw == s.Name.Value {
					raw = "" // `x x = …` 這種「變數名等於型別名」的糖
				}
			}
			if raw == "" {
				raw = inferExprType(s.Value, varTypes, nil, selfType)
			}
			if raw != "" {
				if _, exists := varTypes[s.Name.Value]; !exists {
					varTypes[s.Name.Value] = raw
				}
			}
		}
	case *parser.MultiAssignStatement:
		c.walkExpr(s.Value, varTypes, selfType)
	case *parser.UnwrapAssignStatement:
		c.walkExpr(s.Value, varTypes, selfType)
	case *parser.ReturnStatement:
		c.walkExpr(s.ReturnValue, varTypes, selfType)
	case *parser.ExpressionStatement:
		c.walkExpr(s.Expression, varTypes, selfType)
	case *parser.BlockStatement:
		c.walkBlock(s, varTypes, selfType)
	case *parser.BlockMatchStatement:
		for _, sub := range blockMatchChildStatements(s) {
			c.walkStmt(sub, varTypes, selfType)
		}
	case *parser.ForStatement:
		if s.IterRange != nil {
			c.walkExpr(s.IterRange.Range, varTypes, selfType)
			c.walkExpr(s.IterRange.RangeExpr, varTypes, selfType)
		}
		c.walkExpr(s.Condition, varTypes, selfType)
		c.walkStmt(s.Init, varTypes, selfType)
		c.walkStmt(s.Update, varTypes, selfType)
		if s.Body != nil {
			// 迴圈體用複本：迭代變數與體內局部不該漏到外面汙染同名變數。
			inner := copyVarTypes(varTypes)
			if s.IterRange != nil && s.IterRange.Variable != "" {
				if rt := inferExprType(s.IterRange.RangeExpr, varTypes, nil, selfType); rt != "" {
					if el := parser.ContainerElemType(ty.StripOptionRaw(rt)); el != "" {
						inner[s.IterRange.Variable] = el
					}
				}
			}
			c.walkBlock(s.Body, inner, selfType)
		}
	case *parser.FunctionDefinition:
		// 每個函式一份新作用域：參數 / 結果參數名不得沿用呼叫方的同名變數。
		inner := map[string]string{}
		fnSelf := selfType
		for _, r := range s.Results {
			if r == nil || r.Type == nil {
				continue
			}
			if r.Name == "self" {
				fnSelf = declTypeOfRaw(nil, r, r.Type.String())
				inner["self"] = fnSelf
				continue
			}
			if r.Name != "" {
				inner[r.Name] = declTypeOfRaw(nil, r, r.Type.String())
			}
		}
		for _, p := range s.Parameters {
			if p == nil || p.Name == "" || p.Type == nil {
				continue
			}
			inner[p.Name] = declTypeOfRaw(nil, p, p.Type.String())
		}
		prev := c.curModule
		if m := sourceModuleShort(semSourceFile(c.program, s)); m != "" {
			c.curModule = m
		}
		c.walkBlock(s.Body, inner, fnSelf)
		c.curModule = prev
	default:
	}
}

func (c *fieldAccessWalk) walkBlock(b *parser.BlockStatement, varTypes map[string]string, selfType string) {
	if b == nil {
		return
	}
	for _, sub := range b.Statements {
		c.walkStmt(sub, varTypes, selfType)
	}
}

func (c *fieldAccessWalk) walkExpr(expr parser.Expression, varTypes map[string]string, selfType string) {
	if expr == nil {
		return
	}
	// 型別化的 nil：`Expression` 裡存著 (*parser.RangeExpression)(nil) 時，
	// 上面的 `expr == nil` 不成立，下面卻會匹配到 case 並解參考空指標
	//（RangeExpression 缺一端就是這種）。walkExprForLen 有同一個護欄。
	if rv := reflect.ValueOf(expr); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	switch e := expr.(type) {
	case *parser.DotExpression:
		c.checkDot(e, varTypes, selfType)
		c.walkExpr(e.Receiver, varTypes, selfType)
	case *parser.CallExpression:
		// 方法呼叫位置：`recv.m(args)` 的 `m` 是方法名，不是欄位讀取。
		// 只下鑽接收者與實參（walkExprForLen 同口徑）。
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			c.walkExpr(dot.Receiver, varTypes, selfType)
		} else {
			c.walkExpr(e.Function, varTypes, selfType)
		}
		for _, a := range e.Arguments {
			c.walkExpr(a, varTypes, selfType)
		}
		for _, a := range e.GenericArgs {
			c.walkExpr(a, varTypes, selfType)
		}
	case *parser.InfixExpression:
		c.walkExpr(e.Left, varTypes, selfType)
		c.walkExpr(e.Right, varTypes, selfType)
	case *parser.PrefixExpression:
		c.walkExpr(e.Right, varTypes, selfType)
	case *parser.IndexExpression:
		c.walkExpr(e.Left, varTypes, selfType)
		c.walkExpr(e.Index, varTypes, selfType)
	case *parser.SliceExpression:
		c.walkExpr(e.Left, varTypes, selfType)
		c.walkExpr(e.Range, varTypes, selfType)
	case *parser.RangeExpression:
		c.walkExpr(e.Start, varTypes, selfType)
		c.walkExpr(e.End, varTypes, selfType)
	case *parser.ConditionalExpression:
		c.walkExpr(e.Condition, varTypes, selfType)
		c.walkExpr(e.Consequence, varTypes, selfType)
		c.walkExpr(e.Alternative, varTypes, selfType)
	case *parser.CastExpression:
		c.walkExpr(e.Expr, varTypes, selfType)
	case *parser.RunExpression:
		c.walkExpr(e.Call, varTypes, selfType)
	case *parser.AwaitExpression:
		c.walkExpr(e.Right, varTypes, selfType)
	case *parser.ArrayLiteral:
		for _, el := range e.Elements {
			c.walkExpr(el, varTypes, selfType)
		}
	case *parser.AssignExpression:
		// 左側是寫入目標（DotExpression），只走右值。
		c.walkExpr(e.Value, varTypes, selfType)
	case *parser.SurfaceMatch:
		c.walkExpr(e.Matched, varTypes, selfType)
		for i := range e.Arms {
			arm := &e.Arms[i]
			if arm.Condition != nil {
				c.walkExpr(arm.Condition, varTypes, selfType)
			}
			for _, vp := range arm.MultiValuePatterns {
				if vp != nil {
					c.walkExpr(vp, varTypes, selfType)
				}
			}
			if arm.Body != nil {
				c.walkBlock(arm.Body, copyVarTypes(varTypes), selfType)
			}
		}
	case *parser.IfExpression:
		c.walkExpr(e.Condition, varTypes, selfType)
		c.walkBlock(e.Consequence, copyVarTypes(varTypes), selfType)
		c.walkBlock(e.Alternative, copyVarTypes(varTypes), selfType)
	default:
		// 字面量 / 識別符 / 函式字面量等：沒有可下鑽的子表達式。
	}
}

// copyVarTypes 淺拷貝作用域表：塊內的 `let` 不該外流，而外層宣告仍應可見。
func copyVarTypes(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+4)
	for k, v := range m {
		out[k] = v
	}
	return out
}
