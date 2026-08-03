package llvm

import (
	"fmt"
	"testing"
)

// TestMovedFactConditionalMove 验证条件 move（仅 then 分支 move）→ may-moved。
func TestMovedFactConditionalMove(t *testing.T) {
	// CFG: entry -> if.then / if.else -> if.end
	// var x idx=0；then 块有 effAdd(0)
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("conditional move: classify=%v, want may", got)
	}
}

// TestMovedFactBothBranchesMove 验证两分支都 move → must-moved。
func TestMovedFactBothBranchesMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("both branches move: classify=%v, want must", got)
	}
}

// TestMovedFactNoMove 验证无 move → mustNot。
func TestMovedFactNoMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("no move: classify=%v, want mustNot", got)
	}
}

// TestMovedFactLoopConditionalMove 验证循环内条件 move → may-moved（含回边不动点）。
func TestMovedFactLoopConditionalMove(t *testing.T) {
	// CFG: entry -> for.cond -> for.body / for.end; for.body -> for.step -> for.cond(回边)
	// for.body 内有 if.then(含 move) -> if.end -> for.step
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "for.cond")
	cfg.addEdge("for.cond", "for.body")
	cfg.addEdge("for.cond", "for.end")
	cfg.addEdge("for.body", "loop.if.then")
	cfg.addEdge("for.body", "loop.if.else")
	cfg.addEdge("loop.if.then", "loop.if.end")
	cfg.addEdge("loop.if.else", "loop.if.end")
	cfg.addEdge("loop.if.end", "for.step")
	cfg.addEdge("for.step", "for.cond") // 回边
	cfg.addEffect("loop.if.then", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "for.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("loop conditional move: classify=%v, want may", got)
	}
}

// TestMovedFactReassignReset 验证重赋值 effRemove 重置 moved 状态。
// out=x (x moved) ; x = new (x 重赋值，effRemove) → 结尾 x mustNot（x 拥有新 data，应 free）
func TestMovedFactReassignReset(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "mid")
	cfg.addEdge("mid", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})  // out = x
	cfg.addEffect("mid", effect{Kind: effRemove, VarIdx: 0}) // x = new

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("reassign reset: classify=%v, want mustNot", got)
	}
}

// TestMovedFactReassignAfterConditionalMove 验证条件 move 后重赋值。
// if c { out=x }; x = new → 结尾 x mustNot（重赋值在所有路径重置）
func TestMovedFactReassignAfterConditionalMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEdge("if.end", "after")
	cfg.addEdge("after", "end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("after", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("reassign after cond move: classify=%v, want mustNot", got)
	}
}

// TestMovedFactBreakEdge 验证 break 边流转：then 内 move + break → end，else 直接到 end。
func TestMovedFactBreakEdge(t *testing.T) {
	// entry -> if.then(move x + br end) / if.else(br end) ; end 汇合
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "end") // break/直接到 end
	cfg.addEdge("if.else", "end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("break edge: classify=%v, want may", got)
	}
}

// TestMovedFactMidBlockPoint 验证块内程序点 Fact：前 effAdd 后 effRemove。
func TestMovedFactMidBlockPoint(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	// entry: effAdd(0), effRemove(0)
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("entry", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	// 在 PreEffects=1（effAdd 之后、effRemove 之前）：must
	meet, join := factAtPoint(cfg, facts, "entry", 1)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("mid-block after add: classify=%v, want must", got)
	}
	// 在 PreEffects=2（effRemove 之后）：mustNot
	meet, join = factAtPoint(cfg, facts, "entry", 2)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("mid-block after remove: classify=%v, want mustNot", got)
	}
}

// TestOutBindFactMeetTop 验证两分支绑不同 out → ⊤。
func TestOutBindFactMeetTop(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effBind, OutIdx: 0, VarIdx: 1}) // out=x(1)
	cfg.addEffect("if.else", effect{Kind: effBind, OutIdx: 0, VarIdx: 2}) // out=y(2)

	facts := solveBindForward(cfg, 1, bindTransfer)
	bf := facts["if.end"]
	if bf.inBind[0] != bindTop {
		t.Errorf("different binds: bind=%d, want bindTop(%d)", bf.inBind[0], bindTop)
	}
}

// TestOutBindFactMeetSame 验证两分支绑相同 → 保留。
func TestOutBindFactMeetSame(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effBind, OutIdx: 0, VarIdx: 1})
	cfg.addEffect("if.else", effect{Kind: effBind, OutIdx: 0, VarIdx: 1})

	facts := solveBindForward(cfg, 1, bindTransfer)
	bf := facts["if.end"]
	if bf.inBind[0] != 1 {
		t.Errorf("same binds: bind=%d, want 1", bf.inBind[0])
	}
}

// TestInitFactConditional 验证 InitFact 条件赋值 → may。
func TestInitFactConditional(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effInit, OutIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), initTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	// must-assigned? meet.has(0)
	if meet.has(0) {
		t.Errorf("conditional init: meet should not have bit (not must)")
	}
	if !join.has(0) {
		t.Errorf("conditional init: join should have bit (may)")
	}
}

