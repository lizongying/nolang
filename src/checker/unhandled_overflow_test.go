package checker

import "testing"

// 本檔案是 ValidateUnhandledOverflow 的回歸測試：未標註 #{overflow} 的整數運算
// 預設回傳 option<int>，若結果既沒被 ?= 上拋、也沒被宣告/返回為 ?T，即「沉默
// 泄漏」，必須報錯（編譯硬錯誤，TraceID = ovfhndld）。

const leakSrc = `main = () () {
    a i64 = 5
    b i64 = 10
    x = a + b
    io.outln(x)
}`

// TestUnhandledOverflowDetectsLeaks 驗證真正的沉默泄漏會被報錯。
func TestUnhandledOverflowDetectsLeaks(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantMin int
	}{
		{
			name:    "plain_assign_of_arithmetic",
			src:     leakSrc,
			wantMin: 1,
		},
		{
			name: "return_arithmetic_from_non_option_fn",
			src: `add = (a i64, b i64) (out i64) {
    out = a + b
}`,
			wantMin: 1,
		},
		{
			name: "increment_in_loop",
			src: `f = (n i64) () {
    i i64 = 0
    i < n -> {
        i = i + 1
    }
}`,
			wantMin: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ValidateUnhandledOverflow(parseProg(t, c.src), "src/app.no")
			if len(res) < c.wantMin {
				t.Fatalf("expected >= %d unhandled-overflow error(s), got %d: %+v", c.wantMin, len(res), res)
			}
			for _, r := range res {
				if r.TraceID != unhandledOverflowTraceID {
					t.Fatalf("unexpected TraceID %q: %+v", r.TraceID, r)
				}
			}
		})
	}
}

// TestUnhandledOverflowAllowed 驗證合法寫法不應誤報。
func TestUnhandledOverflowAllowed(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// 加 #{overflow=wrap} 注解回普通 int → 已處理。
			name: "annotated_with_overflow_wrap",
			src: `main = () () {
    a i64 = 5
    b i64 = 10
    #{overflow=wrap}
    x = a + b
    io.outln(x)
}`,
		},
		{
			// 函式回傳 ?T 且用 ?= 上拋 → 已處理。
			name: "unwrap_assign_propagates",
			src: `add = (a i64, b i64) (out ?i64) {
    out ?= a + b
}`,
		},
		{
			// 顯式宣告為 option 型別 → 已處理。
			name: "explicit_option_declaration",
			src: `f = (a i64, b i64) () {
    x ?i64 = a + b
    io.outln('done')
}`,
		},
		{
			// 字串拼接：str 的 `-` 不是整數運算，不產生 option，不得誤報。
			name: "string_concat_not_int_arith",
			src: `f = (seg str) () {
    r str = 'ab'
    r = r - seg
    io.outln(r)
}`,
		},
		{
			// 位元運算不屬於溢出的 + - * /，不得誤報。
			name: "bitwise_ops",
			src: `f = () () {
    v i64 = 1
    n i64 = 2
    seg i64 = v | (n & 0x3f)
    io.outln(seg)
}`,
		},
		{
			// 無號除法永不溢出（a / b <= a），不得誤報。
			name: "unsigned_division",
			src: `f = (a u64, b u64) (out u64) {
    out = a / b
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ValidateUnhandledOverflow(parseProg(t, c.src), "src/app.no")
			if len(res) != 0 {
				t.Fatalf("expected no error for %s, got: %+v", c.name, res)
			}
		})
	}
}

// TestUnhandledOverflowStdExempt 驗證標準庫豁免：標準庫內部大量依賴預設 option
// 語意，若視為未處理會讓每個 no build / no run 失敗。使用者程式必須仍被檢查。
// mainFile 為空/非 std 時恢復檢查，覆蓋「單檔 vet 主程式自身陳述 SourceFile 為空」
// 的情境（見 ast.go CommentedNode 註解）。
func TestUnhandledOverflowStdExempt(t *testing.T) {
	cases := []struct {
		mainFile string
		want     int
	}{
		{mainFile: "std/str.no", want: 0},
		{mainFile: "/repo/src/std/str.no", want: 0},
		{mainFile: "src/std/vec.no", want: 0},
		{mainFile: "src/cmd/ln/ln.no", want: 1},
		{mainFile: "src/app.no", want: 1},
		{mainFile: "", want: 1}, // 無來源資訊時保守檢查，避免漏報
	}
	for _, c := range cases {
		t.Run(c.mainFile, func(t *testing.T) {
			res := ValidateUnhandledOverflow(parseProg(t, leakSrc), c.mainFile)
			if len(res) != c.want {
				t.Fatalf("mainFile=%q: expected %d error(s), got %d: %+v", c.mainFile, c.want, len(res), res)
			}
		})
	}
}

// TestUnhandledOverflowWiredIntoLints 確認規則已接入 RunAllLints（no vet / LSP 路徑）。
func TestUnhandledOverflowWiredIntoLints(t *testing.T) {
	prog := parseProg(t, leakSrc)
	lints := RunAllLints(prog, LintOptions{SourcePath: "src/app.no"})
	found := false
	for _, l := range lints {
		if l.TraceID == unhandledOverflowTraceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("ValidateUnhandledOverflow not surfaced through RunAllLints")
	}
}
