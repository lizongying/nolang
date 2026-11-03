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
// 修法（第一阶段，2026-10-28）：`evalConstIfBranch` 改成三态（val, returned, ok）——解释不了的 if 必须让
// 整个折叠**放弃**，而不是继续扫描。
//
// 修法（第二阶段）：折叠器现在**能解释**这个合成 `if (1) { … }` 包装（它的语义就是
// 「按顺序跑这些语句」，且没有 else 槽），于是直接下钻：臂里的 `return` 会以 returned
// 传出，扫描不会跨过它去执行后面的 `r = true`。因此本测试的断言按上一条注释预留的方向
// 改成「折出来的值必须是 false」——既钉住不许折错，也钉住这个形态真的被折了（否则该断言
// 会因为「根本不折」而空洞通过）。**取值的端到端门禁仍是 `test/std/time.no`**。

import (
	"strings"
	"testing"
)

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

// mainBlock returns the MIR text of `main`'s blocks only, so a constant can be
// attributed to the folded call site rather than to the callee body (which keeps
// existing in the module and holds bool constants of its own).
func mainBlock(m *Module) string {
	lines := strings.Split(m.String(), "\n")
	var out []string
	inMain := false
	for _, ln := range lines {
		switch {
		case ln == "fn main":
			inMain = true
		case inMain && strings.HasPrefix(ln, "fn "):
			return strings.Join(out, "\n")
		}
		if inMain {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

// TestComptimeFoldsCondChainReturnToFalse 钉住：花括号条件链里的提前 `return` 必须
// 被折叠**尊重**（不是被跨过）。折错会得到"非闰年判成闰年"这种静默错答案，
// 比不折叠危险得多；而"干脆不折"会让本文件退化，所以两个方向一起钉。
func TestComptimeFoldsCondChainReturnToFalse(t *testing.T) {
	on, off := comptimePair(t, condChainSrc)

	if !hasCallSym(off, "leap") {
		t.Fatalf("comptime off: the call to leap must be present; syms=%v", callSyms(off))
	}
	if hasCallSym(on, "leap") {
		t.Errorf("leap(2023) kept its call: the `if (1)` wrapper is meant to be interpreted "+
			"(descend into its then-block), so refusing here means the descent regressed; syms=%v", callSyms(on))
	}
	main := mainBlock(on)
	if strings.Contains(main, "const bool 1") || !strings.Contains(main, "const bool 0") {
		t.Errorf("folded leap(2023) is not `false` — the scan ran past the chain's early `return` "+
			"and executed `r = true`, which is the silent wrong answer test/std/time.no reports as \"2023 not leap\";\nmain block:\n%s", main)
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