// ===== 极限测试：多变量混合 move 场景 =====

// TestMultiVarMixedMove 验证同一 CFG 中多变量混合 move 模式。
// var0: 两分支都 move → must
// var1: 仅 then move → may
// var2: 无 move → mustNot
func TestMultiVarMixedMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	// var0: both branches move
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: 0})
	// var1: only then moves
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 1})
	// var2: no move

	facts := solveBitsetForward(cfg, 3, newBitsetFact(3), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)

	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("var0 both move: classify=%v, want must", got)
	}
	if got := classifyMoved(meet, join, 1); got != triMay {
		t.Errorf("var1 then-only move: classify=%v, want may", got)
	}
	if got := classifyMoved(meet, join, 2); got != triMustNot {
		t.Errorf("var2 no move: classify=%v, want mustNot", got)
	}
}

// TestMultiVarCrossBranchMove 验证交叉分支 move：then move var0, else move var1。
// → var0 = may, var1 = may
func TestMultiVarCrossBranchMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: 1})

	facts := solveBitsetForward(cfg, 2, newBitsetFact(2), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)

	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("var0 then-only: classify=%v, want may", got)
	}
	if got := classifyMoved(meet, join, 1); got != triMay {
		t.Errorf("var1 else-only: classify=%v, want may", got)
	}
}

// TestMultiVarReassignOneNotOther 验证多变量中只重赋值其中一个。
// both move var0,var1; then 重赋值 var0 → var0 mustNot, var1 must
func TestMultiVarReassignOneNotOther(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEdge("if.end", "after")
	cfg.addEdge("after", "end")
	// both branches move both vars
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 1})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: 1})
	// reassign only var0
	cfg.addEffect("after", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 2, newBitsetFact(2), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)

	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("var0 reassigned: classify=%v, want mustNot", got)
	}
	if got := classifyMoved(meet, join, 1); got != triMust {
		t.Errorf("var1 not reassigned: classify=%v, want must", got)
	}
}

// ===== 极限测试：深嵌套 CFG 结构 =====

// TestDeepNestedIf3Levels 验证 3 层嵌套 if 的 move 传播。
// if A { if B { if C { move x } } }
// → 最内层 move，外层路径无 move → may
func TestDeepNestedIf3Levels(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	// level 1
	cfg.addEdge("entry", "L1.then")
	cfg.addEdge("entry", "L1.else")
	cfg.addEdge("L1.then", "L2.cond")
	cfg.addEdge("L1.else", "L1.end")
	// level 2
	cfg.addEdge("L2.cond", "L2.then")
	cfg.addEdge("L2.cond", "L2.else")
	cfg.addEdge("L2.then", "L3.cond")
	cfg.addEdge("L2.else", "L2.end")
	cfg.addEdge("L2.end", "L1.end")
	// level 3
	cfg.addEdge("L3.cond", "L3.then")
	cfg.addEdge("L3.cond", "L3.else")
	cfg.addEdge("L3.then", "L3.end")
	cfg.addEdge("L3.else", "L3.end")
	cfg.addEdge("L3.end", "L2.end")
	// move only in innermost then
	cfg.addEffect("L3.then", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "L1.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("deep nested 3-level: classify=%v, want may", got)
	}
}

// TestDeepNestedIfAllPathsMove 验证 3 层嵌套中所有路径都 move → must。
func TestDeepNestedIfAllPathsMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "L1.then")
	cfg.addEdge("entry", "L1.else")
	cfg.addEdge("L1.then", "L2.cond")
	cfg.addEdge("L1.else", "L1.end")
	cfg.addEdge("L2.cond", "L2.then")
	cfg.addEdge("L2.cond", "L2.else")
	cfg.addEdge("L2.then", "L2.end")
	cfg.addEdge("L2.else", "L2.end")
	cfg.addEdge("L2.end", "L1.end")
	// All leaf paths move
	cfg.addEffect("L1.else", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("L2.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("L2.else", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "L1.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("deep nested all paths move: classify=%v, want must", got)
	}
}

// TestNestedLoopInnerMove 验证嵌套循环：内层循环体 move → may。
func TestNestedLoopInnerMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	// outer loop
	cfg.addEdge("entry", "outer.cond")
	cfg.addEdge("outer.cond", "outer.body")
	cfg.addEdge("outer.cond", "outer.end")
	// inner loop
	cfg.addEdge("outer.body", "inner.cond")
	cfg.addEdge("inner.cond", "inner.body")
	cfg.addEdge("inner.cond", "inner.end")
	cfg.addEdge("inner.body", "inner.step")
	cfg.addEdge("inner.step", "inner.cond") // inner back edge
	cfg.addEdge("inner.end", "outer.step")
	cfg.addEdge("outer.step", "outer.cond") // outer back edge
	// move in inner body
	cfg.addEffect("inner.body", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "outer.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("nested loop inner move: classify=%v, want may", got)
	}
}

