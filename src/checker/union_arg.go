package checker

// union_arg.go — 聯合別名實參的「成員歸屬」守衛（unionarg）。
//
// 規則：把一個實參傳給 union 別名形參時，實參型別（允許 `?T` 解包與模組前綴）
// 必須**本身就是某個（遞迴展平後的）成員**。否則在源碼位置報 ERROR，要求先顯式轉換。
//
// 為什麼不能用「相容就放行」：
//
//	isArgTypeCompatible 的聯合別名分支對每個成員**遞迴**跑通用相容規則，於是
//	`byte → i64` 的整數加寬讓 `big = bigint | i64` 接受一個 byte。可選成員的動作
//	發生在更晚的 monomorph（build.inferExprMemberType），那裡只拿型別**名字**去比對
//	成員列表：`byte` 一個也匹配不上，改寫靜默不發作，模板名原樣留在呼叫點，最後以
//	「EmitLLVM: unknown callee bigint.bigint._add」這種內部錯誤收場——沒有源碼位置，
//	也沒有可執行的修法（實測 tmp/bx/d3.no）。
//
// 另一側更貴：union 帶聚合體 fallback 時，推定失敗會默默選中**錯誤的成員**，呼叫點
// 仍按原型別傳值；LLVM 17+ 的 opaque pointer 不在編譯期拒絕，錯誤一路活到運行期，
// 要么踩壞堆，要么靜默算出 0（即 D3 的隨機段）。
//
// 兩條刻意的邊界：
//
//  1. **字面量豁免**：整數/浮點/字串/字符/布爾字面量的型別由既有範圍規則與新類型
//     規則裁決，union 成員選擇另有推定路徑（`m * 10` 的 10 就是這樣過關的）。
//  2. **容器型別不碰**：`[N]T → []T` 這類成員內轉換走的是既有的相容規則，收緊會
//     誤傷 std 的視圖賦值寫法；本守衛只處理「具體標量名不在成員列表裡」這一種。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
)

// unionArgScalarTail 把型別名收斂成「可與成員名比對」的形狀：去掉一層 `?`／`&`
// 與模組前綴（`bigint.bigint` → `bigint`）。容器型別原樣回傳（呼叫方會跳過）。
func unionArgScalarTail(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "?")
	t = strings.TrimPrefix(t, "&")
	if t == "" || strings.ContainsAny(t, "[]") {
		return t
	}
	if i := strings.LastIndex(t, "."); i >= 0 {
		t = t[i+1:]
	}
	return t
}

// unionArgIsLiteralExpr 報告實參是不是一個字面量（豁免本守衛）。
func unionArgIsLiteralExpr(arg parser.Expression) bool {
	switch arg.(type) {
	case *parser.IntegerLiteral, *parser.FloatLiteral, *parser.StringLiteral,
		*parser.CharLiteral, *parser.BooleanLiteral:
		return true
	}
	return false
}

// unionFlattenedMemberNames 回傳 union 別名展平後的成員名（保持宣告順序、去重）。
func unionFlattenedMemberNames(name string, aliases map[string]*parser.TypeAlias) []string {
	var out []string
	seen := make(map[string]bool)
	for _, m := range FlattenUnion(name, aliases) {
		nt, ok := m.(*parser.NamedType)
		if !ok {
			continue
		}
		v := unionArgScalarTail(nt.Value)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, nt.Value)
	}
	return out
}

// inferBareBuiltinCallType 補 inferExprType 漏掉的「裸名註冊」內建方法回傳型別。
//
// `str.byte` / `txt.byte` / `*.set-byte` 在 Go 表裡只以裸方法名註冊（見
// builtin/str.go 的 ReceiverStr 註釋），inferExprType 用 `接收者.方法` 拼鍵查表
// 因此永遠落空、回 ""——`c = s.byte(i)` 於是在參數走查的型別表裡根本不存在，
// 下游 `c - 48` 只能從字面量推成 i64，union 成員守衛（unionarg）就瞎了。
//
// 只在 walker 登記變數型別時使用，不改 inferExprType 本身：那張表餵給溢位／
// 型別相容等一大票檢查，動它的爆炸半徑與這裡完全不是一個量級。
func inferBareBuiltinCallType(e parser.Expression, varTypes map[string]string) string {
	call, ok := e.(*parser.CallExpression)
	if !ok {
		return ""
	}
	dot, ok := call.Function.(*parser.DotExpression)
	if !ok {
		return ""
	}
	recvType := unionArgScalarTail(resolveExprType(dot.Receiver, varTypes, nil))
	if recvType != "str" && recvType != "txt" {
		return ""
	}
	if bm := builtin.FindBuiltinMethod(dot.Property); bm != nil && len(bm.Return) > 0 {
		return bm.Return[0].String()
	}
	return ""
}

