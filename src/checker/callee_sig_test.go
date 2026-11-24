package checker

// callee_sig_test.go — 「單一簽名真源」的等價性守門。
//
// 這組測試不啟用任何新診斷，只驗證 resolveCalleeSig 對三類來源的歸一化
// 與既有 helper 逐字等價；它是後續把 D1 擴到 builtin 實參 / setfield 的
// 安全網：若 resolver 讓既有判定改變，這裡必須先紅。

import (
	"testing"

	"github.com/lizongying/nolang/parser"
)

func testCallIdent(name string) *parser.CallExpression {
	return &parser.CallExpression{Function: &parser.Identifier{Value: name}}
}

func testCallMethod(recv, method string) *parser.CallExpression {
	return &parser.CallExpression{Function: &parser.DotExpression{
		Receiver: &parser.Identifier{Value: recv},
		Property: method,
	}}
}

// TestResolveCalleeSigLocal 本地函式與本地方法都必須從 funcSig 取得
// Params / Results，且 origin 正確區分。
func TestResolveCalleeSigLocal(t *testing.T) {
	sigs := map[string]*funcSig{
		"p": {
			ParamTypes:  []paramInfo{{Name: "v", Type: "i64"}},
			ResultTypes: []paramInfo{{Type: "?i64"}},
		},
		"box.get": {
			ParamTypes:  []paramInfo{{Name: "self", Type: "box"}, {Name: "i", Type: "i64"}},
			ResultTypes: []paramInfo{{Type: "?byte"}},
		},
	}
	vt := map[string]string{"b": "box"}

	got := resolveCalleeSig(testCallIdent("p"), sigs, vt, nil, "")
	if got.Origin != calleeLocalFunc || got.Key != "p" || got.optionResult() != "?i64" {
		t.Fatalf("local func: origin=%s key=%q opt=%q", got.Origin, got.Key, got.optionResult())
	}
	if len(got.Params) != 1 || got.Params[0] != "i64" {
		t.Fatalf("local func params = %v, want [i64]", got.Params)
	}

	got = resolveCalleeSig(testCallMethod("b", "get"), sigs, vt, nil, "")
	if got.Origin != calleeLocalMethod || got.Key != "box.get" || got.optionResult() != "?byte" {
		t.Fatalf("local method: origin=%s key=%q opt=%q", got.Origin, got.Key, got.optionResult())
	}
	if len(got.Params) != 2 || got.Params[0] != "box" {
		t.Fatalf("local method params = %v, want [box i64] (self kept for caller to skip)", got.Params)
	}
}

// TestResolveCalleeSigBuiltinSlice slice-only builtin（push）必須標 Storage，
// 並把元素位置標成泛型變數，不能把 registry 的 i64 當語言層宣告型別。
func TestResolveCalleeSigBuiltinSlice(t *testing.T) {
	vt := map[string]string{"b": "[]byte"}
	got := resolveCalleeSig(testCallMethod("b", "push"), nil, vt, nil, "")
	if got.Origin != calleeBuiltin || !got.Storage {
		t.Fatalf("[]byte.push: origin=%s storage=%v, want builtin+storage", got.Origin, got.Storage)
	}
	if got.GenericElem != "t" {
		t.Fatalf("[]byte.push generic elem = %q, want t", got.GenericElem)
	}
}

// TestResolveCalleeSigStdEquivalence std 簽名表可用時，resolver 的 option
// 判定必須與既有 stdMethodOptionResult / stdFuncOptionResult 完全一致。
// 裸 go test（無 StdSigKey）跳過：既有 helper 在這種二進位下也安靜回 ""。
func TestResolveCalleeSigStdEquivalence(t *testing.T) {
	if StdSigKey == "" {
		t.Skip("no injected StdSigKey: std tables unavailable (use make test)")
	}
	vt := map[string]string{"s": "str"}

	got := resolveCalleeSig(testCallMethod("s", "byte"), nil, vt, nil, "")
	want := stdMethodOptionResult("str", "byte")
	if got.optionResult() != want {
		t.Fatalf("str.byte: resolver=%q existing=%q", got.optionResult(), want)
	}
	if got.Key == "[]t.byte" {
		t.Fatalf("str.byte must not resolve through the slice fallback key")
	}

	got = resolveCalleeSig(testCallIdent("os.arg"), nil, nil, nil, "")
	want = stdFuncOptionResult("os", "arg")
	if got.optionResult() != want {
		t.Fatalf("os.arg: resolver=%q existing=%q", got.optionResult(), want)
	}
}

// TestResolveCalleeSigUnknown 推不出接收者 / 查不到鍵時回 unknown，
// 決不猜一個簽名出來（「寧可漏報、不可誤報」）。
func TestResolveCalleeSigUnknown(t *testing.T) {
	if got := resolveCalleeSig(nil, nil, nil, nil, ""); got.ok() {
		t.Fatalf("nil call must be unknown, got %+v", got)
	}
	got := resolveCalleeSig(testCallMethod("unknownVar", "whatever"), nil, nil, nil, "")
	if got.ok() {
		t.Fatalf("unknown receiver must be unknown, got %+v", got)
	}
}

// TestCalleeStdMethodKeysMapNotSlice `[str]i64` 是 map，不得因為以 '[' 開頭
// 就被加上 []t 回落鍵（上一輪 str 索引寫誤報正是同類 map/slice 混淆）。
func TestCalleeStdMethodKeysMapNotSlice(t *testing.T) {
	keys := calleeStdMethodKeys("[str]i64", "remove")
	if len(keys) != 1 || keys[0] != "[str]i64.remove" {
		t.Fatalf("map keys = %v, want only [str]i64.remove", keys)
	}
	keys = calleeStdMethodKeys("[]byte", "push")
	if len(keys) != 2 || keys[1] != "[]t.push" {
		t.Fatalf("slice keys = %v, want []byte.push + []t.push", keys)
	}
}
