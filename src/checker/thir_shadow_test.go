package checker

// thir_shadow_test.go — Stage 6C 影子双跑门禁。
//
// 对拍对象：第一批迁移（FuncArgCount / DivByZero / StrOrdering）的旧版 vs
// THIR 版诊断多重集，键 = (File,Line,Column,TraceID,Message)。
// 另附签名提取忠实性对拍（THIRView.FuncSigs ≡ funcSigFromDef 逐字段全等）
// ——这是 FuncArgCount 迁移的真正风险面（签名来源从 AST 走查换到 HIR 提取）。
//
// 语料：合成靶向语料（三检查各覆盖触发/不触发/糖形态）+ tests/*.no 全语料。

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// shadowBuildView 解析 src 并构造 THIRView（BuildTHIRView：ASTToHIRWithMap +
// PopulateInferredTypes，含 H0 副作用隔離——与 6D 生産路径 runLintPipeline /
// transpiler 硬錯誤塊一致）。
func shadowBuildView(t *testing.T, src string) (*parser.Program, *THIRView) {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	view := BuildTHIRView(prog)
	if view == nil {
		t.Fatalf("BuildTHIRView returned nil\n--- src ---\n%s", src)
	}
	return prog, view
}

// shadowSynth 是三检查的靶向合成语料：每条只关心第一批迁移的多重集对拍，
// 不断言诊断有无（那是各规则自身测试的职责）。语法遵循 Nolang：字符串字面量
// 用单引号（双引号是 char），`ident < ident` 有泛型歧义（比较用 >= / <=）。
var shadowSynth = []struct{ name, src string }{
	// FuncArgCount：少参 / 默认参数 / 多参（容许）/ 糖形态 match 臂内呼叫
	{"args-short", "foo = (a i64, b i64) {\n}\nmain = () {\n\tfoo(1)\n}\n"},
	{"args-default", "foo = (a i64, b i64 = 2) {\n}\nmain = () {\n\tfoo(1)\n}\n"},
	{"args-ok", "foo = (a i64, b i64) {\n}\nmain = () {\n\tfoo(1, 2)\n}\n"},
	{"args-in-match", "foo = (a i64) {\n}\nmain = (x ?i64) {\n\tx: {\n\t\ti64 -> foo()\n\t\t-> {}\n\t}\n}\n"},
	// DivByZero：触发 / 浮点字面除数不触发 / 取模 / 负零
	{"div-zero", "main = (a i64) {\n\tv i64 = a / 0\n}\n"},
	{"div-float-lit", "main = (a f64) (r f64) {\n\tr = a / 0.0\n}\n"},
	{"mod-zero", "main = (a i64) {\n\tv i64 = a % 0\n}\n"},
	{"div-neg-zero", "main = (a i64) {\n\tv i64 = a / -0\n}\n"},
	// StrOrdering：多字元字串 / 单字元合法 / 数值合法 / match 区间臂去重面
	// （注意 `if a >= b { }` 会把 `b { }` 解析成 struct 字面量——比较须放
	// print()/赋值右值等无歧义位置，见 str_ordering_test.go 同款语料。）
	{"str-order-vars", "main = () {\n\ta str = 'e'\n\tb str = 'a'\n\tprint(a >= b)\n}"},
	{"str-order-char", "main = () {\n\tprint('e' >= 'a')\n}\n"},
	{"str-order-num", "main = () {\n\tn i64 = 3\n\tprint(n > 1 && n < 5)\n}\n"},
	{"str-order-param", "f = (v str) (r bool) {\n\tr = v > 'a'\n}\n"},
	{"str-order-match-range", "main = (c char) {\n\tc: {\n\t\t[\"a\"..\"z\") -> {}\n\t\t-> {}\n\t}\n}\n"},
	// StringConcat：`+` 搭 StringLiteral 触发 / 纯数值不触发
	{"concat-lit", "f = (a str) {\n\tb = a + 'x'\n}\n"},
	{"concat-num", "f = (a i64, b i64) {\n\tc = a + b\n}\n"},
	// OptionComparison：option vs struct 报 / option vs 纯量不报 / ?= 解包后不报
	{"optcmp-struct", "point = (x i64, y i64) {}\nf = () () {\n\tp ?point = make-point()\n\tq point = point { x: 1, y: 2 }\n\tp == q -> return\n}"},
	{"optcmp-scalar", "f = () () {\n\tr ?i64 = get-size()\n\tr == 0 -> return\n}"},
	{"optcmp-unwrap", "get-size = (x i64) (r ?i64) { r = x; return }\nf = (fd i64) (out ?i64) {\n\tsize ?= get-size(fd)\n\tsize == 0 -> return\n}"},
	// IntOverflow：未標註整數算術觸發 / #{overflow} 豁免 / 浮點與無號除法不觸發 /
	// ?T 目標豁免（結果參數與 let）/ 非具名參數保守觸發 / str let 判非整數。
	// 判別力要點：ovf-float、ovf-u-div、ovf-opt-result、ovf-str-let 若登記值丟失
	// （declTypeOf 回退錯誤），舊版 0/1 報告會翻轉 → 影子對拍必須紅。
	{"ovf-arith", "main = (a i64, b i64) {\n\tprint(a + b)\n}\n"},
	{"ovf-wrap", "main = (a i64, b i64) {\n\t#{overflow=wrap}\n\tprint(a + b)\n}\n"},
	{"ovf-float", "main = (a f64, b f64) {\n\tprint(a + b)\n}\n"},
	{"ovf-u-div", "f = (a u64, b u64) (r u64) {\n\tr = a / b\n}\n"},
	{"ovf-s-div", "f = (a i64, b i64) (r i64) {\n\tr = a / b\n}\n"},
	{"ovf-opt-result", "f = (a i64) (q ?i64) {\n\tq = a + 1\n}\n"},
	{"ovf-opt-let", "main = (a i64) {\n\tv ?i64 = a + 1\n}\n"},
	{"ovf-named-param", "f = (a ?i64) {\n\tprint(a + 1)\n}\n"},
	{"ovf-str-let", "main = () {\n\ts str = 'x'\n\tprint(s + 1)\n}\n"},
	// InterfaceImplementation：結果型別不符 / 參數型別不符 / 簽名相符不報 /
	// 未宣告實作的 struct 不比對（範圍限定）。判別力要點：iface-result-mismatch
	// 與 iface-param-mismatch 若簽名字串丟失/錯值，報/不報會翻轉 → 影子必須紅。
	{"iface-result-mismatch", "ord {\n\tt.gt(b t) (res bool)\n}\n\ni8.gt = (b i8) (res i64) {\n\tres = 0\n}\n"},
	{"iface-param-mismatch", "ord {\n\tt.gt(b t) (res bool)\n}\n\ni8.gt = (b f64) (res bool) {\n\tres = true\n}\n"},
	{"iface-ok", "ord {\n\tt.gt(b t) (res bool)\n}\n\ni8.gt = (b i8) (res bool) {\n\tres = . > b\n}\n"},
	{"iface-unscoped", "db enter, leave {\n\tfs.close() (ok bool)\n}\n\nlistener {\n\tfd fd\n}\n\nlistener.close = () {\n}\n"},
	{"iface-count-mismatch", "db enter, leave {\n\tfs.close() (ok bool)\n}\n\ndb-mysql db {\n\tfd fd\n}\n\ndb-mysql.close = () {\n}\n"},
	// LHSInferredBuiltins：with-len/with-cap 無宣告型別觸發 / 有宣告不觸發 /
	// 先宣告後重複指派不觸發（hasExistingType 存在性路徑）/ 參數已帶型別不觸發。
	// 硬闸判別力：登記 gate 若被「副表有型別」擴大（行為變更），lhs-plain 的
	// 報告會被吞掉 → 影子必須紅。map 登記值本身是死資料（僅存在性被查詢），
	// 值路由走 declTypeOf 是統一口徑，不承載行為。
	{"lhs-plain", "save = (path str) (ok bool) {\n\tn = 14\n\tbuf = with-len(n)\n\tok = true\n}\n"},
	{"lhs-annotated", "save = (path str) (ok bool) {\n\tn = 14\n\tbuf []byte = with-len(n)\n\tok = true\n}\n"},
	{"lhs-reassign", "save = (path str) (ok bool) {\n\tn = 14\n\tbuf []byte = with-len(n)\n\tm = 20\n\tbuf = with-len(m)\n\tok = true\n}\n"},
	{"lhs-cap", "grow = () (ok bool) {\n\tn = 14\n\tbuf = with-cap(n)\n\tok = true\n}\n"},
	{"lhs-param-typed", "f = (buf []byte) {\n\tbuf = with-len(1)\n}\n"},
	// StrIndexComplexity：未證明 ASCII 的 str 在迴圈內 s[i] 觸發 / ASCII 字面量
	// 豁免 / byte() 豁免。判別力要點：scopeTypes/funcReturns 是活數據（型別丟失
	// → 查不到 str → 跳過告警），sidx-param-fire、sidx-let-decl（宣告值走
	// declTypeOf）、sidx-let-infer（推斷面調用回傳）三條若登記值丟失必須翻轉
	// 1/0 → 影子必須紅。
	{"sidx-param-fire", "b = (s str) {\n\tn = 0\n\ti <- [0..s.len-bytes()): {\n\t\tc = s[i]\n\t}\n}\n"},
	{"sidx-let-decl", "f = () (r str) {\n}\nprobe = () {\n\tt str = f()\n\tn = 0\n\ti <- [0..t.len-bytes()): {\n\t\tc = t[i]\n\t}\n}\n"},
	{"sidx-let-infer", "f = () (r str) {\n}\nprobe = () {\n\tt = f()\n\tn = 0\n\ti <- [0..t.len-bytes()): {\n\t\tc = t[i]\n\t}\n}\n"},
	{"sidx-lit-exempt", "probe = () {\n\tt str = '/a/b'\n\tn = 0\n\ti <- [0..t.len-bytes()): {\n\t\tc = t[i]\n\t}\n}\n"},
	{"sidx-byte-exempt", "probe = (s str) {\n\tn = 0\n\ti <- [0..s.len-bytes()): {\n\t\tc = s.byte(i)\n\t}\n}\n"},
	// UnhandledOverflow：沉默泄漏觸發 / #{overflow=wrap} 豁免 / ?= 上拋豁免 /
	// 字串拼接不觸發 / 非 option 函式回傳算術觸發。判別力要點：seedVarTypes /
	// let 登記值是活數據（判定運算元是否整數），uhof-leak、uhof-str-concat
	// 若登記值丟失會翻轉報告 → 影子必須紅。
	{"uhof-leak", "main = () () {\n\ta i64 = 5\n\tb i64 = 10\n\tx = a + b\n\tio.outln(x)\n}\n"},
	{"uhof-wrap", "main = () () {\n\ta i64 = 5\n\tb i64 = 10\n\t#{overflow=wrap}\n\tx = a + b\n\tio.outln(x)\n}\n"},
	{"uhof-unwrap", "add = (a i64, b i64) (out ?i64) {\n\tout ?= a + b\n}\n"},
	{"uhof-str-concat", "f = (seg str) () {\n\tr str = 'ab'\n\tr = r - seg\n\tio.outln(r)\n}\n"},
	{"uhof-return", "add = (a i64, b i64) (out i64) {\n\tout = a + b\n}\n"},
	// UnhandledIndex：未處理索引讀取觸發 / ?= 豁免 / #{index-out} 豁免。
	// 判別力要點：uhix-let 的 option 宣告判定字串若錯值（?T 前綴翻轉）會改報告。
	{"uhix-let", "get = (arr []i64, i i64) (res i64) {\n\tres = arr[i]\n}\n"},
	{"uhix-unwrap", "get = (arr []i64, i i64) (res ?i64) {\n\tres ?= arr[i]\n}\n"},
	{"uhix-annot", "get = (arr []i64, i i64) (res i64) {\n\tres = arr[i]  #{index-out = 0}\n}\n"},
	// IndexWrite：下標直寫觸發 / map 放行。
	{"ixw-fire", "set = () () {\n\ta [3]i64 = [1, 2, 3]\n\ta[1] = 2\n}\n"},
	{"ixw-map", "set = (m [str]i64) () {\n\tm['k'] = 1\n}\n"},
	// FuncArgs（實參型別相容，與 FuncArgCount 不同規則）：型別不符觸發 / 相符不觸發 /
	// let+param 註冊後不符觸發（登記值是活數據）/ 預設參數不觸發 / 回傳數不符觸發
	// （sigs 換源判別力：faca-rfcnt 若簽名表錯值會翻轉 rfzrw1nh）。
	{"faca-mismatch", "f = (a i64) {\n}\nmain = () {\n\ts str = 'x'\n\tf(s)\n}\n"},
	{"faca-ok", "f = (a i64) {\n}\nmain = () {\n\tf(1)\n}\n"},
	{"faca-param-reg", "g = (b str) {\n}\nf = (x i64, y str) {\n\tg(y)\n\tg(x)\n}\n"},
	{"faca-default", "foo = (a i64, b i64 = 2) {\n}\nmain = () {\n\tfoo(1)\n}\n"},
	{"faca-rfcnt", "two = () (p i64, q i64) {\n\tp = 1\n\tq = 2\n}\nmain = () {\n\ta, b = two()\n\ta, b, c = two()\n}\n"},
	// ValidateTypes：型別不符指派觸發 / nil 指派觸發 / 相符不觸發 / 推斷後不符觸發
	// （funcTypes+varTypes 是活數據）/ 重複定義觸發（簽名鍵換源判別力）。
	{"vt-mismatch", "main = () {\n\tx i64 = 1\n\tx = 'str'\n}\n"},
	{"vt-nil", "main = () {\n\tx i64 = 1\n\tx = nil\n}\n"},
	{"vt-ok", "main = () {\n\tx i64 = 1\n\tx = 2\n}\n"},
	{"vt-infer-float", "main = () {\n\tz f64 = 2.0\n\ta = z * z\n\tb i64 = a\n}\n"},
	{"vt-dup-fn", "f = (a i64) {\n}\nf = (a i64) {\n}\n"},
	// 混合：match 糖形态 + 多规则交叉
	{"mixed-sugar", "main = (x ?i64) {\n\ta str = 'e'\n\tb str = 'a'\n\tx: {\n\t\ti64 -> print(a >= b)\n\t\t-> {}\n\t}\n\tv i64 = 1 / 0\n}\n"},
}

