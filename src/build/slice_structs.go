package build

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// slice_structs.go — 切片欄位泛型結構體的單態化。
//
// 與 hashmap-*-tmpl（由 map[K]V 使用點驅動、-tmpl 命名）不同，本檔案處理
// 「以切片欄位承載元素型別」的泛型容器：結構體把元素緩衝區存在 `[]t` 欄位，
// 並用工廠方法 `Type.init = (data []t) (h Type)` 建立實例。元素型別 `t` 由
// 工廠呼叫傳入的具體 `[N]E` / `[]E` 緩衝區引數推斷，並沿著接收者變數的型別
// 傳播到後續 `h.method(...)` 呼叫。
//
//   heap { data []t, n i64 }
//   heap.init = (data []t) (h heap) { ... }   // 工廠
//   heap.push = (val t) { ... }               // 值操作方法
//
//   h = heap.init(buf)   // buf: [8]i64  =>  t=i64, h : heap-i64
//   h.push(5)            // 由 monomorphizeGenerics 的 resolveMethodCall 攤平
//
// 由於 `[]t` 欄位在 LLVM 層只是同質切片標頭（ptr+len/cap），記憶體重排對任何
// `t` 都相同，因此只需為「元素存取 / 比較」單態化，不需為每種元素型別重新
// 生成結構體佈局。
//
// 本 pass 必須在 monomorphizeGenerics 之前執行：如此一來實例方法呼叫 `h.push`
// 仍是接收者為變數的 DotExpression，交由 resolveMethodCall（依 varTypes[h]=
// heap-i64）自然攤平成 heap-i64.push；本 pass 只負責：推斷元素型別、生成具現
// 化結構體 + 方法、改寫工廠呼叫的接收者型別名、把工廠結果變數標註為具現化型別，
// 並移除原始模板（與 map 路徑相同，無條件移除所有匹配模板，避免未具現化的 `t`
// 型別定義落到 codegen）。

// isSingleLowerVar 判斷字串是否為單一拉丁小寫字母（t/v/k/e 等型別變數名）。
func isSingleLowerVar(s string) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return c >= 'a' && c <= 'z'
}

// sliceTemplateVarName 回傳結構體欄位中出現的「單一元素型別變數」名稱。
// 判定：某欄位型別為 slice/array/option 包裝的、名稱為單一拉丁小寫字母的
// NamedType（t/v/k/e）。出現恰好一個不同型別變數時回傳其名；多個則回傳空字串
// （本 pass 只支援單一元素型別；map 的 K/V 由 monomorphizeGenericStructs 處理）。
// 名稱以 "-tmpl" 結尾者一律回傳空字串，交還給 map 路徑。
func sliceTemplateVarName(sd *parser.StructDefinition) string {
	if strings.HasSuffix(sd.Name, "-tmpl") {
		return ""
	}
	vars := make(map[string]bool)
	var order []string
	add := func(v string) {
		if v != "" && !vars[v] {
			vars[v] = true
			order = append(order, v)
		}
	}
	for _, f := range sd.Fields {
		add(typeVarOfSliceElem(f.Type))
	}
	if len(order) != 1 {
		return ""
	}
	return order[0]
}

// typeVarOfSliceElem 從欄位型別中抽取「被 slice/array/option/view 包裝的單一
// 小寫字母型別變數名」。裸型別變數欄位也算。回傳空字串表示不是型別變數。
func typeVarOfSliceElem(t parser.Type) string {
	switch typ := t.(type) {
	case *parser.SliceType:
		return namedTypeVarName(typ.Elem)
	case *parser.ArrayType:
		return namedTypeVarName(typ.Elem)
	case *parser.NullableType:
		return typeVarOfSliceElem(typ.Type)
	case *parser.ViewType:
		return typeVarOfSliceElem(typ.Type)
	case *parser.NamedType:
		if isSingleLowerVar(typ.Value) {
			return typ.Value
		}
	}
	return ""
}

func namedTypeVarName(t parser.Type) string {
	nt, ok := t.(*parser.NamedType)
	if !ok {
		return ""
	}
	if isSingleLowerVar(nt.Value) {
		return nt.Value
	}
	return ""
}