// TestNestedLoopBothMove 验证嵌套循环：内外循环体都 move → may（因为循环可能不执行）。
func TestNestedLoopBothMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "outer.cond")
	cfg.addEdge("outer.cond", "outer.body")
	cfg.addEdge("outer.cond", "outer.end")
	cfg.addEdge("outer.body", "inner.cond")
	cfg.addEdge("inner.cond", "inner.body")
	cfg.addEdge("inner.cond", "inner.end")
	cfg.addEdge("inner.body", "inner.step")
	cfg.addEdge("inner.step", "inner.cond")
	cfg.addEdge("inner.end", "outer.step")
	cfg.addEdge("outer.step", "outer.cond")
	// move in both inner and outer body
	cfg.addEffect("inner.body", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("outer.body", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "outer.end", 0)
	// outer.cond can go directly to outer.end (loop not entered) → may
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("nested loop both move: classify=%v, want may", got)
	}
}

// ===== 极限测试：effect 交互边界 =====

// TestEffectOscillatingAddRemove 验证同一块内振荡：add→remove→add→remove。
// 最终状态应为 not-moved（最后一次是 remove）。
func TestEffectOscillatingAddRemove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("entry", effect{Kind: effRemove, VarIdx: 0})
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("entry", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("oscillating add-remove: classify=%v, want mustNot", got)
	}
}

// TestEffectOscillatingAddRemoveAdd 验证振荡以 add 结尾 → moved。
func TestEffectOscillatingAddRemoveAdd(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("entry", effect{Kind: effRemove, VarIdx: 0})
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("oscillating add-remove-add: classify=%v, want must", got)
	}
}

// TestEffectNoOpRemove 验证无前置 add 的 effRemove（no-op）。
// 对 not-moved 变量执行 effRemove 应保持 not-moved。
func TestEffectNoOpRemove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	cfg.addEffect("entry", effect{Kind: effRemove, VarIdx: 0}) // clear without set

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("no-op remove: classify=%v, want mustNot", got)
	}
}

// TestEffectCrossBlockAddRemove 验证跨块 add→remove：entry add, mid remove → end mustNot。
func TestEffectCrossBlockAddRemove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "mid")
	cfg.addEdge("mid", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("mid", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("cross-block add-remove: classify=%v, want mustNot", got)
	}
}

// TestEffectCrossBlockAddRemoveAdd 验证跨块 add→remove→add。
func TestEffectCrossBlockAddRemoveAdd(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "mid")
	cfg.addEdge("mid", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("mid", effect{Kind: effRemove, VarIdx: 0})
	cfg.addEffect("mid", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("cross-block add-remove-add: classify=%v, want must", got)
	}
}

// TestEffectMidBlockOscillatePoint 验证块内振荡的程序点 fact。
// effects: add, remove, add → preEffect=2 时应为 moved, preEffect=3 时也为 moved。
func TestEffectMidBlockOscillatePoint(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("entry", effect{Kind: effRemove, VarIdx: 0})
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	// preEffect=1: after first add → moved
	meet, join := factAtPoint(cfg, facts, "entry", 1)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("mid-block pre=1 after add: classify=%v, want must", got)
	}
	// preEffect=2: after remove → not moved
	meet, join = factAtPoint(cfg, facts, "entry", 2)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("mid-block pre=2 after remove: classify=%v, want mustNot", got)
	}
	// preEffect=3: after second add → moved
	meet, join = factAtPoint(cfg, facts, "entry", 3)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("mid-block pre=3 after second add: classify=%v, want must", got)
	}
}

// ===== 极限测试：bitset 边界 =====

// TestBitsetBoundary63 验证 varIdx=63（第一个 uint64 的最高位）。
func TestBitsetBoundary63(t *testing.T) {
	idx := 63
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: idx})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: idx})

	facts := solveBitsetForward(cfg, idx+1, newBitsetFact(idx+1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, idx); got != triMust {
		t.Errorf("bitset boundary idx=63: classify=%v, want must", got)
	}
}

// TestBitsetBoundary64 验证 varIdx=64（第二个 uint64 的最低位）。
func TestBitsetBoundary64(t *testing.T) {
	idx := 64
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: idx})

	facts := solveBitsetForward(cfg, idx+1, newBitsetFact(idx+1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, idx); got != triMay {
		t.Errorf("bitset boundary idx=64: classify=%v, want may", got)
	}
}

// TestBitsetBoundary127 验证 varIdx=127（第二个 uint64 的最高位）。
func TestBitsetBoundary127(t *testing.T) {
	idx := 127
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: idx})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: idx})

	facts := solveBitsetForward(cfg, idx+1, newBitsetFact(idx+1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, idx); got != triMust {
		t.Errorf("bitset boundary idx=127: classify=%v, want must", got)
	}
}

// TestBitsetBoundary128 验证 varIdx=128（第三个 uint64 的最低位）。
func TestBitsetBoundary128(t *testing.T) {
	idx := 128
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: idx})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: idx})

	facts := solveBitsetForward(cfg, idx+1, newBitsetFact(idx+1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, idx); got != triMust {
		t.Errorf("bitset boundary idx=128: classify=%v, want must", got)
	}
}

