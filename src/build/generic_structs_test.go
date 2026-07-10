package build

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// ---- keyCategory ----

func TestKeyCategory(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"str", "str"},
		{"bool", "bool"},
		{"i8", "int"}, {"i16", "int"}, {"i32", "int"}, {"i64", "int"},
		{"u8", "int"}, {"u16", "int"}, {"u32", "int"}, {"u64", "int"},
		{"f32", ""}, {"f64", ""}, {"byte", ""}, {"char", ""},
		{"V", ""}, {"K", ""}, {"", ""},
	}
	for _, tt := range tests {
		got := keyCategory(tt.in)
		if got != tt.want {
			t.Errorf("keyCategory(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ---- collectMapTypeUsages ----

func TestCollectMapTypeUsagesLetStatement(t *testing.T) {
	// map[str]i64 出現在兩個 LetStatement，加上 map[i64]bool；測試去重
	src := `m map[str]i64
n map[str]i64
b map[i64]bool
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	pairs := collectMapTypeUsages(prog)
	if len(pairs) != 2 {
		t.Fatalf("expected 2 deduped pairs, got %d: %v", len(pairs), pairs)
	}
	want := map[string]bool{"str|i64": false, "i64|bool": false}
	for _, pr := range pairs {
		k := pr.key + "|" + pr.value
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected pair %s", k)
			continue
		}
		want[k] = true
	}
	for k, found := range want {
		if !found {
			t.Errorf("missing pair %s", k)
		}
	}
}

func TestCollectMapTypeUsagesMultiplePositions(t *testing.T) {
	// 以手動建構 AST 測試 FunctionDefinition 參數/回傳與 StructDefinition 欄位位置
	strType := &parser.NamedType{Value: "str"}
	i64Type := &parser.NamedType{Value: "i64"}
	mt := &parser.MapType{Key: strType, Value: i64Type} // [str]i64

	fd := &parser.FunctionDefinition{
		Name: "f",
		FuncSignature: parser.FuncSignature{
			Parameters: []*parser.Parameter{{Name: "p", Type: mt}},
			Results:    []*parser.Parameter{{Name: "r", Type: mt}},
		},
	}
	sd := &parser.StructDefinition{
		Name:   "s",
		Fields: []*parser.StructField{{Name: "fld", Type: mt}},
	}
	prog := &parser.Program{Statements: []parser.Statement{fd, sd}}

	pairs := collectMapTypeUsages(prog)
	// 三個位置皆為同一組 (str,i64)，去重後應為 1
	if len(pairs) != 1 {
		t.Fatalf("expected 1 deduped pair from 3 positions, got %d: %v", len(pairs), pairs)
	}
	if pairs[0].key != "str" || pairs[0].value != "i64" {
		t.Errorf("expected (str,i64), got (%s,%s)", pairs[0].key, pairs[0].value)
	}
}

func TestCollectMapTypeUsagesNestedType(t *testing.T) {
	// MapType 巢狀於 ArrayType：[2][str]i64 應被掃出
	strType := &parser.NamedType{Value: "str"}
	i64Type := &parser.NamedType{Value: "i64"}
	mt := &parser.MapType{Key: strType, Value: i64Type}
	arr := &parser.ArrayType{Size: &parser.IntegerLiteral{Value: 2}, Elem: mt}

	ls := &parser.LetStatement{
		Name: &parser.Identifier{Value: "x"},
		Type: arr,
	}
	prog := &parser.Program{Statements: []parser.Statement{ls}}

	pairs := collectMapTypeUsages(prog)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair from nested ArrayType, got %d: %v", len(pairs), pairs)
	}
	if pairs[0].key != "str" || pairs[0].value != "i64" {
		t.Errorf("expected (str,i64), got (%s,%s)", pairs[0].key, pairs[0].value)
	}
}

// ---- specializeGenericStruct ----

func parseTemplate(t *testing.T, src string) (*parser.StructDefinition, []*parser.FunctionDefinition) {
	t.Helper()
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	var sd *parser.StructDefinition
	var methods []*parser.FunctionDefinition
	for _, stmt := range prog.Statements {
		switch s := stmt.(type) {
		case *parser.StructDefinition:
			sd = s
		case *parser.FunctionDefinition:
			methods = append(methods, s)
		}
	}
	if sd == nil {
		t.Fatalf("no struct definition parsed")
	}
	return sd, methods
}

func TestSpecializeGenericStructStr(t *testing.T) {
	src := `hashmap-str-tmpl {
    cap i64
    vals [2]V
    size i64
}

hashmap-str-tmpl.put = (key str, val V) {
    .vals[0] = val
}

hashmap-str-tmpl.contains = (key str)(found bool) {
    found = false
    val = .get(key)
    val: {
        ok -> found = true
    }
}
`
	sd, methods := parseTemplate(t, src)
	if len(methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(methods))
	}

	generated := specializeGenericStruct("str", "i64", sd, methods)
	// 1 struct + 2 methods
	if len(generated) != 3 {
		t.Fatalf("expected 3 generated statements, got %d", len(generated))
	}

	// 檢查結構定義
	newSD, ok := generated[0].(*parser.StructDefinition)
	if !ok {
		t.Fatalf("expected StructDefinition, got %T", generated[0])
	}
	if newSD.Name != "hashmap-str-i64" {
		t.Errorf("struct name = %q, want hashmap-str-i64", newSD.Name)
	}
	// vals 欄位應為 i64 且 ArraySize 保留為 2
	var valsField *parser.StructField
	for _, f := range newSD.Fields {
		if f.Name == "vals" {
			valsField = f
		}
	}
	if valsField == nil {
		t.Fatalf("no vals field")
	}
	if valsField.Type.String() != "i64" {
		t.Errorf("vals type = %s, want i64 (V substituted)", valsField.Type.String())
	}
	if valsField.ArraySize != 2 {
		t.Errorf("vals ArraySize = %d, want 2 (preserved)", valsField.ArraySize)
	}

	// 檢查方法名稱與簽章
	wantMethods := map[string]bool{"hashmap-str-i64.put": false, "hashmap-str-i64.contains": false}
	for _, stmt := range generated[1:] {
		fd, ok := stmt.(*parser.FunctionDefinition)
		if !ok {
			t.Errorf("expected FunctionDefinition, got %T", stmt)
			continue
		}
		if _, ok := wantMethods[fd.Name]; !ok {
			t.Errorf("unexpected method %q", fd.Name)
			continue
		}
		wantMethods[fd.Name] = true
		// self 參數型別應為 hashmap-str-i64
		if len(fd.Parameters) == 0 || fd.Parameters[0].Type.String() != "hashmap-str-i64" {
			t.Errorf("method %q: self param type = %q, want hashmap-str-i64", fd.Name, fd.Parameters[0].Type.String())
		}
	}
	for name, found := range wantMethods {
		if !found {
			t.Errorf("missing method %q", name)
		}
	}

	// 檢查 put 的 val 參數型別應為 i64
	for _, stmt := range generated[1:] {
		fd := stmt.(*parser.FunctionDefinition)
		if fd.Name == "hashmap-str-i64.put" {
			// param[2] = val: V → i64
			if fd.Parameters[2].Name != "val" || fd.Parameters[2].Type.String() != "i64" {
				t.Errorf("put val param = %s:%s, want val:i64", fd.Parameters[2].Name, fd.Parameters[2].Type.String())
			}
		}
		if fd.Name == "hashmap-str-i64.contains" {
			// body 內 val = .get(key) 返回 ?V → ?i64
			var foundAssign bool
			for _, st := range fd.Body.Statements {
				if ls, ok := st.(*parser.LetStatement); ok && ls.Name.Value == "val" {
					foundAssign = true
					// val 的型別應為 ?i64（V 替換後）
					if ls.Type != nil && ls.Type.String() != "?i64" {
						t.Errorf("contains val type = %s, want ?i64 (V substituted in body)", ls.Type.String())
					}
				}
			}
			_ = foundAssign
		}
	}
}

func TestSpecializeGenericStructInt(t *testing.T) {
	src := `hashmap-int-tmpl {
    keys [2]K
    vals [2]V
}

hashmap-int-tmpl.put = (key K, val V) {
    .keys[0] = key
    .vals[0] = val
}
`
	sd, methods := parseTemplate(t, src)
	generated := specializeGenericStruct("i64", "bool", sd, methods)
	if len(generated) != 2 {
		t.Fatalf("expected 2 generated statements, got %d", len(generated))
	}
	newSD := generated[0].(*parser.StructDefinition)
	if newSD.Name != "hashmap-i64-bool" {
		t.Errorf("struct name = %q, want hashmap-i64-bool", newSD.Name)
	}
	// keys 欄位 K → i64, vals 欄位 V → bool
	var keysField, valsField *parser.StructField
	for _, f := range newSD.Fields {
		if f.Name == "keys" {
			keysField = f
		}
		if f.Name == "vals" {
			valsField = f
		}
	}
	if keysField == nil || keysField.Type.String() != "i64" {
		t.Errorf("keys type = %v, want i64 (K substituted)", keysField)
	}
	if valsField == nil || valsField.Type.String() != "bool" {
		t.Errorf("vals type = %v, want bool (V substituted)", valsField)
	}
	// 方法 self 型別
	fd := generated[1].(*parser.FunctionDefinition)
	if fd.Name != "hashmap-i64-bool.put" {
		t.Errorf("method name = %q, want hashmap-i64-bool.put", fd.Name)
	}
	if fd.Parameters[0].Type.String() != "hashmap-i64-bool" {
		t.Errorf("self type = %s, want hashmap-i64-bool", fd.Parameters[0].Type.String())
	}
	// key: K → i64, val: V → bool
	if fd.Parameters[1].Type.String() != "i64" {
		t.Errorf("key type = %s, want i64", fd.Parameters[1].Type.String())
	}
	if fd.Parameters[2].Type.String() != "bool" {
		t.Errorf("val type = %s, want bool", fd.Parameters[2].Type.String())
	}
}

// ---- monomorphizeGenericStructs ----

func TestMonomorphizeGenericStructsRemovesTemplates(t *testing.T) {
	src := `hashmap-str-tmpl {
    cap i64
    vals [2]V
    size i64
}

hashmap-str-tmpl.put = (key str, val V) {
    .vals[0] = val
}

hashmap-str-tmpl.get = (key str, result V) {
    result = .vals[0]
}

m map[str]i64
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	monomorphizeGenericStructs(prog)

	// 確認模板結構與方法已被移除
	var hasTemplateStruct, hasTemplateMethod bool
	var hasConcreteStruct, hasPut, hasGet bool
	for _, stmt := range prog.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			if sd.Name == "hashmap-str-tmpl" {
				hasTemplateStruct = true
			}
			if sd.Name == "hashmap-str-i64" {
				hasConcreteStruct = true
			}
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if fd.Name == "hashmap-str-tmpl.put" || fd.Name == "hashmap-str-tmpl.get" {
				hasTemplateMethod = true
			}
			if fd.Name == "hashmap-str-i64.put" {
				hasPut = true
			}
			if fd.Name == "hashmap-str-i64.get" {
				hasGet = true
			}
		}
	}
	if hasTemplateStruct {
		t.Errorf("template struct hashmap-str-tmpl should have been removed")
	}
	if hasTemplateMethod {
		t.Errorf("template methods should have been removed")
	}
	if !hasConcreteStruct {
		t.Errorf("concrete struct hashmap-str-i64 should have been generated")
	}
	if !hasPut {
		t.Errorf("concrete method hashmap-str-i64.put should have been generated")
	}
	if !hasGet {
		t.Errorf("concrete method hashmap-str-i64.get should have been generated")
	}
}

func TestMonomorphizeGenericStructsMultiplePairs(t *testing.T) {
	src := `hashmap-str-tmpl {
    vals [2]V
}

hashmap-str-tmpl.put = (key str, val V) {
    .vals[0] = val
}

hashmap-int-tmpl {
    keys [2]K
    vals [2]V
}

hashmap-int-tmpl.put = (key K, val V) {
    .keys[0] = key
}

a map[str]i64
b map[str]bool
c map[i64]i64
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}

	monomorphizeGenericStructs(prog)

	wantStructs := map[string]bool{
		"hashmap-str-i64": false, "hashmap-str-bool": false, "hashmap-i64-i64": false,
	}
	wantMethods := map[string]bool{
		"hashmap-str-i64.put":  false,
		"hashmap-str-bool.put": false,
		"hashmap-i64-i64.put":  false,
	}
	for _, stmt := range prog.Statements {
		if sd, ok := stmt.(*parser.StructDefinition); ok {
			if _, ok := wantStructs[sd.Name]; ok {
				wantStructs[sd.Name] = true
			}
			if sd.Name == "hashmap-str-tmpl" || sd.Name == "hashmap-int-tmpl" {
				t.Errorf("template struct %q should have been removed", sd.Name)
			}
		}
		if fd, ok := stmt.(*parser.FunctionDefinition); ok {
			if _, ok := wantMethods[fd.Name]; ok {
				wantMethods[fd.Name] = true
			}
			if fd.Name == "hashmap-str-tmpl.put" || fd.Name == "hashmap-int-tmpl.put" {
				t.Errorf("template method %q should have been removed", fd.Name)
			}
		}
	}
	for name, found := range wantStructs {
		if !found {
			t.Errorf("missing concrete struct %q", name)
		}
	}
	for name, found := range wantMethods {
		if !found {
			t.Errorf("missing concrete method %q", name)
		}
	}
}

func TestMonomorphizeGenericStructsNoTemplatesNoop(t *testing.T) {
	// 程式中沒有模板時應為 no-op
	src := `x i64
y i64
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	before := len(prog.Statements)
	monomorphizeGenericStructs(prog)
	if len(prog.Statements) != before {
		t.Errorf("expected no change, got %d statements (was %d)", len(prog.Statements), before)
	}
}

// TestSpecializeGenericStructRewritesMethodCalls 驗證方法體內已解析的模板方法呼叫
// （如 resolveSelfMethodCalls 改寫後的 "hashmap-str-tmpl.hash" Identifier）會被替換為
// 具體方法名（"hashmap-str-i64.hash"）。
func TestSpecializeGenericStructRewritesMethodCalls(t *testing.T) {
	// 手動建構模板結構與方法，其 body 內含對另一個模板方法的呼叫（已 resolve 過的形式）
	tmplSD := &parser.StructDefinition{
		Name: "hashmap-str-tmpl",
		Fields: []*parser.StructField{
			{Name: "vals", Type: &parser.NamedType{Value: "V"}, ArraySize: 2},
		},
	}
	// hash 方法：被 get 呼叫的輔助方法
	hashMethod := &parser.FunctionDefinition{
		Name: "hashmap-str-tmpl.hash",
		FuncSignature: parser.FuncSignature{
			Parameters: []*parser.Parameter{
				{Name: "self", Type: &parser.NamedType{Value: "hashmap-str-tmpl"}},
				{Name: "key", Type: &parser.NamedType{Value: "str"}},
			},
		},
		Body: &parser.BlockStatement{},
	}
	// get 方法：body 內呼叫 hashmap-str-tmpl.hash(self, key)
	getMethod := &parser.FunctionDefinition{
		Name: "hashmap-str-tmpl.get",
		FuncSignature: parser.FuncSignature{
			Parameters: []*parser.Parameter{
				{Name: "self", Type: &parser.NamedType{Value: "hashmap-str-tmpl"}},
				{Name: "key", Type: &parser.NamedType{Value: "str"}},
			},
		},
		Body: &parser.BlockStatement{
			Statements: []parser.Statement{
				&parser.ExpressionStatement{
					Expression: &parser.CallExpression{
						Function: &parser.Identifier{Value: "hashmap-str-tmpl.hash"},
						Arguments: []parser.Expression{
							&parser.Identifier{Value: "self"},
							&parser.Identifier{Value: "key"},
						},
					},
				},
			},
		},
	}

	generated := specializeGenericStruct("str", "i64", tmplSD, []*parser.FunctionDefinition{hashMethod, getMethod})
	// 1 struct + 2 methods
	if len(generated) != 3 {
		t.Fatalf("expected 3 statements (struct + 2 methods), got %d", len(generated))
	}
	// 找到 get 方法
	var getFD *parser.FunctionDefinition
	for _, stmt := range generated {
		if fd, ok := stmt.(*parser.FunctionDefinition); ok && fd.Name == "hashmap-str-i64.get" {
			getFD = fd
			break
		}
	}
	if getFD == nil {
		t.Fatalf("missing hashmap-str-i64.get method in generated output")
	}
	// 檢查 body 內的呼叫已從 hashmap-str-tmpl.hash 改為 hashmap-str-i64.hash
	es, ok := getFD.Body.Statements[0].(*parser.ExpressionStatement)
	if !ok {
		t.Fatalf("expected ExpressionStatement, got %T", getFD.Body.Statements[0])
	}
	ce, ok := es.Expression.(*parser.CallExpression)
	if !ok {
		t.Fatalf("expected CallExpression, got %T", es.Expression)
	}
	id, ok := ce.Function.(*parser.Identifier)
	if !ok {
		t.Fatalf("expected Identifier as Function, got %T", ce.Function)
	}
	if id.Value != "hashmap-str-i64.hash" {
		t.Errorf("method call in body = %q, want hashmap-str-i64.hash (template name rewritten)", id.Value)
	}
}
