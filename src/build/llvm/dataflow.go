package llvm

// dataflow.go — CFG 驱动的数据流分析框架。
//
// 用途：取代 movedVar 的「单一全局位图 + 手工 outBindState 快照」模型。
// 每个 BasicBlock 携带 Fact 包，前驱汇合执行 meet 算子，回边参与不动点迭代。
//
// 三类 Fact 在同一 CFG 上流转：
//   - MovedFact  （must-moved 集，bitset over heapVarIndex）：交集 meet
//   - InitFact   （must-assigned out 参数集，bitset over outputParamOrder）：交集 meet
//   - OutBindFact（每个 out 参数当前绑定 var，⊥=无绑定 / ⊤=不确定）：相同保留 / 不同 ⊤
//
// 分类（classify）：基于「在所有前驱 OUT 中(must) / 部分(may) / 无(mustNot)」判定三态。
// must → 静态决策（跳过 free / 补零）；may → 运行时位图检查；mustNot → 静态 free / 补零。

import "fmt"

// ---- CFG 结构 ----

// terminatorKind 描述 BasicBlock 末尾的终结指令类型。
type terminatorKind int

const (
	termUnknown terminatorKind = iota
	termBr                       // br label %X（单后继）
	termCondBr                   // br i1 %c, label %T, label %F（双后继）
	termRet                      // ret（无后继）
)

// BasicBlock 是控制流图的一个节点。
type BasicBlock struct {
	Label      string
	Preds      []string // 前驱 block label（按发现顺序，可能重复，求解时去重）
	Succs      []string // 后继 block label
	Terminator terminatorKind
	Order      int // 在函数中的出现顺序（用于不动点迭代排序）
	// Effects 是该块内按顺序记录的数据流副作用（move / reassign / bind / init）。
	// 求解时 transfer 按 Effects 顺序应用到 IN 得到 OUT。
	Effects []effect
	// FreeSites 是该块内按顺序记录的 free 决策点（freeOldHeapValue / emitHeapFree / retInitZeroFill）。
	// 每个 FreeSite 携带其前序 effect 数量（PreEffects），用于计算该程序点的 Fact。
	FreeSites []*freeSite
}

// effectKind 描述数据流副作用类型。
type effectKind int

const (
	effAdd    effectKind = iota // MovedFact: var 变为 moved（out=X / b=X）
	effRemove                   // MovedFact: var 变为 not-moved（X 重赋值拿到新 data / out 重绑移除旧绑定）
	effBind                     // OutBindFact: out 参数绑定到 var（或 ⊥/⊤）
	effInit                     // InitFact: out 参数被显式赋值
	effAssign                   // AssignedFact: 局部堆變數持有本函數擁有的堆數據（需 free，除非 moved）
)

// effect 是记录在 BasicBlock 上的单条数据流副作用。
// 对 effAdd/effRemove：VarIdx 是 heapVarIndex 下标。
// 对 effBind：OutIdx 是 outputParamOrder 下标，VarIdx 是绑定目标的 varIdx（bindTop=⊤，bindNone=⊥）。
// 对 effInit：OutIdx 是 outputParamOrder 下标。
type effect struct {
	Kind   effectKind
	VarIdx int
	OutIdx int
}

// OutBindFact 的特殊绑定值。
const (
	bindNone = -1 // ⊥ 无绑定
	bindTop  = -2 // ⊤ 不确定（多分支汇合不同绑定）
)

// freeKind 描述 free 决策点的类型。
type freeKind int

const (
	freeReassign freeKind = iota // freeOldHeapValue：重赋值前释放旧值
	freeHeapEnd                  // emitHeapFree：函数结尾释放
	freeRetInit                  // emitRetInitZeroFill：return 路径补零
)

// freeSite 是一个需要在数据流求解后决策的 free/补零点。
// 求解后调用方根据三态分类回填实际 IR。
type freeSite struct {
	Kind       freeKind
	PreEffects int    // 该 site 之前本块已应用的 effect 数（决定程序点 Fact）
	VarIdx     int    // freeReassign: 被重赋值的 var 下标；freeHeapEnd/freeRetInit: -1 表示批量
	OutIdx     int    // freeRetInit: out 参数下标；其它 -1
	Name       string // 变量名（用于 emitVarHeapFree）
	LLVMType   string // 变量 LLVM 类型
	ElemType   string // 元素类型
	Marker     string // 在 bodyBuf 中的占位标记（freeReassign 用）；freeHeapEnd/freeRetInit 可空
}