// bareStructName 去掉型別名的模組前綴（"map.hashmap-str-tmpl" → "hashmap-str-tmpl"）。
func bareStructName(name string) string {
	if dotIdx := strings.Index(name, "."); dotIdx >= 0 {
		return name[dotIdx+1:]
	}
	return name
}

// sliceTemplate 記錄一個待單態化的切片欄位泛型結構體及其方法與工廠呼叫點。
type sliceTemplate struct {
	sd      *parser.StructDefinition
	varName string // 元素型別變數名，如 "t"
	bare    string // 去掉模組前綴的型別名，如 "heap"
	methods []*parser.FunctionDefinition
	suffixes map[string]bool // 方法最後段名集合（init/push/pop/...）
}

// monomorphizeSliceStructs 泛型切片欄位結構體單態化主流程。varTypes 為主程式
// 頂層變數型別表（globalVarTypes），本 pass 會就地更新以把工廠結果標註為具現化型別。
func monomorphizeSliceStructs(program *parser.Program, varTypes map[string]string) {
	if program == nil {
		return
	}

	// 1. 收集切片欄位泛型模板（依裸名索引）。
	templates := make(map[string]*sliceTemplate)
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			vn := sliceTemplateVarName(sd)
			if vn == "" {
				continue
			}
			t := &sliceTemplate{sd: sd, varName: vn, bare: bareStructName(sd.Name), suffixes: make(map[string]bool)}
			templates[t.bare] = t
		}
	}
	if len(templates) == 0 {
		return
	}

	// 2. 收集模板方法（fd.Name 為 "Tmpl.method" 或 "module.Tmpl.method"）。
	for _, stmt := range program.Statements {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		lastDot := strings.LastIndex(fd.Name, ".")
		if lastDot < 0 {
			continue
		}
		if t := templates[bareStructName(fd.Name[:lastDot])]; t != nil {
			t.methods = append(t.methods, fd)
			t.suffixes[fd.Name[lastDot+1:]] = true
		}
	}

	// 3. 掃描工廠呼叫 `Tmpl.<m>(concreteBuffer, ...)`，推斷每個呼叫點的元素型別。
	//    回傳 map: factoryCall 指標 -> (template, elemType)。
	factoryBinds := collectFactoryBinds(program, templates, varTypes)
	if len(factoryBinds) == 0 {
		// 未被實例化：仍移除模板（與 map 路徑一致），避免 `t` 型別定義落到 codegen。
		removeSliceTemplates(program, templates)
		return
	}

	// 4. 依 (template, elemType) 生成具現化結構體 + 方法。
	type tmplElem struct {
		tmpl  *sliceTemplate
		elems map[string]bool
	}
	perTmpl := make(map[string]*tmplElem)
	var generated []parser.Statement
	concreteCache := make(map[string]string) // "bare|elem" -> concrete
	for i := range factoryBinds {
		b := &factoryBinds[i]
		key := b.tmpl.bare + "|" + b.elem
		concrete, ok := concreteCache[key]
		if !ok {
			concrete = b.tmpl.bare + "-" + parser.SanitizeLLVMTypeName(b.elem)
			concreteCache[key] = concrete
			generated = append(generated, specializeSliceStruct(b.tmpl, b.elem, concrete)...)
		}
		b.concrete = concrete
		if perTmpl[b.tmpl.bare] == nil {
			perTmpl[b.tmpl.bare] = &tmplElem{tmpl: b.tmpl, elems: make(map[string]bool)}
		}
		perTmpl[b.tmpl.bare].elems[b.elem] = true
	}

	// 5. 改寫呼叫點：工廠 DotExpression 接收者型別名 → 具現化名；
	//    工廠結果變數標註具現化型別（同時更新 varTypes 供後續 resolveMethodCall）。
	rewriteFactoryCalls(program, factoryBinds, varTypes)

	// 6. 移除原始模板結構體 + 模板方法，附加具現化定義。
	removeSliceTemplates(program, templates)
	program.Statements = append(program.Statements, generated...)
}

// factoryBind 記錄一個工廠呼叫點及其推斷出的模板與元素型別與具現化型別名。
type factoryBind struct {
	call     *parser.CallExpression
	dot      *parser.DotExpression
	tmpl     *sliceTemplate
	elem     string
	concrete string
}