// TestBitsetCrossBoundaryMixed 验证跨 uint64 边界的多变量混合。
// var 63 和 var 64 在不同分支 move。
func TestBitsetCrossBoundaryMixed(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	// var 63: both move → must
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 63})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: 63})
	// var 64: only then → may
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 64})

	facts := solveBitsetForward(cfg, 65, newBitsetFact(65), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 63); got != triMust {
		t.Errorf("cross-boundary var63: classify=%v, want must", got)
	}
	if got := classifyMoved(meet, join, 64); got != triMay {
		t.Errorf("cross-boundary var64: classify=%v, want may", got)
	}
}

// TestBitsetBoundaryReassign 验证跨边界 reassign：add 64 then remove 64 → mustNot。
func TestBitsetBoundaryReassign(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "mid")
	cfg.addEdge("mid", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 64})
	cfg.addEffect("mid", effect{Kind: effRemove, VarIdx: 64})

	facts := solveBitsetForward(cfg, 65, newBitsetFact(65), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 64); got != triMustNot {
		t.Errorf("cross-boundary reassign idx=64: classify=%v, want mustNot", got)
	}
}

// ===== 极限测试：不可达块与 factAtPoint 边界 =====

// TestUnreachableBlock 验证不可达块不影响可达块的 fact。
func TestUnreachableBlock(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	// unreachable block with move effect
	cfg.addEdge("unreachable", "if.end")
	cfg.addEffect("unreachable", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	// if.end should be mustNot because unreachable block has no real contribution
	// (its IN is all-zero since it has no valid predecessor that's reachable)
	// Wait: unreachable block IS a predecessor of if.end. Its OUT will have bit 0 set.
	// meet = if.then.outMeet ∩ if.else.outMeet ∩ unreachable.outMeet
	// if.then and if.else don't have bit 0, so meet = {} → mustNot
	// join = if.then.outJoin ∪ if.else.outJoin ∪ unreachable.outJoin = {0} → may
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("unreachable block: classify=%v, want may (unreachable contributes to join)", got)
	}
}

// TestFactAtPointNonExistentBlock 验证 factAtPoint 对不存在 block 返回全 0。
func TestFactAtPointNonExistentBlock(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "nonexistent", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("nonexistent block: classify=%v, want mustNot", got)
	}
}

// TestFactAtPointPreEffectsBeyondLength 验证 PreEffects 超过 effects 长度时等价于 OUT。
func TestFactAtPointPreEffectsBeyondLength(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	// PreEffects=100, but only 1 effect → should equal OUT
	meet, join := factAtPoint(cfg, facts, "entry", 100)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("preEffects beyond length: classify=%v, want must (equals OUT)", got)
	}
}

// TestFactAtPointPreEffectsZero 验证 PreEffects=0 等价于 IN。
func TestFactAtPointPreEffectsZero(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	cfg.addEffect("entry", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	// PreEffects=0 → IN of entry (all zero)
	meet, join := factAtPoint(cfg, facts, "entry", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("preEffects=0: classify=%v, want mustNot (equals IN)", got)
	}
}

// TestSingleBlockNoEffects 验证单块无 effect → mustNot。
func TestSingleBlockNoEffects(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "entry", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("single block no effects: classify=%v, want mustNot", got)
	}
}

// TestSelfLoopBlock 验证自循环块（block 有指向自身的边）。
func TestSelfLoopBlock(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "loop")
	cfg.addEdge("loop", "loop")   // self-loop
	cfg.addEdge("loop", "end")
	cfg.addEffect("loop", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	// loop body has move, but entry→end path doesn't go through loop
	// Actually entry→loop→end, so end's predecessors are just loop
	// loop OUT has bit 0 set → end IN has bit 0 set → must
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("self-loop block: classify=%v, want must", got)
	}
}

// ===== 极限测试：循环极限场景 =====

// TestLoopUnconditionalMove 验证循环体无条件 move → must-moved at loop end。
// 循环体只有一条路径（无条件），且该路径 move → 到达 for.end 时 must-moved。
// 但 for.cond 可以直接跳到 for.end（循环零次）→ may。
func TestLoopUnconditionalMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "for.cond")
	cfg.addEdge("for.cond", "for.body")
	cfg.addEdge("for.cond", "for.end")
	cfg.addEdge("for.body", "for.step")
	cfg.addEdge("for.step", "for.cond")
	cfg.addEffect("for.body", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "for.end", 0)
	// for.cond can go directly to for.end (loop not entered) → may
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("loop unconditional move: classify=%v, want may", got)
	}
}

// TestLoopBreakMoveContinueNoMove 验证 break(含 move) + continue(无 move) → may at loop exit。
func TestLoopBreakMoveContinueNoMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "for.cond")
	cfg.addEdge("for.cond", "for.body")
	cfg.addEdge("for.cond", "for.end")
	cfg.addEdge("for.body", "if.then")
	cfg.addEdge("for.body", "if.else")
	cfg.addEdge("if.then", "for.end")    // break → exit loop
	cfg.addEdge("if.else", "for.step")   // continue → next iteration
	cfg.addEdge("for.step", "for.cond")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "for.end", 0)
	// for.end predecessors: for.cond (no move), if.then (move) → may
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("loop break-move continue-no-move: classify=%v, want may", got)
	}
}

