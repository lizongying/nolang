package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestParseArrayTypeMethodDefinitionNullableSlice verifies that
// `[?]t.method(…)` is recognized as an array-type method definition
// and the `[?]` prefix is preserved in the function name. Before the
// fix, the parser stripped the `[?]` and the function name was just
// `t.method`, which caused the formatter to output `[nil]t.method`
// for the receiver type.
func TestParseArrayTypeMethodDefinitionNullableSlice(t *testing.T) {
	src := `[?]ord.ast = () (res [?]ord) {
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	fd, ok := prog.Statements[0].(*FunctionDefinition)
	if !ok {
		t.Fatalf("expected *FunctionDefinition, got %T", prog.Statements[0])
	}
	if fd.Name != "[?]ord.ast" {
		t.Errorf("expected name '[?]ord.ast', got %q", fd.Name)
	}
	if len(fd.Parameters) != 0 {
		t.Fatalf("expected 0 input params (self is now out-param), got %d", len(fd.Parameters))
	}
	if len(fd.Results) != 2 || fd.Results[0].Name != "self" {
		t.Fatalf("expected 2 results (self + [?]ord), got %d", len(fd.Results))
	}
	if fd.Results[0].Type == nil || fd.Results[0].Type.String() != "[?]ord" {
		t.Errorf("expected self type '[?]ord', got %v", fd.Results[0].Type)
	}
	if fd.Results[1].Type == nil || fd.Results[1].Type.String() != "[?]ord" {
		t.Errorf("expected result type '[?]ord', got %v", fd.Results[1].Type)
	}
}

// TestParseInterfaceMethodGenericReceiver verifies that an interface can
// declare a method with a generic-receiver form `t.method(params) (results)`,
func TestParseInterfaceMethodGenericReceiver(t *testing.T) {
	src := `ord {
    t.gt(b t) (res bool)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	id, ok := prog.Statements[0].(*InterfaceDefinition)
	if !ok {
		t.Fatalf("expected *InterfaceDefinition, got %T", prog.Statements[0])
	}
	if id.Name != "ord" {
		t.Errorf("expected interface name 'ord', got %q", id.Name)
	}
	if len(id.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(id.Methods))
	}
	m := id.Methods[0]
	if m.Name != "gt" {
		t.Errorf("expected method name 'gt', got %q", m.Name)
	}
	if !m.IsGenericReceiver {
		t.Errorf("expected IsGenericReceiver=true, got false")
	}
	if m.Receiver != "t" {
		t.Errorf("expected Receiver 't', got %q", m.Receiver)
	}
	if len(m.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(m.Parameters))
	}
	if m.Parameters[0].Name != "b" || m.Parameters[0].Type.String() != "t" {
		t.Errorf("expected param (b t), got (%s %s)", m.Parameters[0].Name, m.Parameters[0].Type.String())
	}
	if len(m.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(m.Results))
	}
	if m.Results[0].Name != "res" || m.Results[0].Type.String() != "bool" {
		t.Errorf("expected result (res bool), got (%s %s)", m.Results[0].Name, m.Results[0].Type.String())
	}
}

// ── 接口合併（體內形式）────────────────────────────────────────────

// topInterfaceDef returns the top-level *InterfaceDefinition named name.
func topInterfaceDef(t *testing.T, prog *Program, name string) *InterfaceDefinition {
	t.Helper()
	for _, s := range prog.Statements {
		if id, ok := s.(*InterfaceDefinition); ok && id.Name == name {
			return id
		}
	}
	t.Fatalf("no top-level *InterfaceDefinition named %q", name)
	return nil
}

// ifaceMethodNames 收集介面方法名，便於斷言混排時的保序性。
func ifaceMethodNames(id *InterfaceDefinition) []string {
	out := make([]string, 0, len(id.Methods))
	for _, m := range id.Methods {
		out = append(out, m.Name)
	}
	return out
}

