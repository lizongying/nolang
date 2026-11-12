package build

import (
	"strings"
	"testing"
)

// builtin_layout_struct_ir_test.go — `#{buildin}` 佈局聲明**不得**在 IR 裡留下
// 影子型別。
//
// 這是整條鏈的最後一道釘子。src/std 的四份佈局聲明（str / vec / txt / arr）
// 若被模組前綴階段當成「本模組定義的型別」，就會依次出現：
//
//	Phase 2  txt        → txt.txt
//	Phase 3  txt.to-str → txt.txt.to-str   （呼叫端仍寫 txt.to-str）
//	Phase 4  型別引用   → txt.txt          （codegen 得到 %txt_txt）
//
// 前兩階段在 Go 單測裡各有一份（module_prefix_builtin_recv_test.go、
// builtin_struct_decl_test.go），但只有跑到真正發射 IR 才能證明第三件事：
// 四份聲明都沒有污染 codegen 的型別表。歷史症狀正是這裡——
//
//	%txt_txt = type ...                              ; 影子結構體
//	builtin str-len-bytes receiver %txt_txt: unsupported receiver type [aywuxg5w]
//	EmitLLVM: unknown callee txt.to-str in func t-from [4qhyyund]
//
// 所以本檔同時檢查「有」與「沒有」：真實型別必須**恰好一份**，影子名必須**零**。

// countSub 數 needle 在 s 裡的出現次數（非重疊）。
func countSub(s, needle string) int {
	if needle == "" {
		return 0
	}
	n := 0
	for i := 0; ; {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			return n
		}
		n++
		i += j + len(needle)
	}
}

// TestBuiltinLayoutStructsEmitNoShadowTypes 用一份同時觸及 txt / str / vec / arr
// 的程式，斷言 IR 裡的真實型別各只有一份、且沒有任何 `<mod>_<name>` 影子名。
func TestBuiltinLayoutStructsEmitNoShadowTypes(t *testing.T) {
	src := `layout-probe = (t txt, s str) (out str) {
    b = t.to-bytes()
    o = t.to-str()
    out = o
    k = b.len()
}
t txt = 'hi'
s str = 'yo'
r = layout-probe(t, s)
v []i64 = [1, 2, 3]
a [3]i64 = [4, 5, 6]
nv = v.len()
na = a.len()
print(r.len-bytes(), nv, na)
`
	// sourcePath 標成 std 檔案，讓 txt/str 的方法定義（receiver 為內建型別）
	// 與 bin/no 走同一條合法路徑。
	llvmIR := compileWith(t, src, "src/std/txt.no")

	// --- 「有」：真實佈局各恰好一份 -----------------------------------------
	for _, want := range []struct {
		decl string
		why  string
	}{
		{"%txt = type", "the txt descriptor must be emitted exactly once"},
		{"%str-long = type", "str's physical header is %str-long (len/cap/data)"},
		{"%vec = type", "vec's descriptor is 16 bytes: i32 len, i32 cap, i64 data"},
	} {
		if got := countSub(llvmIR, want.decl); got != 1 {
			t.Errorf("%q appears %d times in the IR, want exactly 1 (%s)", want.decl, got, want.why)
		}
	}

	// --- 「沒有」：影子型別一個都不許有 -------------------------------------
	for _, bad := range []string{"txt_txt", "str_str", "arr_arr", "vec_vec"} {
		if got := countSub(llvmIR, bad); got != 0 {
			t.Errorf("shadow type %q appears %d times in the IR; the `#{buildin}` "+
				"layout declaration leaked into codegen (it is documentation only, "+
				"the real layout is the MIR descriptor)", bad, got)
		}
	}

	// --- 方法名：呼叫端與定義端必須同名 -------------------------------------
	if countSub(llvmIR, "@txt_to_str") == 0 {
		t.Error("no @txt_to_str in the IR: txt.to-str was renamed away (the callee is " +
			"still `txt.to-str` at every call site)")
	}
	if got := countSub(llvmIR, "@txt_txt_to_str"); got != 0 {
		t.Errorf("@txt_txt_to_str appears %d times; the definition was module-prefixed "+
			"but the call sites were not", got)
	}
}