// FuncCFG 是单个函数的控制流图。
type FuncCFG struct {
	Entry    string                 // entry block label
	Blocks   map[string]*BasicBlock // label → block
	Order    []string               // 按出现顺序的 block label
	blockSeq int
}

// newFuncCFG 创建空 CFG。
func newFuncCFG() *FuncCFG {
	return &FuncCFG{Blocks: make(map[string]*BasicBlock)}
}

// getOrCreateBlock 取得或创建 block。
func (c *FuncCFG) getOrCreateBlock(label string) *BasicBlock {
	if b, ok := c.Blocks[label]; ok {
		return b
	}
	b := &BasicBlock{Label: label, Order: c.blockSeq}
	c.blockSeq++
	c.Blocks[label] = b
	c.Order = append(c.Order, label)
	return b
}

// addEdge 添加 CFG 边：from → to（同时维护 from.Succs 与 to.Preds）。
func (c *FuncCFG) addEdge(from, to string) {
	fb := c.getOrCreateBlock(from)
	tb := c.getOrCreateBlock(to)
	fb.Succs = append(fb.Succs, to)
	tb.Preds = append(tb.Preds, from)
}

// setTerminator 设置 block 的终结符类型。
func (c *FuncCFG) setTerminator(label string, k terminatorKind) {
	c.getOrCreateBlock(label).Terminator = k
}

// addEffect 向 block 追加副作用。
func (c *FuncCFG) addEffect(label string, e effect) {
	b := c.getOrCreateBlock(label)
	b.Effects = append(b.Effects, e)
}

// addFreeSite 向 block 追加 free 决策点，PreEffects = 当前 effect 数。
func (c *FuncCFG) addFreeSite(label string, fs *freeSite) {
	b := c.getOrCreateBlock(label)
	fs.PreEffects = len(b.Effects)
	b.FreeSites = append(b.FreeSites, fs)
}

// ---- Fact 表示 ----

// bitsetFact 是基于 []uint64 的位集 Fact（MovedFact / InitFact 通用）。
// bit i 的块号 = i/64，块内偏移 = i%64。
type bitsetFact []uint64

// newBitsetFact 创建大小为 nBits 的全 0 位集。
func newBitsetFact(nBits int) bitsetFact {
	n := (nBits + 63) / 64
	if n < 1 {
		n = 1
	}
	return make([]uint64, n)
}

func (f bitsetFact) set(i int) {
	b := i / 64
	if b >= len(f) {
		return
	}
	f[b] |= 1 << uint(i%64)
}

func (f bitsetFact) clear(i int) {
	b := i / 64
	if b >= len(f) {
		return
	}
	f[b] &^= 1 << uint(i%64)
}

func (f bitsetFact) has(i int) bool {
	b := i / 64
	if b >= len(f) {
		return false
	}
	return f[b]&(1<<uint(i%64)) != 0
}

// copy 返回独立副本。
func (f bitsetFact) copy() bitsetFact {
	out := make([]uint64, len(f))
	copy(out, f)
	return out
}

// meet 交集：f = f ∩ src（原地修改 f）。
func (f bitsetFact) meet(src bitsetFact) bitsetFact {
	for i := range f {
		if i < len(src) {
			f[i] &= src[i]
		} else {
			f[i] = 0
		}
	}
	return f
}

// join 并集：f = f ∪ src（原地修改 f）。
func (f bitsetFact) join(src bitsetFact) bitsetFact {
	for i := range src {
		if i < len(f) {
			f[i] |= src[i]
		}
	}
	return f
}

// equal 判断两位集是否相同。
func (f bitsetFact) equal(src bitsetFact) bool {
	n := len(f)
	if len(src) > n {
		n = len(src)
	}
	for i := 0; i < n; i++ {
		var a, b uint64
		if i < len(f) {
			a = f[i]
		}
		if i < len(src) {
			b = src[i]
		}
		if a != b {
			return false
		}
	}
	return true
}

