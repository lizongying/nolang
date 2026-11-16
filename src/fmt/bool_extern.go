package fmt

// bool_extern.go — boolComparisonFacts 的跨模組證據來源。
//
// expr.go 的 boolComparisonFacts 原本刻意只認本檔證據（「known simplifies,
// unknown does not」），因為 `no fmt` 拿不到跨模組簽名。ParseSourceWithConfig
// 補上注入後，sem.ExternFuncSigs / ExternMethodSigs 會帶 std 與本地模組的
// 結果型別；本檔案把那兩張表轉成呼叫結果型別，並提供模組限定呼叫的查表鍵。
//
// 重要：option 硬門禁（boolOperandIsOptionTyped）完全不動。跨模組呼叫回
// ?T 時 classify 會把變數標進 option，`x == true` 依舊保留；只有明確回傳
// 純 bool 的跨模組呼叫才會被化簡。未注入配置的單檔解析拿不到這兩張表，
// 所有既有的「未知 → 不化簡」測試行為逐位元組不變。

import (
	"strings"

	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// externResultTypes 把語義副表裡的跨模組簽名轉成「呼叫名 → 結果型別」。
// FuncSigs 先放、MethodSigs 只補空缺，與 build 的合併順序一致。
func externResultTypes(sem *parser.SemanticContext) map[string]*ty.Type {
	out := make(map[string]*ty.Type)
	if sem == nil {
		return out
	}
	for _, table := range []map[string][]string{sem.ExternFuncSigs, sem.ExternMethodSigs} {
		for name, rets := range table {
			if len(rets) == 0 || rets[0] == "" {
				continue
			}
			if _, exists := out[name]; exists {
				continue
			}
			if t := ty.Parse(rets[0]); t.IsValid() {
				out[name] = t
			}
		}
	}
	return out
}

// callResultName 取出呼叫目標的查表鍵：
//
//   - 裸函式 `foo()` → "foo"
//   - 模組限定 `fs.write-str()` → "fs.write-str"
//   - 方法呼叫 `s.starts-with()` → "s.starts-with"（以**變數名**為鍵；
//     簽名表是以 `str.starts-with` 這類**型別名**登記，需由
//     externMethodResultsForVars 依 Sem 已推斷的變數型別補一組精確鍵）
//
// 無法判定（例如呼叫結果再呼叫）時回 false，呼叫端一律降級為 unknown。
func callResultName(call *parser.CallExpression) (string, bool) {
	if call == nil || call.Function == nil {
		return "", false
	}
	switch fn := call.Function.(type) {
	case *parser.Identifier:
		return fn.Value, true
	case *parser.DotExpression:
		if id, ok := fn.Receiver.(*parser.Identifier); ok {
			return id.Value + "." + fn.Property, true
		}
	}
	return "", false
}

// externMethodResultsForVars 補上「變數 receiver 的方法呼叫」的結果型別證據。
//
// callResultName 對 `s.starts-with('a')` 產生 `s.starts-with`，但
// ExternMethodSigs 的鍵是 `str.starts-with`（型別名），兩者永遠對不上——
// 實測帶 cfg 時 `ok = s.starts-with(...)` 的 ok 在 VarTypes / FuncVarTypes
// 裡都是空的，boolFacts 只能判 unknown，`ok == true` 因此不被化簡。
//
// 這裡**不做**「方法名全域唯一」的猜測：同名方法完全可能一個回 bool、另一個
// 回 ?bool，誤判就會把 option 當成純 bool 化簡，撞上 option 硬門禁在擋的
// option-as-i1 MIR crash。只按 parser 已經寫進 Sem 的變數型別精確合成
// `<變數名>.<方法名>`；同名變數在不同作用域被推成不同型別時直接跳過
// （boolFacts 是 name-keyed，分不出作用域）。精確鍵仍由 externResultTypes
// 優先提供，這裡只補空缺。
func externMethodResultsForVars(sem *parser.SemanticContext) map[string]*ty.Type {
	out := make(map[string]*ty.Type)
	if sem == nil || len(sem.ExternMethodSigs) == 0 {
		return out
	}

	vars := make(map[string]string) // 變數名 -> 型別名
	conflict := make(map[string]bool)
	record := func(name string, t *ty.Type) {
		if name == "" || t == nil || !t.IsValid() {
			return
		}
		tn := t.String()
		if prev, ok := vars[name]; ok {
			if prev != tn {
				conflict[name] = true
			}
			return
		}
		vars[name] = tn
	}
	for name, t := range sem.VarTypes {
		record(name, t)
	}
	for _, scope := range sem.FuncVarTypes {
		for name, t := range scope {
			record(name, t)
		}
	}

	for key, rets := range sem.ExternMethodSigs {
		if len(rets) == 0 || rets[0] == "" {
			continue
		}
		t := ty.Parse(rets[0])
		if !t.IsValid() {
			continue
		}
		dot := strings.LastIndex(key, ".")
		if dot <= 0 || dot == len(key)-1 {
			continue
		}
		owner, method := key[:dot], key[dot+1:]
		for varName, typeName := range vars {
			if conflict[varName] || typeName != owner {
				continue
			}
			out[varName+"."+method] = t
		}
	}
	return out
}
