package checker

import "testing"

// 本檔案釘住「實參本身是一次呼叫」時的型別解析（2026-11-12）。
//
// 背景：`resolveExprType` 的 default 分支呼叫 `inferExprType(expr, varTypes, nil, "")`
// —— 函式回傳型別表刻意傳 nil。於是**任何使用者函式/方法的呼叫結果**當實參時都
// 解析成空字串，`checkCallArgsInExpr` 的兩道檢查（?T 裸傳 3604icnf、型別相容
// 6fgg3htw）雙雙 `continue` 跳過。
//
// 後果不只是漏報：`?f64` 傳給 `?str` 參數時兩者的 LLVM 佈局不同
// （`{ i8, [1 x i64] }` vs `{ i8, [2 x i64] }`），檢查期靜默通過，到 codegen 才由
// LLVM 驗證器擋下，訊息是 "different aggregate layouts cannot be passed by
// pointer"，完全看不出是哪一行原始碼寫錯（實例：test/std/json.no 的
// `i64-of(it.obj-key(3))`、test/std/yaml.no 的 `s-of(yaml.to-f64('abc'))`）。
//
// 修法：`resolveArgExprType` 在 resolveExprType 回空時，再從 `sigs`（本 program
// 的簽名表）與 std 簽名表查被呼叫者的第一回傳型別；查不到或有歧義仍回空字串
// （維持「不確定就沉默」的既有契約）。

// TestCallArgTypeResolvedFromCallee：形參/實參型別不符且實參是呼叫時，必須報
// 6fgg3htw，而不是放行到 codegen。
func TestCallArgTypeResolvedFromCallee(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 純量：q 回 i64，p 收 str。
			name: "scalar_i64_into_str",
			src: `p = (v str) {
    print(v)
}

q = () (out i64) {
    out = 42
}

p(q())`,
		},
		{
			// option 佈局：g 回 ?str，f 收 ?i64（json 那個形狀的縮影）。
			name: "option_str_into_option_i64",
			src: `f = (v ?i64) (out i64) {
    out = 0
    v: {
        ok -> out = it

        nil -> out = 1

        err -> out = 2
    }
}

g = () (out ?str) {
    out = 'abc'
}

f(g())`,
		},
		{
			// 內建呼叫當實參（`len` 已在 inferExprType 解得 i64），確認新舊兩條
			// 路徑不會互相遮蔽。
			name: "builtin_i64_into_str",
			src: `p = (v str) {
    print(v)
}

p(len('abc'))`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateFuncArgs(parseProg(t, c.src), "")
			if n := countTrace(results, "6fgg3htw"); n == 0 {
				t.Fatalf("call-expression argument with a mismatched type must be rejected, got 0; all: %v", results)
			}
		})
	}
}

// TestCallArgTypeLegalShapes 是反向守衛：型別一致（或只是 option payload 的
// 既有寬鬆形態）時不得誤報。
func TestCallArgTypeLegalShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "same_type",
			src: `p = (v str) {
    print(v)
}

q = () (out str) {
    out = 'abc'
}

p(q())`,
		},
		{
			// `x.byte(i)` 回 ?byte 傳給 i64 參數：codegen 會自動取 payload，
			// 這不是「?T 傳給 ?U」的佈局衝突（std 有 13 處這種既有寫法）。
			name: "option_byte_payload_into_i64",
			src: `p = (v i64) {
    print(v)
}

f = (s str, i i64) {
    p(s.byte(i))
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results := ValidateFuncArgs(parseProg(t, c.src), "")
			if n := countTrace(results, "6fgg3htw"); n != 0 {
				t.Fatalf("legal shape must not be rejected, got %d 6fgg3htw; all: %v", n, results)
			}
		})
	}
}
