package checker

// literal_convert.go — 对「数字字面量直接做类型转换」的提示（HINT）。
//
// Nolang 里 `(123).to-str()`、`(3.14).to-f32()` 这类写法把一个已经能直接书写
// 的字面量，绕一圈方法调用转成目标类型。既然源文本就是常量，直接写目标类型
// 的字面量更直观：
//
//	(123).to-str()   →  '123'          （str 字面量）
//	(3.14).to-f32()  →  v f32 = 3.14   （带类型标注的字面量声明）
//
// 只对「值不变」的转换发言：
//   - 任意数字字面量 → str（.to-str()）：只是把数字排成字符串，值等价。
//   - 整数字面量 → 整数/浮点类型：只要落在目标类型范围内即可直接声明。
//   - 浮点字面量 → 浮点类型（f32/f64）：可直接声明。
//   - 浮点字面量 → 整数类型：.to-i64() 会截断，字面量无法等价表达 → 不提示。
//
// 严重级别 HINT：不阻断编译，只建议改用更直接的写法。

import (
	"fmt"
	"math"
	"strconv"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
)

// literalConvTraceID 是数字字面量冗余转换提示的 TraceID。
const literalConvTraceID = "2mak36nl"

// literalConvTargets 把 `to-<type>` 方法名映射到目标类型名。
// 布尔/字符/字节等非数字目标不在表内（`(1).to-bool()` 语义并非字面量直写）。
var literalConvTargets = map[string]string{
	"to-str":  "str",
	"to-i8":   "i8",
	"to-i16":  "i16",
	"to-i32":  "i32",
	"to-i64":  "i64",
	"to-i128": "i128",
	"to-u8":   "u8",
	"to-u16":  "u16",
	"to-u32":  "u32",
	"to-u64":  "u64",
	"to-u128": "u128",
	"to-f32":  "f32",
	"to-f64":  "f64",
}

// ValidateLiteralConversion 报告 program 中所有「对数字字面量直接做 .to-<type>()
// 转换、可改写成目标类型字面量」的调用点。
func ValidateLiteralConversion(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	for _, stmt := range program.Statements {
		results = append(results, literalConvInStmt(program.Sem, stmt, program.Sem.SourceFileOf(stmt))...)
	}
	return results
}

// literalConvInStmt 递归走访语句树。file 为外层顶层语句的来源档（合并模式只有
// 顶层语句被 SetSourceFile 标记），供诊断归属使用。
func literalConvInStmt(sem *parser.SemanticContext, stmt parser.Statement, file string) []ValidateResult {
	var results []ValidateResult
	if stmt == nil {
		return nil
	}
	if f := sem.SourceFileOf(stmt); f != "" {
		file = f
	}
	switch s := stmt.(type) {
	case *parser.ExpressionStatement:
		results = append(results, literalConvInExpr(s.Expression, file)...)
	case *parser.LetStatement:
		results = append(results, literalConvInExpr(s.Value, file)...)
	case *parser.ReturnStatement:
		results = append(results, literalConvInExpr(s.ReturnValue, file)...)
	case *parser.MultiAssignStatement:
		results = append(results, literalConvInExpr(s.Value, file)...)
	case *parser.UnwrapAssignStatement:
		results = append(results, literalConvInExpr(s.Value, file)...)
	case *parser.FunctionDefinition:
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				results = append(results, literalConvInStmt(sem, b, file)...)
			}
		}
	case *parser.BlockStatement:
		for _, b := range s.Statements {
			results = append(results, literalConvInStmt(sem, b, file)...)
		}
	case *parser.ForStatement:
		results = append(results, literalConvInStmt(sem, s.Init, file)...)
		results = append(results, literalConvInExpr(s.Condition, file)...)
		results = append(results, literalConvInStmt(sem, s.Update, file)...)
		if s.Body != nil {
			for _, b := range s.Body.Statements {
				results = append(results, literalConvInStmt(sem, b, file)...)
			}
		}
	}
	return results
}

