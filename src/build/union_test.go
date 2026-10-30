package build

import (
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestValidateUnionTypes(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64
float = f32 | f64
num = int | float
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	aliases, results := checker.ValidateUnionTypes(prog)
	if len(results) > 0 {
		t.Errorf("unexpected validate results: %v", results)
	}
	if len(aliases) != 3 {
		t.Errorf("expected 3 aliases, got %d", len(aliases))
	}
	if _, ok := aliases["num"]; !ok {
		t.Errorf("expected 'num' alias")
	}
	if _, ok := aliases["int"]; !ok {
		t.Errorf("expected 'int' alias")
	}
}

func TestFlattenUnionChain(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64
float = f32 | f64
num = int | float
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	aliases, _ := checker.ValidateUnionTypes(prog)
	members := checker.FlattenUnion("num", aliases)
	// Should expand num → int|float → i8|i16|i32|i64|f32|f64
	if len(members) != 6 {
		t.Errorf("expected 6 flattened members, got %d: %v", len(members), members)
	}
}

func TestMonomorphizeUnionFunction(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64
num = int
max = (a ..num) (r num) {
    r = a
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	// ValidateUnionTypes is needed to populate VariadicUnion.
	checker.ValidateUnionTypes(prog)
	// Confirm the function got tagged.
	var fd *parser.FunctionDefinition
	for _, s := range prog.Statements {
		if f, ok := s.(*parser.FunctionDefinition); ok {
			if f.Name == "max" {
				fd = f
			}
		}
	}
	if fd == nil {
		t.Fatalf("max not found")
	}
	// `num` is a single-type alias to `int`, so VariadicUnion stays empty;
	// but FlattenUnion will resolve `num` → `int` → {i8, i16, i32, i64}.
	// To force the variadic to monomorphize, we can mark VariadicUnion
	// explicitly here to simulate "num" being a union.
	prog.Sem.SetVariadicUnionOf(fd, "num")
	monomorphizeUnions(prog)
	// After monomorphization: max__num_TEMPLATE, max__i8, max__i16, max__i32, max__i64
	names := make(map[string]bool)
	for _, s := range prog.Statements {
		if fd, ok := s.(*parser.FunctionDefinition); ok {
			names[fd.Name] = true
		}
	}
	if !names["max__num_TEMPLATE"] {
		t.Errorf("expected max__num_TEMPLATE")
	}
	for _, m := range []string{"max__i8", "max__i16", "max__i32", "max__i64"} {
		if !names[m] {
			t.Errorf("expected %s", m)
		}
	}
}

func TestUnionTypeAliasDuplicate(t *testing.T) {
	src := `int = i8
int = i64
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	_, results := checker.ValidateUnionTypes(prog)
	if len(results) != 1 {
		t.Errorf("expected 1 duplicate error, got %d: %v", len(results), results)
	}
}

// TestGenericUnionMonomorphize verifies that a non-variadic function whose
// parameters and results all use the same union alias is monomorphized into
// one concrete function per union member. The result types must be
// specialized to the concrete member (e.g. i8), not the union alias.
func TestGenericUnionMonomorphize(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64
float = f32 | f64
num = int | float
abs = (a num) (r num) {
    r = a
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	// ValidateUnionTypes sets GenericUnion for non-variadic functions
	// whose params/results are all the same union alias.
	checker.ValidateUnionTypes(prog)

	// Confirm abs is tagged.
	var abs *parser.FunctionDefinition
	for _, s := range prog.Statements {
		if f, ok := s.(*parser.FunctionDefinition); ok {
			if f.Name == "abs" {
				abs = f
			}
		}
	}
	if abs == nil {
		t.Fatalf("abs not found")
	}
	if got := prog.Sem.GenericUnionOf(abs); got != "num" {
		t.Errorf("expected GenericUnion=num, got %q", got)
	}

	monomorphizeUnions(prog)
	names := make(map[string]string) // name → result type
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var rt string
		if len(fd.Results) > 0 {
			rt = fd.Results[0].Type.String()
		}
		names[fd.Name] = rt
	}
	// Check concrete monomorphized functions with correct result type
	for _, mem := range []string{"i8", "i16", "i32", "i64", "f32", "f64"} {
		name := "abs__" + mem
		rt, ok := names[name]
		if !ok {
			t.Errorf("missing monomorphized function: %s", name)
			continue
		}
		if rt != mem {
			t.Errorf("abs__%s result type = %q, want %q", mem, rt, mem)
		}
	}
	// Check the template marker exists
	if _, ok := names["abs__num_TEMPLATE"]; !ok {
		t.Errorf("expected abs__num_TEMPLATE")
	}
}

// TestMonomorphizeVariadicMax verifies that max = (a ..num) (r num) { ... }
// expands into one concrete max function per num member (i8, i16, ..., f64).
// It also verifies the template is renamed, parameter slice element type
// becomes the concrete member, and result type is the concrete member.
func TestMonomorphizeVariadicMax(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float
max = (a ..num) (r num) {
    r = a[0]
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	checker.ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	type sig struct {
		paramSliceElem string
		resultType     string
	}
	got := map[string]sig{}
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var sliceElem, rt string
		if fd.IsVariadic && len(fd.Parameters) > 0 {
			if st, ok := fd.Parameters[len(fd.Parameters)-1].Type.(*parser.SliceType); ok {
				if nt, ok := st.Elem.(*parser.NamedType); ok {
					sliceElem = nt.Value
				}
			}
		}
		if len(fd.Results) > 0 {
			if nt, ok := fd.Results[0].Type.(*parser.NamedType); ok {
				rt = nt.Value
			}
		}
		got[fd.Name] = sig{paramSliceElem: sliceElem, resultType: rt}
	}

	want := []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64"}
	for _, m := range want {
		name := "max__" + m
		s, ok := got[name]
		if !ok {
			t.Errorf("missing monomorphized function: %s", name)
			continue
		}
		if s.paramSliceElem != m {
			t.Errorf("%s slice elem = %q, want %q", name, s.paramSliceElem, m)
		}
		if s.resultType != m {
			t.Errorf("%s result = %q, want %q", name, s.resultType, m)
		}
	}
	// Template should be renamed with __TEMPLATE suffix.
	if _, ok := got["max__num_TEMPLATE"]; !ok {
		t.Errorf("expected max__num_TEMPLATE")
	}
}

// TestMonomorphizeGenericSign verifies that sign = (a num) (r num) { ... }
// (non-variadic generic) generates per-member concrete versions with both
// the parameter and result type specialized.
func TestMonomorphizeGenericSign(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64
float = f32 | f64
num = int | float
sign = (a num) (r num) {
    r = 0
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	checker.ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	type sig struct {
		param, result string
	}
	got := map[string]sig{}
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		var p, r string
		if len(fd.Parameters) > 0 {
			if nt, ok := fd.Parameters[0].Type.(*parser.NamedType); ok {
				p = nt.Value
			}
		}
		if len(fd.Results) > 0 {
			if nt, ok := fd.Results[0].Type.(*parser.NamedType); ok {
				r = nt.Value
			}
		}
		got[fd.Name] = sig{param: p, result: r}
	}

	for _, m := range []string{"i8", "i16", "i32", "i64", "f32", "f64"} {
		name := "sign__" + m
		s, ok := got[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		if s.param != m {
			t.Errorf("%s param = %q, want %q", name, s.param, m)
		}
		if s.result != m {
			t.Errorf("%s result = %q, want %q", name, s.result, m)
		}
	}
}

// TestMonomorphizeIntegerOnlyGeneric verifies that `even` / `odd` / `pow`
// functions declared as (a int) are only monomorphized for integer members
// (not floats), since `int` union does not include float types.
func TestMonomorphizeIntegerOnlyGeneric(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float
even = (a int) (yes int) {
    yes = 0
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	checker.ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	got := map[string]bool{}
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok {
			continue
		}
		got[fd.Name] = true
	}
	// even should be expanded for all 8 integer members, NOT for floats
	for _, m := range []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64"} {
		name := "even__" + m
		if !got[name] {
			t.Errorf("expected %s", name)
		}
	}
	for _, m := range []string{"f32", "f64"} {
		name := "even__" + m
		if got[name] {
			t.Errorf("unexpected %s (even should not be monomorphized for floats)", name)
		}
	}
	if !got["even__int_TEMPLATE"] {
		t.Errorf("expected even__int_TEMPLATE")
	}
}

// TestMonomorphizeRecursiveCallInBody verifies that when a generic function
// recursively calls itself, the monomorphized body's call is renamed from
// the original generic name to the concrete monomorphized name.
func TestMonomorphizeRecursiveCallInBody(t *testing.T) {
	src := `int = i8 | i16
float = f32 | f64
num = int | float
gcd = (a num, b num) (r num) {
    if b == 0 {
        r = a
    } else {
        gcd(b, a % b, r)
    }
}
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	checker.ValidateUnionTypes(prog)
	monomorphizeUnions(prog)

	// Find the monomorphized gcd__i8 and check its body references
	// "gcd__i8" instead of "gcd".
	for _, s := range prog.Statements {
		fd, ok := s.(*parser.FunctionDefinition)
		if !ok || fd.Name != "gcd__i8" {
			continue
		}
		// Walk the body and find call expressions
		hasOldCall, hasNewCall := false, false
		var walk func(parser.Expression)
		walk = func(e parser.Expression) {
			if e == nil {
				return
			}
			switch x := e.(type) {
			case *parser.CallExpression:
				if id, ok := x.Function.(*parser.Identifier); ok {
					switch id.Value {
					case "gcd":
						hasOldCall = true
					case "gcd__i8":
						hasNewCall = true
					}
				}
			}
		}
		_ = walk
		// Simple: look at the body's last statement
		if fd.Body == nil || len(fd.Body.Statements) == 0 {
			t.Fatalf("gcd__i8 has empty body")
		}
		// Find the recursive call by looking at all expressions in body
		var scan func(parser.Expression)
		var scanStmt func(parser.Statement)
		scan = func(e parser.Expression) {
			if e == nil {
				return
			}
			switch x := e.(type) {
			case *parser.CallExpression:
				if id, ok := x.Function.(*parser.Identifier); ok {
					switch id.Value {
					case "gcd":
						hasOldCall = true
					case "gcd__i8":
						hasNewCall = true
					}
				}
			case *parser.IfExpression:
				scan(x.Condition)
				if x.Consequence != nil {
					for _, ss := range x.Consequence.Statements {
						scanStmt(ss)
					}
				}
				if x.Alternative != nil {
					for _, ss := range x.Alternative.Statements {
						scanStmt(ss)
					}
				}
			}
		}
		scanStmt = func(st parser.Statement) {
			if st == nil {
				return
			}
			if es, ok := st.(*parser.ExpressionStatement); ok {
				scan(es.Expression)
			}
		}
		for _, ss := range fd.Body.Statements {
			scanStmt(ss)
		}
		if hasOldCall {
			t.Errorf("gcd__i8 still has a call to 'gcd' (the generic name)")
		}
		if !hasNewCall {
			t.Errorf("gcd__i8 does not have a recursive call to 'gcd__i8'")
		}
		return
	}
	t.Errorf("gcd__i8 not found")
}

// TestFlattenUnionAllMembers enumerates all members produced by flattening
// `num = int | float` with the full int/float declarations used in number.no.
// This guards against accidentally dropping or duplicating members during
// recursive flattening.
func TestFlattenUnionAllMembers(t *testing.T) {
	src := `int = i8 | i16 | i32 | i64 | u8 | u16 | u32 | u64
float = f32 | f64
num = int | float
`
	l := lexer.New(src)
	p := parser.New(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	aliases, _ := checker.ValidateUnionTypes(prog)
	members := checker.FlattenUnion("num", aliases)
	if len(members) != 10 {
		t.Fatalf("expected 10 members for num, got %d: %v", len(members), members)
	}
	want := []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64", "f32", "f64"}
	got := make([]string, len(members))
	for i, m := range members {
		if nt, ok := m.(*parser.NamedType); ok {
			got[i] = nt.Value
		}
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("members[%d] = %q, want %q (full list: %v)", i, got[i], w, got)
		}
	}
}

// TestInferArgMemberTypeInfixArg 钉住 D2 崩溃的判据：union 泛型實參是**算術表達式**
// （`pick((k - 1))`、std 實見 `n = m + (c - 1)`）時，必須按運算元把成員型別推出來。
// 舊碼把整顆 InfixExpression 当成「無法推定」，於是「union 內恰有一個聚合體成員」
// 的回退邏輯選中聚合體（`_add__bigint`），而呼叫點槽位仍是 i64——LLVM 17+ 的 opaque
// pointer 不再在編譯期拒絕 ABI 不符的指標實參，錯誤一路活到運行期，表現為
// `malloc: *** error for object …: pointer being freed was not allocated` → abort。
//
// 直接測決策函式（而不是走 ParseProgram + rewriteUnionCalls）：後者會受同 package
// 內先前測試殘留的全域 parser 狀態影響，把 `m = pick(...)` 提前降糖成 unwrap 塊，
// 使斷言隨測試執行順序翻轉（實測單跑綠、全量跑空），無法作為回歸閘門。
func TestInferArgMemberTypeInfixArg(t *testing.T) {
	id := func(s string) *parser.Identifier { return &parser.Identifier{Value: s} }
	argCall := func(arg parser.Expression) *parser.CallExpression {
		return &parser.CallExpression{Function: id("pick"), Arguments: []parser.Expression{arg}}
	}
	// big = i64 | agg9：恰有一個聚合體成員，故 fallbackMember = agg9。
	tpl := &unionTemplateInfo{
		origName:       "pick",
		unionName:      "u9",
		members:        []string{"i64", "agg9"},
		unionArgIdx:    0,
		fallbackMember: "agg9",
	}
	vt := map[string]string{"k": "i64", "a": "agg9"}
	sub := &parser.GroupedExpression{Expression: &parser.InfixExpression{
		Left:     id("k"),
		Operator: "-",
		Right:    &parser.IntegerLiteral{Value: 1},
	}}
	if got := inferArgMemberType(argCall(sub), tpl, vt); got != "i64" {
		t.Errorf("(k - 1) 推定為 %q，want \"i64\"——算術實參被回退成聚合體成員", got)
	}

	// 反空轉對照組 1：推不定時**仍須**回退 agg9（證明 fallbackMember 生效、
	// 也證明上條斷言不是「回退剛好也是 i64」的假綠）。
	unknown := &parser.GroupedExpression{Expression: id("no-such-var")}
	if got := inferArgMemberType(argCall(unknown), tpl, vt); got != "agg9" {
		t.Errorf("推不定的實參推定為 %q，want \"agg9\"（回退路徑被改壞）", got)
	}

	// 反空轉對照組 2：`1 + a`——左側整數字面量只表數量級，變數才表意圖，
	// 必須取右側的 agg9，不能被左側字面量定成 i64。
	litLeft := &parser.InfixExpression{
		Left:     &parser.IntegerLiteral{Value: 1},
		Operator: "+",
		Right:    id("a"),
	}
	if got := inferArgMemberType(argCall(litLeft), tpl, vt); got != "agg9" {
		t.Errorf("(1 + a) 推定為 %q，want \"agg9\"（字面量蓋掉了變數意圖）", got)
	}

	// 反空轉對照組 3：`a + 1`（a 推不定）整體仍視為未知，保留舊的回退行為。
	leftUnknown := &parser.InfixExpression{
		Left:     id("no-such-var"),
		Operator: "+",
		Right:    &parser.IntegerLiteral{Value: 1},
	}
	if got := inferArgMemberType(argCall(leftUnknown), tpl, vt); got != "agg9" {
		t.Errorf("(no-such-var + 1) 推定為 %q，want \"agg9\"（未知運算元被武斷定成 i64）", got)
	}
}

// TestRewriteUnionCallsDescendsIntoSurfaceMatch 是回歸釘，不是缺陷再現：
// match 在本 pass 之後才降糖（DesugarSurfaceMatches / H0 都晚於 rewriteUnionCalls），
// 所以「Matched、臂 Condition、臂 MultiValuePatterns、臂體」這四個位置都必須被下鑽，
// 否則呼叫點保留未實例化的模板名，MIR 直接 `unknown callee`。這四個位置現已正確走訪
// （rewriteUnionCallExpr 的 *parser.SurfaceMatch 分支），本測試把它釘住，防止將來
// 有人把 match 走訪退回成「只認降糖後的 IfExpression」。
func TestRewriteUnionCallsDescendsIntoSurfaceMatch(t *testing.T) {
	id := func(s string) *parser.Identifier { return &parser.Identifier{Value: s} }
	newCall := func() *parser.CallExpression {
		return &parser.CallExpression{Function: id("pick9"), Arguments: []parser.Expression{id("k")}}
	}
	// 三條位置各一顆呼叫。
	inMatched, inCond, inArmBody := newCall(), newCall(), newCall()
	// 對照組：函數體層級（舊码已支援），若連它都失敗代表走訪整體壞了。
	inBody := newCall()
	armBody := &parser.BlockStatement{Statements: []parser.Statement{
		&parser.LetStatement{Name: id("m"), Value: inArmBody},
	}}
	sm := &parser.SurfaceMatch{
		Matched: inMatched,
		Arms: []parser.MatchArm{{
			Condition:          &parser.InfixExpression{Left: id("k"), Operator: ">", Right: &parser.IntegerLiteral{Value: 0}},
			MultiValuePatterns: []parser.Expression{inCond},
			Body:               armBody,
		}},
	}
	fn := &parser.FunctionDefinition{
		Name: "g9",
		Body: &parser.BlockStatement{Statements: []parser.Statement{
			&parser.LetStatement{Name: id("z"), Value: inBody},
			&parser.ExpressionStatement{Expression: sm},
		}},
	}
	prog := &parser.Program{Statements: []parser.Statement{fn}}
	templates := map[string]*unionTemplateInfo{
		"pick9": {origName: "pick9", unionName: "u9", members: []string{"i64", "agg9"}, unionArgIdx: 0, fallbackMember: "agg9"},
	}
	vt := map[string]string{"k": "i64"}
	rewriteUnionCallStmts(prog.Statements, templates, vt, nil)

	callee := func(c *parser.CallExpression) string {
		if i, ok := c.Function.(*parser.Identifier); ok {
			return i.Value
		}
		return "<non-identifier>"
	}
	// 對照組先綠：體層級的呼叫一定要被改寫。
	if got := callee(inBody); got != "pick9__i64" {
		t.Fatalf("control: 函數體層級的呼叫 = %q, want \"pick9__i64\"——走訪整體失效", got)
	}
	for _, tc := range []struct {
		where string
		c     *parser.CallExpression
	}{
		{"臂體", inArmBody},
		{"Matched", inMatched},
		{"臂 MultiValuePatterns", inCond},
	} {
		if got := callee(tc.c); got != "pick9__i64" {
			t.Errorf("%s 內的呼叫 = %q, want \"pick9__i64\"——SurfaceMatch 未被下鑽，模板名留成 dangling", tc.where, got)
		}
	}
}

// TestRewriteUnionCallsArmSeesArmLocalVarTypes 是回歸釘（非缺陷再現）：進入臂體時
// 實參推定必須**看得見臂內宣告**的型別（`d i64 = …` 後 `recv._add(d)`），否則推定
// 退回「未知」→ fallbackMember 選了聚合體成員（`_add__agg9`），呼叫點卻仍傳 i64 槽；
// opaque pointer 让 LLVM 不在編譯期拒絕，运行期静默算出 0（D3 的症狀形態）。
//
// 注意：本測試目前為綠——函數體層級的 collectVarTypesFromBody 會連臂體一起收集，
// 故臂內宣告對推定可見。保留它是為了在有人把「按臂作用域」改成「共用外層表」時
// 立刻暴露（那會讓別臂的同名綁定互相污染）。D3 的實際成因尚未定案，見下。
func TestRewriteUnionCallsArmSeesArmLocalVarTypes(t *testing.T) {
	id := func(s string) *parser.Identifier { return &parser.Identifier{Value: s} }
	// `recv._add(d)`——方法式呼叫已由 resolveMethodCall 把接收者前置成 arguments[0]。
	newAddCall := func() *parser.CallExpression {
		return &parser.CallExpression{
			Function: &parser.DotExpression{Receiver: id("recv"), Property: "_add"},
			Arguments: []parser.Expression{
				id("recv"),
				id("d"),
			},
		}
	}
	// 臂內：`d i64 = 4` 後 `mm = recv._add(d)`。
	armAdd, armD := newAddCall(), &parser.LetStatement{
		Name:  id("d"),
		Type:  &parser.NamedType{Value: "i64"},
		Value: &parser.IntegerLiteral{Value: 4},
	}
	armBody := &parser.BlockStatement{Statements: []parser.Statement{
		armD,
		&parser.LetStatement{Name: id("mm"), Value: armAdd},
	}}
	// 對照組：同样的两条语句放在函数体层级（外層已收集體內型別，理應正確）。
	// 名稱刻意與臂內不同（da 而非 d）——同名會讓體層級的收集順帶把臂內那條也補上，
	// 斷言就空轉了（這正是本測試第一版漏掉 D3 的原因）。
	bodyAdd := newAddCall()
	bodyAdd.Arguments[1] = id("da")
	bodyStmts := []parser.Statement{
		&parser.LetStatement{Name: id("da"), Type: &parser.NamedType{Value: "i64"}, Value: &parser.IntegerLiteral{Value: 4}},
		&parser.LetStatement{Name: id("mmb"), Value: bodyAdd},
	}
	fn := &parser.FunctionDefinition{
		Name: "g9",
		Body: &parser.BlockStatement{Statements: append([]parser.Statement{
			&parser.ExpressionStatement{Expression: &parser.SurfaceMatch{
				Matched: id("b"),
				Arms:    []parser.MatchArm{{Body: armBody, IsBlockBody: true}},
			}},
		}, bodyStmts...)},
	}
	prog := &parser.Program{Statements: []parser.Statement{fn}}
	tpl := &unionTemplateInfo{
		origName: "agg9._add", unionName: "big9",
		members: []string{"agg9", "i64"}, unionArgIdx: 1, fallbackMember: "agg9",
	}
	templates := map[string]*unionTemplateInfo{"agg9._add": tpl}
	// 外層 varTypes 刻意不含 d：只有「按臂收集」才看得见它。
	rewriteUnionCallStmts(prog.Statements, templates, map[string]string{"recv": "agg9", "b": "i64"}, nil)

	prop := func(c *parser.CallExpression) string {
		if dot, ok := c.Function.(*parser.DotExpression); ok {
			return dot.Property
		}
		return "<non-dot>"
	}
	// 對照組先綠：體層級推得 i64 成員。
	if got := prop(bodyAdd); got != "_add__i64" {
		t.Fatalf("control: 函數體層級的屬性 = %q, want \"_add__i64\"——推定整體失效", got)
	}
	if got := prop(armAdd); got != "_add__i64" {
		t.Errorf("臂內的屬性 = %q, want \"_add__i64\"——臂內宣告的 d:i64 對推定不可見，"+
			"回退成聚合體成員（呼叫點仍傳 i64 槽 → 运行期静默 0）", got)
	}
}
