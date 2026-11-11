package mir

import (
	"strings"
	"testing"
)

// 本檔案釘住 `#{overflow = ...}` 的**模式字串**從 HIR 旗標一路到 MIR 指令，
// 以及 codegen 真的依模式取值。
//
// 缺陷（2026-11-11 已修）：hir2mir 把五個 hir.FlagOverflow* 位用 ovfFlagMask
// OR 成單一 bool `OvfAnnotated`，codegen 只用它跳過 `*.with.overflow`，之後
// 直接發裸 add → wrap/clamp0/min/max/saturate 五種註解**全部等價於 wrap**。
// 現在 mode 解回 Inst.OvfMode，codegen 依 mode 在 i256 寬域取值（見
// emitAnnotatedOverflowValue）。端到端真值表（i64 上下溢 / i8 / u8 / 索引 /
// 取負）以 `no run` 實測，記錄在 .workbuddy/memory 與 glossary 詞條。

// arithModes 收集模組內所有算術指令的 (模式, 是否帶註解)。
func arithModes(mod *Module) map[string]int {
	out := map[string]int{}
	for i := range mod.Insts {
		switch mod.Insts[i].Op {
		case OpAdd, OpSub, OpMul, OpNeg:
			mode := mod.Insts[i].OvfMode
			if !mod.Insts[i].OvfAnnotated {
				mode = "<unannotated>"
			} else if mode == "" {
				mode = "<empty>"
			}
			out[mode]++
		}
	}
	return out
}

func overflowModeFixture(ann string) string {
	return "f = (a i64, b i64) (r i64) {\n" +
		ann +
		"    c = a + b\n" +
		"    r = c: {\n" +
		"        ok -> it\n" +
		"\n" +
		"        -> 0\n" +
		"    }\n" +
		"}\n" +
		"\n" +
		"print(f(1, 2))\n"
}

func TestOverflowModeSurvivesHIRToMIR(t *testing.T) {
	cases := []struct {
		ann  string
		want string
	}{
		{"", "<unannotated>"},
		{"    #{overflow = wrap}\n", "wrap"},
		{"    #{overflow = clamp0}\n", "clamp0"},
		{"    #{overflow = min}\n", "min"},
		{"    #{overflow = max}\n", "max"},
		{"    #{overflow = saturate}\n", "saturate"},
		// 型別前綴形式由 parser.NormalizeOverflowMode 歸一為方向，邊界按
		// 運算結果的實際型別決定（見 LSP hover 與 annotation.go 註釋）。
		{"    #{overflow = u8-max}\n", "max"},
	}
	for _, c := range cases {
		t.Run(strings.TrimSpace(c.ann), func(t *testing.T) {
			mod := mustLower(t, overflowModeFixture(c.ann))
			modes := arithModes(mod)
			if modes[c.want] == 0 {
				t.Fatalf("no arithmetic instruction carries mode %q: %v", c.want, modes)
			}
			for mode, n := range modes {
				if mode != c.want {
					t.Errorf("unexpected mode %q on %d arithmetic inst(s); all=%v", mode, n, modes)
				}
			}
		})
	}
}

// TestOverflowModeCodegenEmitsClamp 釘住「非 wrap 模式真的發出取值 IR」。
// wrap 必須維持歷史上的裸 add（IR 逐位元不變，std 全量測試依賴它）；其餘
// 四種必須出現 i256 寬域運算 + 截斷，且各自邊界常數不同。
func TestOverflowModeCodegenEmitsClamp(t *testing.T) {
	cases := []struct {
		mode      string
		wantClamp bool
		wantLit   string
	}{
		{"wrap", false, ""},
		{"clamp0", true, "i256 0,"},
		{"min", true, "i256 -9223372036854775808,"},
		{"max", true, "i256 9223372036854775807,"},
		{"saturate", true, "i256 -9223372036854775808,"},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			mod := mustLower(t, overflowModeFixture("    #{overflow = "+c.mode+"}\n"))
			ir, err := mod.EmitLLVM()
			if err != nil {
				t.Fatalf("EmitLLVM: %v", err)
			}
			hasWide := strings.Contains(ir, "= add i256 ") && strings.Contains(ir, "= trunc i256 ")
			if hasWide != c.wantClamp {
				t.Fatalf("i256 clamp present=%v, want %v", hasWide, c.wantClamp)
			}
			if c.wantLit != "" && !strings.Contains(ir, c.wantLit) {
				t.Errorf("IR does not contain %q (mode %q did not pick its value)", c.wantLit, c.mode)
			}
		})
	}
}