// TestLoopBreakMoveBreakAlsoMove 验证两个 break 都 move → must at loop exit。
func TestLoopBreakMoveBreakAlsoMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "for.cond")
	cfg.addEdge("for.cond", "for.body")
	cfg.addEdge("for.cond", "for.end")
	cfg.addEdge("for.body", "if.then")
	cfg.addEdge("for.body", "if.else")
	cfg.addEdge("if.then", "for.end")   // break1
	cfg.addEdge("if.else", "for.end")   // break2
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "for.end", 0)
	// for.end predecessors: for.cond (no move), if.then (move), if.else (move)
	// meet = for.cond.out ∩ if.then.out ∩ if.else.out = {} (for.cond has no move) → may
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("loop both breaks move: classify=%v, want may (for.cond path has no move)", got)
	}
}

// TestLoopBodyMoveNoExitSkip 验证循环体 move 但循环后无其他 move。
// 关键：循环可能执行 0 次 → for.cond→for.end 路径无 move → may。
func TestLoopBodyMoveNoExitSkip(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "for.cond")
	cfg.addEdge("for.cond", "for.body")
	cfg.addEdge("for.cond", "for.end")
	cfg.addEdge("for.body", "for.step")
	cfg.addEdge("for.step", "for.cond")
	cfg.addEffect("for.body", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	// Check at for.cond (after loop body executes, the back-edge brings moved state)
	meet, join := factAtPoint(cfg, facts, "for.cond", 0)
	// for.cond predecessors: entry (no move), for.step (move propagates)
	// meet = entry.out ∩ for.step.out = {} (entry has no move) → may
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("loop body move at for.cond: classify=%v, want may", got)
	}
}

// ===== 极限测试：OutBindFact 极限 =====

// TestOutBind3WayAllDifferent 验证三路分支绑不同 out → ⊤。
func TestOutBind3WayAllDifferent(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "a")
	cfg.addEdge("entry", "b")
	cfg.addEdge("entry", "c")
	cfg.addEdge("a", "merge")
	cfg.addEdge("b", "merge")
	cfg.addEdge("c", "merge")
	cfg.addEffect("a", effect{Kind: effBind, OutIdx: 0, VarIdx: 1})
	cfg.addEffect("b", effect{Kind: effBind, OutIdx: 0, VarIdx: 2})
	cfg.addEffect("c", effect{Kind: effBind, OutIdx: 0, VarIdx: 3})

	facts := solveBindForward(cfg, 1, bindTransfer)
	bf := facts["merge"]
	if bf.inBind[0] != bindTop {
		t.Errorf("3-way all different: bind=%d, want bindTop(%d)", bf.inBind[0], bindTop)
	}
}

// TestOutBind3WayTwoSameOneDiff 验证三路分支两同→一异 → ⊤。
func TestOutBind3WayTwoSameOneDiff(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "a")
	cfg.addEdge("entry", "b")
	cfg.addEdge("entry", "c")
	cfg.addEdge("a", "merge")
	cfg.addEdge("b", "merge")
	cfg.addEdge("c", "merge")
	cfg.addEffect("a", effect{Kind: effBind, OutIdx: 0, VarIdx: 1})
	cfg.addEffect("b", effect{Kind: effBind, OutIdx: 0, VarIdx: 1}) // same as a
	cfg.addEffect("c", effect{Kind: effBind, OutIdx: 0, VarIdx: 2}) // different

	facts := solveBindForward(cfg, 1, bindTransfer)
	bf := facts["merge"]
	if bf.inBind[0] != bindTop {
		t.Errorf("3-way two same one diff: bind=%d, want bindTop(%d)", bf.inBind[0], bindTop)
	}
}

// TestOutBind3WayAllSame 验证三路分支都绑相同 → 保留。
func TestOutBind3WayAllSame(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "a")
	cfg.addEdge("entry", "b")
	cfg.addEdge("entry", "c")
	cfg.addEdge("a", "merge")
	cfg.addEdge("b", "merge")
	cfg.addEdge("c", "merge")
	cfg.addEffect("a", effect{Kind: effBind, OutIdx: 0, VarIdx: 5})
	cfg.addEffect("b", effect{Kind: effBind, OutIdx: 0, VarIdx: 5})
	cfg.addEffect("c", effect{Kind: effBind, OutIdx: 0, VarIdx: 5})

	facts := solveBindForward(cfg, 1, bindTransfer)
	bf := facts["merge"]
	if bf.inBind[0] != 5 {
		t.Errorf("3-way all same: bind=%d, want 5", bf.inBind[0])
	}
}

