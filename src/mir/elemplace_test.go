package mir

// elemplace_test.go — `x <- a` 迭代變數是元素「位置」而非副本的回歸。
//
// 使用者側兩種寫法必須等價：
//
//	i <- [0..4): { a[i]: { ok -> it = 0 ... } }   ; D9 place-match 寫回元素
//	x <- a:      { x:   { ok -> it = 0 ... } }    ; 迭代變數寫回元素
//
// 修復前第二種完全靜默：`x` 綁在每輪的載入副本上，`it = v` 只重綁局部；
// 且 `x: { ok -> ... }` 的臂條件退化成 `x == 0`（裸 `ok` 變體的判別值），
// 只有元素值恰為 0 才進臂。四項不變量各自對應一個修復面：
//
//   - 寫入落成 OpIndexStore（不是 move 進死槽）
//   - 臂條件折成常數（元素必然存在，與「界內可證」的 a[i] 同解）
//   - 讀取重新讀元素（`x = x + 1` 是讀-改-寫）
//   - `c <- s`（str）不建立位置（元素是解碼出的碼點，寫入必須留在局部）
//
// 寫入類用例以「同一程式有無該陳述」的差分斷言，而不是絕對次數：容器字面量
// 本身就會發射 OpIndexStore（`[1,2,3]` 三次），絕對次數會讓斷言失去意義。

import "testing"

// countOpInMain 統計 main 中某 opcode 的出現次數（只算 main，避免 std 干擾）。
func countOpInMain(m *Module, op Op) int {
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.Name != "main" {
			continue
		}
		n := 0
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				if inst := m.Inst(iid); inst != nil && inst.Op == op {
					n++
				}
			}
		}
		return n
	}
	return 0
}

// loopWithBody 把 body 放進 `x <- a` 的迴圈體（a 為定長陣列）。
func loopWithBody(body string) string {
	return `main = () () {
    a [3]i64 = [1, 2, 3]
    x <- a: {
` + body + `
    }
}`
}

// TestLoopVarWriteStoresIntoElement：`x = v` 必須寫回容器元素。
func TestLoopVarWriteStoresIntoElement(t *testing.T) {
	base := countOpInMain(lowerSrc(t, loopWithBody("")), OpIndexStore)
	got := countOpInMain(lowerSrc(t, loopWithBody("        x = 5")), OpIndexStore)
	if got-base != 1 {
		t.Errorf("`x = 5` over `x <- a` added %d OpIndexStore (baseline %d, with write %d); expected exactly 1 — the write went to the per-iteration copy (elemplace.go not applied)", got-base, base, got)
	}
}

// TestLoopVarMatchWriteStoresIntoElement：`it = v` 必須寫回容器元素。
func TestLoopVarMatchWriteStoresIntoElement(t *testing.T) {
	base := countOpInMain(lowerSrc(t, loopWithBody("")), OpIndexStore)
	body := `        x: {
            ok -> it = 5
            -> _
        }`
	got := countOpInMain(lowerSrc(t, loopWithBody(body)), OpIndexStore)
	if got-base != 1 {
		t.Errorf("`it = 5` in `x: { ok -> ... }` over `x <- a` added %d OpIndexStore (baseline %d, with write %d); expected exactly 1 — the arm write was lost", got-base, base, got)
	}
}

// TestLoopVarPresenceMatchIsConstant：對非 option 元素，`x: { ok -> ... }` 的臂
// 條件必須是常數（元素必然存在），不能退化成 `x == 0`。
func TestLoopVarPresenceMatchIsConstant(t *testing.T) {
	m := lowerSrc(t, loopWithBody(`        x: {
            ok -> it = 5
            -> _
        }`))
	// 定長陣列 + 迭代變數：除臂條件外沒有其它比較來源，故 OpEq 應為 0。
	if c := countOpInMain(m, OpEq); c != 0 {
		t.Errorf("presence match on a loop variable still emitted %d OpEq — arm condition degenerated to `x == 0` instead of folding to a constant", c)
	}
}

// TestLoopVarReadRereadsElement：`x = x + 1` 是元素的讀-改-寫，讀必須重新下標。
func TestLoopVarReadRereadsElement(t *testing.T) {
	m := lowerSrc(t, loopWithBody("        x = x + 1"))
	// 進入 body 的一次讀 + 右手邊的一次讀。修復前右手邊沿用局部副本 → 只有 1 次。
	if c := countOpInMain(m, OpIndex); c < 2 {
		t.Errorf("`x = x + 1` emitted %d OpIndex; the read side must re-read the element (expected >= 2)", c)
	}
}

// TestStrLoopVarIsNotAPlace：`c <- s` 的元素是解碼出的碼點，寫入不得回到字串。
func TestStrLoopVarIsNotAPlace(t *testing.T) {
	m := lowerSrc(t, `main = () () {
    s = 'abc'
    c <- s: {
        c = "z"
    }
}`)
	if c := countOpInMain(m, OpIndexStore); c != 0 {
		t.Errorf("`c = ...` over `c <- s` emitted %d OpIndexStore — a str element must not be a place", c)
	}
}

// TestOptionElementLoopWriteStoresIntoElement：元素是 option（`[]?T`）時寫入同樣要
// 落回元素。
//
// 這裡**不**斷言臂條件保留 tag 比較：`[]?T` 的 MIR 元素型別是載荷型別（dump 為
// `index dst=…:i64`），故存在性折疊同樣適用；而下標對照形態 `a[i]: { ok -> … }`
// 在 `[ok(1), nil]` 上也是兩個元素都進 ok 臂（既有行為，與本修復無關）。
// 兩種寫法的一致性由 tests/iter-place-write.no §7 以「同一語料下兩形態的臂命中
// 次數相等」鎖定，此處只鎖寫入面。
func TestOptionElementLoopWriteStoresIntoElement(t *testing.T) {
	src := func(body string) string {
		return `main = () () {
    a []?i64 = [ok(1), ok(2)]
    x <- a: {
` + body + `
    }
}`
	}
	base := countOpInMain(lowerSrc(t, src("")), OpIndexStore)
	got := countOpInMain(lowerSrc(t, src(`        x: {
            ok -> it = 9
            -> _
        }`)), OpIndexStore)
	if d := got - base; d != 1 {
		t.Errorf("`it = 9` over a `[]?i64` element added %d OpIndexStore (baseline %d, with write %d); expected exactly 1 — the write must land back in the element", d, base, got)
	}
}

// TestMapLoopVarIsNotAPlace：map 的「下標」是鍵，位置寫入會 GEP 進 hashmap 結構，
// 故 map 迭代變數不建立位置（寫入維持局部重綁，表本身不受影響）。
func TestMapLoopVarIsNotAPlace(t *testing.T) {
	base := countOpInMain(lowerSrc(t, `main = () () {
    m [str]i64 = { 'a': 1, 'b': 2 }
    k <- m: {
    }
}`), OpIndexStore)
	got := countOpInMain(lowerSrc(t, `main = () () {
    m [str]i64 = { 'a': 1, 'b': 2 }
    k <- m: {
        k = 'z'
    }
}`), OpIndexStore)
	if got != base {
		t.Errorf("`k = ...` over `k <- m` added %d OpIndexStore; a map key is not a positional element — the write must stay local", got-base)
	}
}
