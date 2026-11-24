package checker

// 本檔承載 checker 的「單一簽名真源」：把本地函式/方法、std 內嵌簽名表、
// Go builtin 註冊表三類呼叫簽名收斂到同一個查詢入口與同一套鍵正規化規則。
//
// 為什麼需要（2026-11-24）：
//   - 引數檢查走 lookupCalleeSig / stdMethodParamTypes / builtinSliceMethodParamTypes；
//     回傳型別推斷另走 inferExprType + stdMethodOptionResult / stdFuncOptionResult；
//     D1（option 不得隱式拆包）又各自散在算術、呼叫、索引、欄位四個 helper。
//   - 結果是同一個呼叫在「引數查得到、回傳查不到」時静默拆包（checker.go:5719
//     的註釋就是在記錄這個不對稱），而 2026-11-08 的 optionResultOfCallTarget
//     只是補丁，不是根治。
//
// 本檔第一步只做等價重構：resolveCalleeSig 回傳歸一化視圖，既有診斷行為不變。
// 後續把 D1 擴到 setfield 與 builtin 實參時，統一只問這一個入口。

import (
	"strings"

	"github.com/lizongying/nolang/builtin"
	"github.com/lizongying/nolang/parser"
	"github.com/lizongying/nolang/ty"
)

// calleeSigOrigin 標識簽名來自哪一張表；只用於診斷與除錯，不影響判定。
type calleeSigOrigin uint8

const (
	calleeUnknown calleeSigOrigin = iota
	calleeLocalFunc
	calleeLocalMethod
	calleeStdFunc
	calleeStdMethod
	calleeBuiltin
)

func (o calleeSigOrigin) String() string {
	switch o {
	case calleeLocalFunc:
		return "local-func"
	case calleeLocalMethod:
		return "local-method"
	case calleeStdFunc:
		return "std-func"
	case calleeStdMethod:
		return "std-method"
	case calleeBuiltin:
		return "builtin"
	default:
		return "unknown"
	}
}

// calleeSig 是一次呼叫的歸一化簽名視圖。
//
// Params / Results 都是**宣告**型別字串：
//   - 本地函式：funcSig.ParamTypes / ResultTypes 的 Type；
//   - std：StdSigEntry.Params / Results；
//   - builtin-only slice 方法：Params 是 storage shape（元素位置已標成
//     GenericElem 對應的型別變數），不是語言層宣告，Storage=true 標記之。
//
// Key 是正規化後的查表鍵（`os.arg`、`[]t.push`、`json.get-str`）；
// GenericElem 是接收者泛型變數（`[]t.eq` → "t"），空字串表示具體型別；
// Ambiguous 表示同名方法被多個 std 模組定義，鍵不可信，必須放棄判定。
type calleeSig struct {
	Origin       calleeSigOrigin
	Key          string
	Params       []string
	Results      []string
	ReceiverType string
	GenericElem  string
	Storage      bool
	Ambiguous    bool
}

func (s calleeSig) ok() bool { return s.Origin != calleeUnknown }

// optionResult 回傳「確定是 option」的第一個結果型別 `?T`；非 option、無結果、
// 或仍帶型別變數（泛型）時回 ""。與既有 optionResultOfSigEntry 完全同口徑
// （只認 Results[0]，不做「任一個結果是 option」的猜測）。
func (s calleeSig) optionResult() string {
	if len(s.Results) == 0 {
		return ""
	}
	r := s.Results[0]
	if !ty.IsOptionRaw(r) || mentionsTypeVar(r) {
		return ""
	}
	return r
}

