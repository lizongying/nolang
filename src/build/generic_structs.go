package build

import (
	"fmt"
	"os"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// mapTypePair 記錄一組 (keyType, valueType) 字串對，用於泛型 hashmap 特化。
type mapTypePair struct {
	key   string
	value string
}

// keyCategory 根據鍵型別名稱回傳其分類："str" / "int" / "bool"；
// 不支援的鍵型別回傳空字串。
func keyCategory(keyType string) string {
	switch keyType {
	case "str":
		return "str"
	case "bool":
		return "bool"
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64":
		return "int"
	default:
		return ""
	}
}

// isMapTypeString 判斷型別字串是否為 map 型別（[K]V 形式，K 為已知鍵型別）。
// 用於 vet 驗證中區分 map 與 array/slice 型別。
func isMapTypeString(s string) bool {
	if !strings.HasPrefix(s, "[") {
		return false
	}
	closeBracket := strings.IndexByte(s, ']')
	if closeBracket <= 1 {
		return false
	}
	keyType := s[1:closeBracket]
	return keyCategory(keyType) != ""
}

// mapTypeToHashmapName 將 map 型別字串（如 "[bool]i64"）轉換為特化結構名稱（如 "hashmap-bool-i64"）。
// 若輸入不是 map 型別，回傳空字串。
func mapTypeToHashmapName(mapType string) string {
	if !strings.HasPrefix(mapType, "[") {
		return ""
	}
	closeBracket := strings.IndexByte(mapType, ']')
	if closeBracket <= 1 {
		return ""
	}
	keyType := mapType[1:closeBracket]
	if keyCategory(keyType) == "" {
		return ""
	}
	valueType := mapType[closeBracket+1:]
	return "hashmap-" + keyType + "-" + valueType
}

// scanForMapTypes 遞迴掃描 Type 節點，將所有 MapType 的 (key,value) 收集至 results。
// 處理巢狀型別（ArrayType/SliceType/NullableType/PointerType/FunctionType/MapType）。
func scanForMapTypes(t parser.Type, results *[]mapTypePair) {
	if t == nil {
		return
	}
	switch typ := t.(type) {
	case *parser.MapType:
		*results = append(*results, mapTypePair{key: typ.Key.String(), value: typ.Value.String()})
		scanForMapTypes(typ.Key, results)
		scanForMapTypes(typ.Value, results)
	case *parser.ArrayType:
		scanForMapTypes(typ.Elem, results)
	case *parser.SliceType:
		scanForMapTypes(typ.Elem, results)
	case *parser.NullableType:
		scanForMapTypes(typ.Type, results)
	case *parser.PointerType:
		scanForMapTypes(typ.Type, results)
	case *parser.FunctionType:
		for _, p := range typ.Params {
			scanForMapTypes(p.Type, results)
		}
		for _, r := range typ.Results {
			scanForMapTypes(r.Type, results)
		}
	}
}

// scanStmtForMapTypes 遞迴掃描陳述句中所有型別位置，收集 MapType 使用點。
func scanStmtForMapTypes(stmt parser.Statement, results *[]mapTypePair) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *parser.LetStatement:
		scanForMapTypes(s.Type, results)
		scanExprForMapTypes(s.Value, results)
	case *parser.FunctionDefinition:
		for _, p := range s.Parameters {
			scanForMapTypes(p.Type, results)
		}
		for _, r := range s.Results {
			scanForMapTypes(r.Type, results)
		}
		scanBlockForMapTypes(s.Body, results)
	case *parser.StructDefinition:
		for _, f := range s.Fields {
			scanForMapTypes(f.Type, results)
		}
	case *parser.ExpressionStatement:
		scanExprForMapTypes(s.Expression, results)
	case *parser.BlockStatement:
		scanBlockForMapTypes(s, results)
	case *parser.ForStatement:
		scanBlockForMapTypes(s.Body, results)
		scanExprForMapTypes(s.Condition, results)
		scanExprForMapTypes(s.CountExpr, results)
	}
}

// scanBlockForMapTypes 掃描區塊內所有陳述句。
func scanBlockForMapTypes(body *parser.BlockStatement, results *[]mapTypePair) {
	if body == nil {
		return
	}
	for _, st := range body.Statements {
		scanStmtForMapTypes(st, results)
	}
}