// bindFact 是 OutBindFact：每个 out 参数 → 绑定的 varIdx（或 bindNone/bindTop）。
type bindFact []int

func newBindFact(nOut int) bindFact {
	out := make([]int, nOut)
	for i := range out {
		out[i] = bindNone
	}
	return out
}

func (f bindFact) copy() bindFact {
	out := make([]int, len(f))
	copy(out, f)
	return out
}

// meet：相同保留，不同 → bindTop。
func (f bindFact) meet(src bindFact) bindFact {
	for i := range f {
		if i >= len(src) {
			f[i] = bindTop
			continue
		}
		if f[i] == src[i] {
			continue
		}
		f[i] = bindTop
	}
	return f
}

func (f bindFact) equal(src bindFact) bool {
	n := len(f)
	if n != len(src) {
		return false
	}
	for i := range f {
		if f[i] != src[i] {
			return false
		}
	}
	return true
}

// ---- 三态分类 ----

type triState int

const (
	triMust    triState = iota // 在所有路径成立
	triMustNot                 // 在所有路径不成立
	triMay                     // 部分路径成立（需运行时检查）
)

// ---- blockFact 存储每块的求解结果 ----

type blockFact struct {
	inMeet  bitsetFact // IN = ∩ pred OUT（must）
	inJoin  bitsetFact // ∪ pred OUT（may 检测）
	outMeet bitsetFact
	outJoin bitsetFact
	inBind  bindFact
	outBind bindFact
}

// bitsetTransfer 对一个 block 的 effects 应用到输入 fact，返回输出 fact。
type bitsetTransfer func(in bitsetFact, effects []effect) bitsetFact

// solveBitsetForward 对 CFG 跑前向不动点，同时求 meet-lattice 与 join-lattice。
// 返回每块的 inMeet/inJoin/outMeet/outJoin。
func solveBitsetForward(cfg *FuncCFG, nBits int, entryInit bitsetFact, transfer bitsetTransfer) map[string]*blockFact {
	result := make(map[string]*blockFact, len(cfg.Order))
	for _, label := range cfg.Order {
		bf := &blockFact{
			inMeet:  newBitsetFact(nBits),
			inJoin:  newBitsetFact(nBits),
			outMeet: newBitsetFact(nBits),
			outJoin: newBitsetFact(nBits),
		}
		result[label] = bf
	}

	// 初始化 entry 的 IN 为 entryInit（meet 与 join 相同）。
	if eb, ok := result[cfg.Entry]; ok {
		copy(eb.inMeet, entryInit)
		copy(eb.inJoin, entryInit)
	}

	// 工作表算法：前向迭代直至收敛。
	changed := true
	for changed {
		changed = false
		for _, label := range cfg.Order {
			b := cfg.Blocks[label]
			bf := result[label]
			// 计算新 IN（entry 除外）
			if label != cfg.Entry {
				newMeet := newBitsetFact(nBits)
				newJoin := newBitsetFact(nBits)
				first := true
				seen := make(map[string]bool)
				for _, p := range b.Preds {
					if seen[p] {
						continue
					}
					seen[p] = true
					pb := result[p]
					if first {
						copy(newMeet, pb.outMeet)
						copy(newJoin, pb.outJoin)
						first = false
					} else {
						newMeet.meet(pb.outMeet)
						newJoin.join(pb.outJoin)
					}
				}
				// 若无前驱（不可达块），IN 保持全 0。
				if !first {
					if !bf.inMeet.equal(newMeet) || !bf.inJoin.equal(newJoin) {
						bf.inMeet = newMeet
						bf.inJoin = newJoin
						changed = true
					}
				}
			}
			// OUT = transfer(IN, effects)
			newOutMeet := transfer(bf.inMeet.copy(), b.Effects)
			newOutJoin := transfer(bf.inJoin.copy(), b.Effects)
			if !bf.outMeet.equal(newOutMeet) || !bf.outJoin.equal(newOutJoin) {
				bf.outMeet = newOutMeet
				bf.outJoin = newOutJoin
				changed = true
			}
		}
	}
	return result
}