// resolveCalleeSig 是 func/method/builtin 三類簽名的單一查询入口。
//
// 鍵規則集中在 calleeStdMethodKeys / calleeStdFuncSig，不再由每個 helper
// 各自拼字串：
//   - 本地：`sigs` 同名鍵（含 `mod.fn`、`base.method`、`uniqueMethodSuffix`
//     三種既有形態，沿用 lookupCalleeSig）；
//   - std func：`module.fn` 或裸名；
//   - std method：`recv.method`，slice 再試 `[]t.method`；ambiguous 跳過；
//   - builtin：slice storage shape 走 builtinSliceMethodParamTypes；
//     str/txt 裸名回落走 builtin.FindBuiltinMethod。
//
// 邊界（沿用既有「寧可漏報、不可誤報」原則）：
//   - 接收者型別推不出來 ⇒ 回 calleeUnknown；
//   - std 簽名表未暖表（StdSigKey 為空，例如裸 go test）⇒ 不碰 std 表，
//     避免 CollectStdModuleSignatures 的 os.Exit(1)；
//   - 本地 sigs 為空時仍可查 std/builtin（單檔 vet 的常態）。
func resolveCalleeSig(call *parser.CallExpression, sigs map[string]*funcSig, varTypes map[string]string, structFields map[string]map[string]string, selfType string) calleeSig {
	if call == nil || call.Function == nil {
		return calleeSig{}
	}
	switch fn := call.Function.(type) {
	case *parser.Identifier:
		if sig, ok := sigs[fn.Value]; ok {
			return calleeSigFromLocal(sig, fn.Value, calleeLocalFunc, "")
		}
		if e, key, ok := calleeStdFuncSig(fn.Value); ok {
			return calleeSigFromStd(e, key, calleeStdFunc, "")
		}
		return calleeSig{}

	case *parser.DotExpression:
		if fn.Property == "" {
			return calleeSig{}
		}
		// 多層模組路徑 `mymod.remote.fn`：根不是變數時按完整路徑查。
		if path, root, ok := dottedCallName(fn); ok {
			if _, isVar := varTypes[root]; !isVar {
				if sig, ok := sigs[path]; ok {
					return calleeSigFromLocal(sig, path, calleeLocalFunc, "")
				}
				if e, key, ok := calleeStdFuncSig(path); ok {
					return calleeSigFromStd(e, key, calleeStdFunc, "")
				}
			}
		}
		if recv, ok := fn.Receiver.(*parser.Identifier); ok {
			if recvType, isVar := varTypes[recv.Value]; isVar {
				return resolveMethodSig(recvType, fn.Property, sigs, structFields)
			}
			// 接收者是模組名（`os.arg`）：查模組限定鍵，不猜方法。
			key := recv.Value + "." + fn.Property
			if sig, ok := sigs[key]; ok {
				return calleeSigFromLocal(sig, key, calleeLocalFunc, "")
			}
			if e, k, ok := calleeStdFuncSig(key); ok {
				return calleeSigFromStd(e, k, calleeStdFunc, "")
			}
			return calleeSig{}
		}
		// 接收者是運算式（`(m._mul(10))._add(x)`）：先.resolveExprType，
		// 再退回 inferExprType（與 checkStdMethodCallArgs /
		// optionResultOfCallTarget 各自的既有口徑並集）。
		recvType := resolveExprType(fn.Receiver, varTypes, structFields)
		if recvType == "" {
			recvType = inferExprType(fn.Receiver, varTypes, nil, selfType)
		}
		if recvType == "" {
			return calleeSig{}
		}
		return resolveMethodSig(recvType, fn.Property, sigs, structFields)

	default:
		return calleeSig{}
	}
}

// resolveMethodSig 統一 "接收者型別 + 方法名" 的查表順序：
// 本地 → std → builtin。任何一步查到就回傳，不再讓引數與回傳各走一條路。
func resolveMethodSig(recvType, method string, sigs map[string]*funcSig, structFields map[string]map[string]string) calleeSig {
	base := baseTypeNameForMethod(recvType)
	localKey := base + "." + method
	if sig, ok := sigs[localKey]; ok {
		return calleeSigFromLocal(sig, localKey, calleeLocalMethod, recvType)
	}
	if e, key, ok := calleeStdMethodSig(recvType, method); ok {
		return calleeSigFromStd(e, key, calleeStdMethod, recvType)
	}
	// builtin-only slice 方法（目前實際只有 push）：Params 是 storage shape，
	// 不能當語言層宣告型別直接比對；標 Storage 讓消費者沿用既有寬鬆規則。
	if params, key := builtinSliceMethodParamTypes(recvType, method); len(params) > 0 {
		out := calleeSig{
			Origin:       calleeBuiltin,
			Key:          key,
			Params:       params,
			ReceiverType: recvType,
			Storage:      true,
			GenericElem:  genericElemOfKey(key),
		}
		return out
	}
	// str/txt 裸名 builtin（byte / push 等）在 Go 表裡以裸名註冊。
	if recvType == "str" || recvType == "txt" {
		if bm := builtin.FindBuiltinMethod(method); bm != nil {
			out := calleeSig{Origin: calleeBuiltin, Key: method, ReceiverType: recvType, GenericElem: genericElemOfKey(method)}
			for _, p := range bm.Params {
				out.Params = append(out.Params, p.String())
			}
			for _, r := range bm.Return {
				out.Results = append(out.Results, r.String())
			}
			return out
		}
	}
	return calleeSig{}
}

