package checker

// index_write_test.go — D9「下标写入必须显式解包」的 checker 侧回归测试。
//
// 规则：`a[i] = v` 是**编译错误**（trace `idxwrite`，ERROR，阻断 no build）。
// 必须显式解包，二选一：
//  1. place-match（多行；ok 臂 it = 元素位置，可读可写；match 臂以换行分隔，逗号/分号非分隔符）
//  2. 错误上抛：`a[i] ?= v`（越界上抛 nil，要求所在函数有 option 结果参数）
//
// 关键不变量：
//   - 合成节点（D9 place-write 降糖自己产生的 `a[__idx] = __place`）**不得**误报；
//   - map `[K]V` 走 put/get，无下标写入语义，**必须放行**（且 containerElemType
//     会把 `[str]i64` 误判为数组，故必须先判 map 再判容器）；
//   - surface AST（LSP / no fmt，SkipSafeIndexLowering=true）与降级 AST 两端口径一致。

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/parser"
)

// idxWriteSrc 是最小的「下标直写」正例：定长数组 + 常量下标直接赋值。
const idxWriteSrc = `set = () () {
    a [3]i64 = [1, 2, 3]
    a[1] = 2
}`

// TestIndexWriteReported 正例：单赋值 `a[i] = v` 必须报 idxwrite，且位置锚在
// 下标写入目标（`a[1]`，第 3 行第 5 列），文案给出两种正确写法。
func TestIndexWriteReported(t *testing.T) {
	prog := parseProg(t, idxWriteSrc)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 1 {
		t.Fatalf("expected exactly 1 idxwrite report, got %d: %+v", len(res), res)
	}
	r := res[0]
	if r.TraceID != indexWriteTraceID {
		t.Errorf("TraceID = %q, want %q", r.TraceID, indexWriteTraceID)
	}
	if r.Line != 3 || r.Column != 5 {
		t.Errorf("position = L%d:C%d, want L3:C5 (the subscript write target)", r.Line, r.Column)
	}
	// 文案必须把两种正确写法都印出来，否则用户无从改写。
	// place-match 以多行呈现（本语言 match 臂以换行分隔，逗号/分号非分隔符，故推荐多行）。
	for _, want := range []string{"`a[1] = 2`", "a[1]: {", "ok -> it = 2", "`a[1] ?= 2`"} {
		if !strings.Contains(r.Message, want) {
			t.Errorf("message missing %q:\n%s", want, r.Message)
		}
	}
}

