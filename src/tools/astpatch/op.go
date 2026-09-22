// op.go: 补丁操作（op）的定义、解码与基于反射的 AST 遍历/匹配/改写。
package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// pat 是节点匹配模式：JSON 对象，键为节点导出字段名（大小写不敏感），
// 值为标量（字符串/数字/布尔）或嵌套模式（对象/数组）。特殊键 "kind"
// 指定节点的 Go 类型名（如 "Identifier"、"CallExpression"）。
type pat map[string]json.RawMessage

// op 是一条已解码的补丁操作。
type op struct {
	Kind     string `json:"op"`
	Match    pat    `json:"match"`
	Code     string `json:"code"`
	From     string `json:"from"`
	To       string `json:"to"`
	Scope    string `json:"scope"`    // 限定作用域：仅在某具名函数体内生效
	Position string `json:"position"` // insert_stmt: "top"(默认) | "before" | "after"
	Index    int    `json:"index"`    // insert_stmt: top 位置的插入下标（-1 或省略=追加末尾）
}

func decodeOp(raw json.RawMessage) (*op, error) {
	var o op
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	if o.Kind == "" {
		return nil, fmt.Errorf("缺少 \"op\" 字段")
	}
	if o.Index == 0 && o.Position == "" {
		o.Index = -1 // 默认追加到末尾
	}
	return &o, nil
}

var (
	nodeType = reflect.TypeOf((*parser.Node)(nil)).Elem()
)

// implementsNode 判断某类型（值或指针接收者）是否为 AST 节点。
func implementsNode(t reflect.Type) bool {
	return t.Implements(nodeType) || reflect.PointerTo(t).Implements(nodeType)
}

// runner 承载单次 op 的遍历状态。
type runner struct {
	o     *op
	count int

	newExpr parser.Expression  // replace 用：替换表达式
	newStmt []parser.Statement // replace_stmt / insert 用：替换/插入语句
	// 遍历 Identifier 等节点时的重命名标记
	renaming bool
}

// applyOp 把一条 op 应用到 program，返回受影响节点数。
func applyOp(program *parser.Program, o *op) (int, error) {
	progVal := reflect.ValueOf(program).Elem() // 可寻址的 Program 结构体

	switch o.Kind {
	case "rename":
		r := &runner{o: o, renaming: true}
		if o.Scope != "" {
			fn := findFunction(program, o.Scope)
			if fn == nil || fn.Body == nil {
				return 0, fmt.Errorf("找不到作用域函数 %q", o.Scope)
			}
			r.walkStruct(reflect.ValueOf(fn.Body).Elem())
			return r.count, nil
		}
		r.walkStruct(progVal)
		return r.count, nil

	case "replace":
		if o.Match == nil {
			return 0, fmt.Errorf("replace 需要 \"match\"")
		}
		e, err := parseCodeExpr(o.Code)
		if err != nil {
			return 0, err
		}
		r := &runner{o: o, newExpr: e}
		r.walkStruct(progVal)
		return r.count, nil

	case "replace_stmt":
		if o.Match == nil {
			return 0, fmt.Errorf("replace_stmt 需要 \"match\"")
		}
		st, err := parseCodeStmts(o.Code)
		if err != nil {
			return 0, err
		}
		r := &runner{o: o, newStmt: st}
		r.walkStruct(progVal)
		return r.count, nil

	case "delete_node":
		if o.Match == nil {
			return 0, fmt.Errorf("delete_node 需要 \"match\"")
		}
		r := &runner{o: o}
		r.walkStruct(progVal)
		return r.count, nil

	case "delete_stmt":
		if o.Match == nil {
			return 0, fmt.Errorf("delete_stmt 需要 \"match\"")
		}
		r := &runner{o: o}
		r.walkStruct(progVal)
		return r.count, nil

	case "insert_stmt":
		st, err := parseCodeStmts(o.Code)
		if err != nil {
			return 0, err
		}
		return insertStmt(program, o, st)

	default:
		return 0, fmt.Errorf("未知 op %q", o.Kind)
	}
}

// ---- 遍历 ----

// walkStruct 处理一个可寻址的节点结构体：先做标量重命名，再递归其字段。
func (r *runner) walkStruct(s reflect.Value) {
	if s.Kind() != reflect.Struct {
		return
	}
	if r.renaming {
		r.editScalars(s)
	}
	t := s.Type()
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() {
			continue
		}
		r.walkField(s.Field(i))
	}
	// 遍历 slice/map 之外的特殊节点：无
}