// solveBindForward 对 OutBindFact 求解（相同保留 / 不同 ⊤）。
func solveBindForward(cfg *FuncCFG, nOut int, transfer func(in bindFact, effects []effect) bindFact) map[string]*blockFact {
	result := make(map[string]*blockFact, len(cfg.Order))
	for _, label := range cfg.Order {
		bf := &blockFact{inBind: newBindFact(nOut), outBind: newBindFact(nOut)}
		result[label] = bf
	}
	changed := true
	for changed {
		changed = false
		for _, label := range cfg.Order {
			b := cfg.Blocks[label]
			bf := result[label]
			if label != cfg.Entry {
				newIn := newBindFact(nOut)
				first := true
				seen := make(map[string]bool)
				for _, p := range b.Preds {
					if seen[p] {
						continue
					}
					seen[p] = true
					pb := result[p]
					if first {
						copy(newIn, pb.outBind)
						first = false
					} else {
						newIn.meet(pb.outBind)
					}
				}
				if !first && !bf.inBind.equal(newIn) {
					bf.inBind = newIn
					changed = true
				}
			}
			newOut := transfer(bf.inBind.copy(), b.Effects)
			if !bf.outBind.equal(newOut) {
				bf.outBind = newOut
				changed = true
			}
		}
	}
	return result
}

// ---- transfer 实现 ----

// movedTransfer 应用 MovedFact effects：effAdd→set，effRemove→clear。
// effBind/effInit 忽略。
func movedTransfer(in bitsetFact, effects []effect) bitsetFact {
	out := in
	for _, e := range effects {
		switch e.Kind {
		case effAdd:
			out.set(e.VarIdx)
		case effRemove:
			out.clear(e.VarIdx)
		}
	}
	return out
}

// bindTransfer 应用 OutBindFact effects：effBind→设 bind[out]=var。
func bindTransfer(in bindFact, effects []effect) bindFact {
	out := in
	for _, e := range effects {
		if e.Kind == effBind {
			if e.OutIdx >= 0 && e.OutIdx < len(out) {
				out[e.OutIdx] = e.VarIdx
			}
		}
	}
	return out
}

// initTransfer 应用 InitFact effects：effInit→set out bit。
func initTransfer(in bitsetFact, effects []effect) bitsetFact {
	out := in
	for _, e := range effects {
		if e.Kind == effInit {
			out.set(e.OutIdx)
		}
	}
	return out
}

// assignedTransfer 应用 AssignedFact effects：effAssign→set（变量持有本函数拥有的堆数据）。
// 与 moved 事实正交：移动（effAdd）不改变 assigned（emitHeapFree 优先查 moved，
// 仅当 moved=triMustNot 才据此直接 free），故此处无需 clear。
func assignedTransfer(in bitsetFact, effects []effect) bitsetFact {
	out := in
	for _, e := range effects {
		if e.Kind == effAssign {
			out.set(e.VarIdx)
		}
	}
	return out
}

// ---- 程序点 Fact 计算 ----

// factAtPoint 计算 block 内某个 PreEffects 位置上的 must(meet) 与 may(join) fact。
// 返回 (meet, join) 用于三态分类。
func factAtPoint(cfg *FuncCFG, facts map[string]*blockFact, label string, preEffects int) (bitsetFact, bitsetFact) {
	bf := facts[label]
	if bf == nil {
		// block 不在求解結果中（可能不可達或未註冊），返回全 0 fact
		return newBitsetFact(0), newBitsetFact(0)
	}
	meet := bf.inMeet.copy()
	join := bf.inJoin.copy()
	b := cfg.Blocks[label]
	if b == nil {
		return meet, join
	}
	for i := 0; i < preEffects && i < len(b.Effects); i++ {
		e := b.Effects[i]
		switch e.Kind {
		case effAdd:
			meet.set(e.VarIdx)
			join.set(e.VarIdx)
		case effRemove:
			meet.clear(e.VarIdx)
			join.clear(e.VarIdx)
		}
	}
	return meet, join
}