// resolveArgType 是 unionarg 專用的實參型別解析：先走通用 resolveExprType，
// 解不出來再補「降糖後的運算符呼叫」這一種形狀。
//
// 為什麼需要補：checker 看到的 `m + (c - 48)` 已經被 parser 降成
// `m._add(c._sub(48))`（見 parser/lowering.go 的 maybeDesugarInfix），而
// resolveExprType 對 `接收者.方法` 的**回傳型別**只能查表；`_sub` 這類原生整數
// 運算沒有重載方法可查（表裡沒有 `byte._sub`），於是回 ""，整個實參被跳過——
// 守衛恰好在自己要攔的形狀上失聲。
//
// 原生運算符呼叫的結果型別就是接收者的數值型別（去 `?`，與 build 端
// inferExprMemberType 對 `(c - 48)` 的推定一致）。
func resolveArgType(e parser.Expression, varTypes map[string]string, structFields map[string]map[string]string) string {
	e = ungroupExpr(e)
	if t := resolveExprType(e, varTypes, structFields); t != "" {
		return t
	}
	call, ok := e.(*parser.CallExpression)
	if !ok {
		return ""
	}
	dot, ok := call.Function.(*parser.DotExpression)
	if !ok || !parser.IsOperatorOverloadName(dot.Property) {
		return ""
	}
	return unionArgScalarTail(resolveArgType(dot.Receiver, varTypes, structFields))
}

// ungroupExpr 剝掉括號，讓 `(c - 48)` 這種實參的型別能被解出。
func ungroupExpr(e parser.Expression) parser.Expression {
	for {
		g, ok := e.(*parser.GroupedExpression)
		if !ok || g.Expression == nil {
			return e
		}
		e = g.Expression
	}
}

// checkOperatorOverloadUnionArg 對中綴運算符語法補一次 union 成員守衛。
//
// 為什麼需要這一條：`a OP b` 在 checker 階段**還是 InfixExpression**，改寫成
// `a._op(b)` 發生在 build 期的 lowering，因此 checkStdMethodCallArgs 永遠看不到
// 這種寫法——而它恰恰是缺陷的原始形狀（`m * 10 + (c - 48)`）。
//
// 查不到重載方法參數時一律沉默：本函式只補漏，不承擔派發決策。
func checkOperatorOverloadUnionArg(inf *parser.InfixExpression, varTypes map[string]string,
	structFields map[string]map[string]string) []ValidateResult {
	if inf == nil || inf.Left == nil || inf.Right == nil {
		return nil
	}
	method, ok := parser.OperatorMethod(inf.Operator)
	if !ok {
		return nil
	}
	recvType := resolveExprType(ungroupExpr(inf.Left), varTypes, structFields)
	if recvType == "" {
		return nil
	}
	params, _, _ := stdMethodParamTypes(recvType, method)
	if len(params) == 0 || params[0] == "" {
		return nil
	}
	arg := ungroupExpr(inf.Right)
	argType := resolveArgType(arg, varTypes, structFields)
	if argType == "" {
		return nil
	}
	return checkUnionMemberArg(arg, params[0], argType, recvType+"."+method, 1)
}

// checkUnionMemberArg 在 paramType 是 union 別名時，檢查實參型別是否為其成員。
//
// callDesc 只用於訊息（如 `bigint._add` 或 `f`），argIdx 是 1-based 實參序號。
// 回傳空切片代表放行；本守衛對非 union 形參、字面量、容器型別、無法解出的型別
// 一律不表態（寧可漏報，也不猜）。
func checkUnionMemberArg(arg parser.Expression, paramType, argType, callDesc string, argIdx int) []ValidateResult {
	if arg == nil || argType == "" || paramType == "" {
		return nil
	}
	aliases := validationUnionTypeAliases
	if aliases == nil {
		return nil
	}
	// 形參可能是模組限定寫法（`bigint.big`），兩條鍵都試。
	var ta *parser.TypeAlias
	for _, name := range []string{paramType, stripModulePrefix(paramType)} {
		if a, ok := aliases[name]; ok && a != nil && a.IsUnion() {
			ta = a
			paramType = name
			break
		}
	}
	if ta == nil {
		return nil
	}
	if unionArgIsLiteralExpr(arg) {
		return nil
	}
	// 實參本身是 union（`i64 | err`）或容器：交給既有相容規則，本守衛不介入。
	if strings.Contains(argType, "|") {
		return nil
	}
	argTail := unionArgScalarTail(argType)
	if argTail == "" || strings.ContainsAny(argTail, "[]") {
		return nil
	}
	members := unionFlattenedMemberNames(paramType, aliases)
	for _, m := range members {
		if unionArgScalarTail(m) == argTail {
			return nil
		}
	}
	// 成員列表為空說明展平失敗（別名表不全），沒有事實可依 —— 不表態。
	if len(members) == 0 {
		return nil
	}

	names := make([]string, 0, len(members))
	for _, m := range members {
		names = append(names, unionArgScalarTail(m))
	}
	sort.Strings(names)
	line, col := 0, 0
	if p := arg.Pos(); p.Line > 0 {
		line, col = p.Line, p.Column
	}
	return []ValidateResult{{
		TraceID: "unionarg",
		Line:    line,
		Column:  col,
		Message: fmt.Sprintf("argument %d of '%s': type '%s' is not a member of union '%s' (%s); "+
			"the union dispatches per member at compile time, so convert the argument explicitly first",
			argIdx, callDesc, argType, paramType, strings.Join(names, " | ")),
	}}
}
