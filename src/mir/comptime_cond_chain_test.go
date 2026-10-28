package mir

// comptime_cond_chain_test.go — 回归：花括号条件链 `{ c -> s }` 里的 `return`
// 不能被 comptime 折叠跨过。
//
// 背景（2026-10-28 实测）：`{ c -> s }` 这种带花括号的条件链会被降糖成一个
// **合成的** `if <常量> { … }`，条件不是 bool（真正的臂-if 在它里面）。折叠器原先
// 用单个 bool 表示"取到的分支有没有 return"，于是这个合成 if 被读成"没 return"，
// 扫描**越过**链里的 `return` 继续走后面的语句：
//
//	leap = false
//	{ y % 4 != 0 -> return }   ; 2023 命中
//	leap = true                ; 照跑 -> 折成 true
//
// 后果是 `is-leap(1900)` / `is-leap(2023)` 都返回 true，`test/std/time.no` 报
// "1900 not leap" / "2023 not leap"（同一二进制 NOLANG_COMPTIME=0 时 41/41 全过）。
//
// 修法：`evalConstIfBranch` 改成三态（val, returned, ok）——解释不了的 if 必须让
// 整个折叠**放弃**，而不是继续扫描。因此本测试钉的是"不许折错"：
// 该调用必须留下来。若将来折叠器能正确解释这个合成 if（折成 false），本断言需要
// 改成"折出来的值必须是 false"；**取值的端到端门禁是 `test/std/time.no`**。

import "testing"

// condChainSrc 是 std/time.no 里 is-leap 的最小复现。
const condChainSrc = `leap = (y i64) (r bool) {
    r = false
    {
        y % 4 != 0 -> return

        y % 100 == 0 -> {
            y % 400 == 0 -> r = true
            return
        }
    }
    r = true
}

main = () () {
    b = leap(2023)
    print(b)
}
`

// TestComptimeDoesNotFoldAcrossCondChainReturn 钉住：折叠器不能跨过条件链里的
// 提前 `return`。折错会得到"非闰年判成闰年"这种静默错答案，比不折叠危险得多。
func TestComptimeDoesNotFoldAcrossCondChainReturn(t *testing.T) {
	on, off := comptimePair(t, condChainSrc)

	if !hasCallSym(off, "leap") {
		t.Fatalf("comptime off: the call to leap must be present; syms=%v", callSyms(off))
	}
	if !hasCallSym(on, "leap") {
		t.Errorf("leap(2023) was folded even though its body is a braced cond-chain the "+
			"evaluator cannot interpret. Folding it means the scan ran past the chain's "+
			"early `return` and executed `r = true`, so the constant is `true` — the "+
			"silent wrong answer test/std/time.no reports as \"2023 not leap\". "+
			"Either refuse the fold or fold it to `false`; syms=%v", callSyms(on))
	}
}

// TestComptimeCondChainWithNoReturnStillFolds 对照：同样的花括号条件链，但链里
// **没有** `return`（纯赋值），必须仍然可以折叠 —— 否则上一条会因为"任何条件链都
// 不折叠"而空洞通过。
func TestComptimeCondChainWithNoReturnStillFolds(t *testing.T) {
	// 8*9 = 72 不是源码里的字面量，所以 OFF 侧不该出现 72。
	src := `pick = (a i64) (r i64) {
    {
        a > 10 -> r = 6 * 7
        -> r = 8 * 9
    }
}

main = () () {
    r = pick(5)
    print(r)
}
`
	assertFoldedInt(t, src, 72)
}
