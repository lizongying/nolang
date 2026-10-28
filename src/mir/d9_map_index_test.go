package mir

// d9_map_index_test.go — D9 的 map 豁免（`m[k]` / `m[k] = v`）的 MIR 级回归。
//
// D9（docs/design/option-unified.md §12）要求下标写入显式解包，理由是「位置可能
// 越界，写入无落地处」。map 的下标是**键**而非位置：读缺席 → nil，写 = put
// （对任何键都有定义），没有越界写，也就没有需要解包的东西。故 §12.2.1 的结论是
// `m[k] = v` 直写合法，且 map 下标应降糖为 get/put。
//
// 在此之前 MIR **完全没有** map 下标路径：写落到 emitIndexStore 的数组 GEP
// （str 键被当成 i64 下标 → 非法 IR `%lvN defined with type '%str-long' but
// expected 'i64'`），读落到 OpIndex（"index dst slot (dst=N type=void)"）。
// 而 `no vet` 全绿 —— vet-green / build-red 是最难在评审里看见的形态，所以这里
// 用「缺席断言 + 正向对照」两侧夹住：
//
//   - 写侧：map 基底**不得**产生 OpIndexStore；数组基底**必须**仍然产生
//     （证明分支是条件触发，不是把数组写一起关掉）。
//   - 读侧：map 基底**不得**产生 OpIndex；数组基底**必须**仍然产生。
//   - 两侧都不得出现 OpLen/OpGe 界检查块 —— map 没有长度越界这回事。
//
// 正向证据有两条：要么发射了 `hashmap-<K>-<V>.put/get` 调用（std 在场时），
// 要么抛出 kind="map-index" 的 lowering 诊断（std 不在场时）。二者任一都证明
// **map 分支被进入**；本包的单测不加载 std（见 lowerSrc），故走诊断那条。
// 运行期行为由 test/std/map-index.no 端到端锁住（真的插入、覆写、读回）。

import (
	"strings"
	"testing"
)

// mirCountCallSym 统计模块中符号为 sym 的 OpCall 次数（仅 live 函数，与
// mirCountOp 同口径）。
func mirCountCallSym(m *Module, sym string) int {
	n := 0
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if !optFuncLive(f) {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				if inst := m.Inst(iid); inst != nil && inst.Op == OpCall && inst.Sym == sym {
					n++
				}
			}
		}
	}
	return n
}

// mirCountDiagKind 统计某类 lowering 诊断的出现次数。
func mirCountDiagKind(diags []LowerDiag, kind string) int {
	n := 0
	for _, d := range diags {
		if d.Kind == kind {
			n++
		}
	}
	return n
}

// TestMapSubscriptWriteIsPutNotArrayGEP 写侧：`m['b'] = 2` 必须走 map 路径。
//
// 空字面量 `{}` 只发射 init（无 put），故此处任何 put 都只能来自下标写 ——
// 这正是把「字面量自带的 put」与「下标写的 put」区分开的办法。
func TestMapSubscriptWriteIsPutNotArrayGEP(t *testing.T) {
	src := `main = () () {
    m [str]i64 = {}
    m['b'] = 2
}`
	m, diags := lowerSrcWithDiags(t, src)
	if c := mirCountOp(m, OpIndexStore); c != 0 {
		t.Errorf("map subscript write emitted %d OpIndexStore; the array GEP path must not be reachable for a map base (the str key would be used as an i64 index -> invalid IR)", c)
	}
	if hasBlockNamed(m, "idx.ok") || hasBlockNamed(m, "idx.none") || hasBlockNamed(m, "idx.merge") {
		t.Errorf("map subscript write emitted safe-index bounds-check blocks; a map key has no out-of-range case")
	}
	if c := mirCountOp(m, OpGe); c != 0 {
		t.Errorf("map subscript write emitted %d OpGe; no bounds comparison is meaningful for a key lookup", c)
	}
	if c := mirCountCallSym(m, "hashmap-str-i64.put"); c == 0 && mirCountDiagKind(diags, "map-index") == 0 {
		t.Fatalf("map subscript write never reached the map path: no hashmap-str-i64.put call and no map-index diagnostic (diags=%v)", diags)
	}
}

