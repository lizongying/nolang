package llvm

import "testing"

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
