package checker

import (
	"strings"

	"github.com/lizongying/nolang/parser"
)

// stdCallResultTypes 用編譯期烤進二進位的 std 簽名表解析「呼叫的回傳型別列表」。
//
// 為什麼需要：`no fmt` 走單檔解析（SkipLowering、不合併程式），validationFuncTypes
// 只收本檔的定義，於是 `bigint.from-i64(…)` / `num-s.div-mod(…)` /
// `mw._mod-i64(…)` 這類跨模組／std 方法呼叫一律推斷成 ""，「標註 == 推斷型別」永遠
// 不成立 —— `m ?i64 = mw._mod-i64(…)` 這種「std 宣告回傳 ?i64、標註也寫 ?i64」的
// 冗餘標註就消不掉。補上簽名表回退後，整條鏈（模組名 → bigint → ?bigint → ?i64）
// 在單檔形態也能走通。
//
// 邊界與 stdMethodOptionResult 逐條對稱（寧可漏報，不可誤報）：
//   - 接收者識別符要么是「本作用域已知型別的變數」（拿它的型別當前綴），要么是
//     「已知 std 模組名」（bigint、sha1…）；型別未知的區域變數一律不猜 —— 否則
//     一個叫 `fs` 的區域變數上的 `fs.read(…)` 會誤命中模組鍵。
//   - 被多個 std 模組定義的同名結構體方法（structMethodIsAmbiguous）→ 鍵不可信，跳過。
//   - 任何結果仍帶型別變數（泛型 `?v`，如 hashmap.get）→ 整條跳過。
//   - StdSigKey 未注入（不是走 `make` 建出來的二進位）→ 直接回 nil：
//     knownStdModules / CollectStdModuleSignatures 在該情況下會 os.Exit(1)。
//
// 只在冗餘標註規則（tcpoxtfd）裡、且共享推斷落空時呼叫；刻意不改 inferExprType，
// 免得把新的型別知識一次性灌進所有 validation。
func stdCallResultTypes(e parser.Expression, varTypes map[string]string) []string {
	if StdSigKey == "" {
		return nil
	}
	call, ok := e.(*parser.CallExpression)
	if !ok || call.Function == nil {
		return nil
	}
	dot, ok := call.Function.(*parser.DotExpression)
	if !ok || dot.Receiver == nil || dot.Property == "" {
		return nil
	}
	recv, ok := dot.Receiver.(*parser.Identifier)
	if !ok {
		return nil
	}
	prefix := ""
	if t, exists := varTypes[recv.Value]; exists && t != "" {
		// `?T` / `%T`（option 接收者）剝回內層型別再查表：方法綁在內層結構體上。
		prefix = strings.TrimPrefix(strings.TrimPrefix(t, "?"), "%")
	} else if _, isModule := knownStdModules()[recv.Value]; isModule {
		prefix = recv.Value
	}
	if prefix == "" {
		return nil
	}
	if stdSigsCache == nil && stdMethodSigsCache == nil {
		CollectStdModuleSignatures()
		if stdSigsCache == nil && stdMethodSigsCache == nil {
			return nil
		}
	}
	keys := []string{prefix + "." + dot.Property, prefix + "." + prefix + "." + dot.Property}
	if strings.HasPrefix(prefix, "[]") {
		keys = append(keys, "[]t."+dot.Property)
	}
	for _, key := range keys {
		if structMethodIsAmbiguous(key) {
			continue
		}
		for _, table := range []map[string]StdSigEntry{stdMethodSigsCache, stdSigsCache} {
			entry, ok := table[key]
			if !ok || len(entry.Results) == 0 {
				continue
			}
			for _, r := range entry.Results {
				if r == "" || mentionsTypeVar(r) {
					return nil // 泛型未綁定：整條不猜
				}
			}
			return entry.Results
		}
	}
	return nil
}

// redundancyValueTypeForLet 是 tcpoxtfd 專屬的右值型別回退推斷：共享的
// inferExprType 落空時，再問 std 簽名表／識別字別名。回 "" 表示推不出（不報冗餘）。
func redundancyValueTypeForLet(value parser.Expression, varTypes map[string]string) string {
	if value == nil {
		return ""
	}
	if id, ok := value.(*parser.Identifier); ok {
		// `wv = qw`：別名直接繼承登記過的型別（無算術、無 option 疑慮）。
		return varTypes[id.Value]
	}
	if rs := stdCallResultTypes(value, varTypes); len(rs) > 0 {
		return rs[0]
	}
	return ""
}

// registerRedundancyLetAlias 在「無使用者標註」的 let／多重賦值上補登記推斷型別，
// 讓後續依賴該變數型別的冗餘判定也能解析跨模組 std 呼叫。只在原規則的局部
// varTypes 表上工作，不影響任何溢位／索引 lint。
func registerRedundancyLetAlias(name string, value parser.Expression, varTypes map[string]string) {
	if name == "" || varTypes == nil || varTypes[name] != "" {
		return
	}
	if t := redundancyValueTypeForLet(value, varTypes); t != "" {
		varTypes[name] = t
	}
}

// registerRedundancyMultiAssign 處理 `q, r = num-s.div-mod(den-s)`：把各目標登記成
// std 宣告的第 n 個回傳型別（推不出就不登記，保守）。只認裸識別符目標。
func registerRedundancyMultiAssign(s *parser.MultiAssignStatement, varTypes map[string]string) {
	if s == nil || varTypes == nil || len(s.Targets) == 0 {
		return
	}
	results := stdCallResultTypes(s.Value, varTypes)
	if len(results) != len(s.Targets) {
		// 單回傳值的多重賦值（极少見）或推斷落空：不登記。
		if len(results) != 1 {
			return
		}
	}
	for i, target := range s.Targets {
		id, ok := target.(*parser.Identifier)
		if !ok || id.Value == "" {
			continue
		}
		rt := results[i%len(results)]
		if rt == "" || varTypes[id.Value] != "" {
			continue
		}
		varTypes[id.Value] = rt
	}
}
