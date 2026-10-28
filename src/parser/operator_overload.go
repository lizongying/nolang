package parser

import "strings"

// operator_overload.go — 運算符重載（operator overloading）支援。
//
// 任意用戶類型只要在該類型上定義 `_add` / `_sub` / `_mul` / `_div` / `_mod`
// 方法，編譯器就會把對應的中綴運算符自動重寫為方法呼叫：
//
//	a + b  →  a._add(b)
//	a - b  →  a._sub(b)
//	a * b  →  a._mul(b)
//	a / b  →  a._div(b)
//	a % b  →  a._mod(b)
//
// 這些方法的結果型別雖可寫成 `?bigint`（書寫統一、提示「這是算術運算符」），
// 但編譯器將其視作純 payload 型別（如 `bigint`）處理：方法體與呼叫點都拿到
// `bigint`，不產生 option 包裝／解包；除零等失敗按內建數值型別邏輯處理
// （見 int.div 的零除守衛）。`_div` / `_mod` 的除零由被呼叫方法體自行守衛。

// opToMethod 把可重載的中綴運算符對應到其運算符重載方法名。
var opToMethod = map[string]string{
	"+": "_add",
	"-": "_sub",
	"*": "_mul",
	"/": "_div",
	"%": "_mod",
}

// OperatorMethod 回傳運算符 op 對應的運算符重載方法名。
// 若 op 不在可重載集合內（如比較、位運算），ok 為 false。
func OperatorMethod(op string) (string, bool) {
	m, ok := opToMethod[op]
	return m, ok
}

// IsOperatorOverloadName 回報 name 是否為保留的運算符重載方法名
// （_add / _sub / _mul / _div / _mod）。這些名字被專用於運算符重載，
// 不會與普通以 `_` 開頭的私有方法混淆（因為是一個閉集）。
func IsOperatorOverloadName(name string) bool {
	switch name {
	case "_add", "_sub", "_mul", "_div", "_mod":
		return true
	}
	return false
}

// stripOperatorOverloadReturn 把運算符重載方法（_add 等）的回傳型別由書寫形式
// `?T` 還原為生效的 payload `T`。非運算符重載方法原樣返回（保留其 `?T` 語義）。
//
// 這是鏈式運算子（如 `a + b + c`、`(a + b) * c`）派發的關鍵：內層 `a._add(b)`
// 的回傳型別若仍是 `?bigint`，外層把該 CallExpression 當接收者解析時會得到
// `?bigint`，導致 `?bigint._add` 查不到簽名而放棄改寫、退回中綴運算子的原生
// codegen（在 struct 上 emit `add`／`mul` → LLVM invalid operand）。統一在此
// 剥掉 `?`，讓運算符重載方法的結果型別在任何巢狀深度都為純 payload。
func stripOperatorOverloadReturn(method, retType string) string {
	if IsOperatorOverloadName(method) {
		return strings.TrimPrefix(retType, "?")
	}
	return retType
}

// OperatorOverloadBaseName 從可能帶限定前綴的方法定義名（如 `bigint._add`）
// 取出裸方法名（`_add`）。無點號時原樣返回。
func OperatorOverloadBaseName(name string) string {
	if i := lastDotIndex(name); i >= 0 {
		return name[i+1:]
	}
	return name
}

func lastDotIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// operandPromoters 是運算符重載的「實參提升」回退表（路線 A）。
//
// 運算符重載要求兩操作數同型：`a + b` desugar 成 `a._add(b)`，`_add` 的形參是
// concrete `bigint`，若實參是 i64 標量會被 checker 判 `stdargtyp`（實測 `a * 256`
// 報 "expected 'bigint', got 'i64'"）。原本想改用「聯合形參 `(b big)` + 型別解構
// match」（路線 C）讓運算子原生吃多種運算數，但實測證明該路徑對用戶／main 碼無效：
// main 的中綴運算子在 monomorphizeUnions/rewriteUnionCalls **之後**才 desugar 成
// `a._add(...)`（降級延到 HIR/hir2mir），而 resolveCallee 只按「接收者型別」派發、
// 無法依「實參型別」選中 `__member` 複本，且 union 模板名 `bigint.bigint._add__
// bigint.big_TEMPLATE` 也無法被 unionTemplateCallee（以接收者型別拼模板名）重建 →
// 連 `a + b`（兩 bigint）都退化成 "unknown callee bigint.bigint._add"。
//
// 故採計劃明文核准的回退方案：desugar 時偵測右操作數為可提升標量，直接包一層
// 提升建構函式 `a._add(bigint.from-i64(1))`，讓呼叫綁回既有 concrete `_add(b bigint)`，
// 不動聯合／單態化機制。表鍵為接收者裸型別末段，值為「實參裸型別末段 → 模組限定
// 建構函式名（簽名 `(v <實參型別>) (out <接收者型別>)`）」。僅登记有對應建構函式的
// 標量寬度；無對應 ctor 的實參保持原樣（交回 checker 既有型別檢查）。
var operandPromoters = map[string]map[string]string{
	"bigint": {
		"i64": "bigint.from-i64",
	},
}

// baseTypeTail 取型別名的模組限定末段並剝 option／view 前綴：
// `bigint.bigint`→`bigint`、`?i64`→`i64`、`&point`→`point`、`i64`→`i64`。
func baseTypeTail(name string) string {
	name = strings.TrimPrefix(strings.TrimPrefix(name, "?"), "&")
	if i := lastDotIndex(name); i >= 0 {
		return name[i+1:]
	}
	return name
}

// operandStaticType 回報中綴實參的靜態型別（供提升判定）。字面量按內建預設型別
// （整數 i64、浮點 f64、布林 bool、字串 str）；其餘交 resolveReceiverType（含
// 變數查表、巢狀呼叫回傳型別、括號／中綴遞迴）。無法推斷時回傳空字串。
func (p *Parser) operandStaticType(e Expression) string {
	switch e.(type) {
	case *IntegerLiteral:
		return "i64"
	case *FloatLiteral:
		return "f64"
	case *BooleanLiteral:
		return "bool"
	case *StringLiteral:
		return "str"
	default:
		return p.resolveReceiverType(e)
	}
}

// operandPromotionCtor 回傳把實參 right 提升為 recvType 所需的建構函式名；
// 若 recvType 不在提升表、或實參型別已等於接收者型別（同型運算數，走既有運算符
// 重載）、或實參型別無對應 ctor，回傳空字串（不提升）。
func (p *Parser) operandPromotionCtor(recvType string, right Expression) string {
	tbl, ok := operandPromoters[baseTypeTail(recvType)]
	if !ok {
		return ""
	}
	argTail := baseTypeTail(p.operandStaticType(right))
	if argTail == "" || argTail == baseTypeTail(recvType) {
		return ""
	}
	return tbl[argTail]
}

// OperatorOverloadReturnType 探測「中綴 `left OP right`」是否可派發為運算符重載
// 方法呼叫（`left._op(right)`）。若左操作數的靜態型別定義了對應的 `_op`
// 方法，回傳該方法的（去掉 `?` 後的）回傳型別；否則回傳空字串。
//
// 實作上構造一個等價的 method-call probe，並複用 checker 既有的回傳型別查表
// （returnTypeFromCallExpr），確保 parser 端派發與 checker 端的型別解析完全一致。
func (p *Parser) OperatorOverloadReturnType(inf *InfixExpression) string {
	if inf == nil {
		return ""
	}
	method, ok := OperatorMethod(inf.Operator)
	if !ok {
		return ""
	}
	probe := &CallExpression{
		Token: inf.Token,
		Function: &DotExpression{
			Token:    inf.Token,
			Receiver: inf.Left,
			Property: method,
		},
		Arguments: []Expression{inf.Right},
	}
	rt := p.returnTypeFromCallExpr(probe)
	if rt == "" {
		return ""
	}
	// 運算符重載方法書寫為 `?T`，但編譯器視作 payload `T` 處理，故回傳時去掉 `?`。
	return strings.TrimPrefix(rt, "?")
}
