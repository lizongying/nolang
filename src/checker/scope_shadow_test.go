package checker

import "testing"

// TestLocalLetShadowsTopLevelSameName 守護 15w45dqk 同名遮蔽誤報：
// 函式內 `h0 u64` 必須遮蔽頂層 `h0 u32`，大整數指派不該被當成 i64 → u32 窄化。
// 重構前 varTypes 快照複製頂層 + 「已存在不覆寫」會讓局部 u64 被頂層 u32 擋住。
func TestLocalLetShadowsTopLevelSameName(t *testing.T) {
	src := `h0 u32 = 0

test-shadow = () {
    h0 u64 = 0
    h0 = 7640891576956012808
}
test-shadow()`
	res := ValidateTypes(parseProg(t, src))
	if n := countTrace(res, "15w45dqk"); n != 0 {
		t.Fatalf("函式內 u64 應遮蔽頂層同名 u32，實際 %d 條 15w45dqk: %+v", n, res)
	}
}

// TestResultParamShadowsTopLevelSameName 結果參數（最內幀）同名頂層時，
// 其宣告型別優先，指派不該報窄化。
func TestResultParamShadowsTopLevelSameName(t *testing.T) {
	src := `h0 u32 = 0

f = () (h0 u64) {
    h0 = 7640891576956012808
}
f()`
	res := ValidateTypes(parseProg(t, src))
	if n := countTrace(res, "15w45dqk"); n != 0 {
		t.Fatalf("結果參數 u64 應遮蔽頂層同名 u32，實際 %d 條 15w45dqk: %+v", n, res)
	}
}

// TestFunctionLocalsDoNotLeak：兩函式同名不同型別互不污染；
// 且函式內區域宣告在函式結束後不外洩到頂層 varTypes。
func TestFunctionLocalsDoNotLeak(t *testing.T) {
	src := `f = () {
    v u64 = 7640891576956012808
}
g = () {
    v u8 = 255
}
f()
g()`
	res := ValidateTypes(parseProg(t, src))
	if n := countTrace(res, "15w45dqk"); n != 0 {
		t.Fatalf("兩函式同名不同型別不該互相污染，實際 %d 條 15w45dqk: %+v", n, res)
	}
}

// TestScopeTableBindPopRestore 直接單測 scopeTable 的 declare/resolve 分流。
func TestScopeTableBindPopRestore(t *testing.T) {
	top := map[string]string{"h0": "u32"}
	st := newScopeTable(top)

	if got, ok := st.Resolve("h0"); !ok || got != "u32" {
		t.Fatalf("頂層解析失敗: got=%q ok=%v", got, ok)
	}
	if st.BoundHere("h0") {
		t.Fatal("頂層綁定不該算在目前幀")
	}

	st.Push()
	st.Bind("h0", "u64")
	if got, _ := st.Resolve("h0"); got != "u64" {
		t.Fatalf("Bind 後應解析到 u64，實際 %q", got)
	}
	if !st.BoundHere("h0") {
		t.Fatal("Bind 後 BoundHere 應為真")
	}
	// flat 直接引用頂層 map（頂層 let 需寫回供後續頂層陳述可見），
	// 故函式幀 Bind 期間頂層 map 會被暫時遮蔽；正確性由 Pop 還原保證。
	if top["h0"] != "u64" {
		t.Fatalf("Bind 期間 flat 應遮蔽為 u64，實際 %q", top["h0"])
	}

	st.Pop()
	if got, _ := st.Resolve("h0"); got != "u32" {
		t.Fatalf("Pop 後應還原頂層 u32，實際 %q", got)
	}
	if top["h0"] != "u32" {
		t.Fatalf("Pop 後頂層 map 應還原 u32，實際 %q", top["h0"])
	}
	if st.BoundHere("h0") {
		t.Fatal("Pop 後目前幀不該再有 h0")
	}
}