// collectFactoryBinds 走訪所有語句（含函數體），找出模板工廠呼叫並推斷元素型別。
// 頂層語句以 globalVars 作為可見變數型別；進入函數體時改以該函數的區域變數型別，
// 這樣 `b = box.init(arr)` 裡 `arr` 若為區域變數也能推斷出元素型別。
func collectFactoryBinds(program *parser.Program, templates map[string]*sliceTemplate, globalVars map[string]string) []factoryBind {
	var binds []factoryBind
	for _, stmt := range program.Statements {
		scanStmtForFactories(stmt, templates, globalVars, program, &binds)
	}
	return binds
}

// scanStmtForFactories 遞迴走訪語句。scope 為目前可見的變數型別表。
// 進入 FunctionDefinition 時建立該函數专属的區域 scope（參數 + 函數體 let）。
func scanStmtForFactories(stmt parser.Statement, templates map[string]*sliceTemplate, scope map[string]string, program *parser.Program, binds *[]factoryBind) {
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		scanExprForFactories(s.Expression, templates, scope, program, binds)
	case *parser.LetStatement:
		scanExprForFactories(s.Value, templates, scope, program, binds)
	case *parser.FunctionDefinition:
		local := make(map[string]string)
		for _, p := range s.Parameters {
			if p.Type != nil {
				local[p.Name] = p.Type.String()
			}
		}
		collectVarTypesFromBody(s.Body, local)
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				scanStmtForFactories(b, templates, local, program, binds)
			}
		}
	case *parser.BlockStatement:
		for _, b := range s.Statements {
			scanStmtForFactories(b, templates, scope, program, binds)
		}
	case *parser.ForStatement:
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				scanStmtForFactories(b, templates, scope, program, binds)
			}
		}
	}
}

func scanExprForFactories(expr parser.Expression, templates map[string]*sliceTemplate, scope map[string]string, program *parser.Program, binds *[]factoryBind) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.CallExpression:
		if dot, ok := e.Function.(*parser.DotExpression); ok {
			tryRecordFactory(dot, e, templates, scope, program, binds)
			scanExprForFactories(dot.Receiver, templates, scope, program, binds)
		}
		for _, arg := range e.Arguments {
			scanExprForFactories(arg, templates, scope, program, binds)
		}
	case *parser.InfixExpression:
		scanExprForFactories(e.Left, templates, scope, program, binds)
		scanExprForFactories(e.Right, templates, scope, program, binds)
	case *parser.IndexExpression:
		scanExprForFactories(e.Left, templates, scope, program, binds)
		scanExprForFactories(e.Index, templates, scope, program, binds)
	case *parser.AssignExpression:
		scanExprForFactories(e.Left, templates, scope, program, binds)
		scanExprForFactories(e.Value, templates, scope, program, binds)
	case *parser.IfExpression:
		scanExprForFactories(e.Condition, templates, scope, program, binds)
		if e.Consequence != nil {
			for _, b := range e.Consequence.Statements {
				scanStmtForFactories(b, templates, scope, program, binds)
			}
		}
		if e.Alternative != nil {
			for _, b := range e.Alternative.Statements {
				scanStmtForFactories(b, templates, scope, program, binds)
			}
		}
	}
}

// tryRecordFactory 判斷 dot 是否為某模板的工廠呼叫（接收者是型別名而非變數、
// 屬性是模板方法名），若是則推斷元素型別並記錄。
func tryRecordFactory(dot *parser.DotExpression, call *parser.CallExpression, templates map[string]*sliceTemplate, scope map[string]string, program *parser.Program, binds *[]factoryBind) {
	recv, ok := dot.Receiver.(*parser.Identifier)
	if !ok {
		return
	}
	// 接收者必須是型別名（不在目前 scope 中作為變數存在）。
	if _, isVar := scope[recv.Value]; isVar {
		return
	}
	tmpl := templates[recv.Value]
	if tmpl == nil || !tmpl.suffixes[dot.Property] {
		return
	}
	elem := inferFactoryElem(call, tmpl, program, scope)
	if elem == "" {
		return
	}
	*binds = append(*binds, factoryBind{call: call, dot: dot, tmpl: tmpl, elem: elem})
}

