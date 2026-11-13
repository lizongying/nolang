package checker

// 命名規範的「型別名」这一半：型別宣告名 + 源碼顯式寫出的型別標註。
//
// ValidateNaming 原本只檢查宣告名（頂層/區域 LetStatement、MultiAssign
// 目標、FunctionDefinition 名），完全不下鑽運算式或 Type 子樹，所以
// `MyData { ... }`、`x MyData = nil`、`c = net.Conn { ... }` 這類大寫型別名
// 全部逃過 2dhoris2（實例：compiler/tests/fixtures/t21-struct-dotted.no）。
//
// 這個檔案補三處，全部沿用既有 TraceID 2dhoris2：
//   ① StructLiteral.Type（上一輪已補，點分型別名逐段檢查）；
//   ② StructDefinition.Name（型別宣告名）；
//   ③ 所有顯式型別標註裡的 NamedType（let / 參數 / 回傳 / 欄位 / cast /
//      型別別名 RHS / 標籤列舉載荷），含 []T、[N]T、[K]V、?T、*T、&T、
//      A|B、fn 型別的遞迴下鑽。
//
// 兩條刻意為之的邊界：
//   - 不套 isAllCapsConst：那是「模組級常數」豁免（i64.MAX、
//     base64.BASE64-STD），型別名不是常數，套上去會讓 `a.b.C` /
//     `pkg.Sub.K` 的單字母大寫段溜走。
//   - 跳過 IsInferred / IsSynthetic：推斷型別與降糖合成節點不是使用者
//     寫下的名字，報了會變成滿江紅的假陽性。
//   - 型別宣告名的其他種類（TypeAlias / Enum / TaggedEnum / Interface
//     的 Name）與 GenericParams 不在本輪範圍；它們被「引用」為型別標註時
//     仍會被 ③ 攔下。

