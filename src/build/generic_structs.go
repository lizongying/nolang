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
		if s.Type != nil {
			fmt.Fprintf(os.Stderr, "[DEBUG scanStmt] LetStatement name=%q type=%T typeStr=%q\n", s.Name.Value, s.Type, s.Type.String())
		}
		scanForMapTypes(s.Type, results)
		scanExprForMapTypes(s.Value, results)
	case *parser.FunctionDefinition:
		fmt.Fprintf(os.Stderr, "[DEBUG scanStmt] FunctionDefinition name=%q body stmts=%d\n", s.Name, len(s.Body.Statements))
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
// 支援兩種判定：帶有 GenericParams，或名稱以 "-tmpl" 結尾（hashmap-{cat}-tmpl）。
func isGenericStructTemplate(sd *parser.StructDefinition) bool {
	if len(sd.GenericParams) > 0 {
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
		subst["V"] = valueStr
	case "int":
		subst["K"] = keyStr
		subst["V"] = valueStr
	}
	// 將模板結構名稱對應至具體名稱，以替換 self 參數型別
	subst[tmplSD.Name] = concreteName

	// 將模板方法名（如 "hashmap-str-tmpl.hash"）對應至具體方法名（"hashmap-str-i64.hash"），
	// 以替換方法體內經 resolveSelfMethodCalls 改寫後的 self 呼叫。
	for _, fd := range tmplMethods {
		if dotIdx := strings.LastIndex(fd.Name, "."); dotIdx >= 0 {
			subst[fd.Name] = concreteName + fd.Name[dotIdx:]
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
			if isGenericStructTemplate(sd) {
				templateStructs[sd.Name] = sd
			}
		}
	}
	fmt.Fprintf(os.Stderr, "[DEBUG monomorphizeGenericStructs] templateStructs=%d\n", len(templateStructs))
	for name := range templateStructs {
		fmt.Fprintf(os.Stderr, "  template: %s\n", name)
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
	for name, methods := range templateMethods {
		fmt.Fprintf(os.Stderr, "  templateMethods[%s]=%d\n", name, len(methods))
	}

	// 2. 收集 map 型別使用點
	pairs := collectMapTypeUsages(program)
	fmt.Fprintf(os.Stderr, "[DEBUG monomorphizeGenericStructs] pairs=%d\n", len(pairs))
	for _, p := range pairs {
		fmt.Fprintf(os.Stderr, "  pair: key=%s value=%s\n", p.key, p.value)
	}

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
			continue // 該分類無對應模板
		}
		generated = append(generated, specializeGenericStruct(pair.key, pair.value, tmplSD, templateMethods[tmplName])...)
	}

	// 4. 移除原始模板陳述句（模板結構定義與模板方法）
	filtered := make([]parser.Statement, 0, len(program.Statements))
	for _, stmt := range program.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			if isGenericStructTemplate(sd) {
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