// TestIndexWriteMultiAssignReported 正例：多赋值里的下标目标
// （`b, a[1] = two()`）同样未经解包，必须报。
func TestIndexWriteMultiAssignReported(t *testing.T) {
	src := `two = () (p i64, q i64) {
    p = 1
    q = 2
}
set = () () {
    a [3]i64 = [1, 2, 3]
    b, a[1] = two()
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 1 {
		t.Fatalf("expected exactly 1 idxwrite report for multi-assign, got %d: %+v", len(res), res)
	}
	if !strings.Contains(res[0].Message, "`a[1]`") {
		t.Errorf("multi-assign message should name the subscript target:\n%s", res[0].Message)
	}
}

// TestIndexWriteMapNotReported 负例：map `[K]V` 无下标写入语义（走 put/get），
// 必须放行。这是 containerElemType("[str]i64") 会把 map 型别字符串误判为数组的
// 直接回归测试——若漏掉「先判 map」这道闸门，此处就会误报。
func TestIndexWriteMapNotReported(t *testing.T) {
	src := `set = (m [str]i64) () {
    m["k"] = 1
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 0 {
		t.Fatalf("map subscript write must NOT be reported, got %d: %+v", len(res), res)
	}
}

// TestIndexWritePlaceMatchNotReported 负例：place-match 解包是 D9 的正确写法，
// 但其降糖产物内部就是 `a[__idx] = __place`（IsSynthetic）。若不跳过合成节点，
// 编译器会把自己的输出当成用户的直写来报错——这是必须守住的不变量。
func TestIndexWritePlaceMatchNotReported(t *testing.T) {
	src := `set = () (res ?i64) {
    res = nil
    a [3]i64 = [1, 2, 3]
    a[1]: {
        ok -> it = 2
        -> _
    }
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 0 {
		t.Fatalf("place-match unwrap must NOT be reported (synthetic store), got %d: %+v", len(res), res)
	}
}

// TestIndexWriteUnwrapAssignNotReported 负例：`a[i] ?= v` 是 D9 的另一种正确
// 写法，同样不得误报（降糖后是合成写入）。
func TestIndexWriteUnwrapAssignNotReported(t *testing.T) {
	src := `set = () (res ?i64) {
    res = nil
    a [3]i64 = [1, 2, 3]
    a[1] ?= 2
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 0 {
		t.Fatalf("`?=` write must NOT be reported, got %d: %+v", len(res), res)
	}
}

// TestIndexWriteStructFieldBaseReported 正例：struct-field 基底
// （`.keys[i] = v` / `s.keys[i] = v`）与直接变量基底同罪。
//
// 旧用例写的是 `node = struct { ... }` —— 那不是合法的 Nolang struct 定义
// （真实形式是 `node { ... }`，无 `=`），解析不出 StructDefinition，字段型别
// 永远查不到，于是「不报」被误读成「豁免」。改用真实定义后，隐式 self 字段
// 写入必须报 idxwrite：D11 之后索引读一律是 option，写侧不得再有豁免区。
func TestIndexWriteStructFieldBaseReported(t *testing.T) {
	src := `node {
    keys []i64
}

node.set = (i i64) () {
    .keys[i] = 1
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 1 {
		t.Fatalf("struct-field base write must be reported, got %d: %+v", len(res), res)
	}
	if res[0].TraceID != indexWriteTraceID {
		t.Fatalf("TraceID = %q, want %q", res[0].TraceID, indexWriteTraceID)
	}
}

// TestIndexWriteStructFieldPlaceMatchNotReported 负例：字段基底的 place-match
// 降糖产物是合成写入（IsSynthetic），不得二次上报。
func TestIndexWriteStructFieldPlaceMatchNotReported(t *testing.T) {
	src := `node {
    keys []i64
}

node.set = (i i64) () {
    .keys[i]: {
        ok -> it = 1

        -> _
    }
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	for _, r := range res {
		if r.TraceID == indexWriteTraceID {
			t.Fatalf("place-match on a struct field must NOT be reported, got %+v", r)
		}
	}
}

// TestIndexWriteBothASTModesConsistent 两端口径一致：LSP / no fmt 走 surface AST
// （SkipSafeIndexLowering=true），no vet / no build 走降级 AST。同一个 `a[1] = 2`
// 在两种 AST 上都必须报，否则用户会在编辑器与 CLI 看到不一致的结果。
func TestIndexWriteBothASTModesConsistent(t *testing.T) {
	for _, prog := range []*parser.Program{parseProg(t, idxWriteSrc), parseSurfaceProg(t, idxWriteSrc)} {
		res := ValidateIndexWrite(prog, "src/app.no")
		if len(res) != 1 {
			t.Fatalf("expected 1 idxwrite report on both AST modes, got %d: %+v", len(res), res)
		}
	}
}

// TestIndexWriteWiredIntoLints 确认已接入 RunAllLints（no vet / LSP 路径），
// 且严重度为 LintError、来源标记 nolang-index-write（与 idxhndld 的 HINT 不同：
// 写侧越界是 raw OOB write，是内存安全洞而非风格问题）。
func TestIndexWriteWiredIntoLints(t *testing.T) {
	prog := parseProg(t, idxWriteSrc)
	lints := RunAllLints(prog, LintOptions{SourcePath: "src/app.no"})
	found := false
	for _, l := range lints {
		if l.TraceID == indexWriteTraceID {
			if l.Severity != LintError {
				t.Errorf("severity = %v, want LintError (write-side OOB is a memory-safety hole)", l.Severity)
			}
			if l.Source != "nolang-index-write" {
				t.Errorf("source = %q, want %q", l.Source, "nolang-index-write")
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("ValidateIndexWrite not surfaced through RunAllLints as idxwrite")
	}
}

// ── with-cap / with-cap-len 緩衝增長豁免（with-len 已不在此列）──────
//
// 只有**能構造 len=0 緩衝**的 with-cap(n) / with-cap-len(cap, len) 豁免：
// len=0 時逐位元組直接下標寫入是唯一能把長度增長起來的寫法（见 fmt.no
// fmt-apply-spec、number.no u64.to-str），checker 建議的替代形體
// （place-match / set-byte）對 len=0 緩衝會靜默產空字串。
//
// with-len(n) 的 len 已就位：place-match 讀 i（i < len）落在 ok 臂、寫回同位，
// 與直接下標寫完全等價 ⇒ **不豁免**。早期把它一起豁免是過寬，讓
// `hex[i * 2 + 1] = b` 這類直寫在整個語料裡長期不受保護；2026-11-10 已全量收口。

// TestIndexWriteWithCapExempt 負例：with-cap 建立的 str 緩衝逐位元組直寫不得報。
func TestIndexWriteWithCapExempt(t *testing.T) {
	src := `build = (n i64) (buf str) {
    buf str = with-cap(n)
    buf[0] = 65
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 0 {
		t.Fatalf("with-cap buffer byte-fill write must NOT be reported, got %d: %+v", len(res), res)
	}
}

// TestIndexWriteWithLenReported 正例：with-len 建立的緩衝**不再是豁免區**。
// with-len(n) 的 len 已就位，place-match 讀 i（i < len）落在 ok 臂、寫回同位，
// 與直接下標寫完全等價 ⇒ 直接寫入沒有理由放行，必須報並給出兩種正確寫法。
//
// 歷史上這條曾被豁免（註解理由是「with-len 是預填充緩衝的標準填充慣例」），
// 但該理由只對 with-cap（len=0，直寫是唯一能增長長度的寫法）成立；豁免擴到
// with-len 讓 `hex[i * 2 + 1] = b` 這類直寫在整個語料裡長期不受保護。
func TestIndexWriteWithLenReported(t *testing.T) {
	src := `copy2 = (self str, start i64, end i64) (val str) {
    n = end - start
    val str = with-len(n)
    val[0] = self.byte(start)
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 1 {
		t.Fatalf("with-len buffer write must be reported, got %d: %+v", len(res), res)
	}
	if res[0].TraceID != indexWriteTraceID {
		t.Fatalf("TraceID = %q, want %q", res[0].TraceID, indexWriteTraceID)
	}
	for _, want := range []string{"`val[0] = self.byte(start)`", "val[0]: {", "ok -> it = self.byte(start)", "`val[0] ?= self.byte(start)`"} {
		if !strings.Contains(res[0].Message, want) {
			t.Errorf("message missing %q:\n%s", want, res[0].Message)
		}
	}
}

// TestIndexWriteWithLenPlaceMatchNotReported 守門：with-len 基底改用 place-match
// 後就必須放行（這正是使用者被要求改成的那種寫法）。若這裡誤報，使用者會被
// 逼進「兩種寫法都報」的死角。
func TestIndexWriteWithLenPlaceMatchNotReported(t *testing.T) {
	src := `copy2 = (self str, start i64, end i64) (val str) {
    n = end - start
    val str = with-len(n)
    val[0]: {
        ok -> it = self.byte(start)

        -> _
    }
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 0 {
		t.Fatalf("place-match on a with-len buffer must NOT be reported, got %d: %+v", len(res), res)
	}
}

// TestIndexWriteWithCapLenNotReported with-cap-len 也構造 len=0 的容量緩衝
// （len 由第二個參數決定、語料裡從未使用），與 with-cap 同罪，豁免不放寬也不收緊。
func TestIndexWriteWithCapLenNotReported(t *testing.T) {
	src := `build = (n i64) (buf str) {
    buf str = with-cap-len(n, 0)
    buf[0] = 65
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 0 {
		t.Fatalf("with-cap-len(_, 0) buffer write must NOT be reported, got %d: %+v", len(res), res)
	}
}

// TestIndexWriteWithCapExemptScoped 守門：豁免只針對 by with-cap/with-len 構造
// 的那個基底；同一函式內以字面量定長陣列建立的另一基底仍必須報（越界寫保護不流失）。
func TestIndexWriteWithCapExemptScoped(t *testing.T) {
	src := `mixed = (n i64) () {
    buf str = with-cap(n)
    a [3]i64 = [1, 2, 3]
    buf[0] = 65
    a[1] = 2
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 1 {
		t.Fatalf("only the literal-array base must be reported (with-cap base exempt), got %d: %+v", len(res), res)
	}
	if !strings.Contains(res[0].Message, "`a[1] = 2`") {
		t.Errorf("report should name the literal-array write, not the with-cap base:\n%s", res[0].Message)
	}
}