// walkField 处理一个可寻址字段，可能持有子节点。
func (r *runner) walkField(fv reflect.Value) {
	switch fv.Kind() {
	case reflect.Interface:
		if !implementsNode(fv.Type()) || fv.IsNil() {
			return
		}
		cp := fv.Elem() // 具体值（通常为指针）
		r.walkConcrete(cp)
		r.slotSingle(fv, cp)
	case reflect.Slice:
		et := fv.Type().Elem()
		if !implementsNode(et) {
			return
		}
		r.walkSlice(fv, et)
	case reflect.Ptr:
		if !implementsNode(fv.Type()) || fv.IsNil() {
			return
		}
		r.walkConcrete(fv)
	}
}

// walkConcrete 进入一个指向节点结构体的指针值。
func (r *runner) walkConcrete(cv reflect.Value) {
	if cv.Kind() != reflect.Ptr || cv.IsNil() {
		return
	}
	elem := cv.Elem()
	if elem.Kind() != reflect.Struct {
		return
	}
	r.walkStruct(elem)
}

// walkSlice 遍历节点切片，支持替换 / 删除 / （语句）插入，返回是否需要重建。
func (r *runner) walkSlice(fv reflect.Value, et reflect.Type) {
	n := fv.Len()
	if n == 0 && r.o.Kind != "insert_stmt" {
		return
	}
	isStmt := et == reflect.TypeOf((*parser.Statement)(nil)).Elem()
	changed := false
	var kept []reflect.Value
	for i := 0; i < n; i++ {
		ei := fv.Index(i) // 接口元素，可寻址
		if ei.Kind() == reflect.Interface && ei.IsNil() {
			kept = append(kept, reflect.Value{})
			continue
		}
		cp := ei.Elem()
		r.walkConcrete(cp)

		if r.matches(cp) {
			switch r.o.Kind {
			case "delete_node", "delete_stmt":
				r.count++
				changed = true
				kept = append(kept, reflect.Value{}) // 标记删除
				continue
			case "replace":
				if r.newExpr != nil && et == reflect.TypeOf((*parser.Expression)(nil)).Elem() {
					ei.Set(reflect.ValueOf(r.newExpr).Convert(et))
					r.count++
					changed = true
				}
			case "replace_stmt":
				if isStmt && len(r.newStmt) == 1 {
					ei.Set(reflect.ValueOf(r.newStmt[0]).Convert(et))
					r.count++
					changed = true
				}
			}
		}
		kept = append(kept, ei)
	}
	if changed {
		// 重建切片（剔除删除项）
		out := reflect.MakeSlice(fv.Type(), 0, len(kept))
		for _, kv := range kept {
			if !kv.IsValid() {
				continue
			}
			out = reflect.Append(out, kv)
		}
		fv.Set(out)
	}
}

// slotSingle 处理非切片的单子节点槽位（如 InfixExpression.Left），仅支持表达式替换。
func (r *runner) slotSingle(fv, cp reflect.Value) {
	if r.o.Kind != "replace" || r.newExpr == nil {
		return
	}
	if !implementsNode(fv.Type()) {
		return
	}
	if fv.Type() != reflect.TypeOf((*parser.Expression)(nil)).Elem() {
		return
	}
	if r.matches(cp) {
		fv.Set(reflect.ValueOf(r.newExpr).Convert(fv.Type()))
		r.count++
	}
}

// editScalars 在重命名模式下修改节点上的名字字段。
func (r *runner) editScalars(s reflect.Value) {
	if r.o.From == "" {
		return
	}
	setIf := func(fieldName, nodeKind string) {
		if s.Type().Name() != nodeKind {
			return
		}
		f := s.FieldByName(fieldName)
		if f.IsValid() && f.Kind() == reflect.String && f.CanSet() && f.String() == r.o.From {
			f.SetString(r.o.To)
			r.count++
		}
	}
	setIf("Value", "Identifier")
	setIf("Name", "FunctionDefinition")
	setIf("Name", "Parameter")
	setIf("Property", "DotExpression")
}

// ---- 匹配 ----

// matches 判断具体节点值 cp 是否匹配当前 op 的 match 模式。
func (r *runner) matches(cp reflect.Value) bool {
	if r.o.Match == nil {
		return false
	}
	return matchConcrete(cp, r.o.Match)
}

func matchConcrete(cv reflect.Value, p pat) bool {
	if cv.Kind() != reflect.Ptr || cv.IsNil() {
		return false
	}
	elem := cv.Elem()
	if elem.Kind() != reflect.Struct {
		return false
	}
	kindName := elem.Type().Name()
	if pk, ok := p["kind"]; ok {
		var s string
		_ = json.Unmarshal(pk, &s)
		if s != "" && s != kindName {
			return false
		}
	}
	for key, raw := range p {
		if key == "kind" {
			continue
		}
		sf, ok := findField(elem.Type(), key)
		if !ok {
			return false
		}
		if !matchValue(elem.FieldByIndex(sf.Index), raw) {
			return false
		}
	}
	return true
}