// inferFactoryElem 直接從模板工廠方法簽名推斷元素型別：找出「以 varName 作為
// 元素」的參數（[]t / [n]t / ?t / 裸 t），以其對應引數的具體型別取出元素。
// 不复用 inferGenericArgs——它的容器分支要求 len(paramType)>3，会漏掉 `[]t`
// （長度恰好為 3）。
func inferFactoryElem(call *parser.CallExpression, tmpl *sliceTemplate, program *parser.Program, scope map[string]string) string {
	dot, ok := call.Function.(*parser.DotExpression)
	if !ok {
		return ""
	}
	var fd *parser.FunctionDefinition
	for _, m := range tmpl.methods {
		lastDot := strings.LastIndex(m.Name, ".")
		if lastDot >= 0 && m.Name[lastDot+1:] == dot.Property {
			fd = m
			break
		}
	}
	if fd == nil {
		return ""
	}
	for pi, param := range fd.Parameters {
		if pi >= len(call.Arguments) {
			break
		}
		carries, container := paramCarriesVar(param.Type, tmpl.varName)
		if !carries {
			continue
		}
		argType := scopedArgType(call.Arguments[pi], program, scope)
		if argType == "" {
			continue
		}
		elem := argType
		if container {
			elem = elemOfContainerType(argType)
		}
		if elem != "" {
			return elem
		}
	}
	return ""
}

// scopedArgType 推斷引數型別：先查目前 scope（含函數區域變數），再退回
// inferArgType（掃描頂層 let / 字面值）。
func scopedArgType(expr parser.Expression, program *parser.Program, scope map[string]string) string {
	if id, ok := expr.(*parser.Identifier); ok {
		if t, has := scope[id.Value]; has && t != "" {
			return t
		}
	}
	return inferArgType(expr, program)
}

// paramCarriesVar 判斷參數型別是否以 varName 作為其元素型別。
// container=true 表示 varName 被 slice/array 包裝（引數是緩衝區，需取元素）；
// container=false 表示裸 varName 或 option 包裝（引數型別即元素型別）。
func paramCarriesVar(t parser.Type, varName string) (carries bool, container bool) {
	if varName == "" {
		return false, false
	}
	switch typ := t.(type) {
	case *parser.SliceType:
		return namedTypeVarName(typ.Elem) == varName, true
	case *parser.ArrayType:
		return namedTypeVarName(typ.Elem) == varName, true
	case *parser.NullableType:
		return paramCarriesVar(typ.Type, varName)
	case *parser.ViewType:
		return paramCarriesVar(typ.Type, varName)
	case *parser.NamedType:
		return typ.Value == varName, false
	}
	return false, false
}

// elemOfContainerType 從容器型別字串取出元素型別：`[]i64`→`i64`、
// `[4]i64`→`i64`、`[?]str`→`str`；非容器型別原樣回傳。
func elemOfContainerType(t string) string {
	if strings.HasPrefix(t, "[]") {
		return t[2:]
	}
	if len(t) > 2 && t[0] == '[' {
		if cb := strings.IndexByte(t, ']'); cb > 0 && cb+1 < len(t) {
			return t[cb+1:]
		}
	}
	return t
}

// rewriteFactoryCalls 把工廠 DotExpression 的接收者型別名改為具現化名，並把
// 承接工廠結果的 let 變數（頂層或函數區域）標註為具現化型別，使後續實例方法
// 呼叫能依型別攤平。頂層 let 同時更新 varTypes 供 resolveMethodCall 使用。
func rewriteFactoryCalls(program *parser.Program, binds []factoryBind, varTypes map[string]string) {
	byCall := make(map[*parser.CallExpression]*factoryBind)
	for i := range binds {
		byCall[binds[i].call] = &binds[i]
	}
	// 遍歷（含函數體）標註 let 型別。
	for _, stmt := range program.Statements {
		retypesFactoryLets(stmt, byCall, varTypes)
	}
	// 所有工廠呼叫（含巢狀 / 丟棄結果）改寫接收者型別名。
	for i := range binds {
		b := &binds[i]
		if b.dot != nil {
			if recv, ok := b.dot.Receiver.(*parser.Identifier); ok && recv.Value == b.tmpl.bare {
				b.dot.Receiver = &parser.Identifier{Value: b.concrete}
			}
		}
	}
}

