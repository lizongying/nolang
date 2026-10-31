package parser

import "github.com/lizongying/nolang/ty"

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
// 結果型別按**書寫**生效：寫 `?bigint` 即回傳 option，callee ABI 是 %option*
// 結果槽，呼叫端因而能 match 運算符結果的 ok／err／nil 三分支——把 `?` 在簽名表裡
// 剝掉會讓 `x % 256` 這類 subject 退化成對結構體 emit icmp（LLVM 拒收）。
// `?` 只在**消費位置**剝成 payload T：
//   - 巢狀運算子的接收者（`(a + b) * c` 的左半）——見 type.go resolveReceiverType；
//   - 把結果重新賦值給已宣告為 T 的變數（累加器 `c = c + 1`）——見 stmt.go。
// 除零等失敗由被呼叫方法體自行守衛（見 int.div 的零除守衛）。

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

// OperatorOverloadReturnType 探測「中綴 `left OP right`」是否可派發為運算符重載
// 方法呼叫（`left._op(right)`）。若左操作數的靜態型別定義了對應的 `_op`
// 方法，回傳該方法**書寫**的回傳型別（寫 `?T` 即 `?T`）；否則回傳空字串。
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
	// 回傳書寫的 `?T` 原文：`?` 只在消費位置剝成 payload（巢狀運算子的接收者
	// 見 resolveReceiverType，重新賦值見 parseLetStatement）。
	return p.returnTypeFromCallExpr(probe)
}

// IsOperatorOverloadCallExpr 回報呼叫點是否派發到運算符重載方法（`_add` 等
// 保留名）——無論是方法式 `x._add(y)`，還是 build 攤平後的裸名 `bigint._add(x, y)`。
func IsOperatorOverloadCallExpr(call *CallExpression) bool {
	if call == nil {
		return false
	}
	switch fn := call.Function.(type) {
	case *DotExpression:
		return IsOperatorOverloadName(fn.Property)
	case *Identifier:
		return IsOperatorOverloadName(OperatorOverloadBaseName(fn.Value))
	}
	return false
}

// preserveDeclaredPayloadForOperatorAssign 回報「把運算符重載結果 `?T` 賦給變數
// name」是否屬於消費位置：name 已在當前函數宣告為非 option 型別時保留宣告型別，
// 不讓推斷的 `?T` 覆寫型別表（見 parser/stmt.go parseLetStatement 的
// CallExpression 分支）。全新綁定（無宣告型別）不在此列——`hc = h._add(c)` 仍
// 保留 `?T`，這是 n1 design B 的語義。
func (p *Parser) preserveDeclaredPayloadForOperatorAssign(call *CallExpression, name string) bool {
	if !IsOperatorOverloadCallExpr(call) || name == "" {
		return false
	}
	existing, ok := p.sem.FuncVarType(p.curFuncName, name)
	return ok && existing != "" && !ty.IsOptionRaw(existing)
}