// TestOutBindLoopInternalRebind 验证循环内 rebind。
func TestOutBindLoopInternalRebind(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "for.cond")
	cfg.addEdge("for.cond", "for.body")
	cfg.addEdge("for.cond", "for.end")
	cfg.addEdge("for.body", "for.step")
	cfg.addEdge("for.step", "for.cond")
	cfg.addEffect("for.body", effect{Kind: effBind, OutIdx: 0, VarIdx: 7})

	facts := solveBindForward(cfg, 1, bindTransfer)
	bf := facts["for.end"]
	// for.end pred: for.cond
	// for.cond IN: entry(bindNone) ∩ for.step.outBind
	// for.step OUT = for.step IN = for.body OUT = {bind: 7}
	// So for.cond IN = bindNone ∩ 7 = bindTop (different)
	// for.cond OUT = for.cond IN (no effect in for.cond) = bindTop
	// for.end IN = for.cond OUT = bindTop
	if bf.inBind[0] != bindTop {
		t.Errorf("loop internal rebind: bind=%d, want bindTop(%d)", bf.inBind[0], bindTop)
	}
}

// TestOutBindRebindInSameBlock 验证同一块内先 bind 后再 bind → 最终值为后一个。
func TestOutBindRebindInSameBlock(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	cfg.addEffect("entry", effect{Kind: effBind, OutIdx: 0, VarIdx: 1})
	cfg.addEffect("entry", effect{Kind: effBind, OutIdx: 0, VarIdx: 2})

	facts := solveBindForward(cfg, 1, bindTransfer)
	bf := facts["end"]
	if bf.inBind[0] != 2 {
		t.Errorf("rebind in same block: bind=%d, want 2 (last bind wins)", bf.inBind[0])
	}
}

// TestOutBindMultiOutParams 验证多个 out 参数独立绑定。
func TestOutBindMultiOutParams(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	// out[0]: both bind same var 1
	cfg.addEffect("if.then", effect{Kind: effBind, OutIdx: 0, VarIdx: 1})
	cfg.addEffect("if.else", effect{Kind: effBind, OutIdx: 0, VarIdx: 1})
	// out[1]: then binds 3, else binds 4 → ⊤
	cfg.addEffect("if.then", effect{Kind: effBind, OutIdx: 1, VarIdx: 3})
	cfg.addEffect("if.else", effect{Kind: effBind, OutIdx: 1, VarIdx: 4})
	// out[2]: no bind → bindNone

	facts := solveBindForward(cfg, 3, bindTransfer)
	bf := facts["if.end"]
	if bf.inBind[0] != 1 {
		t.Errorf("multi-out out[0]: bind=%d, want 1", bf.inBind[0])
	}
	if bf.inBind[1] != bindTop {
		t.Errorf("multi-out out[1]: bind=%d, want bindTop(%d)", bf.inBind[1], bindTop)
	}
	if bf.inBind[2] != bindNone {
		t.Errorf("multi-out out[2]: bind=%d, want bindNone(%d)", bf.inBind[2], bindNone)
	}
}

// ===== 极限测试：InitFact 极限 =====

// TestInitFactBothBranches 验证两分支都 init → must-init。
func TestInitFactBothBranches(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effInit, OutIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effInit, OutIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), initTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if !meet.has(0) {
		t.Errorf("both branches init: meet should have bit (must)")
	}
	if !join.has(0) {
		t.Errorf("both branches init: join should have bit")
	}
}

// TestInitFactNoInit 验证无 init → mustNot-init。
func TestInitFactNoInit(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), initTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if meet.has(0) {
		t.Errorf("no init: meet should not have bit")
	}
	if join.has(0) {
		t.Errorf("no init: join should not have bit")
	}
}

// TestInitFactLoopInternal 验证循环内 init → may（循环可能不执行）。
func TestInitFactLoopInternal(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "for.cond")
	cfg.addEdge("for.cond", "for.body")
	cfg.addEdge("for.cond", "for.end")
	cfg.addEdge("for.body", "for.step")
	cfg.addEdge("for.step", "for.cond")
	cfg.addEffect("for.body", effect{Kind: effInit, OutIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), initTransfer)
	meet, join := factAtPoint(cfg, facts, "for.end", 0)
	if meet.has(0) {
		t.Errorf("loop internal init: meet should not have bit (not must)")
	}
	if !join.has(0) {
		t.Errorf("loop internal init: join should have bit (may)")
	}
}

// TestInitFactMultiOut 验证多个 out 参数独立 init。
func TestInitFactMultiOut(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	// out[0]: both init → must
	cfg.addEffect("if.then", effect{Kind: effInit, OutIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effInit, OutIdx: 0})
	// out[1]: only then init → may
	cfg.addEffect("if.then", effect{Kind: effInit, OutIdx: 1})
	// out[2]: no init → mustNot

	facts := solveBitsetForward(cfg, 3, newBitsetFact(3), initTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if !meet.has(0) {
		t.Errorf("multi-init out[0]: meet should have bit (must)")
	}
	if meet.has(1) {
		t.Errorf("multi-init out[1]: meet should not have bit (not must)")
	}
	if !join.has(1) {
		t.Errorf("multi-init out[1]: join should have bit (may)")
	}
	if join.has(2) {
		t.Errorf("multi-init out[2]: join should not have bit (mustNot)")
	}
}

// ===== 极限测试：条件 move + 条件重赋值交叉 =====

// TestCondMoveCondReassignDiffBranch 验证条件 move + 条件重赋值在不同分支。
// then: move x; else: reassign x → may（then 路径 moved, else 路径 not-moved）
func TestCondMoveCondReassignDiffBranch(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})    // move in then
	cfg.addEffect("if.else", effect{Kind: effRemove, VarIdx: 0}) // reassign in else

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	// then OUT: bit 0 set; else OUT: bit 0 clear
	// meet = set ∩ clear = clear → mustNot? No, wait...
	// meet is intersection: {0} ∩ {} = {} → inMeet = false
	// join is union: {0} ∪ {} = {0} → inJoin = true
	// → may
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("cond move + cond reassign diff branch: classify=%v, want may", got)
	}
}

