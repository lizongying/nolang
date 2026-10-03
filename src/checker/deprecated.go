package checker

// deprecated.go — std 弃用函數呼叫提示（HINT）。
//
// std 中部分早期以「自由函數」形式提供的工具，後來都有了等價的方法形式
// （接收者即參數）。舊函數保留兼容不刪除，但 lint 在每個呼叫點發出 HINT，
// 建議改用新方法。例：number.char-to-str(c) 的取代是 char.to-str()（std/char.no），
// 呼叫點可改寫為 c.to-str()。
//
// callee 識別與 listdir.go 相同：未合併（LSP/單測）時 `number.char-to-str`
// 是 DotExpression；模組合併（no vet）後可能成為 Value="number.char-to-str"
// 的點分 Identifier，故統一按「點號最後一段（base）」匹配，兩種形態都能命中。
//
// 嚴重級別：HINT（不阻斷編譯；--strict 升級為 error）。

import (
	"fmt"
	"strings"

	"github.com/lizongying/nolang/parser"
)

const deprecatedCallTraceID = "nolang-deprecated-call"

// deprecatedCallReplacements 是「棄用函數 base 名 → 替代信息」表。
// 新增棄用項只需在此加一行，RunAllLints 無需改動。
//
// 注意：std 中部分替代方法的實現就是委託給棄用函數本身（如 number.no 的
// i64.to-str 函數體為 `out = i64-to-str(.)`）。這類方法定義內部對舊函數的
// 呼叫屬於實現細節（遷移完成前無法內聯），不該被提示；由 isSelfImplCall 抑制。
var deprecatedCallReplacements = map[string]struct {
	repl string // 建議替代寫法
	note string // 補充說明（可選，跟在提示括號內）
}{
	"char-to-str": {"char.to-str()", "接收者即參數，如 c.to-str()，見 std/char.no"},
	"i64-to-str":  {"i64.to-str()", "接收者即參數，如 n.to-str()，見 std/number.no"},
	"u64-to-str":  {"u64.to-str()", "接收者即參數，如 u.to-str()，見 std/number.no"},
	"f64-to-str":  {"f64.to-str()", "接收者即參數，如 x.to-str()，見 std/number.no"},
}

// isSelfImplCall 報告棄用函數的呼叫是否屬於 std 內部實現（兩類豁免）：
//  1. to-str 家族方法的底層實現跨型別委託給舊自由函數（i32.to-str → i64-to-str、
//     f64.to-str → f64-to-str），curFn 為外層頂層函數名（合併形態可能帶模組
//     前綴，如 number.i64.to-str），呼叫是方法實現細節、不提示；
//  2. 棄用函數自身的函數體（如 f64-to-str 內部借 i64-to-str 拼指數段）——
//     整個函數已被棄用，其內部再逐點提示只是噪音。
//
// 用戶在 .to-str 方法裡呼叫舊函數被一并豁免的漏報代價極小（僅 HINT 級）。
func isSelfImplCall(curFn, base string) bool {
	if strings.HasSuffix(curFn, ".to-str") && strings.HasSuffix(base, "-to-str") {
		return true
	}
	_, curIsDeprecated := deprecatedCallReplacements[lastDotSegment(curFn)]
	return curIsDeprecated
}

// ValidateDeprecatedCalls 報告 program 中所有對棄用函數的呼叫。
func ValidateDeprecatedCalls(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	v := &deprecatedVisitor{}
	v.walkBlock(program.Statements, "", "")
	return v.results
}

type deprecatedVisitor struct {
	results []ValidateResult
}

// walkBlock 遍歷語句列表；file/curFn 為外層上下文（來源檔、外層頂層函數名），
// 嵌套語句在合併模式下 SourceFile 為空，需逐層傳入（同 overflow lint 的口徑）。
func (v *deprecatedVisitor) walkBlock(stmts []parser.Statement, file, curFn string) {
	for _, s := range stmts {
		v.walkStmt(s, file, curFn)
	}
}

func (v *deprecatedVisitor) walkStmt(s parser.Statement, file, curFn string) {
	if s == nil {
		return
	}
	if f := parser.GetSourceFile(s); f != "" {
		file = f
	}
	switch st := s.(type) {
	case *parser.FunctionDefinition:
		// 記錄本函數名作為自實現抑制的匹配鍵（空名回退外層）。
		name := st.Name
		if name == "" {
			name = curFn
		}
		if st.Body != nil {
			v.walkBlock(st.Body.Statements, file, name)
		}
	case *parser.BlockStatement:
		v.walkBlock(st.Statements, file, curFn)
	case *parser.ForStatement:
		v.walkExpr(st.Condition, file, curFn)
		if st.Body != nil {
			v.walkBlock(st.Body.Statements, file, curFn)
		}
	case *parser.LetStatement:
		v.walkExpr(st.Value, file, curFn)
	case *parser.ExpressionStatement:
		v.walkExpr(st.Expression, file, curFn)
	case *parser.MultiAssignStatement:
		v.walkExpr(st.Value, file, curFn)
	case *parser.UnwrapAssignStatement:
		v.walkExpr(st.Value, file, curFn)
	case *parser.ReturnStatement:
		v.walkExpr(st.ReturnValue, file, curFn)
	}
}

func (v *deprecatedVisitor) walkIf(ie *parser.IfExpression, file, curFn string) {
	if ie == nil {
		return
	}
	v.walkExpr(ie.Condition, file, curFn)
	if ie.Consequence != nil {
		v.walkBlock(ie.Consequence.Statements, file, curFn)
	}
	if ie.Alternative != nil {
		v.walkBlock(ie.Alternative.Statements, file, curFn)
	}
}

func (v *deprecatedVisitor) walkExpr(e parser.Expression, file, curFn string) {
	if e == nil {
		return
	}
	switch ex := e.(type) {
	case *parser.CallExpression:
		full, base := calleeParts(ex.Function)
		if info, ok := deprecatedCallReplacements[base]; ok && !isSelfImplCall(curFn, base) {
			name := full
			if name == "" {
				name = base
			}
			msg := fmt.Sprintf("函數 %s 已棄用，建議使用 %s 替代", name, info.repl)
			if info.note != "" {
				msg += "（" + info.note + "）"
			}
			pos := ex.Pos()
			if ex.Function != nil {
				pos = ex.Function.Pos()
			}
			v.results = append(v.results, ValidateResult{
				Line:      pos.Line,
				Column:    pos.Column,
				EndColumn: pos.Column + len(name),
				File:      file,
				Message:   msg,
				TraceID:   deprecatedCallTraceID,
			})
		}
		v.walkExpr(ex.Function, file, curFn)
		for _, a := range ex.Arguments {
			v.walkExpr(a, file, curFn)
		}
	case *parser.IfExpression:
		v.walkIf(ex, file, curFn)
	case *parser.InfixExpression:
		v.walkExpr(ex.Left, file, curFn)
		v.walkExpr(ex.Right, file, curFn)
	case *parser.PrefixExpression:
		v.walkExpr(ex.Right, file, curFn)
	case *parser.GroupedExpression:
		v.walkExpr(ex.Expression, file, curFn)
	case *parser.IndexExpression:
		v.walkExpr(ex.Left, file, curFn)
		v.walkExpr(ex.Index, file, curFn)
	case *parser.AssignExpression:
		v.walkExpr(ex.Left, file, curFn)
		v.walkExpr(ex.Value, file, curFn)
	case *parser.ArrayLiteral:
		for _, el := range ex.Elements {
			v.walkExpr(el, file, curFn)
		}
	}
}