import (
	"reflect"
	"strings"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// isNilASTNode 攔住 typed-nil：AST 欄位（例如 SliceExpression.Range.Start、
// ForStatement.Init、Parameter.Type）常是「介面非 nil、底層指標為 nil」，
// `n == nil` 擋不到，直接 switch 取欄位會 SIGSEGV（TestLintSplitEquivalence
// 實測踩過）。
func isNilASTNode(n any) bool {
	if n == nil {
		return true
	}
	v := reflect.ValueOf(n)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// validateTypeNameNaming 走訪全程式，檢查 struct 字面量型別名、struct
// 定義名、以及所有顯式型別標註。點分名的每一段都必須是「小寫字母 +
// 數字 + 中劃線」，違規段各出一條 2dhoris2 診斷。
func validateTypeNameNaming(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}

	var results []ValidateResult
	var walkStmt func(s parser.Statement, file string)
	var walkExpr func(e parser.Expression, file string)
	var checkType func(t parser.Type, file string)

	fileOf := func(n parser.Node, fallback string) string {
		if isNilASTNode(n) {
			return fallback
		}
		if program.Sem != nil {
			if f := program.Sem.SourceFileOf(n); f != "" {
				return f
			}
		}
		return fallback
	}

	// checkName 是型別名的唯一出口：單段名直接查，點分名逐段查
	// （net.Conn 的 Conn、pkg.Sub.K 的 Sub 與 K 各自出診斷）。
	checkName := func(name string, tok lexer.Token, file string) {
		if name == "" {
			return
		}
		for _, seg := range strings.Split(name, ".") {
			if seg == "" || seg == "_" || strings.HasPrefix(seg, "__") {
				continue
			}
			if isValidVarName(seg) {
				continue
			}
			results = append(results, ValidateResult{
				TraceID: "2dhoris2",
				File:    file,
				Line:    tok.Line,
				Column:  tok.Column,
				Message: namingMessage(seg),
			})
		}
	}

	checkParams := func(params []*parser.Parameter, file string) {
		for _, p := range params {
			if isNilASTNode(p) {
				continue
			}
			checkType(p.Type, file)
		}
	}

	// checkType 只認使用者寫下的型別；複合型別的 IsInferred 為 true 時
	// 整棵子樹跳過（內層 NamedType 未必帶標記，不能只靠葉節點判斷）。
	checkType = func(t parser.Type, file string) {
		if isNilASTNode(t) {
			return
		}
		switch x := t.(type) {
		case *parser.NamedType:
			if !x.IsInferred {
				checkName(x.Value, x.Token, file)
			}
		case *parser.ArrayType:
			if !x.IsInferred {
				checkType(x.Elem, file)
			}
		case *parser.SliceType:
			if !x.IsInferred {
				checkType(x.Elem, file)
			}
		case *parser.MapType:
			if !x.IsInferred {
				checkType(x.Key, file)
				checkType(x.Value, file)
			}
		case *parser.NullableType:
			if !x.IsInferred {
				checkType(x.Type, file)
			}
		case *parser.PointerType:
			checkType(x.Type, file)
		case *parser.ViewType:
			if !x.IsInferred {
				checkType(x.Type, file)
			}
		case *parser.UnionType:
			for _, member := range x.Types {
				checkType(member, file)
			}
		case *parser.FunctionType:
			checkParams(x.Params, file)
			checkParams(x.Results, file)
		}
	}

	walkExpr = func(e parser.Expression, file string) {
		if isNilASTNode(e) {
			return
		}
		// 合併模式下節點可能帶自己的來源檔標記，優先採用。
		file = fileOf(e, file)
		switch x := e.(type) {
		case *parser.AssignExpression:
			walkExpr(x.Left, file)
			walkExpr(x.Value, file)
		case *parser.FunctionLiteral:
			checkParams(x.Parameters, file)
			checkParams(x.Results, file)
			if x.Body != nil {
				for _, bs := range x.Body.Statements {
					walkStmt(bs, file)
				}
			}
		case *parser.DotExpression:
			walkExpr(x.Receiver, file)
		case *parser.PrefixExpression:
			walkExpr(x.Right, file)
		case *parser.InfixExpression:
			walkExpr(x.Left, file)
			walkExpr(x.Right, file)
		case *parser.ConditionalExpression:
			walkExpr(x.Condition, file)
			walkExpr(x.Consequence, file)
			walkExpr(x.Alternative, file)
		case *parser.GroupedExpression:
			walkExpr(x.Expression, file)
		case *parser.IfExpression:
			walkExpr(x.Condition, file)
			if x.Consequence != nil {
				for _, bs := range x.Consequence.Statements {
					walkStmt(bs, file)
				}
			}
			if x.Alternative != nil {
				for _, bs := range x.Alternative.Statements {
					walkStmt(bs, file)
				}
			}
		case *parser.IndexExpression:
			walkExpr(x.Left, file)
			walkExpr(x.Index, file)
		case *parser.SliceExpression:
			walkExpr(x.Left, file)
			if x.Range != nil {
				walkExpr(x.Range.Start, file)
				walkExpr(x.Range.End, file)
			}
		case *parser.RangeExpression:
			walkExpr(x.Start, file)
			walkExpr(x.End, file)
		case *parser.StructLiteral:
			checkName(x.Type, x.Token, file)
			for _, f := range x.Fields {
				if isNilASTNode(f) {
					continue
				}
				checkType(f.Type, file)
				walkExpr(f.Value, file)
			}
		case *parser.CastExpression:
			walkExpr(x.Expr, file)
			checkType(x.Type, file)
		case *parser.CallExpression:
			walkExpr(x.Function, file)
			for _, a := range x.Arguments {
				walkExpr(a, file)
			}
		case *parser.RunExpression:
			walkExpr(x.Call, file)
		case *parser.AwaitExpression:
			walkExpr(x.Right, file)
		case *parser.MapLiteral:
			for _, pair := range x.Pairs {
				if isNilASTNode(pair) {
					continue
				}
				walkExpr(pair.Key, file)
				walkExpr(pair.Value, file)
			}
		case *parser.ArrayLiteral:
			for _, elem := range x.Elements {
				walkExpr(elem, file)
			}
		case *parser.SliceLiteral:
			for _, elem := range x.Elements {
				walkExpr(elem, file)
			}
		case *parser.SurfaceMatch:
			// 糖形態 match 的臂條件/臂體只掛在 SurfaceMatch 上，漏了
			// 這個 case 會整棵子樹看不見（見 sugar_walk.go）。
			walkSurfaceMatchChildren(x, nil,
				func(child parser.Expression) { walkExpr(child, file) },
				func(child parser.Statement) { walkStmt(child, file) })
		}
	}

	walkStmt = func(s parser.Statement, file string) {
		if isNilASTNode(s) {
			return
		}
		file = fileOf(s, file)
		switch x := s.(type) {
		case *parser.FunctionDefinition:
			checkParams(x.Parameters, file)
			checkParams(x.Results, file)
			if x.Body != nil {
				for _, bs := range x.Body.Statements {
					walkStmt(bs, file)
				}
			}
		case *parser.LetStatement:
			// IsSynthetic 是降糖注入的 let（`it = matched` 等），其 Type
			// 不是使用者寫的，跳過以免對合成名報警。
			if !x.IsSynthetic {
				checkType(x.Type, file)
			}
			walkExpr(x.Value, file)
		case *parser.MultiAssignStatement:
			walkExpr(x.Value, file)
			for _, tgt := range x.Targets {
				walkExpr(tgt, file)
			}
		case *parser.StructDefinition:
			checkName(x.Name, x.Token, file)
			for _, f := range x.Fields {
				if isNilASTNode(f) {
					continue
				}
				checkType(f.Type, file)
			}
		case *parser.InterfaceDefinition:
			// 定義名本身不在本輪範圍，但方法簽章的型別標註要查。
			for _, m := range x.Methods {
				if isNilASTNode(m) {
					continue
				}
				checkParams(m.Parameters, file)
				checkParams(m.Results, file)
			}
		case *parser.TypeAlias:
			checkType(x.Type, file)
			if x.Union != nil {
				for _, member := range x.Union.Types {
					checkType(member, file)
				}
			}
		case *parser.TaggedEnumDefinition:
			for _, v := range x.Variants {
				if isNilASTNode(v) {
					continue
				}
				checkType(v.Type, file)
				for _, f := range v.Fields {
					if isNilASTNode(f) {
						continue
					}
					checkType(f.Type, file)
				}
			}
		case *parser.ExternStatement:
			checkParams(x.Parameters, file)
			checkParams(x.Results, file)
		case *parser.ExpressionStatement:
			walkExpr(x.Expression, file)
		case *parser.ForStatement:
			walkStmt(x.Init, file)
			walkExpr(x.Condition, file)
			walkStmt(x.Update, file)
			if x.IterRange != nil {
				walkExpr(x.IterRange.Range, file)
				walkExpr(x.IterRange.RangeExpr, file)
			}
			walkExpr(x.CountExpr, file)
			if x.Body != nil {
				for _, bs := range x.Body.Statements {
					walkStmt(bs, file)
				}
			}
		case *parser.BlockStatement:
			for _, bs := range x.Statements {
				walkStmt(bs, file)
			}
		case *parser.BlockMatchStatement:
			for _, bs := range blockMatchChildStatements(x) {
				walkStmt(bs, file)
			}
		case *parser.ReturnStatement:
			walkExpr(x.ReturnValue, file)
		}
	}

	for _, stmt := range program.Statements {
		walkStmt(stmt, fileOf(stmt, ""))
	}
	return results
}