// TestMapSubscriptReadIsGetNotOpIndex 读侧：`m['a']` 必须是 get，不是 OpIndex。
func TestMapSubscriptReadIsGetNotOpIndex(t *testing.T) {
	src := `peek = (k str) (r ?i64) {
    m [str]i64 = {}
    r = m[k]
}
main = () () {
    x ?i64 = peek('a')
}`
	m, diags := lowerSrcWithDiags(t, src)
	if c := mirCountOp(m, OpIndex); c != 0 {
		t.Errorf("map subscript read emitted %d OpIndex; elementTypeOf on a hashmap struct is void, which is exactly the 'index dst slot (dst=N type=void)' failure", c)
	}
	if c := mirCountCallSym(m, "hashmap-str-i64.get"); c == 0 && mirCountDiagKind(diags, "map-index") == 0 {
		t.Fatalf("map subscript read never reached the map path: no hashmap-str-i64.get call and no map-index diagnostic (diags=%v)", diags)
	}
}

// TestMapSubscriptWriteRejectsOptionValue 值侧守卫：`m['a'] = m['b']` 里的右侧
// 是 `?V`，不能喂给 put 的 `v` 参数。
//
// 这条路径 checker 不拒（索引写的 RHS 不经过 option 捕获 pass），所以没有守卫时
// codegen 会发 `store %str-long %lvN`（%lvN 是 %option）——一个没有任何源级解释的
// LLVM verifier 错误。守卫必须报出 kind="map-index" 的诊断，而 build 侧的
// firstFatalLowerDiag 把该 kind 视为致命（否则写会被**静默丢弃**，变成
// wrong-but-exit-0）。
func TestMapSubscriptWriteRejectsOptionValue(t *testing.T) {
	// The value comes from a CALL returning `?str` rather than from `m['b']`,
	// because this harness does not load std: a map read would need the
	// specialised `get` and would fail earlier with its own diag, masking the
	// guard under test. An empty literal still yields a real hashmap struct
	// value (lowerMapLit returns the OpStructLit even when `init` is missing),
	// so the map side is genuine.
	src := `g = () (r ?str) {
    r = 'x'
}
main = () () {
    m [str]str = {}
    m['a'] = g()
}`
	_, diags := lowerSrcWithDiags(t, src)
	found := ""
	for _, d := range diags {
		if d.Kind == "map-index" && strings.Contains(d.Msg, "is an option") {
			found = d.Msg
			break
		}
	}
	if found == "" {
		t.Fatalf("an option-valued map subscript write must be reported as `map-index: ... is an option ...`; diags=%v", diags)
	}
	if !strings.Contains(found, "`str`") {
		t.Errorf("diagnostic does not name the map's value type: %q", found)
	}
}

// TestMapSubscriptWriteAcceptsPlainValue 对照：同形但右侧是普通 `str` 时必须没有
// map-index 诊断（否则上一条也会因为「任何 map 写都报错」而空洞通过）。
func TestMapSubscriptWriteAcceptsPlainValue(t *testing.T) {
	src := `main = () () {
    m [str]str = {}
    m['a'] = 'z'
}`
	_, diags := lowerSrcWithDiags(t, src)
	for _, d := range diags {
		if d.Kind == "map-index" && strings.Contains(d.Msg, "is an option") {
			t.Fatalf("a plain `str` value must be accepted; got diag %q", d.Msg)
		}
	}
}

// TestArraySubscriptStillUsesIndexOps 正向对照：数组下标必须仍然走 OpIndex /
// OpIndexStore 与界检查。没有这条，「map 基底不产生 OpIndexStore」也能靠「永远
// 不产生」通过 —— 那正是空洞断言。
func TestArraySubscriptStillUsesIndexOps(t *testing.T) {
	src := `main = () () {
    a [3]i64 = [0, 0, 0]
    a[1]: {
        ok -> it = 7
        -> _
    }
}`
	m, _ := lowerSrcWithDiags(t, src)
	if c := mirCountOp(m, OpIndexStore); c == 0 {
		t.Errorf("array subscript write emitted no OpIndexStore; the control proves the map exemption is conditional, so this must stay on the array path")
	}
	if c := mirCountCallSym(m, "hashmap-str-i64.put"); c != 0 {
		t.Errorf("array subscript write emitted %d hashmap put calls; an array base must never take the map path", c)
	}
}