func TestOverflowIntBounds(t *testing.T) {
	cases := []struct {
		elem, dst, res string
		lo, hi         string
		unsigned       bool
		ok             bool
	}{
		{"i64", "", "i64", "-9223372036854775808", "9223372036854775807", false, true},
		{"u64", "", "i64", "0", "18446744073709551615", true, true},
		{"u8", "", "i64", "0", "255", true, true},
		{"i8", "", "i64", "-128", "127", false, true},
		{"u16", "", "i64", "0", "65535", true, true},
		{"i16", "", "i64", "-32768", "32767", false, true},
		{"u32", "", "i64", "0", "4294967295", true, true},
		{"i32", "", "i64", "-2147483648", "2147483647", false, true},
		// plain-int 路徑沒有 option 元素，退回 LLVM 結果型別。
		{"", "?i64", "i64", "-9223372036854775808", "9223372036854775807", false, true},
		// byte / char 是文件明定的特例，註解不管轄。
		{"byte", "", "i8", "", "", false, false},
		{"char", "", "i32", "", "", false, false},
		{"i128", "", "i64", "", "", false, false},
	}
	for _, c := range cases {
		lo, hi, unsigned, ok := overflowIntBounds(c.elem, c.dst, c.res)
		if ok != c.ok || unsigned != c.unsigned || lo != c.lo || hi != c.hi {
			t.Errorf("overflowIntBounds(%q,%q,%q) = (%q,%q,%v,%v), want (%q,%q,%v,%v)",
				c.elem, c.dst, c.res, lo, hi, unsigned, ok, c.lo, c.hi, c.unsigned, c.ok)
		}
	}
}

// TestOverflowModeNegReachesMIR：陣列讀避免常數傳播把整支函式折掉
// （`f(-9223372036854775808)` 會連函式一起消失，那條 fixture 測不到 OpNeg）。
func TestOverflowModeNegReachesMIR(t *testing.T) {
	const src = "xs [1]i64\n" +
		"xs[0] = -9223372036854775808\n" +
		"a = xs[0]: {\n" +
		"    ok -> it\n" +
		"\n" +
		"    -> 0\n" +
		"}\n" +
		"#{overflow = max}\n" +
		"c = -a\n" +
		"print(c)\n"
	mod := mustLower(t, src)
	found := false
	for i := range mod.Insts {
		if mod.Insts[i].Op == OpNeg {
			found = true
			if !mod.Insts[i].OvfAnnotated || mod.Insts[i].OvfMode != "max" {
				t.Errorf("OpNeg: OvfAnnotated=%v OvfMode=%q, want true/max",
					mod.Insts[i].OvfAnnotated, mod.Insts[i].OvfMode)
			}
		}
	}
	if !found {
		t.Fatal("no OpNeg lowered — fixture no longer exercises the path")
	}
	ir, err := mod.EmitLLVM()
	if err != nil {
		t.Fatalf("EmitLLVM: %v", err)
	}
	if !strings.Contains(ir, "= sub i256 ") {
		t.Error("annotated negation did not emit the i256 wide-domain subtraction")
	}
}

