package mir

import "testing"

// `?=`（`a ?= a * 16 + b`）上的 `#{overflow=wrap}` 必須真的影響語意。
//
// 缺陷（已修）：UnwrapAssignStatement 既沒有 OverflowMode 欄位，也不在
// parser.setStmtOverflowMode 的型別分支裡；降糖把右式搬進合成的
// `__unwrap_N = <rhs>` 時也沒有把模式帶過去 ⇒ HIR 的 KLet 旗標為空 ⇒ 這裡的
// Inst.OvfAnnotated 永遠是 false ⇒ 標了 wrap 的算術仍然走預設「溢出回 err」。
// 實測（修前）：`a i64 = i64.MAX` 後 `a ?= a + 1`，帶 wrap 仍得到
// `err=integer overflow`；修後得到 ok(-2^63)，與不帶註解的 err 明確不同。
//
// 本測試釘住 MIR 層的判據：帶註解時該算術指令必須有 OvfAnnotated。
func TestUnwrapAssignHonoursOverflowAnnotation(t *testing.T) {
	arithOvf := func(mod *Module) (annotated, total int) {
		for i := range mod.Insts {
			switch mod.Insts[i].Op {
			case OpAdd, OpSub, OpMul, OpDiv, OpMod:
				total++
				if mod.Insts[i].OvfAnnotated {
					annotated++
				}
			}
		}
		return annotated, total
	}

	// 末尾必須有呼叫點：未被呼叫的函式會被 LowerHIR 當死碼整支略過，
	// 模組裡只剩一條 `invalid` 指令，斷言會空轉。
	body := func(ann string) string {
		return "f = (b i64) (r ?i64) {\n" +
			"    a i64 = 1\n" +
			ann +
			"    a ?= a * 16 + b\n" +
			"    r = a\n" +
			"}\n" +
			"\n" +
			"f(2): {\n" +
			"    ok -> _\n" +
			"    -> _\n" +
			"}\n"
	}

	annotated, total := arithOvf(mustLower(t, body("    #{overflow=wrap}\n")))
	if total == 0 {
		t.Fatal("no integer arithmetic lowered — fixture no longer exercises the path")
	}
	if annotated != total {
		t.Errorf("`#{overflow=wrap}` on `?=` did not reach MIR: %d/%d arithmetic insts carry OvfAnnotated", annotated, total)
	}

	// 沒有註解時，預設必須是「未標註」＝溢出回 err（option），不可被靜默改成 wrap。
	annotated, total = arithOvf(mustLower(t, body("")))
	if annotated != 0 {
		t.Errorf("unannotated `?=` arithmetic must not be OvfAnnotated: %d/%d", annotated, total)
	}
}

func mustLower(t *testing.T, src string) *Module {
	t.Helper()
	mod, diags := lowerSrcWithDiags(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lowering diagnostics: %v", diags)
	}
	return mod
}