// literalConvInExpr 递归走访表达式树，命中 `(数字字面量).to-<type>()` 调用即产出一条 HINT。
func literalConvInExpr(expr parser.Expression, file string) []ValidateResult {
	var results []ValidateResult
	switch e := expr.(type) {
	case *parser.CallExpression:
		if len(e.Arguments) == 0 {
			if dot, ok := e.Function.(*parser.DotExpression); ok {
				if r := checkLiteralConvCall(dot, file); r != nil {
					results = append(results, *r)
				}
			}
		}
		results = append(results, literalConvInExpr(e.Function, file)...)
		for _, a := range e.Arguments {
			results = append(results, literalConvInExpr(a, file)...)
		}
	case *parser.GroupedExpression:
		results = append(results, literalConvInExpr(e.Expression, file)...)
	case *parser.DotExpression:
		results = append(results, literalConvInExpr(e.Receiver, file)...)
	case *parser.InfixExpression:
		results = append(results, literalConvInExpr(e.Left, file)...)
		results = append(results, literalConvInExpr(e.Right, file)...)
	case *parser.PrefixExpression:
		results = append(results, literalConvInExpr(e.Right, file)...)
	case *parser.IndexExpression:
		results = append(results, literalConvInExpr(e.Left, file)...)
		results = append(results, literalConvInExpr(e.Index, file)...)
	case *parser.AssignExpression:
		results = append(results, literalConvInExpr(e.Left, file)...)
		results = append(results, literalConvInExpr(e.Value, file)...)
	case *parser.ArrayLiteral:
		for _, el := range e.Elements {
			results = append(results, literalConvInExpr(el, file)...)
		}
	case *parser.SliceLiteral:
		for _, el := range e.Elements {
			results = append(results, literalConvInExpr(el, file)...)
		}
	}
	return results
}

// checkLiteralConvCall 判断一个无参方法调用 dot 是否为「数字字面量直接转换」，
// 是则返回一条 HINT，否则返回 nil。
func checkLiteralConvCall(dot *parser.DotExpression, file string) *ValidateResult {
	target, ok := literalConvTargets[dot.Property]
	if !ok {
		return nil
	}
	// 解开可选的括号分组：`(123)` → IntegerLiteral。
	recv := dot.Receiver
	if grp, isGrouped := recv.(*parser.GroupedExpression); isGrouped {
		recv = grp.Expression
	}

	var suggestion string
	switch lit := recv.(type) {
	case *parser.IntegerLiteral:
		val := lit.Value
		switch target {
		case "str":
			suggestion = fmt.Sprintf("'%s'", strconv.FormatInt(val, 10))
		default:
			if !intFitsInType(target, val) {
				return nil // 越界：目标类型无法直接承载，不提示
			}
			suggestion = fmt.Sprintf("v %s = %s", target, strconv.FormatInt(val, 10))
		}
	case *parser.FloatLiteral:
		// 浮点 → 整数会截断，字面量无法等价表达 → 不提示。
		if target != "str" && target != "f32" && target != "f64" {
			return nil
		}
		raw := literalRaw(lit.Token.Literal, lit.Raw)
		if target == "str" {
			suggestion = fmt.Sprintf("'%s'", formatFloatLiteral(lit.Value))
		} else {
			suggestion = fmt.Sprintf("v %s = %s", target, raw)
		}
	default:
		return nil
	}

	pos := dot.Receiver.Pos()
	end := dot.Receiver.EndPos()
	message := i18n.T("xtblgfzq", "calling %s on a numeric literal is redundant; write the target type directly (e.g. %s)", dot.Property, suggestion)
	col := pos.Column
	endCol := end.Column
	if endCol <= col {
		endCol = col + 1
	}
	return &ValidateResult{
		Line:      pos.Line,
		Column:    col,
		EndColumn: endCol,
		File:      file,
		Message:   message,
		TraceID:   literalConvTraceID,
	}
}

// literalRaw 返回字面量的源文本（优先 Token.Literal，回退 Raw）。
func literalRaw(tokenLiteral, raw string) string {
	if tokenLiteral != "" {
		return tokenLiteral
	}
	return raw
}

// formatFloatLiteral 复刻 f64.to-str() 的通用（自动去尾零）格式：常规量级用
// shortest round-trip（Go 'g'），与 std 期望一致（3.14→"3.14"、0.0→"0"）。
func formatFloatLiteral(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// intFitsInType 判断整数值能否被目标整数类型直接承载（用于是否值得提示）。
func intFitsInType(target string, v int64) bool {
	switch target {
	case "i8":
		return v >= math.MinInt8 && v <= math.MaxInt8
	case "i16":
		return v >= math.MinInt16 && v <= math.MaxInt16
	case "i32":
		return v >= math.MinInt32 && v <= math.MaxInt32
	case "i64", "i128":
		return true
	case "u8":
		return v >= 0 && v <= math.MaxUint8
	case "u16":
		return v >= 0 && v <= math.MaxUint16
	case "u32":
		return v >= 0 && v <= math.MaxUint32
	case "u64", "u128":
		return v >= 0
	default:
		// str / f32 / f64：始终可直接书写。
		return true
	}
}