// TestInterfaceMergeItemsInBody 鎖定體內合併形：
//
//	serialize {
//	    json
//	    noml
//	}
//
// 首成員是「獨佔一行的裸接口名」，既不是 struct 欄位（同行無型別），也不是
// C 風格列舉（無逗號），因此必須被 classifyBlock 判為 blockIface 並把兩個名字
// 追加到 Implements（複用該欄位，不新增 AST 欄位）。
func TestInterfaceMergeItemsInBody(t *testing.T) {
	src := `json {
    to-json()
}
noml {
    to-noml()
}
serialize {
    json
    noml
}
`
	prog := parseSrc(t, src)

	j := topInterfaceDef(t, prog, "json")
	if len(j.Implements) != 0 {
		t.Fatalf("json.Implements = %v, want empty", j.Implements)
	}
	if got := ifaceMethodNames(j); len(got) != 1 || got[0] != "to-json" {
		t.Fatalf("json methods = %v, want [to-json]", got)
	}

	ser := topInterfaceDef(t, prog, "serialize")
	if got, want := strings.Join(ser.Implements, ","), "json,noml"; got != want {
		t.Fatalf("serialize.Implements = %v, want %v", ser.Implements, want)
	}
	if len(ser.Methods) != 0 {
		t.Fatalf("serialize.Methods = %v, want empty (merge-only body)", ifaceMethodNames(ser))
	}
}

// TestInterfaceMergeMixedWithMethods 鎖定「合併項與方法混排」：合併項按出現順序
// 追加到 Implements，方法照舊進 Methods，兩者互不干擾。
func TestInterfaceMergeMixedWithMethods(t *testing.T) {
	src := `db {
    enter
    leave
    close() (ok bool)
    fs.close()
}
`
	prog := parseSrc(t, src)
	id := topInterfaceDef(t, prog, "db")
	if got, want := strings.Join(id.Implements, ","), "enter,leave"; got != want {
		t.Fatalf("db.Implements = %v, want %v", id.Implements, want)
	}
	if got, want := strings.Join(ifaceMethodNames(id), ","), "close,close"; got != want {
		t.Fatalf("db methods = %v, want %v", got, want)
	}
	if len(id.Methods[0].Results) != 1 {
		t.Fatalf("db.close results = %v, want one result", id.Methods[0].Results)
	}
	if !id.Methods[1].IsGenericReceiver || id.Methods[1].Receiver != "fs" {
		t.Fatalf("db.fs.close = %+v, want generic receiver fs", id.Methods[1])
	}
}

// TestInterfaceDottedMergeItem 鎖定點號父接口：`sql.stmt⏎` 是合併項，
// 而 `t.method(...)` 仍是泛型接收者方法，兩者都以 DOT 開頭。
func TestInterfaceDottedMergeItem(t *testing.T) {
	src := `stmt-mysql {
    sql.stmt
    sql.rows
    t.gt(b t) (res bool)
}
`
	prog := parseSrc(t, src)
	id := topInterfaceDef(t, prog, "stmt-mysql")
	if got, want := strings.Join(id.Implements, ","), "sql.stmt,sql.rows"; got != want {
		t.Fatalf("stmt-mysql.Implements = %v, want %v", id.Implements, want)
	}
	if len(id.Methods) != 1 || id.Methods[0].Name != "gt" || !id.Methods[0].IsGenericReceiver {
		t.Fatalf("stmt-mysql methods = %v, want single generic-receiver gt", ifaceMethodNames(id))
	}
}

// TestInterfaceEmptyBody 鎖定空介面體仍可解析（改造後不得把 `{}` 誤判）。
func TestInterfaceEmptyBody(t *testing.T) {
	prog := parseSrc(t, "empty {\n}\n")
	id := topInterfaceDef(t, prog, "empty")
	if len(id.Implements) != 0 || len(id.Methods) != 0 {
		t.Fatalf("empty = implements %v methods %v, want both empty", id.Implements, id.Methods)
	}
}

// TestInterfaceMergeRegressionStructEnum 鎖定 classifyBlock 新規則不誤傷：
//   - `holder { json noml }`（同行有型別）仍是 struct 欄位；
//   - `color { red,\n green }`（逗號）仍是 C 風格列舉。
func TestInterfaceMergeRegressionStructEnum(t *testing.T) {
	src := `holder {
    json noml
}
color {
    red,
    green
}
`
	prog := parseSrc(t, src)

	var sd *StructDefinition
	var ed *EnumDefinition
	for _, s := range prog.Statements {
		switch v := s.(type) {
		case *StructDefinition:
			if v.Name == "holder" {
				sd = v
			}
		case *EnumDefinition:
			if v.Name == "color" {
				ed = v
			}
		}
	}
	if sd == nil {
		t.Fatalf("holder was not parsed as a struct definition")
	}
	if len(sd.Fields) != 1 || sd.Fields[0].Name != "json" {
		t.Fatalf("holder.Fields = %+v, want one field 'json'", sd.Fields)
	}
	if ed == nil {
		t.Fatalf("color was not parsed as an enum definition")
	}
	if len(ed.Values) != 2 {
		t.Fatalf("color.Values = %+v, want 2 variants", ed.Values)
	}
}