// retypesFactoryLets 遞迴走訪語句，將值為工廠呼叫的 let 標註為具現化型別。
// 頂層 let 同時寫入 varTypes；函數區域 let 僅改 AST 型別（MIR 會自行推斷）。
func retypesFactoryLets(stmt parser.Statement, byCall map[*parser.CallExpression]*factoryBind, varTypes map[string]string) {
	switch s := stmt.(type) {
	case *parser.LetStatement:
		if ce, isCall := s.Value.(*parser.CallExpression); isCall {
			if b, isFactory := byCall[ce]; isFactory {
				s.Type = &parser.NamedType{Value: b.concrete, IsInferred: true}
				if varTypes != nil && s.Name != nil {
					varTypes[s.Name.Value] = b.concrete
				}
			}
		}
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				retypesFactoryLets(b, byCall, nil)
			}
		}
	case *parser.BlockStatement:
		for _, b := range s.Statements {
			retypesFactoryLets(b, byCall, varTypes)
		}
	case *parser.ForStatement:
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				retypesFactoryLets(b, byCall, varTypes)
			}
		}
	case *parser.ExpressionStatement:
		if s.Expression != nil {
			if ce, isCall := s.Expression.(*parser.CallExpression); isCall {
				for _, arg := range ce.Arguments {
					if inner, ok := arg.(*parser.CallExpression); ok {
						retypesFactoryLets(&parser.LetStatement{Value: inner}, byCall, varTypes)
					}
				}
			}
		}
	}
}

// removeSliceTemplates 從程式中移除所有匹配的切片模板結構體與其模板方法。
func removeSliceTemplates(program *parser.Program, templates map[string]*sliceTemplate) {
	filtered := make([]parser.Statement, 0, len(program.Statements))
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			if templates[bareStructName(sd.Name)] != nil && sliceTemplateVarName(sd) != "" {
				continue
			}
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			lastDot := strings.LastIndex(fd.Name, ".")
			if lastDot >= 0 {
				if templates[bareStructName(fd.Name[:lastDot])] != nil {
					continue
				}
			}
		}
		filtered = append(filtered, stmt)
	}
	program.Statements = filtered
}

// specializeSliceStruct 依元素型別從模板結構體 + 方法生成具現化定義。
func specializeSliceStruct(tmpl *sliceTemplate, elemType, concreteName string) []parser.Statement {
	subst := map[string]string{tmpl.varName: elemType}
	subst[tmpl.bare] = concreteName
	if tmpl.sd.Name != tmpl.bare {
		subst[tmpl.sd.Name] = concreteName
	}
	// 方法名映射（含模組前綴 / 裸名）：供方法體内 self 呼叫改寫。
	for _, fd := range tmpl.methods {
		lastDot := strings.LastIndex(fd.Name, ".")
		if lastDot < 0 {
			continue
		}
		suffix := fd.Name[lastDot:]
		cmn := concreteName + suffix
		subst[fd.Name] = cmn
		bareMethod := tmpl.bare + suffix
		if bareMethod != fd.Name {
			subst[bareMethod] = cmn
		}
	}

	var generated []parser.Statement

	newFields := make([]*parser.StructField, len(tmpl.sd.Fields))
	for i, f := range tmpl.sd.Fields {
		newType := substituteType(f.Type, subst)
		if f.ArraySize > 0 {
			if at, ok := newType.(*parser.ArrayType); ok {
				newType = at.Elem
			}
		}
		newFields[i] = &parser.StructField{
			Token:     f.Token,
			Name:      f.Name,
			Type:      newType,
			ArraySize: f.ArraySize,
			IsSlice:   f.IsSlice,
			Value:     f.Value,
		}
	}
	generated = append(generated, &parser.StructDefinition{
		Token:  tmpl.sd.Token,
		Name:   concreteName,
		Fields: newFields,
	})

	for _, fd := range tmpl.methods {
		generated = append(generated, cloneMethod(fd, subst, concreteName))
	}
	return generated
}
