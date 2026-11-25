package mir

import "testing"

// 危綁 b：match arm 體內「手寫」的 let（非合成、無 FlagArmBinding）若與可見
// 模組全域同名，舊行為會走全局寫回分支——把值逕直寫進全域槽位。arm 內外語義
// 因此不同：外面以為自己改的是本地，實則改了全域（arm 結束後/global 之後
// 讀到被改寫的值，且結果取決於有沒有走過那個 arm）。
//
// 修復：hir2mir 的寫回閘門加 l.matchDepth == 0；arm 內為名字建立臂局部陰影
// （armScopedBind{name, absent:true}），arm 結束時由 restoreArmScoped 刪除，
// 使 arm 之後仍解析到全域。合成臂綁定（armBinding）走既有路徑，不受影響。
//
// 觀測點選在 MIR 而非 stdout：arm-shadow 函式對全域 cnt 槽位的 move 數必須是
// 0（pre-fix 是 1）；同檔 plain-write 做非空洞對照——不在 arm 內的一般賦值
// 仍須寫回全域，證明這個計數真的能看到全局寫回，而不是全局寫回被整個拿掉。

const armShadowGlobalSrc = `cnt i64 = 7
arm-shadow = () (v i64) {
    o ?i64 = ok(3)
    o: {
        ok -> {
            cnt = 99
        }
        ->
    }
    v = cnt
}
plain-write = () {
    cnt = cnt + 1
}
plain-write()
print(arm-shadow().to-str())
`

func TestMatchArmHandwrittenLetDoesNotWriteGlobal(t *testing.T) {
	mod := lowerForTest(t, armShadowGlobalSrc)
	slot := cntGlobalSlot(t, mod)

	if n := globalWriteBackMoves(mod, "arm-shadow", slot); n != 0 {
		t.Fatalf("`arm-shadow` performs %d move(s) into the module global `cnt` slot (id %d), want 0. "+
			"A hand-written `cnt = 99` inside a match arm must be an arm-scoped shadow "+
			"(armScopedBind{absent:true}, removed by restoreArmScoped when the arm ends), "+
			"not a write-back through the global slot: pre-fix the write-back gate ignored "+
			"matchDepth, so the arm silently mutated the global and `v = cnt` after the arm "+
			"read 99 instead of 7.\n%s", n, slot, mod.String())
	}

	// 非空洞對照：同樣是赋给 cnt，不在 arm 内的 plain-write 必须照常寫回全域。
	// 沒有這一半，上面那個 0 可能只是「全局寫回被整條路徑拿掉」的副作用。
	if n := globalWriteBackMoves(mod, "plain-write", slot); n < 1 {
		t.Fatalf("`plain-write` performs %d move(s) into the module global `cnt` slot (id %d), "+
			"want >= 1; a plain (non-arm) global accumulator write-back must keep working, "+
			"otherwise the 0 above proves nothing.\n%s", n, slot, mod.String())
	}
}