// TestInterfaceLegacyListThenBodyMergeKeepsOrder 鎖定「前置列表在前、體內項在後」
// 的出現順序（fmt / `no ast` 的輸出穩定性依賴它）。
func TestInterfaceLegacyListThenBodyMergeKeepsOrder(t *testing.T) {
	src := `both a, b {
    c
    d
    m()
}
`
	prog := parseSrc(t, src)
	id := topInterfaceDef(t, prog, "both")
	if got, want := strings.Join(id.Implements, ","), "a,b,c,d"; got != want {
		t.Fatalf("both.Implements = %v, want %v", id.Implements, want)
	}
}

// TestParseInterfaceWithImplements verifies that an interface can inherit
// from other interfaces using the `name iface1, iface2 { ... }` syntax,
// e.g.:
//
//	db enter, leave {
//	    close() (ok bool)
//	    query(sql str) (rs rows)
//	}
//
// This is interface inheritance/merging — the interface `db` inherits the
// method signatures from `enter` and `leave` and adds its own methods.
func TestParseInterfaceWithImplements(t *testing.T) {
	src := `db enter, leave {
    close() (ok bool)
    query(sql str) (rs rows)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	id, ok := prog.Statements[0].(*InterfaceDefinition)
	if !ok {
		t.Fatalf("expected *InterfaceDefinition, got %T", prog.Statements[0])
	}
	if id.Name != "db" {
		t.Errorf("expected interface name 'db', got %q", id.Name)
	}
	if len(id.Implements) != 2 {
		t.Fatalf("expected 2 implemented interfaces, got %d", len(id.Implements))
	}
	if id.Implements[0] != "enter" || id.Implements[1] != "leave" {
		t.Errorf("expected implements [enter, leave], got %v", id.Implements)
	}
	if len(id.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(id.Methods))
	}
	if id.Methods[0].Name != "close" {
		t.Errorf("expected first method 'close', got %q", id.Methods[0].Name)
	}
	if id.Methods[1].Name != "query" {
		t.Errorf("expected second method 'query', got %q", id.Methods[1].Name)
	}
}

// TestParseInterfaceSingleImplements verifies interface inheritance with
// a single inherited interface.
func TestParseInterfaceSingleImplements(t *testing.T) {
	src := `writer flush {
    write(data str) (n i64)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	id, ok := prog.Statements[0].(*InterfaceDefinition)
	if !ok {
		t.Fatalf("expected *InterfaceDefinition, got %T", prog.Statements[0])
	}
	if id.Name != "writer" {
		t.Errorf("expected interface name 'writer', got %q", id.Name)
	}
	if len(id.Implements) != 1 || id.Implements[0] != "flush" {
		t.Errorf("expected implements [flush], got %v", id.Implements)
	}
	if len(id.Methods) != 1 || id.Methods[0].Name != "write" {
		t.Errorf("expected 1 method 'write', got %v", id.Methods)
	}
}

// TestParseStructWithImplementsStillWorks verifies that struct-with-implements
// syntax (`name iface { fields }`) is still parsed as a struct, not an interface.
func TestParseStructWithImplementsStillWorks(t *testing.T) {
	src := `file enter, leave {
    path str
    fd i64
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	sd, ok := prog.Statements[0].(*StructDefinition)
	if !ok {
		t.Fatalf("expected *StructDefinition, got %T", prog.Statements[0])
	}
	if sd.Name != "file" {
		t.Errorf("expected struct name 'file', got %q", sd.Name)
	}
	if len(sd.Implements) != 2 {
		t.Fatalf("expected 2 implemented interfaces, got %d", len(sd.Implements))
	}
	if sd.Implements[0] != "enter" || sd.Implements[1] != "leave" {
		t.Errorf("expected implements [enter, leave], got %v", sd.Implements)
	}
	if len(sd.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sd.Fields))
	}
}

// TestParseStructWithDottedImplements verifies that struct-with-implements
// syntax supports dotted/qualified interface names from other modules,
// e.g. `stmt-mysql sql.stmt { ... }` or `db-mysql sql.db, sql.rows { ... }`.
func TestParseStructWithDottedImplements(t *testing.T) {
	src := `stmt-mysql sql.stmt {
    fd i64
    sql str
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	sd, ok := prog.Statements[0].(*StructDefinition)
	if !ok {
		t.Fatalf("expected *StructDefinition, got %T", prog.Statements[0])
	}
	if sd.Name != "stmt-mysql" {
		t.Errorf("expected struct name 'stmt-mysql', got %q", sd.Name)
	}
	if len(sd.Implements) != 1 {
		t.Fatalf("expected 1 implemented interface, got %d", len(sd.Implements))
	}
	if sd.Implements[0] != "sql.stmt" {
		t.Errorf("expected implements [sql.stmt], got %v", sd.Implements)
	}
	if len(sd.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(sd.Fields))
	}
}

// TestParseStructWithMultipleDottedImplements verifies that struct-with-implements
// syntax supports multiple dotted/qualified interface names separated by commas,
// e.g. `db-mysql sql.db, sql.rows { ... }`.
func TestParseStructWithMultipleDottedImplements(t *testing.T) {
	src := `db-mysql sql.db, sql.rows {
    fd i64
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	sd, ok := prog.Statements[0].(*StructDefinition)
	if !ok {
		t.Fatalf("expected *StructDefinition, got %T", prog.Statements[0])
	}
	if sd.Name != "db-mysql" {
		t.Errorf("expected struct name 'db-mysql', got %q", sd.Name)
	}
	if len(sd.Implements) != 2 {
		t.Fatalf("expected 2 implemented interfaces, got %d", len(sd.Implements))
	}
	if sd.Implements[0] != "sql.db" || sd.Implements[1] != "sql.rows" {
		t.Errorf("expected implements [sql.db, sql.rows], got %v", sd.Implements)
	}
}

// TestParseStructWithMixedImplements verifies that struct-with-implements
// syntax supports a mix of dotted and bare interface names,
// e.g. `file sql.db, enter, leave { ... }`.
func TestParseStructWithMixedImplements(t *testing.T) {
	src := `file sql.db, enter, leave {
    path str
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	sd, ok := prog.Statements[0].(*StructDefinition)
	if !ok {
		t.Fatalf("expected *StructDefinition, got %T", prog.Statements[0])
	}
	if len(sd.Implements) != 3 {
		t.Fatalf("expected 3 implemented interfaces, got %d", len(sd.Implements))
	}
	expected := []string{"sql.db", "enter", "leave"}
	for i, exp := range expected {
		if sd.Implements[i] != exp {
			t.Errorf("expected implements[%d] = %q, got %q", i, exp, sd.Implements[i])
		}
	}
}

// TestParseInterfaceMethodWithResult verifies that interface methods
// can declare a return type using `(res type)` after the parameter
// list, e.g.:
//
//	ord {
//	    gt(x t) (res bool)
//	}
//
// Before the fix, the parser rejected the trailing `(res type)` with
// "expected method name in interface, got LPAREN", which caused
// `bool` to leak out as a free-standing identifier and get flagged
// as undefined by the validator.
func TestParseInterfaceMethodWithResult(t *testing.T) {
	src := `ord {
    gt(x t) (res bool)
}
`
	l := lexer.New(src)
	p := New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	if len(prog.Statements) != 1 {
		t.Fatalf("expected 1 top-level statement, got %d", len(prog.Statements))
	}
	id, ok := prog.Statements[0].(*InterfaceDefinition)
	if !ok {
		t.Fatalf("expected *InterfaceDefinition, got %T", prog.Statements[0])
	}
	if id.Name != "ord" {
		t.Errorf("expected interface name 'ord', got %q", id.Name)
	}
	if len(id.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(id.Methods))
	}
	m := id.Methods[0]
	if m.Name != "gt" {
		t.Errorf("expected method name 'gt', got %q", m.Name)
	}
	if len(m.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(m.Parameters))
	}
	if m.Parameters[0].Name != "x" || m.Parameters[0].Type.String() != "t" {
		t.Errorf("expected param (x t), got (%s %s)", m.Parameters[0].Name, m.Parameters[0].Type.String())
	}
	if len(m.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(m.Results))
	}
	if m.Results[0].Name != "res" || m.Results[0].Type.String() != "bool" {
		t.Errorf("expected result (res bool), got (%s %s)", m.Results[0].Name, m.Results[0].Type.String())
	}
}
