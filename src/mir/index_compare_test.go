package mir

// index_compare_test.go — 比較運算元裡的索引讀取必須是**安全索引**。
//
// 語言契約（docs/docs/lang/syntax.md 〈安全索引〉）：
//
//	「對 arr / vec / slice / 定長陣列 / str / txt 的索引 v[i]，結果一律是 ?T：
//	  越界時回傳 nil，在界內回傳 ok(值)，絕不會 abort。」
//
// 安全索引分支在 KIndex 裡由**消費者的型別提示**驅動：型別綁定（`v ?i64 = a[i]`）、
// `?=`、`#{index-out=…}` 都自帶提示，比較運算子沒有（它自己的型別是 bool）。
// 修復前 `a[i] != 5` 因此掉進裸讀 `OpIndex`：沒有界檢查、沒有 nil，越界即讀野內存
// （實測 SIGTRAP）。
//
// 這兩個不變量把修復釘死：
//  1. 比較的某個運算元型別是 option（＝讀取走了安全索引，emitCmp 因而比 payload）；
//  2. 該讀取帶了界檢查（OpLen + OpGe/OpLt），不是裸 OpIndex。
//
// 反向對照：`v i64 = a[i]` 是文檔明示的 plain 綁定，仍必須是裸讀（無 option）。

import "testing"

// mainOpCount 統計 main 中某 opcode 的出現次數。
func mainOpCount(m *Module, op Op) int {
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

// cmpOperandKinds 回傳 main 中每個比較指令兩個運算元的型別 kind。
func cmpOperandKinds(m *Module) [][2]TypeKind {
	var out [][2]TypeKind
	kindOf := func(v ValueID) TypeKind {
		if v == NoVal {
			return KindVoid
		}
		if val := m.Value(v); val != nil {
			if t := m.Type(val.Type); t != nil {
				return t.Kind
			}
		}
		return KindVoid
	}
	for i := range m.Funcs {
		f := &m.Funcs[i]
		if f.Name != "main" {
			continue
		}
		for _, bid := range f.Blocks {
			blk := m.Block(bid)
			if blk == nil {
				continue
			}
			for _, iid := range blk.Insts {
				inst := m.Inst(iid)
				if inst == nil {
					continue
				}
				switch inst.Op {
				case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
					if len(inst.Args) < 2 {
						continue
					}
					out = append(out, [2]TypeKind{kindOf(inst.Args[0]), kindOf(inst.Args[1])})
				}
			}
		}
	}
	return out
}

// TestIndexCompareOperandIsSafeIndex：比較語境的索引讀取必須是 ?T 且帶界檢查。
func TestIndexCompareOperandIsSafeIndex(t *testing.T) {
	m := lowerSrc(t, `main = () () {
    a []i64 = [5, 6, 7]
    i i64 = 0
    if a[i] != 5 { print('ne') }
}`)
	kinds := cmpOperandKinds(m)
	if len(kinds) == 0 {
		t.Fatal("no comparison instruction in main")
	}
	sawOption := false
	for _, k := range kinds {
		if k[0] == KindOption || k[1] == KindOption {
			sawOption = true
		}
	}
	if !sawOption {
		t.Errorf("comparison operand is not an option: `a[i] != 5` lowered to a bare OpIndex (unchecked read, no nil); kinds=%v", kinds)
	}
	// 界檢查：OpLen 與 OpGe/OpLt 各至少一次，且讀取結果被 option-wrap。
	if n := mainOpCount(m, OpLen); n == 0 {
		t.Errorf("no OpLen in main: the comparison operand read is not bounds-checked")
	}
	if n := mainOpCount(m, OpGe) + mainOpCount(m, OpLt); n == 0 {
		t.Errorf("no OpGe/OpLt in main: the comparison operand read is not bounds-checked")
	}
	if n := mainOpCount(m, OpOptionWrap); n == 0 {
		t.Errorf("no OpOptionWrap in main: the index read was not wrapped into ?T")
	}
}

// TestIndexComparePlainBindingStaysBareRead：`v i64 = a[i]` 仍是文檔明示的裸讀。
func TestIndexComparePlainBindingStaysBareRead(t *testing.T) {
	m := lowerSrc(t, `main = () () {
    a []i64 = [5, 6, 7]
    i i64 = 0
    v i64 = a[i]
    if v != 5 { print('ne') }
}`)
	for _, k := range cmpOperandKinds(m) {
		if k[0] == KindOption || k[1] == KindOption {
			t.Errorf("plain binding comparison compared an option: %v", k)
		}
	}
	if n := mainOpCount(m, OpOptionWrap); n != 0 {
		t.Errorf("plain binding emitted %d OpOptionWrap; `v i64 = a[i]` must stay a bare read", n)
	}
}

// TestIndexCompareInBoundsFastPath：可證界內（for-range 歸納變數）不發界檢查塊，
// 但仍必須是 ?T —— 否則 `a[i] == 5` 在熱迴圈裡又退回裸讀。
func TestIndexCompareInBoundsFastPath(t *testing.T) {
	m := lowerSrc(t, `main = () () {
    a [3]i64 = [5, 6, 7]
    i <- [0..3): {
        if a[i] != 5 { print('ne') }
    }
}`)
	sawOption := false
	for _, k := range cmpOperandKinds(m) {
		if k[0] == KindOption || k[1] == KindOption {
			sawOption = true
		}
	}
	if !sawOption {
		t.Errorf("in-bounds comparison operand is not an option: %v", cmpOperandKinds(m))
	}
}

// TestIndexCompareStrOperand：str 下標（碼點）在比較語境同樣是 ?char。
func TestIndexCompareStrOperand(t *testing.T) {
	m := lowerSrc(t, `main = () () {
    s str = 'abc'
    if s[0] == 'a' { print('eq') }
}`)
	sawOption := false
	for _, k := range cmpOperandKinds(m) {
		if k[0] == KindOption || k[1] == KindOption {
			sawOption = true
		}
	}
	if !sawOption {
		t.Errorf("str index comparison operand is not an option: %v", cmpOperandKinds(m))
	}
}

// TestIndexCompareNestedIndexStaysUnhinted：`a[i][j]` 的**容器本身就是索引讀取**，
// 提示會被容器 lowering 繼承下去 —— 外層讀取被當成結果再包一層 option（實測
// `[32]str` 的元素 `str` 被包成 `?char`），內層讀取於是對一個 `?char` 取 len/下標，
// 後端報 "lencap kind: len/cap unsupported on ?char"。src/std/net/http.no 的
// `.header-names[i][j] != name.byte(j)` 就是這個形狀。
//
// 不變量：巢狀比較不得出現 option 運算元（提示必須在此讓位，維持既有 lowering）。
func TestIndexCompareNestedIndexStaysUnhinted(t *testing.T) {
	m := lowerSrc(t, `main = () () {
    a [2]str = ['ab', 'cd']
    i i64 = 0
    if a[i][0] != 'a' { print('ne') }
}`)
	for _, k := range cmpOperandKinds(m) {
		if k[0] == KindOption || k[1] == KindOption {
			t.Errorf("nested index comparison compared an option: %v", k)
		}
	}
}
