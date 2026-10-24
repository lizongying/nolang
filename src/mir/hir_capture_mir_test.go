package mir

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestHIRCaptureTrackProducesIdenticalMIR 把 S3-b 的等價保證從 HIR 層推進到
// **MIR 層**。
//
// 已有一道門禁（parser 包的 TestHIRCaptureMatchesASTDesugar）比的是
// `hir.Package.Dump()`。但 HIR 相同不代表 MIR 相同：MIR 的型別解析有自己的
// 兜底路徑（`inferredTypeOf` 的 void 退化、`letDeclaredType` + `valueTypeOf`
// 的 nil 臂目標型別推導、owned option 的 drop 歸屬），任何一處對「新形態
// KLet」的識別不同，都會在 HIR 完全一致的情況下產出不同的 MIR。
//
// 所以這道門禁是「S3-c 敢不敢切換」的實質依據：它證明的不是「同一份 HIR」，
// 而是「同一份 MIR」——codegen 看到的东西一模一样。
//
// 语料与 parser 侧那道门禁保持一致（7 条形态），避免两处覆盖各说各话。
func TestHIRCaptureTrackProducesIdenticalMIR(t *testing.T) {
	defer func() { parser.HIRCaptureEnabled = false }()

	srcs := []struct {
		name string
		src  string
		// mustDifferFrom 非空時，要求本語料的 MIR 與該語料**不同**。
		// 用來防止「案例名不實」：was-declared 若真的關掉了預宣告，產物必然
		// 與 opt-operand 不同（實測兩者位元組數相同 ⇒ 一度疑似未生效）。
		mustDifferFrom string
	}{
		{"opt-operand", "main = () {\n\to ?i64 = 1\n\tx = o + 1\n}\n", ""},
		{"nested-safe-index", "main = () {\n\tp []i64 = [1, 2, 3]\n\ti i64 = 0\n\tx = p[i] + 1\n}\n", ""},
		{"multi-bind", "main = () {\n\to1 ?i64 = 1\n\to2 ?i64 = 2\n\to3 ?i64 = 3\n\tx = o1 + o2 / o3\n}\n", ""},
		{"float-inner", "main = () {\n\tf1 ?f64 = 1.0\n\tf2 ?f64 = 2.0\n\tx = f1 * f2\n}\n", ""},
		{"str-inner", "main = () {\n\ts1 ?str = 'a'\n\ts2 ?str = 'b'\n\tx = s1 + s2\n}\n", ""},
		// ⚠️ 不設 mustDifferFrom：WasDeclared=true 時省掉的合成預宣告
		// `x ?T = nil` 與使用者自己寫的等價 ⇒ 產物與 opt-operand 位元組相同
		// 是**正確**的，不能當成「未生效」。該欄位由 parser 包的
		// TestCaptureFactWasDeclared 直接讀副表斷言。
		{"was-declared", "main = () {\n\to ?i64 = 1\n\tx ?i64 = nil\n\tx = o + 1\n}\n", ""},
		{"call-arg", "g = (v i64) (r i64) {\n\tr = v\n}\nmain = () {\n\to ?i64 = 1\n\tx = g(o + 1)\n}\n", ""},
	}
	byName := map[string]string{}
	for _, c := range srcs {
		parser.HIRCaptureEnabled = false
		astTrack := captureTrackMIR(t, c.src)
		parser.HIRCaptureEnabled = true
		hirTrack := captureTrackMIR(t, c.src)
		parser.HIRCaptureEnabled = false
		// 自校验：两侧都空则「相等」毫无意义。
		if astTrack == "" || hirTrack == "" {
			t.Fatalf("[%s] a track produced no MIR; the equality assertion would be vacuous", c.name)
		}
		if astTrack != hirTrack {
			t.Errorf("[%s] MIR differs between the AST-desugar and H0-direct capture tracks\n"+
				"--- AST desugar ---\n%s\n--- H0 direct ---\n%s", c.name, astTrack, hirTrack)
		}
		t.Logf("[%s] MIR identical (%d bytes)", c.name, len(astTrack))
		byName[c.name] = astTrack
		if c.mustDifferFrom != "" {
			if other, ok := byName[c.mustDifferFrom]; ok && astTrack == other {
				t.Errorf("[%s] MIR is byte-identical to [%s]; the fixture does not exercise "+
					"the shape its name claims", c.name, c.mustDifferFrom)
			}
		}
	}
}

// captureTrackMIR lowers src end-to-end (ParseProgram → ASTToHIR → LowerHIR)
// under the currently-set capture track and returns the MIR dump.
func captureTrackMIR(t *testing.T, src string) string {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v\n--- src ---\n%s", p.Errors(), src)
	}
	pkg, idMap := parser.ASTToHIRWithMap(prog)
	if pkg == nil {
		t.Fatal("ASTToHIRWithMap returned nil")
	}
	parser.PopulateInferredTypes(pkg, idMap, prog.Sem)
	mod, _, _ := LowerHIR(pkg, nil)
	if mod == nil {
		t.Fatal("LowerHIR returned nil")
	}
	return mod.String()
}