// matchValue 把字段值与模式条目比对：对象→嵌套节点模式；数组→切片逐项；标量→按字段类型比较。
func matchValue(fv reflect.Value, raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var sub pat
		if json.Unmarshal(raw, &sub) != nil {
			return false
		}
		cv := derefToNode(fv)
		if !cv.IsValid() {
			return false
		}
		return matchConcrete(cv, sub)
	}
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if fv.Kind() != reflect.Slice {
			return false
		}
		var subs []pat
		if json.Unmarshal(raw, &subs) != nil {
			return false
		}
		if len(subs) != fv.Len() {
			return false
		}
		for i, sp := range subs {
			b, _ := json.Marshal(sp)
			if !matchValue(fv.Index(i), b) {
				return false
			}
		}
		return true
	}
	return jsonScalarEq(raw, fv)
}

// derefToNode 取字段中承载的节点具体值（接口→elem，指针→自身）。
func derefToNode(fv reflect.Value) reflect.Value {
	switch fv.Kind() {
	case reflect.Interface:
		if fv.IsNil() {
			return reflect.Value{}
		}
		return fv.Elem()
	case reflect.Ptr:
		return fv
	}
	return reflect.Value{}
}

func jsonScalarEq(raw json.RawMessage, fv reflect.Value) bool {
	switch fv.Kind() {
	case reflect.String:
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return false
		}
		return fv.String() == s
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var n int64
		if json.Unmarshal(raw, &n) != nil {
			return false
		}
		return fv.Int() == n
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var n uint64
		if json.Unmarshal(raw, &n) != nil {
			return false
		}
		return fv.Uint() == n
	case reflect.Float32, reflect.Float64:
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			return false
		}
		return fv.Float() == f
	case reflect.Bool:
		var b bool
		if json.Unmarshal(raw, &b) != nil {
			return false
		}
		return fv.Bool() == b
	}
	return false
}

// findField 在结构体类型上按字段名查找（大小写不敏感，精确匹配优先）。
func findField(t reflect.Type, name string) (reflect.StructField, bool) {
	if f, ok := t.FieldByName(name); ok {
		return f, true
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if strings.EqualFold(f.Name, name) && f.IsExported() {
			return f, true
		}
	}
	return reflect.StructField{}, false
}

// ---- 定位与插入 ----

func findFunction(program *parser.Program, name string) *parser.FunctionDefinition {
	var found *parser.FunctionDefinition
	var walk func(s parser.Statement) bool
	walk = func(s parser.Statement) bool {
		switch n := s.(type) {
		case *parser.FunctionDefinition:
			if n.Name == name {
				found = n
				return true
			}
			if n.Body != nil {
				for _, b := range n.Body.Statements {
					if walk(b) {
						return true
					}
				}
			}
		case *parser.BlockStatement:
			for _, b := range n.Statements {
				if walk(b) {
					return true
				}
			}
		}
		return false
	}
	for _, s := range program.Statements {
		if walk(s) {
			break
		}
	}
	return found
}

// insertStmt 在顶层或指定函数体内插入语句。
func insertStmt(program *parser.Program, o *op, st []parser.Statement) (int, error) {
	if o.Scope != "" {
		fn := findFunction(program, o.Scope)
		if fn == nil || fn.Body == nil {
			return 0, fmt.Errorf("找不到作用域函数 %q 或其函数体", o.Scope)
		}
		fn.Body.Statements = spliceStmts(fn.Body.Statements, st, o)
		return len(st), nil
	}
	program.Statements = spliceStmts(program.Statements, st, o)
	return len(st), nil
}

// spliceStmts 依据 position/index/anchor 计算插入点。
func spliceStmts(list []parser.Statement, st []parser.Statement, o *op) []parser.Statement {
	pos := o.Position
	if pos == "" {
		pos = "top"
	}
	switch pos {
	case "top":
		idx := o.Index
		if idx < 0 || idx > len(list) {
			idx = len(list)
		}
		out := make([]parser.Statement, 0, len(list)+len(st))
		out = append(out, list[:idx]...)
		out = append(out, st...)
		out = append(out, list[idx:]...)
		return out
	case "before", "after":
		r := &runner{o: o}
		for i, s := range list {
			if r.matches(reflect.ValueOf(s)) {
				at := i
				if pos == "after" {
					at = i + 1
				}
				out := make([]parser.Statement, 0, len(list)+len(st))
				out = append(out, list[:at]...)
				out = append(out, st...)
				out = append(out, list[at:]...)
				return out
			}
		}
		// 未找到锚点：退化为追加
		return append(list, st...)
	default:
		return append(list, st...)
	}
}