// calleeStdMethodKeys 是 std 方法鍵的**唯一**生成處：`recv.method`，slice 再加
// `[]t.method` 回落。必須比對兩個字符 "[]" —— map 寫成 `[K]V` 也以 '[' 開頭，
// 若只看第一個字符會把 `[str]i64.remove` 誤導到 slice 的整數索引參數。
func calleeStdMethodKeys(recvType, method string) []string {
	keys := []string{recvType + "." + method}
	if strings.HasPrefix(recvType, "[]") {
		keys = append(keys, "[]t."+method)
	}
	return keys
}

// stdTablesWarm 報告 std 簽名表是否可用。StdSigKey 為空代表不是 `make` 出的
// 二進位；此時 CollectStdModuleSignatures 會 os.Exit(1)，故所有 std 查詢都
// 必須先過這一關（與 stdMethodOptionResult / stdFuncOptionResult 同口徑）。
func stdTablesWarm() bool {
	if stdSigsCache != nil || stdMethodSigsCache != nil {
		return true
	}
	if StdSigKey == "" {
		return false
	}
	CollectStdModuleSignatures()
	return stdSigsCache != nil || stdMethodSigsCache != nil
}

// calleeStdMethodSig 查 std 方法表，與 stdMethodParamTypes / stdMethodOptionResult
// 逐字對稱（同鍵順序、同 ambiguous 跳過）。
func calleeStdMethodSig(recvType, method string) (StdSigEntry, string, bool) {
	if recvType == "" || method == "" || !stdTablesWarm() {
		return StdSigEntry{}, "", false
	}
	for _, key := range calleeStdMethodKeys(recvType, method) {
		if structMethodIsAmbiguous(key) {
			continue
		}
		if entry, ok := stdMethodSigsCache[key]; ok {
			return entry, key, true
		}
		if entry, ok := stdSigsCache[key]; ok {
			return entry, key, true
		}
	}
	return StdSigEntry{}, "", false
}

// calleeStdFuncSig 查 std 函式表（模組限定 `module.fn` 或裸名）。
func calleeStdFuncSig(name string) (StdSigEntry, string, bool) {
	if name == "" || !stdTablesWarm() {
		return StdSigEntry{}, "", false
	}
	if entry, ok := stdSigsCache[name]; ok {
		return entry, name, true
	}
	return StdSigEntry{}, "", false
}

func calleeSigFromLocal(sig *funcSig, key string, origin calleeSigOrigin, recvType string) calleeSig {
	out := calleeSig{Origin: origin, Key: key, ReceiverType: recvType}
	if sig == nil {
		return out
	}
	for _, p := range sig.ParamTypes {
		out.Params = append(out.Params, p.Type)
	}
	for _, r := range sig.ResultTypes {
		out.Results = append(out.Results, r.Type)
	}
	return out
}

func calleeSigFromStd(entry StdSigEntry, key string, origin calleeSigOrigin, recvType string) calleeSig {
	return calleeSig{
		Origin:       origin,
		Key:          key,
		Params:       append([]string(nil), entry.Params...),
		Results:      append([]string(nil), entry.Results...),
		ReceiverType: recvType,
		GenericElem:  genericElemOfKey(key),
	}
}
