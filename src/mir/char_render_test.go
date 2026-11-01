package mir

import (
	"testing"
)

// char_render_test.go — char 的 i32 加寬必須**有號**，兩條渲染路徑都要一致。
//
// 同一個 char 值有兩條渲染路徑：
//
//	print(c)      → codegen 的 print 分支，明確 sext 到 i64 再 call @print_i64
//	print('{c}')  → lowerFormatField 選 fmt-int，參數經 OpCast 從 i32 加寬到 i64
//
// 兩條路徑的符號判據原本不同步：print 分支按 `case "i32"` 明確 sext，而
// emitCast 的判據是「raw 名以 i 開頭」，`char` 不以 i 開頭 ⇒ 被當成無號 zext。
// 結果是**同一個值兩種輸出**：`print(c)` 印 -1，`print('{c}')` 印 4294967295。
//
// 這裡釘兩件事：判據本身（wideningIsSigned），以及插值路徑真的會產生那道 cast。
// 真正的輸出由 e2e 的 tests/char-render.no 守（本套件沒有 std，emit 不出 fmt-int）。

// TestWideningIsSignedPerType 判據逐型別釘死。`char` 與 `i32` 必須同結論；
// `byte`/`u8`/`i8` 必須零擴展（MIR 把三者映到同一個 LLVM i8，無號位元組是語料主流）；
// `i1` 是 bool，渲染成 1/0。
func TestWideningIsSignedPerType(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"i16", true},
		{"i32", true},
		{"i64", true},
		{"char", true}, // 與 i32 同結論 —— 這正是本修正
		{"i8", false},
		{"i1", false},
		{"u8", false},
		{"u16", false},
		{"u32", false},
		{"u64", false},
		{"byte", false},
		{"bool", false},
		// `int` 以 `i` 開頭，本來就被前綴判據當成有號（既有行為，非本修正）。
		// `uint` 以 `u` 開頭 ⇒ 零擴展。
		{"int", true},
		{"uint", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := wideningIsSigned(tc.raw); got != tc.want {
			t.Errorf("wideningIsSigned(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestCharInterpolationCastsCharToI64 插值路徑必須真的產生 char→i64 的 OpCast；
// 沒有這道 cast，fmt-int 會拿到 i32 而在 out-parameter store 上型別不符。
func TestCharInterpolationCastsCharToI64(t *testing.T) {
	src := "c char = \"A\"\nprint('{c}')\n"
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) > 0 {
		t.Fatalf("unexpected lowering diagnostics: %+v", diags)
	}
	found := false
	for _, fn := range mod.Funcs {
		for _, bid := range fn.Blocks {
			for _, iid := range mod.Blocks[bid].Insts {
				inst := mod.Insts[iid]
				if inst.Op != OpCast || len(inst.Args) == 0 {
					continue
				}
				src := mod.Value(inst.Args[0])
				dst := mod.Value(inst.Dst)
				if src == nil || dst == nil {
					continue
				}
				st, dt := mod.Type(src.Type), mod.Type(dst.Type)
				if st == nil || dt == nil {
					continue
				}
				if st.Raw == "char" && dt.Raw == "i64" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("char interpolation does not widen char to i64 via OpCast")
	}
}