// classifyMoved 在给定 (meet, join) 下分类 varIdx 的 moved 三态。
func classifyMoved(meet, join bitsetFact, varIdx int) triState {
	inMeet := meet.has(varIdx)
	inJoin := join.has(varIdx)
	switch {
	case inMeet:
		return triMust // 所有路径 moved → 静态跳过 free
	case inJoin:
		return triMay // 部分路径 moved → 运行时检查
	default:
		return triMustNot // 无路径 moved → 静态 free
	}
}

// String 用于调试。
func (t triState) String() string {
	switch t {
	case triMust:
		return "must"
	case triMay:
		return "may"
	default:
		return "mustNot"
	}
}

// debugDump 用于调试 CFG。
func (c *FuncCFG) debugDump() string {
	s := fmt.Sprintf("CFG entry=%s\n", c.Entry)
	for _, label := range c.Order {
		b := c.Blocks[label]
		s += fmt.Sprintf("  %s preds=%v succs=%v term=%d effects=%d freesites=%d\n",
			label, b.Preds, b.Succs, b.Terminator, len(b.Effects), len(b.FreeSites))
	}
	return s
}

// ---- Generator 上的 CFG 辅助方法 ----
// 这些方法在 g.curCFG == nil 时为空操作，确保未启用数据流时零开销、零行为变化。

// cfgOn 报告当前函数是否启用了 CFG 数据流分析。
func (g *Generator) cfgOn() bool { return g.curCFG != nil }

// cfgBlockLabel 返回当前程序点所在的 block label。
// 若 g.currentBlock 为空（函数 entry，LLVM 无显式 label），返回 g.curCFG.Entry（合成标签，仅存在于 CFG，不写入 IR）。
func (g *Generator) cfgBlockLabel() string {
	if g.currentBlock != "" {
		return g.currentBlock
	}
	if g.curCFG != nil {
		return g.curCFG.Entry
	}
	return ""
}

// cfgEdge 记录 CFG 边 from→to。
func (g *Generator) cfgEdge(from, to string) {
	if g.curCFG != nil && from != "" && to != "" {
		g.curCFG.addEdge(from, to)
	}
}

// cfgTerm 记录 block 的终结符类型。
func (g *Generator) cfgTerm(label string, k terminatorKind) {
	if g.curCFG != nil && label != "" {
		g.curCFG.setTerminator(label, k)
	}
}

// cfgRegisterBlock 在 emitLabel 时注册 block（确保它在 CFG 中存在，即使无前驱）。
func (g *Generator) cfgRegisterBlock(label string) {
	if g.curCFG != nil && label != "" {
		g.curCFG.getOrCreateBlock(label)
	}
}

// cfgAddEffect 向当前 block 追加数据流副作用。
func (g *Generator) cfgAddEffect(e effect) {
	if g.curCFG != nil {
		g.curCFG.addEffect(g.cfgBlockLabel(), e)
	}
}

// cfgAddFreeSite 向当前 block 追加 free 决策点。
func (g *Generator) cfgAddFreeSite(fs *freeSite) {
	if g.curCFG != nil {
		g.curCFG.addFreeSite(g.cfgBlockLabel(), fs)
	}
}

// computeReachableBlocks 計算從 Entry 可達的所有 block（BFS）。
// 用於排除內部 codegen 產生的孤立塊（如 heapfree.skip.N 等），
// 這些塊可能未連接到 CFG 但被 emitLabel 註冊。
func (g *Generator) computeReachableBlocks() map[string]bool {
	reachable := make(map[string]bool)
	if g.curCFG == nil || g.curCFG.Entry == "" {
		return reachable
	}
	queue := []string{g.curCFG.Entry}
	reachable[g.curCFG.Entry] = true
	for len(queue) > 0 {
		label := queue[0]
		queue = queue[1:]
		b, ok := g.curCFG.Blocks[label]
		if !ok {
			continue
		}
		for _, succ := range b.Succs {
			if !reachable[succ] {
				reachable[succ] = true
				queue = append(queue, succ)
			}
		}
	}
	return reachable
}
