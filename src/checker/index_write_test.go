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

// ── with-cap / with-cap-len 緩衝增長豁免（with-len 與 str 皆不在此列）──────
//
// 只有**能構造 len=0 緩衝**的 with-cap(n) / with-cap-len(cap, len) 豁免：
// len=0 時逐位元組直接下標寫入是唯一能把長度增長起來的寫法（见 fmt.no
// fmt-apply-spec、number.no u64.to-str），checker 建議的替代形體
// （place-match；以及私有的 `_set-byte`）對 len=0 緩衝會靜默產空字串。
//
// 兩類基底**不豁免**：
//   - with-len(n)：len 已就位，place-match 讀 i（i < len）落在 ok 臂、寫回同位，
//     與直接下標寫完全等價。早期把它一起豁免是過寬，讓 `hex[i * 2 + 1] = b`
//     這類直寫在整個語料裡長期不受保護；2026-11-10 已全量收口。
//   - `str`：2026-11-11 起全域禁直寫（見 TestIndexWriteStrWithCapReported）。
//     逐位元組構造改走 `[]byte` + `to-bytes-str()`，原位變更走 `str.fill`。

// TestIndexWriteWithCapExempt 負例：with-cap 建立的 []byte 緩衝逐位元組直寫不得報。
// （2026-11-11 前這個 fixture 用的是 `str`；str 退出豁免後改用 []byte，
//
//	豁免本身對非 str 容器一字未改。）
func TestIndexWriteWithCapExempt(t *testing.T) {
	src := `build = (n i64) (buf []byte) {
    buf []byte = with-cap(n)
    buf[0] = 65
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 0 {
		t.Fatalf("with-cap buffer byte-fill write must NOT be reported, got %d: %+v", len(res), res)
	}
}

// TestIndexWriteStrWithCapReported 正例：`str` 的 with-cap 豁免已于 2026-11-11 撤销。
//
// `s[i] = v` 曾经靠 with-cap 豁免放行，而该豁免同时也是「越界写入已分配缓冲之外」
// 的唯一来源（len=0 缓冲靠直写增长，i >= cap 时跨出配置区，见 codegen 的
// ensureStrLongBuffer 增长护栏）。现在 str 一律报错，不管基底是不是 with-cap 造的。
//
// 两个反例一起钉住，防止豁免被「顺手」加回来：
//   - with-cap(n)：老豁免的直接目标；
//   - with-cap-len(n, n)：len 已就位，连「len=0 需要增长」的理由都不成立。
func TestIndexWriteStrWithCapReported(t *testing.T) {
	cases := map[string]string{
		"with-cap": `build = (n i64) (buf str) {
    buf str = with-cap(n)
    buf[0] = 65
}`,
		"with-cap-len": `build = (n i64) (buf str) {
    buf str = with-cap-len(n, n)
    buf[0] = 65
}`,
	}
	for name, src := range cases {
		prog := parseProg(t, src)
		res := ValidateIndexWrite(prog, "src/app.no")
		if len(res) != 1 {
			t.Fatalf("%s: str index write must be reported, got %d: %+v", name, len(res), res)
		}
		if res[0].TraceID != indexWriteTraceID {
			t.Fatalf("%s: TraceID = %q, want %q", name, res[0].TraceID, indexWriteTraceID)
		}
		if !strings.Contains(res[0].Message, "`buf[0] = 65`") {
			t.Errorf("%s: message should name the write, got:\n%s", name, res[0].Message)
		}
	}
}

// TestIndexWriteWithLenReported 正例：with-len 建立的緩衝**不再是豁免區**。
// with-len(n) 的 len 已就位，place-match 讀 i（i < len）落在 ok 臂、寫回同位，
// 與直接下標寫完全等價 ⇒ 直接寫入沒有理由放行，必須報並給出兩種正確寫法。
//
// 歷史上這條曾被豁免（註解理由是「with-len 是預填充緩衝的標準填充慣例」），
// 但該理由只對 with-cap（len=0，直寫是唯一能增長長度的寫法）成立；豁免擴到
// with-len 讓 `hex[i * 2 + 1] = b` 這類直寫在整個語料裡長期不受保護。
//
// 註：2026-11-11 之後 `str` 本身也一律禁直寫，所以這個 str fixture 會同時命中
// 兩條理由（with-len 不豁免 + str 不豁免）。單獨釘住 with-len 的那條由下面的
// TestIndexWriteWithLenReportedBytes（[]byte 基底）負責。
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

// TestIndexWriteWithLenReportedBytes 與上一個同規則，但基底改用 []byte。
// 目的：排除「只是因為基底是 str 才報」的解釋，單獨釘住 with-len 不被豁免。
func TestIndexWriteWithLenReportedBytes(t *testing.T) {
	src := `copy2 = (n i64) () {
    val []byte = with-len(n)
    val[0] = 65
}`
	prog := parseProg(t, src)
	res := ValidateIndexWrite(prog, "src/app.no")
	if len(res) != 1 {
		t.Fatalf("with-len []byte write must be reported, got %d: %+v", len(res), res)
	}
	if res[0].TraceID != indexWriteTraceID {
		t.Fatalf("TraceID = %q, want %q", res[0].TraceID, indexWriteTraceID)
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
// 2026-11-11 前這個 fixture 用的是 `str`，str 退出豁免後改用 []byte。
func TestIndexWriteWithCapLenNotReported(t *testing.T) {
	src := `build = (n i64) (buf []byte) {
    buf []byte = with-cap-len(n, 0)
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
// 2026-11-11 前這個 fixture 的 with-cap 基底是 `str`，str 退出豁免後改用 []byte。
func TestIndexWriteWithCapExemptScoped(t *testing.T) {
	src := `mixed = (n i64) () {
    buf []byte = with-cap(n)
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

// TestIndexWriteStrTopLevelReported 钉住**顶层脚本**（curFunc == ""）的 str 直写。
//
// 背景：idxwrite 的基底型别来自 IdxLocalTypes（按函数作用域登记），顶层语句的
// curFunc 是空串，所以 `s str = with-cap(4)` ⏎ `s[0] = 89` 这类脚本层写入一直
// 漏报 —— 全域禁用 str 下标写在脚本层等于开了后门。runIndexWriteCore 因此补了
// 一份 collectTopLevelLets 的 str 名单（topStrNames）。
//
// 这个补丁刻意只认 str：其它容器（[]byte / []T / 固定数组）的顶层判定是另一条
// 独立的歷史缺口，本测试不得把那条也一并钉死，否则日后有人顺手放开 []T 会把
// 这边一起翻红。下面第三个 case 就是那条边界的守门。
func TestIndexWriteStrTopLevelReported(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// 字面量 str：型别由 collectTopLevelLets 的推断登记拿到。
			name: "literal-str",
			src: `s str = 'abc'
s[0] = 89`,
			want: 1,
		},
		{
			// with-cap 基底同样不豁免（与函数内同口径）。
			name: "with-cap-str",
			src: `s str = with-cap(4)
s[0] = 89`,
			want: 1,
		},
		{
			// 边界守门：顶层 []byte + with-cap 仍是豁免区，这条不该被本补丁带红。
			name: "bytes-with-cap-exempt",
			src: `b []byte = with-cap(4)
b[0] = 89`,
			want: 0,
		},
	}
	for _, tc := range cases {
		prog := parseProg(t, tc.src)
		res := ValidateIndexWrite(prog, "src/app.no")
		if len(res) != tc.want {
			t.Fatalf("%s: got %d diagnostics, want %d: %+v", tc.name, len(res), tc.want, res)
		}
		if tc.want > 0 && res[0].TraceID != indexWriteTraceID {
			t.Fatalf("%s: TraceID = %q, want %q", tc.name, res[0].TraceID, indexWriteTraceID)
		}
	}
}

// TestIndexWriteStrResultParamReported 钉住**结果参数**（`f = () (out str)`）的 str 直写。
//
// 背景：IdxLocalTypes 只登记函数体里的 `x T = …` 显式标注，所以
//
//	f = () (out str) {
//	    out = with-cap(4)   ; 没有标注 ⇒ 查不到型别
//	    out[0] = 65
//	}
//
// 这条「结果参数 + with-cap 造 str 缓冲」的经典形状整条漏报 —— 而它正是本轮
// 禁用 str 下标直写要堵的代码（旧 docs 的 str.to-upper 范例就是这个）。修法是把
// 结果参数里的 str 名字登记进 resStrParams，作为 IdxLocalTypes 落空时的兜底。
//
// 第二个 case 是回归：体内写了标注时本来就靠 IdxLocalTypes 报（不依赖本兜底），
// 两条路都得继续报，否则日后有人删掉兜底这边会静默退回漏报。
func TestIndexWriteStrResultParamReported(t *testing.T) {
	cases := map[string]string{
		"untyped-rebind": `f = () (out str) {
    out = with-cap(4)
    out[0] = 65
}`,
		"typed-bind": `f = (n i64) (out str) {
    out str = with-cap(n)
    out[1] = 66
}`,
	}
	for name, src := range cases {
		prog := parseProg(t, src)
		res := ValidateIndexWrite(prog, "src/app.no")
		if len(res) != 1 {
			t.Fatalf("%s: str index write on a result param must be reported, got %d: %+v", name, len(res), res)
		}
		if res[0].TraceID != indexWriteTraceID {
			t.Fatalf("%s: TraceID = %q, want %q", name, res[0].TraceID, indexWriteTraceID)
		}
	}
}

// TestIndexWriteTxtReported 钉住 txt 的元素下标直写（2026-11-12 起与 str 同案禁止）。
//
// 理由：`t[i] = v` 是**按码点重编码写回**，长度会随写入隐式改变 —— i == 码点数时
// 等價追加、超 255 字节的部分静默丢弃。「一次下标写偷偷改了长度」正是 idxwrite 要
// 消灭的东西，txt 虽然无堆、无扩容、元素型别也是 char，但长照样会被改掉。
//
// 函数内的那一条在禁令扩到 txt 之前就已经由 ContainerElemType("txt") == "char"
// 命中（isContainerWriteBase 末尾的通用容器分支），此处作为回归锁；顶层 / struct
// 字段 / 结果参数三条是本次补的登记点。
func TestIndexWriteTxtReported(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "in-function-typed",
			src: `f = () () {
    t txt = 'ab'
    t[0] = "Z"
}`,
			want: 1,
		},
		{
			name: "top-level-untyped-literal",
			src: `t txt = 'ab'
t[0] = "Z"`,
			want: 1,
		},
		{
			// 结果参数 + 体内无标注：IdxLocalTypes 查不到，靠 resStrTxtParams 兜底。
			name: "result-param-untyped-rebind",
			src: `f = () (out txt) {
    out = 'ab'
    out[0] = "Z"
}`,
			want: 1,
		},
	}
	for _, tc := range cases {
		prog := parseProg(t, tc.src)
		res := ValidateIndexWrite(prog, "src/app.no")
		if len(res) != tc.want {
			t.Fatalf("%s: got %d diagnostics, want %d: %+v", tc.name, len(res), tc.want, res)
		}
		if tc.want > 0 && res[0].TraceID != indexWriteTraceID {
			t.Fatalf("%s: TraceID = %q, want %q", tc.name, res[0].TraceID, indexWriteTraceID)
		}
	}
}

// TestIndexWriteStrTxtStructFieldTopLevelReported 钉住**顶层 struct 字段基底**
// （`b.t[0] = v` / `b.s[0] = v`）。
//
// 背景：旧的 topStrNames 只处理 `*parser.Identifier` 基底，`b.t[0] = "J"` 永远
// 查不到型别（tests/byte.no:226 的漏报就是这么来的）。collectTopLevelVarRawForIdx
// 把顶层 `b = box{…}` 的 struct 名收进表，topIndexBaseRaw 再按键查 structFields，
// 第四条登记点才成立。
//
// 🔴 这里刻意把 str 与 txt 一起钉：两者走同一条基底解析路径，只测一半会让
// 「某天有人只留 str 分支」静默过关。
func TestIndexWriteStrTxtStructFieldTopLevelReported(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "txt-field",
			src: `box {
    s str
    t txt
}
b = box{
    s: 'hello'
    t: 'hi'
}
b.t[0] = "J"`,
			want: 1,
		},
		{
			name: "str-field",
			src: `box {
    s str
    t txt
}
b = box{
    s: 'hello'
    t: 'hi'
}
b.s[0] = "J"`,
			want: 1,
		},
		{
			name: "both-fields-two-reports",
			src: `box {
    s str
    t txt
}
b = box{
    s: 'hello'
    t: 'hi'
}
b.s[0] = "Z"
b.t[0] = "J"`,
			want: 2,
		},
	}
	for _, tc := range cases {
		prog := parseProg(t, tc.src)
		res := ValidateIndexWrite(prog, "src/app.no")
		if len(res) != tc.want {
			t.Fatalf("%s: got %d diagnostics, want %d: %+v", tc.name, len(res), tc.want, res)
		}
		for _, r := range res {
			if r.TraceID != indexWriteTraceID {
				t.Fatalf("%s: TraceID = %q, want %q", tc.name, r.TraceID, indexWriteTraceID)
			}
		}
	}
}

// TestIndexWriteTopLevelNonStrTxtNotReported 是**防过界守门**：
// 顶层变数表只许用来把 str / txt 填进 lt，绝不许整体赋值。一旦有人写成
// `lt = topIndexBaseRaw(...)`，顶层 []byte / []i64 / [4]i64 的直写就会掉进
// isContainerWriteBase 末尾的 return true —— 顶层 curFunc 为空、capBases 没有条目，
// 于是大批 tests/*.no 的顶层数组写一起翻红。本测试就是那条边界的警报器。
func TestIndexWriteTopLevelNonStrTxtNotReported(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"bytes-with-len", `b []byte = with-len(4)
b[0] = 89`},
		{"bytes-with-cap-exempt", `b []byte = with-cap(4)
b[0] = 89`},
		{"slice-i64-with-len", `v []i64 = with-len(4)
v[0] = 7`},
		{"fixed-array", `a [4]i64 = [1, 2, 3, 4]
a[0] = 9`},
		{"struct-non-strtxt-field", `box {
    n i64
}
b = box{
    n: 1
}
b.n = 2`},
	}
	for _, tc := range cases {
		prog := parseProg(t, tc.src)
		res := ValidateIndexWrite(prog, "src/app.no")
		if len(res) != 0 {
			t.Fatalf("%s: must stay silent, got %d: %+v", tc.name, len(res), res)
		}
	}
}
