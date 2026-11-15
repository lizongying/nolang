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
//   - 方法呼叫 `s.starts-with()` → "s.starts-with"（簽名表也以
//     `type.method` 形式登記，兩種拼法都能命中）
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