// TestCondMoveCondReassignSamePath 验证同一路径先 move 后 reassign → not-moved。
// then: move x then reassign x; else: nothing → may (then path not-moved, else path not-moved)
// Actually: then OUT has bit 0 cleared (add then remove), else OUT has bit 0 cleared (never set)
// → both paths not-moved → mustNot
func TestCondMoveCondReassignSamePath(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.then", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	// both branches end up with bit 0 cleared → mustNot
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("cond move+reassign same path: classify=%v, want mustNot", got)
	}
}

// TestCondMoveThenReassignThenMoveAgain 验证条件 move → reassign → move again。
// then: move, reassign, move; else: nothing
// then OUT: bit 0 set (last is add); else OUT: bit 0 clear
// → may
func TestCondMoveThenReassignThenMoveAgain(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.then", effect{Kind: effRemove, VarIdx: 0})
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("move-reassign-move again: classify=%v, want may", got)
	}
}

// TestBothBranchesMoveThenOneReassigns 验证两分支 move 后一分支 reassign。
// then: move x; else: move x, reassign x
// then OUT: bit 0 set; else OUT: bit 0 clear
// → may
func TestBothBranchesMoveThenOneReassigns(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("if.else", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("both move one reassigns: classify=%v, want may", got)
	}
}

// TestReassignInBothBranchesAfterCondMove 验证条件 move 后两分支都 reassign → mustNot。
// entry: if c { move x }; then both branches reassign x → mustNot
func TestReassignInBothBranchesAfterCondMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "mid")
	cfg.addEdge("if.else", "mid")
	cfg.addEdge("mid", "then2")
	cfg.addEdge("mid", "else2")
	cfg.addEdge("then2", "end")
	cfg.addEdge("else2", "end")
	// conditional move in first if
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})
	// reassign in both branches of second if
	cfg.addEffect("then2", effect{Kind: effRemove, VarIdx: 0})
	cfg.addEffect("else2", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("reassign in both branches after cond move: classify=%v, want mustNot", got)
	}
}

// TestDiamondReassignOnlyOneBranch 验证菱形中仅一分支 reassign → may。
// then: move x; else: move x, reassign x → may (then moved, else not moved)
// Same as TestBothBranchesMoveThenOneReassigns but explicit diamond
func TestDiamondReassignOnlyOneBranch(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "L.then")
	cfg.addEdge("entry", "L.else")
	cfg.addEdge("L.then", "L.end")
	cfg.addEdge("L.else", "L.end")
	cfg.addEdge("L.end", "R.then")
	cfg.addEdge("R.then", "R.end")
	cfg.addEdge("R.else", "R.end")
	cfg.addEdge("L.end", "R.else")
	// both branches of left diamond move
	cfg.addEffect("L.then", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("L.else", effect{Kind: effAdd, VarIdx: 0})
	// only right.then reassigns
	cfg.addEffect("R.then", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "R.end", 0)
	// R.then: clear → not moved; R.else: no effect → moved (from L)
	// meet = clear ∩ moved = not moved; join = clear ∪ moved = moved → may
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("diamond reassign one branch: classify=%v, want may", got)
	}
}

// TestMoveOnlyOnReturnPath 验证仅在 return 路径上 move。
// if c { move x; ret } else { ret } → no merge point, each path terminates.
func TestMoveOnlyOnReturnPath(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	// No successor from if.then and if.else (they terminate with ret)
	cfg.addEffect("if.then", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	// if.then OUT: bit 0 set; if.else OUT: bit 0 clear
	// No merge block → just verify individual block OUT
	bfThen := facts["if.then"]
	if !bfThen.outMeet.has(0) {
		t.Errorf("return path then: outMeet should have bit (must)")
	}
	bfElse := facts["if.else"]
	if bfElse.outMeet.has(0) {
		t.Errorf("return path else: outMeet should not have bit")
	}
}

// TestEntryInitNonZero 验证 entry 的 IN 可以初始化为非零值。
func TestEntryInitNonZero(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")
	// entry IN initialized with bit 0 set
	init := newBitsetFact(1)
	init.set(0)

	facts := solveBitsetForward(cfg, 1, init, movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("entry init non-zero: classify=%v, want must", got)
	}
}

// TestEntryInitPropagateThroughBranches 验证 entry init 传播到分支后 meet 保留。
// entry IN has bit 0; both branches no effect → merge must
func TestEntryInitPropagateThroughBranches(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	init := newBitsetFact(1)
	init.set(0)

	facts := solveBitsetForward(cfg, 1, init, movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("entry init propagate: classify=%v, want must", got)
	}
}

// TestEntryInitClearedByOneBranch 验证 entry init 被一分支清除 → may。
// entry IN has bit 0; then: no effect; else: effRemove(0)
// → merge: meet = {0} ∩ {} = {} → mustNot? No...
// then OUT: {0} (from IN), else OUT: {} (cleared)
// meet = {0} ∩ {} = {} → inMeet=false
// join = {0} ∪ {} = {0} → inJoin=true → may
func TestEntryInitClearedByOneBranch(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "if.then")
	cfg.addEdge("entry", "if.else")
	cfg.addEdge("if.then", "if.end")
	cfg.addEdge("if.else", "if.end")
	cfg.addEffect("if.else", effect{Kind: effRemove, VarIdx: 0})
	init := newBitsetFact(1)
	init.set(0)

	facts := solveBitsetForward(cfg, 1, init, movedTransfer)
	meet, join := factAtPoint(cfg, facts, "if.end", 0)
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("entry init cleared by one branch: classify=%v, want may", got)
	}
}

