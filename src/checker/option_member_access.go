package checker

import (
	"fmt"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// option_member_access.go — 「對 option 值取 len/cap 或下標」的編譯期診斷。
//
// ── 為什麼要有這條規則 ──
// D6 統一 option 規則下，有號整數算術、可失敗呼叫、安全索引都會產生 `?T`；
// 一個變數/參數的靜態型別一旦是 `?T`，它就只能當 option 用（match / `?=` /
// 顯式 `?T` 綁定），不能當 `T` 用。`len()` / `.cap()` / `x[i]` 都是「只用 T
// 的成員」，對 `?T` 取用沒有語意 —— option 在 MIR 是 `{tag, payload}` 標籤結構，
// 對它取長度取到的是標籤欄位，下標則根本無從計算。
//
// 這些形態過去一路活到 MIR，由 emitLenCap 的 default 分支拋
// `t5qq2leo: len/cap unsupported on ?str (option) in func 13`。那是**後端兜底**：
// 使用者拿到的是一行 codegen 內部訊息（含 MIR 的函式索引），既沒有可用的原始碼
// 位置，也沒有修法提示；`no vet` 更是完全看不到，只能靠 `no build` 撞出來。
// 本檔把判定搬到 checker：有型別、有位置、有修法。
//
// ── 兩個 TraceID 為什麼分開 ──
// 修法不同（一個是把 len/cap 移到解包後，一個是把下標移到解包後），分開才能給
// 各自的訊息；也方便 build 側只按這兩個 ID 匯入硬錯誤。
const (
	// optionLenCapRecvTraceID：`p.len()` / `p.cap()` 的接收者是 option。
	optionLenCapRecvTraceID = "qwl2enr7"
	// optionIndexRecvTraceID：`p[i]` / `p[i] = v` 的基底是 option。
	optionIndexRecvTraceID = "qwl2idx4"
)

// OptionLenCapRecvTraceID / OptionIndexRecvTraceID 是上面兩個常量的導出別名，
// 供 build 套件過濾成硬錯誤（沿用 BareIndexUseTraceID 的雙常量模式）。
const (
	OptionLenCapRecvTraceID = optionLenCapRecvTraceID
	OptionIndexRecvTraceID  = optionIndexRecvTraceID
)

// ValidateOptionMemberAccess 報告所有「對 option 值取 len/cap 或下標」的位置。
//
// 兩個入口共用本函式：build 路徑（transpiler_validate.go）把它匯入硬錯誤清單，
// RunAllLints 的 option-member-access 步把它變成 vet / LSP 診斷。
//
// 型別來源是語義副表（FuncVarType / VarType / struct 欄位表），不讀 AST 型別
// 節點 —— 推斷出來的 `?T`（`p = s.slice(n + 1, 5)` 這類）只存在於副表。
func ValidateOptionMemberAccess(program *parser.Program) []ValidateResult {
	if program == nil || program.Sem == nil {
		return nil
	}
	sem := program.Sem
	structFields := collectStructFields(program)

	var results []ValidateResult
	seen := map[string]bool{}

	add := func(n parser.Node, file, traceID, msg string) {
		if n == nil {
			return
		}
		pos := n.Pos()
		key := fmt.Sprintf("%s|%d:%d|%s", file, pos.Line, pos.Column, traceID)
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, ValidateResult{
			Line:    pos.Line,
			Column:  pos.Column,
			File:    file,
			Message: msg,
			TraceID: traceID,
		})
	}

	// rawOf 解析「被取用目標」的靜態型別字串，**保留 option 前綴**。
	//
	// 刻意不複用 indexBaseExprRaw / indexBaseReceiverRaw：那兩支在每個出口都
	// StripOptionRaw，正是本規則要看的東西。此處只處理「名字 / 欄位」這幾種
	// 能在副表裡查到型別的形態；呼叫結果（`s.slice(...)` 這類）需要
	// returnTypeFromCallExpr 的完整推斷，屬 parser 側職責，這裡不猜。
	var rawOf func(e parser.Expression, curFunc, selfType string) string
	rawOf = func(e parser.Expression, curFunc, selfType string) string {
		switch x := e.(type) {
		case *parser.Identifier:
			if x.Value == "self" && selfType != "" {
				return selfType
			}
			if t, ok := sem.FuncVarType(curFunc, x.Value); ok && t != "" {
				return t
			}
			if t, ok := sem.VarType(x.Value); ok {
				return t
			}
		case *parser.GroupedExpression:
			return rawOf(x.Expression, curFunc, selfType)
		case *parser.DotExpression:
			if x == nil {
				return ""
			}
			if rt := rawOf(x.Receiver, curFunc, selfType); rt != "" {
				if fs, ok := structFields[ty.StripOptionRaw(rt)]; ok {
					if ft, ok := fs[x.Property]; ok {
						return ft
					}
				}
			}
			// 自由函數裡的隱式接收者 `.field` 是對同名局部變數的存取
			// （與 indexBaseExprRaw 的最後一條回退同口徑）。
			if id, ok := x.Receiver.(*parser.Identifier); ok && id.Value == "self" && selfType == "" {
				if t, ok := sem.FuncVarType(curFunc, x.Property); ok {
					return t
				}
			}
		}
		return ""
	}

	// nameOf 給訊息用的受體名字：識別符直接用它，其餘描述為「運算式」。
	nameOf := func(e parser.Expression) string {
		if id, ok := e.(*parser.Identifier); ok && id.Value != "" {
			return "`" + id.Value + "`"
		}
		return "the receiver"
	}

	fileOf := func(n parser.Node, curFile string) string {
		if f := sem.SourceFileOf(n); f != "" {
			return f
		}
		return curFile
	}

	var walkStmt func(stmt parser.Statement, curFunc, selfType, file string)
	var walkBlock func(b *parser.BlockStatement, curFunc, selfType, file string)
	var walkExpr func(e parser.Expression, curFunc, selfType, file string)

	walkBlock = func(b *parser.BlockStatement, curFunc, selfType, file string) {
		if b == nil {
			return
		}
		for _, st := range b.Statements {
			walkStmt(st, curFunc, selfType, file)
		}
	}

	walkExpr = func(e parser.Expression, curFunc, selfType, file string) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *parser.CallExpression:
			// `p.len()` / `p.cap()`：接收者必須是 T，不能是 ?T。
			if dot, ok := x.Function.(*parser.DotExpression); ok && dot != nil {
				if dot.Property == "len" || dot.Property == "cap" {
					if t := rawOf(dot.Receiver, curFunc, selfType); ty.IsOptionRaw(t) {
						add(x, fileOf(x, file), optionLenCapRecvTraceID,
							i18n.T("qwl2enr7",
								"`%s()` is called on %s, whose type is %s — an option has no members of its own (it is a tag plus a payload). Unwrap it first and call `%s()` on the payload: `%s: { ok -> it.%s() ... -> <fallback> }`, or propagate with `%s ?= <expr>`, or bind an unwrapped local `v = <option expr>: { ok -> it ... }`.",
								dot.Property, nameOf(dot.Receiver), t, dot.Property,
								nameOf(dot.Receiver), dot.Property, nameOf(dot.Receiver)))
					}
				}
				walkExpr(dot.Receiver, curFunc, selfType, file)
			}
			if x.Function != nil {
				if _, isDot := x.Function.(*parser.DotExpression); !isDot {
					walkExpr(x.Function, curFunc, selfType, file)
				}
			}
			for _, a := range x.Arguments {
				walkExpr(a, curFunc, selfType, file)
			}
		case *parser.IndexExpression:
			// `p[i]` / `p[i] = v`：基底必須是 T，不能是 ?T。
			if t := rawOf(x.Left, curFunc, selfType); ty.IsOptionRaw(t) {
				add(x, fileOf(x, file), optionIndexRecvTraceID,
					i18n.T("qwl2idx4",
						"an index is taken on %s, whose type is %s — an option cannot be indexed (it is a tag plus a payload, not a container). Unwrap it first and index the payload: `%s: { ok -> it[<i>] ... -> <fallback> }`, or propagate with `%s ?= <expr>`, or bind an unwrapped local `v = <option expr>: { ok -> it ... }`.",
						nameOf(x.Left), t, nameOf(x.Left), nameOf(x.Left)))
			}
			walkExpr(x.Left, curFunc, selfType, file)
			walkExpr(x.Index, curFunc, selfType, file)
		case *parser.DotExpression:
			walkExpr(x.Receiver, curFunc, selfType, file)
		case *parser.InfixExpression:
			walkExpr(x.Left, curFunc, selfType, file)
			walkExpr(x.Right, curFunc, selfType, file)
		case *parser.PrefixExpression:
			walkExpr(x.Right, curFunc, selfType, file)
		case *parser.GroupedExpression:
			walkExpr(x.Expression, curFunc, selfType, file)
		case *parser.AssignExpression:
			walkExpr(x.Left, curFunc, selfType, file)
			walkExpr(x.Value, curFunc, selfType, file)
		case *parser.ArrayLiteral:
			for _, el := range x.Elements {
				walkExpr(el, curFunc, selfType, file)
			}
		case *parser.SliceExpression:
			if x != nil {
				walkExpr(x.Left, curFunc, selfType, file)
				if x.Range != nil {
					walkExpr(x.Range.Start, curFunc, selfType, file)
					walkExpr(x.Range.End, curFunc, selfType, file)
				}
			}
		case *parser.IfExpression:
			walkExpr(x.Condition, curFunc, selfType, file)
			walkBlock(x.Consequence, curFunc, selfType, file)
			walkBlock(x.Alternative, curFunc, selfType, file)
		case *parser.SurfaceMatch:
			// 糖形態 match：與 walkExprDivByZero 同口徑，只下鑽各臂。
			if x != nil {
				for i := range x.Arms {
					walkExpr(x.Arms[i].Condition, curFunc, selfType, file)
					walkBlock(x.Arms[i].Body, curFunc, selfType, file)
				}
			}
		}
	}

	walkStmt = func(stmt parser.Statement, curFunc, selfType, file string) {
		if stmt == nil {
			return
		}
		if f := sem.SourceFileOf(stmt); f != "" {
			file = f
		}
		switch s := stmt.(type) {
		case *parser.FunctionDefinition:
			inner := ""
			if s.IsMethodDef && len(s.Results) > 0 && s.Results[0] != nil &&
				s.Results[0].Name == "self" && s.Results[0].Type != nil {
				inner = s.Results[0].Type.String()
			}
			walkBlock(s.Body, s.Name, inner, file)
		case *parser.LetStatement:
			walkExpr(s.Value, curFunc, selfType, file)
		case *parser.ReturnStatement:
			walkExpr(s.ReturnValue, curFunc, selfType, file)
		case *parser.ExpressionStatement:
			walkExpr(s.Expression, curFunc, selfType, file)
		case *parser.MultiAssignStatement:
			walkExpr(s.Value, curFunc, selfType, file)
		case *parser.UnwrapAssignStatement:
			walkExpr(s.Value, curFunc, selfType, file)
		case *parser.ForStatement:
			walkExpr(s.Condition, curFunc, selfType, file)
			walkExpr(s.CountExpr, curFunc, selfType, file)
			walkStmt(s.Init, curFunc, selfType, file)
			walkStmt(s.Update, curFunc, selfType, file)
			if s.IterRange != nil {
				walkExpr(s.IterRange.RangeExpr, curFunc, selfType, file)
				if r := s.IterRange.Range; r != nil {
					walkExpr(r.Start, curFunc, selfType, file)
					walkExpr(r.End, curFunc, selfType, file)
				}
			}
			walkBlock(s.Body, curFunc, selfType, file)
		case *parser.BlockStatement:
			walkBlock(s, curFunc, selfType, file)
		case *parser.BlockMatchStatement:
			for _, b := range blockMatchChildStatements(s) {
				walkStmt(b, curFunc, selfType, file)
			}
		}
	}

	for _, stmt := range program.Statements {
		walkStmt(stmt, "", "", "")
	}
	return results
}
