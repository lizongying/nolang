package mir

import (
	"strings"
	"testing"
)

// 🔴 2026-11-05：`os.arg(i + 1)` 曾讓整個 binary 編譯失敗。
//
// 症狀：codegen 發出 `add %str-long %lv.29, %lv.30`，LLVM 拒收
// （"'%lv.29' defined with type 'i64' but expected '%str-long = type { i64, i64,
// ptr }'"）。
//
// 根因：`os.arg` 是 **純內建**（`#{buildin}`，沒有 HIR 身體），所以它不在
// `l.funcNames` 裡，`paramRawTypesOfCallee` 對它回傳 nil——也就是「引數位置的
// 預期型別」是**未知**的。於是 enclosing 陳述的型別提示（`x ?str = …` 的 `?str`）
// 洩漏進了引數：`i + 1` 這個 infix 被降成**字串串接**，於是 codegen 拿 `%str-long`
// 當加法的操作數型別。
//
// 修法：純內建的參數型別在 **builtin registry** 裡（`os.arg` → `(idx i64)`），
// 那也正是呼叫被 dispatch 進去的同一張表——所以那些型別就是引數位置的預期型別。
// 見 builtinParamTypes 與 lowerCallArgsWith。
//
// 本檔釘住兩件事：
//  1. builtinParamTypes 只回**全域**內建的參數型別（方法內建的 registry 條目列的
//     是方法自己的參數，與 argv[0]（接收者）對不齊，必須排除）。
//  2. 一個帶**算術引數**的全域內建呼叫，其引數不得被降成字串型別。
func TestBuiltinParamTypesForGlobalBuiltins(t *testing.T) {
	// os.arg → (idx i64)
	got := builtinParamTypes("os.arg")
	if len(got) != 1 || got[0] != "i64" {
		t.Fatalf("os.arg params must resolve to [i64], got %v", got)
	}
	// with-len → (len i64)
	got = builtinParamTypes("with-len")
	if len(got) != 1 || got[0] != "i64" {
		t.Fatalf("with-len params must resolve to [i64], got %v", got)
	}
	// 方法內建必須排除：registry 條目列的是方法自己的參數，而 argv[0] 是接收者。
	if got := builtinParamTypes("str.byte"); got != nil {
		t.Fatalf("a method builtin's params must not be used as the argument list, got %v", got)
	}
	// 未知內建 → 不猜。
	if got := builtinParamTypes("no-such-builtin"); got != nil {
		t.Fatalf("unknown builtin must not resolve, got %v", got)
	}
	// 空 callee → 不猜。
	if got := builtinParamTypes(""); got != nil {
		t.Fatalf("empty callee must not resolve, got %v", got)
	}
}

// TestGlobalBuiltinArithArgumentKeepsIntegerType 釘住症狀本身：帶算術引數的全域
// 內建呼叫，其引數的 infix 必須維持整數型別，不得被 enclosing 的 option 提示帶成
// 字串（那會產生 `add %str-long <i64>, <i64>` 的無效 IR）。
//
// `probe` 必須從一個**運行期值**呼叫點到達，否則 LowerHIR 的可達性驅動不會把它
// 放進模組，斷言就成了空轉。
func TestGlobalBuiltinArithArgumentKeepsIntegerType(t *testing.T) {
	const src = `probe = (i i64) (x ?str) {
    x = os.arg(i + 1)
}

i i64 = 0
p = probe(i)
p: {
    ok -> print(it)
    -> print('nil')
}
`
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	ll, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM failed: %v", err)
	}
	// The invalid form: an `add` whose operand type is the string struct.
	if strings.Contains(ll, "add %str-long") {
		t.Fatalf("the arithmetic argument was typed as str — emitted invalid IR:\n%s", ll)
	}
	// The correct form: an integer add inside @probe.
	pb := extractDefineBlock(t, ll, "@probe")
	if pb == "" {
		t.Fatalf("@probe not found in the emitted module:\n%s", ll)
	}
	if !strings.Contains(pb, "add i64") {
		t.Fatalf("@probe: the index arithmetic must be an integer add\n%s", indented(pb))
	}
}
