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

// TestIndexWriteStructFieldBaseExempt 负例：struct-field 基底
// （`s.keys[i] = v`）是 wave 1 豁免区——其读侧本就走 legacy 非 option 路径。
// isContainerWriteBase 只认直接变量基底（*parser.Identifier），故天然排除。
func TestIndexWriteStructFieldBaseExempt(t *testing.T) {
	src := `node = struct {
    keys []i64
}
set = (s node) () {
    s.keys[0] = 1
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 0 {
		t.Fatalf("struct-field base write is wave-1 exempt, must NOT be reported, got %d: %+v", len(res), res)
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