// TestTHIRShadowDualRun 是 6C 门禁主测试：语料上 ShadowCompareTHIR 须空。
func TestTHIRShadowDualRun(t *testing.T) {
	check := func(name, src string) {
		t.Helper()
		prog, view := shadowBuildView(t, src)
		if ms := ShadowCompareTHIR(prog, view); len(ms) > 0 {
			for _, m := range ms {
				t.Errorf("%s: mismatch: %s", name, m)
			}
		}
	}
	for _, s := range shadowSynth {
		check("synth/"+s.name, s.src)
	}
	matches, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.no"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) < 100 {
		t.Fatalf("corpus too small: %d", len(matches))
	}
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatalf("read %s: %v", m, err)
		}
		check(m, string(b))
	}
}

// TestTHIRFuncSigExtractionMatchesAST 对拍 THIRView.FuncSigs（HIR KFuncDef
// 提取）与 AST funcSigFromDef 收集的签名表逐字段全等——FuncArgCount 迁移
// 的忠实性证明。
func TestTHIRFuncSigExtractionMatchesAST(t *testing.T) {
	compare := func(name, src string) {
		t.Helper()
		prog, view := shadowBuildView(t, src)
		astSigs := make(map[string]*funcSig)
		for _, stmt := range prog.Statements {
			if fd, ok := stmt.(*parser.FunctionDefinition); ok {
				astSigs[fd.Name] = funcSigFromDef(fd)
			}
		}
		hirSigs := view.FuncSigs()
		for fn, as := range astSigs {
			hs, ok := hirSigs[fn]
			if !ok {
				t.Errorf("%s: fn %q present in AST sigs but missing from HIR FuncSigs", name, fn)
				continue
			}
			if !reflect.DeepEqual(as, hs) {
				t.Errorf("%s: fn %q sig mismatch\n  AST: %+v\n  HIR: %+v", name, fn, as, hs)
			}
		}
		// HIR 可以有 AST 没有的名字——H0 会为 `co`/`go` 合成 `-async` 单态化
		// 副本（MonomorphizeGo），它们不是源码里的 FunctionDefinition。只要求
		// 反向：AST 签名必须被 HIR 忠实保留（上面已逐字段对拍）。
	}
	for _, s := range shadowSynth {
		compare("synth/"+s.name, s.src)
	}
	matches, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.no"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatalf("read %s: %v", m, err)
		}
		compare(m, string(b))
	}
}