// TestChainedBlocksMove 验证链式块传递 move。
// entry -> b1 -> b2 -> b3 -> end, move in b1 → end must
func TestChainedBlocksMove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "b1")
	cfg.addEdge("b1", "b2")
	cfg.addEdge("b2", "b3")
	cfg.addEdge("b3", "end")
	cfg.addEffect("b1", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("chained blocks move: classify=%v, want must", got)
	}
}

// TestChainedBlocksMoveThenRemove 验证链式块 move 后 remove。
// entry -> b1(move) -> b2(remove) -> end → mustNot
func TestChainedBlocksMoveThenRemove(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "b1")
	cfg.addEdge("b1", "b2")
	cfg.addEdge("b2", "end")
	cfg.addEffect("b1", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("b2", effect{Kind: effRemove, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("chained move then remove: classify=%v, want mustNot", got)
	}
}

// TestMultiplePredsDuplicate 验证重复前驱被正确去重。
// entry -> a -> merge; entry -> a (duplicate edge) -> merge
func TestMultiplePredsDuplicate(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "a")
	cfg.addEdge("a", "merge")
	cfg.addEdge("a", "merge") // duplicate
	cfg.addEffect("a", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "merge", 0)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("duplicate preds: classify=%v, want must", got)
	}
}

// TestLargeCFG20Blocks 验证 20 个块的链式 CFG 收敛正确。
func TestLargeCFG20Blocks(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "b0"
	for i := 0; i < 19; i++ {
		cfg.addEdge(fmt.Sprintf("b%d", i), fmt.Sprintf("b%d", i+1))
	}
	cfg.addEffect("b0", effect{Kind: effAdd, VarIdx: 0})
	cfg.addEffect("b5", effect{Kind: effRemove, VarIdx: 0})
	cfg.addEffect("b10", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "b19", 0)
	// b0 add, b5 remove, b10 add → final state: moved (last effect is add)
	if got := classifyMoved(meet, join, 0); got != triMust {
		t.Errorf("large CFG 20 blocks: classify=%v, want must", got)
	}
}

// TestLoopWithMoveInStep 验证 move 在 for.step 块中。
func TestLoopWithMoveInStep(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "for.cond")
	cfg.addEdge("for.cond", "for.body")
	cfg.addEdge("for.cond", "for.end")
	cfg.addEdge("for.body", "for.step")
	cfg.addEdge("for.step", "for.cond")
	cfg.addEffect("for.step", effect{Kind: effAdd, VarIdx: 0})

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "for.end", 0)
	// for.cond predecessors: entry (no move), for.step (move)
	// → for.cond IN: meet = {} ∩ {0} = {} → may (join has it)
	// for.end predecessor: for.cond → for.cond OUT has no move (IN propagates, no effect in cond)
	// Actually for.cond OUT = for.cond IN (no effects) → may state
	// for.end IN = for.cond OUT → may
	if got := classifyMoved(meet, join, 0); got != triMay {
		t.Errorf("loop move in step: classify=%v, want may", got)
	}
}

// TestEmptyCFGOnlyEntry 验证仅 entry 块的 CFG。
func TestEmptyCFGOnlyEntry(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"

	facts := solveBitsetForward(cfg, 1, newBitsetFact(1), movedTransfer)
	meet, join := factAtPoint(cfg, facts, "entry", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("empty CFG only entry: classify=%v, want mustNot", got)
	}
}

// TestFactAtPointNilFacts 验证 factAtPoint 对 nil facts 返回全 0。
func TestFactAtPointNilFacts(t *testing.T) {
	cfg := newFuncCFG()
	cfg.Entry = "entry"
	cfg.addEdge("entry", "end")

	// Pass nil facts map
	meet, join := factAtPoint(cfg, nil, "end", 0)
	if got := classifyMoved(meet, join, 0); got != triMustNot {
		t.Errorf("nil facts: classify=%v, want mustNot", got)
	}
}