// scanExprForMapTypes 遞迴掃描表達式中的型別位置（主要為 FunctionLiteral 的參數/回傳型別）。
func scanExprForMapTypes(expr parser.Expression, results *[]mapTypePair) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.FunctionLiteral:
		for _, p := range e.Parameters {
			scanForMapTypes(p.Type, results)
		}
		for _, r := range e.Results {
			scanForMapTypes(r.Type, results)
		}
		scanBlockForMapTypes(e.Body, results)
	case *parser.CallExpression:
		scanExprForMapTypes(e.Function, results)
		for _, a := range e.Arguments {
			scanExprForMapTypes(a, results)
		}
	case *parser.InfixExpression:
		scanExprForMapTypes(e.Left, results)
		scanExprForMapTypes(e.Right, results)
	case *parser.PrefixExpression:
		scanExprForMapTypes(e.Right, results)
	case *parser.IndexExpression:
		scanExprForMapTypes(e.Left, results)
		scanExprForMapTypes(e.Index, results)
	case *parser.DotExpression:
		scanExprForMapTypes(e.Receiver, results)
	case *parser.GroupedExpression:
		scanExprForMapTypes(e.Expression, results)
	}
}

// collectMapTypeUsages 掃描整個程式，收集所有去重後的 (keyType, valueType) 對。
// 對不支援的鍵型別（非 str/int/bool）透過 os.Stderr 回報編譯錯誤。
func collectMapTypeUsages(program *parser.Program) []mapTypePair {
	var raw []mapTypePair
	for _, stmt := range program.Statements {
		scanStmtForMapTypes(stmt, &raw)
	}
	seen := make(map[string]bool)
	var out []mapTypePair
	for _, p := range raw {
		k := p.key + "|" + p.value
		if seen[k] {
			continue
		}
		seen[k] = true
		if keyCategory(p.key) == "" {
			fmt.Fprintf(os.Stderr, "compile error: unsupported map key type %q (expected str/int/bool)\n", p.key)
		}
		out = append(out, p)
	}
	return out
}

// isGenericStructTemplate 判斷一個 StructDefinition 是否為泛型模板。
// 支援兩種判定：side-table 中帶有泛型參數（#{generic=[K,V]}），或名稱以 "-tmpl"
// 結尾（hashmap-{cat}-tmpl）。
func isGenericStructTemplate(sem *parser.SemanticContext, sd *parser.StructDefinition) bool {
	if len(sem.GenericParamsOf(sd)) > 0 {
		return true
	}
	return strings.HasSuffix(sd.Name, "-tmpl")
}

// isTemplateMethodName 判斷函式名稱是否屬於任一模板的方法（即 "tmplName.method" 形式）。
func isTemplateMethodName(name string, templates map[string]*parser.StructDefinition) bool {
	for tmplName := range templates {
		if strings.HasPrefix(name, tmplName+".") {
			return true
		}
	}
	return false
}

// specializeGenericStruct 依據具體鍵/值型別，從模板結構與方法產生具體定義。
// 回傳生成的陳述句列表（結構定義 + 各方法定義）。
func specializeGenericStruct(keyStr, valueStr string, tmplSD *parser.StructDefinition, tmplMethods []*parser.FunctionDefinition) []parser.Statement {
	cat := keyCategory(keyStr)
	concreteName := "hashmap-" + keyStr + "-" + valueStr

	// 建立替換表
	subst := make(map[string]string)
	switch cat {
	case "str", "bool":
		subst["v"] = valueStr
	case "int":
		subst["k"] = keyStr
		subst["v"] = valueStr
	}
	// 將模板結構名稱對應至具體名稱，以替換 self 參數型別。
	// tmplSD.Name 可能帶模組前綴（如 "map.hashmap-str-tmpl"，由 prefixModuleStatements 重命名），
	// 但 rewriteTypeRefs 因 isModulePrefixBuiltinType 將 "hashmap-*" 視為內建型別而「不」加上模組前綴，
	// 故型別參考仍為裸名 "hashmap-str-tmpl"。兩種形式皆需提供替換鍵。
	bareTmplName := tmplSD.Name
	if dotIdx := strings.Index(bareTmplName, "."); dotIdx >= 0 {
		bareTmplName = bareTmplName[dotIdx+1:]
	}
	subst[tmplSD.Name] = concreteName
	if bareTmplName != tmplSD.Name {
		subst[bareTmplName] = concreteName
	}

	// 將模板方法名（如 "hashmap-str-tmpl.hash"）對應至具體方法名（"hashmap-str-i64.hash"），
	// 以替換方法體內經 resolveSelfMethodCalls 改寫後的 self 呼叫。
	// 同樣需提供帶模組前綴與裸名兩種形式：
	//   - 模組前綴形式："map.hashmap-str-tmpl.get"（prefixMethodNames 重命名後的方法定義名）
	//   - 裸名形式："hashmap-str-tmpl.get"（resolveSelfMethodCalls 以未前綴的 selfType 產生）
	for _, fd := range tmplMethods {
		lastDot := strings.LastIndex(fd.Name, ".")
		if lastDot < 0 {
			continue
		}
		methodSuffix := fd.Name[lastDot:] // e.g. ".get"
		concreteMethodName := concreteName + methodSuffix
		// 完整名（含模組前綴）
		subst[fd.Name] = concreteMethodName
		// 裸名（不含模組前綴）
		bareMethodName := bareTmplName + methodSuffix
		if bareMethodName != fd.Name {
			subst[bareMethodName] = concreteMethodName
		}
	}

	var generated []parser.Statement

	// 複製結構定義（保留 ArraySize/IsSlice，替換元素型別）
	newFields := make([]*parser.StructField, len(tmplSD.Fields))
	for i, f := range tmplSD.Fields {
		newFields[i] = &parser.StructField{
			Token:     f.Token,
			Name:      f.Name,
			Type:      substituteType(f.Type, subst),
			ArraySize: f.ArraySize,
			IsSlice:   f.IsSlice,
			Value:     f.Value,
		}
	}
	generated = append(generated, &parser.StructDefinition{
		Token:  tmplSD.Token,
		Name:   concreteName,
		Fields: newFields,
	})

	// 複製每個方法
	for _, fd := range tmplMethods {
		generated = append(generated, cloneMethod(fd, subst, concreteName))
	}

	return generated
}