// mirMainBody 只取 _nolang_main 函式本文，避免 prelude / 其他 std 函式裡的
// trunc/sext 讓「有沒有做窄化」的斷言變成假陽性。
func mirMainBody(ir string) string {
	lines := strings.Split(ir, "\n")
	for i, l := range lines {
		if !strings.Contains(l, "define void @_nolang_main()") {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if lines[j] == "}" {
				return strings.Join(lines[i:j+1], "\n")
			}
		}
		return strings.Join(lines[i:], "\n")
	}
	return ir
}

// TestOverflowNarrowSignedWrapDirection 釘住「宣告寬度決定回繞值」：
// wrap 與 clamp0/min/max/saturate 的未箝位方向都必須給真正的 8-bit 回繞。
//
// 缺陷（2026-11-11 已修）：?i8/?u8/... 的 payload 一律以 i64 存放
// （optionPayloadLLVMType），而 emitArith / emitNeg 的 option 寫回只把
// **dstRaw** 交給 wrapIntToStorage。option 的宣告寬度其實在 **elemRaw**，
// dstRaw 常常是空的 ⇒ 不做窄化，`i8 -120 - 20` 存進 -140；同時 u8 的 plain
// 路徑是 `add i8`，250+10 自然回繞成 4，兩種路徑互相矛盾。
//
// 修法：overflowStorageRaw 在「已註解 + option 結果」時改傳 elemRaw，
// wrapIntToStorage 便發 `%cw.N = trunc i64 → i8` + `sext i8 → i64`。裸
// `sub i64` 仍保留（wrap 的 IR 形狀不變），i64/u64 與 byte/char 不受影響。
// 端到端真值見 .workbuddy/memory 與 glossary/data/annotations.noml。
func TestOverflowNarrowSignedWrapDirection(t *testing.T) {
	const head = "x i8 = -120\ny i8 = 20\n"
	cases := []struct {
		mode     string
		wantOp   string
		wantWide bool
	}{
		{"wrap", "= sub i64 ", false},
		{"max", "= sub i256 ", true},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			mod := mustLower(t, head+"#{overflow = "+c.mode+"}\nc = x - y\nprint(c)\n")
			ir, err := mod.EmitLLVM()
			if err != nil {
				t.Fatalf("EmitLLVM: %v", err)
			}
			body := mirMainBody(ir)
			if !strings.Contains(body, c.wantOp) {
				t.Fatalf("IR has no %q (mode %q changed its arithmetic shape)", c.wantOp, c.mode)
			}
			if hasWide := strings.Contains(body, "= sub i256 "); hasWide != c.wantWide {
				t.Fatalf("i256 wide-domain present=%v, want %v", hasWide, c.wantWide)
			}
			// 兩條路徑都必須在寫回 option payload 之前按宣告寬度窄化：
			// trunc i64 → i8、sext i8 → i64、store i64 %cw.*。
			for _, lit := range []string{"= trunc i64 ", " to i8", "= sext i8 ", " to i64"} {
				if !strings.Contains(body, lit) {
					t.Errorf("main body is missing %q — result not normalized to the declared i8 width", lit)
				}
			}
			stored := false
			for _, line := range strings.Split(body, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "store i64 %cw.") {
					stored = true
				}
			}
			if !stored {
				t.Error("option payload was stored without the width-normalized wrap value")
			}
			if !c.wantWide {
				return
			}
			// 邊界仍取自宣告寬度，不是 i64 payload 寬度。
			for _, lit := range []string{"icmp sgt i256 ", ", 127", "icmp slt i256 ", ", -128"} {
				if !strings.Contains(body, lit) {
					t.Errorf("IR is missing %q — i8 declared bounds not used", lit)
				}
			}
			wideTruncs := 0
			for _, line := range strings.Split(body, "\n") {
				if !strings.Contains(line, "= trunc i256 ") {
					continue
				}
				wideTruncs++
				if !strings.HasSuffix(strings.TrimSpace(line), " to i64") {
					t.Errorf("wide clamp truncated straight to a narrow type: %s", line)
				}
			}
			if wideTruncs == 0 {
				t.Error("no `trunc i256 ... to i64` emitted — the clamp result is not written back")
			}
		})
	}
}