// TestTHIRShadowSanity 钉住对拍的敏感度：已知触发诊断在两变体上同键同数，
// 防止「双跑恒空」假通过（对拍器坏掉时它必须红）。
func TestTHIRShadowSanity(t *testing.T) {
	cases := []struct {
		name string
		src  string
		run  func(*parser.Program, *THIRView) []ValidateResult
		old  func(*parser.Program) []ValidateResult
	}{
		// div-by-zero：legacy 侧传 nil view（純語法、精化前口徑）；整數除零用例
		// 兩側都必須報 ⇒ 全等。浮點精化（被除數 f64 跳過）的行為差異由
		// TestDivByZeroFloatRefine / TestDivByZeroRefineOnlyRemovesReports 鎖定。
		{"div-by-zero", "main = (a i64) {\n\tv i64 = a / 0\n}\n", ValidateDivByZeroTHIR,
			func(p *parser.Program) []ValidateResult { return ValidateDivByZero(p, nil) }},
		{"func-args", "foo = (a i64, b i64) {\n}\nmain = () {\n\tfoo(1)\n}\n", ValidateFuncArgCountTHIR, ValidateFuncArgCount},
		{"str-ordering", "main = () {\n\ta str = 'e'\n\tb str = 'a'\n\tprint(a >= b)\n}", ValidateStrOrderingTHIR, ValidateStrOrdering},
		// string-concat：legacy 侧传 nil view（純語法、只認 StringLiteral）。字面量
		// 用例兩側都必須報 ⇒ 全等。`str+str` 補報的行為差異由 TestStringConcatRefine 鎖定。
		{"string-concat", "f = (a str) {\n\tb = a + 'x'\n}\n", ValidateStringConcatTHIR,
			func(p *parser.Program) []ValidateResult { return ValidateStringConcat(p, nil) }},
		{"option-comparison", "point = (x i64, y i64) {}\nf = () () {\n\tp ?point = make-point()\n\tq point = point { x: 1, y: 2 }\n\tp == q -> return\n}", ValidateOptionComparisonTHIR, ValidateOptionComparison},
		// int-overflow：未標註有號整數加法必須報（ovf-int-default），新舊同多重集。
		{"int-overflow", "main = (a i64, b i64) {\n\tprint(a + b)\n}\n", ValidateIntOverflowTHIR, ValidateIntOverflow},
		// interface-impl：結果型別不符必須報（expected 'bool'），新舊同多重集。
		{"interface-impl", "ord {\n\tt.gt(b t) (res bool)\n}\n\ni8.gt = (b i8) (res i64) {\n\tres = 0\n}\n", ValidateInterfaceImplementationTHIR, ValidateInterfaceImplementation},
		// lhs-inferred-builtins：with-len 無宣告型別必須報（cannot infer type），新舊同多重集。
		{"lhs-inferred-builtins", "save = (path str) (ok bool) {\n\tn = 14\n\tbuf = with-len(n)\n\tok = true\n}\n", ValidateLHSInferredBuiltinsTHIR, ValidateLHSInferredBuiltins},
		// str-index-complexity：未證明 ASCII 的 str 參數在迴圈內 s[i] 必須報（STR_INDEX_COMPLEXITY），新舊同多重集。
		{"str-index-complexity", "b = (s str) {\n\tn = 0\n\ti <- [0..s.len-bytes()): {\n\t\tc = s[i]\n\t}\n}\n", ValidateStrIndexComplexityTHIR, ValidateStrIndexComplexity},
		// unhandled-overflow：沉默泄漏必須報（ovfhndld/optunuse），新舊同多重集（mainFile 固定同参）。
		{"unhandled-overflow", "main = () () {\n\ta i64 = 5\n\tb i64 = 10\n\tx = a + b\n\tio.outln(x)\n}\n",
			func(p *parser.Program, v *THIRView) []ValidateResult {
				return ValidateUnhandledOverflowTHIR(p, "src/app.no", v)
			},
			func(p *parser.Program) []ValidateResult { return ValidateUnhandledOverflow(p, "src/app.no") }},
		// unhandled-index：未處理索引讀取必須報（idxhndld），新舊同多重集。
		{"unhandled-index", "get = (arr []i64, i i64) (res i64) {\n\tres = arr[i]\n}\n",
			func(p *parser.Program, v *THIRView) []ValidateResult {
				return ValidateUnhandledIndexTHIR(p, "src/app.no", v)
			},
			func(p *parser.Program) []ValidateResult { return ValidateUnhandledIndex(p, "src/app.no") }},
		// index-write：下標直寫必須報（idxwrite），新舊同多重集。
		{"index-write", "set = () () {\n\ta [3]i64 = [1, 2, 3]\n\ta[1] = 2\n}\n",
			func(p *parser.Program, v *THIRView) []ValidateResult {
				return ValidateIndexWriteTHIR(p, "src/app.no", v)
			},
			func(p *parser.Program) []ValidateResult { return ValidateIndexWrite(p, "src/app.no") }},
		// func-args-compat：型別不符必須報，新舊同多重集（rootDir 同参）。
		{"func-args-compat", "f = (a i64) {\n}\nmain = () {\n\ts str = 'x'\n\tf(s)\n}\n",
			func(p *parser.Program, v *THIRView) []ValidateResult { return ValidateFuncArgsTHIR(p, "", v) },
			func(p *parser.Program) []ValidateResult { return ValidateFuncArgs(p, "") }},
		// validate-types：型別不符指派必須報（15w45dqk），新舊同多重集。
		{"validate-types", "main = () {\n\tx i64 = 1\n\tx = 'str'\n}\n", ValidateTypesTHIR, ValidateTypes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, view := shadowBuildView(t, tc.src)
			old := tc.old(prog)
			cur := tc.run(prog, view)
			if len(old) == 0 {
				t.Fatalf("legacy variant reported 0 diagnostics; corpus case no longer exercises the rule")
			}
			if len(cur) != len(old) {
				t.Fatalf("count mismatch: old=%d new=%d", len(old), len(cur))
			}
			om, cm := shadowMultiset(old), shadowMultiset(cur)
			for k, c := range om {
				if cm[k] != c {
					t.Errorf("diag multiset mismatch: key=%q old=%d new=%d", k, c, cm[k])
				}
			}
		})
	}
}

// TestTHIRViewTypeOfSmoke 只钉 TypeOf 的管道连通性：Parameter 节点应能经
// idMap 取到宣告型别；表达式节点当下多半为 ""（见 TypeOf 注释）——后者
// 只断言不 panic、不假造型别，不断言具体值（型别覆盖面是后续批次的事）。
func TestTHIRViewTypeOfSmoke(t *testing.T) {
	const src = `foo = (a i64, b str) (r i64) {
	return a
}
`
	prog, view := shadowBuildView(t, src)
	var fd *parser.FunctionDefinition
	for _, stmt := range prog.Statements {
		if f, ok := stmt.(*parser.FunctionDefinition); ok {
			fd = f
		}
	}
	if fd == nil {
		t.Fatal("no FunctionDefinition in fixture")
	}
	if got := view.TypeOf(fd.Parameters[0]); got != "i64" {
		t.Errorf("TypeOf(param a) = %q, want i64", got)
	}
	if got := view.TypeOf(fd.Parameters[1]); got != "str" {
		t.Errorf("TypeOf(param b) = %q, want str", got)
	}
	_ = strconv.Itoa // keep import if assertions above change shape
}