// cloneMethod 複製一個模板方法，套用替換表並更名為 "concreteName.methodSuffix"。
func cloneMethod(fd *parser.FunctionDefinition, subst map[string]string, concreteName string) *parser.FunctionDefinition {
	methodSuffix := fd.Name
	if dotIdx := strings.LastIndex(fd.Name, "."); dotIdx >= 0 {
		methodSuffix = fd.Name[dotIdx+1:]
	}
	newName := concreteName + "." + methodSuffix

	newParams := make([]*parser.Parameter, len(fd.Parameters))
	for i, p := range fd.Parameters {
		newParams[i] = &parser.Parameter{
			Token:       p.Token,
			Name:        p.Name,
			Type:        substituteType(p.Type, subst),
			DefaultExpr: p.DefaultExpr,
		}
	}

	newResults := make([]*parser.Parameter, len(fd.Results))
	for i, r := range fd.Results {
		newResults[i] = &parser.Parameter{
			Token:       r.Token,
			Name:        r.Name,
			Type:        substituteType(r.Type, subst),
			DefaultExpr: r.DefaultExpr,
		}
	}

	return &parser.FunctionDefinition{
		Token: fd.Token,
		Name:  newName,
		FuncSignature: parser.FuncSignature{
			Parameters: newParams,
			Results:    newResults,
		},
		Body: substituteBody(fd.Body, subst),
	}
}

// monomorphizeGenericStructs 泛型結構體單態化主流程：
//  1. 收集模板結構與方法
//  2. 收集所有 map[K]V 使用點
//  3. 對每組 (K,V) 自對應模板生成具體結構與方法
//  4. 移除原始模板定義，附加生成的具體定義
func monomorphizeGenericStructs(program *parser.Program) {
	// 1. 收集模板結構
	templateStructs := make(map[string]*parser.StructDefinition)
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			if isGenericStructTemplate(program.Sem, sd) {
				templateStructs[sd.Name] = sd
			}
		}
	}
	if len(templateStructs) == 0 {
		return
	}

	// 依模板名稱分組方法
	templateMethods := make(map[string][]*parser.FunctionDefinition)
	for _, stmt := range program.Statements {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			for tmplName := range templateStructs {
				if strings.HasPrefix(fd.Name, tmplName+".") {
					templateMethods[tmplName] = append(templateMethods[tmplName], fd)
					break
				}
			}
		}
	}

	// 2. 收集 map 型別使用點
	pairs := collectMapTypeUsages(program)

	// 3. 對每組 (K,V) 生成具體定義
	var generated []parser.Statement
	for _, pair := range pairs {
		cat := keyCategory(pair.key)
		if cat == "" {
			continue // 不支援的鍵型別，已於 collectMapTypeUsages 回報
		}
		tmplName := "hashmap-" + cat + "-tmpl"
		tmplSD, ok := templateStructs[tmplName]
		if !ok {
			// 模組導入的模板可能帶模組前綴（如 "map.hashmap-str-tmpl"），
			// 嘗試在 templateStructs 中查找以 tmplName 結尾的鍵。
			for k, v := range templateStructs {
				if strings.HasSuffix(k, "."+tmplName) {
					tmplSD = v
					tmplName = k
					ok = true
					break
				}
			}
		}
		if !ok {
			continue // 該分類無對應模板
		}
		generated = append(generated, specializeGenericStruct(pair.key, pair.value, tmplSD, templateMethods[tmplName])...)
	}

	// 4. 移除原始模板陳述句（模板結構定義與模板方法）
	filtered := make([]parser.Statement, 0, len(program.Statements))
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			if isGenericStructTemplate(program.Sem, sd) {
				continue
			}
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if isTemplateMethodName(fd.Name, templateStructs) {
				continue
			}
		}
		filtered = append(filtered, stmt)
	}

	// 5. 附加生成的具體定義
	program.Statements = append(filtered, generated...)
}
