package parser

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
)

// TestCaptureFactsRecorded 驗證「結構捕獲」的判定結果被物化進副表（S3-b 第一步）。
//
// 本表當下只寫不讀（無消費方），故這裡只斷言記錄面。核心不變量是**對偶**：
//
//	登記 1 條  ⟺  AST 真的被注入 `__cap_N` 合成節點（V4 違規面）
//
// 兩側都斷言，才能保證副表的記錄面與「真正需要搬到 HIR 層的那一族」完全重合——
// 這是 S3-b 切換（tohir 改讀副表產 KLet + KIf 鏈）唯一依賴的前提。
//
// ── 為什麼「純字面量算術」不登記（排查結論）──
// `x = 1 + 4 / 2` 之所以 CaptureFactsLen=0，不是 bug：`signedIntOperandType`
// 對整數字面量直接回 i64，兩側都能判定 ⇒ `inferOptionArith` 成功推出 `?i64`
// 並拉起 `InferredByOptionArith` ⇒ maybeCaptureOptionAssign 在寫入點之前的
// 閘門（lowering.go:1197）就 return nil。整條短路由註解明確記載：「算術根且
// 已推斷 ?T ⇒ 本 pass 不介入，codegen 逐層包裹」。
//
// ── 為什麼「安全索引根」不登記，且**不是缺口**（實測更正）──
// `x = p[i]` 走 lowering.go:1208-1213，只合成 `?elem` 交 codegen，**實測
// 合成 let = 0、`__cap_` 標識 = 0** ⇒ 它根本不注入 AST 節點，不屬 V4 違規，
// 不在 S3 範圍內。其 `?elem` 已由 S3 的 `sem.InferredType` 承載
// （見 s3_inferred_side_table_test.go 對 `x = p[i]` 的 `?i64` 斷言）。
//
// ⚠️ 這條一度被誤判為「副表覆蓋缺口」（2026-10-24 上一批），實測後更正：
// 本表只需覆蓋「結構 desugar 族」，索引根不需記錄。
func TestCaptureFactsRecorded(t *testing.T) {
	// ① API 往返：Set → Of 讀回相同內容。
	sem := &SemanticContext{}
	ls := &LetStatement{}
	want := &CaptureFact{Inner: "i64", Binds: []CaptureBind{{Expr: ls, Inner: "i64", Name: "__cap_1"}}}
	sem.SetCaptureFact(ls, want)
	if got := sem.CaptureFactOf(ls); got == nil || got.Inner != "i64" || len(got.Binds) != 1 || got.Binds[0].Name != "__cap_1" {
		t.Fatalf("CaptureFactOf round-trip = %+v, want %+v", got, want)
	}
	if got := sem.CaptureFactsLen(); got != 1 {
		t.Errorf("CaptureFactsLen = %d, want 1", got)
	}
	// 未登記的節點必須回 nil（「未判定」與「判定不捕獲」有別）。
	if got := sem.CaptureFactOf(&LetStatement{}); got != nil {
		t.Errorf("CaptureFactOf(unrecorded) = %+v, want nil", got)
	}
	// nil receiver 容忍（同 TypeFacts 的既有口徑）。
	var nilSem *SemanticContext
	if got := nilSem.CaptureFactOf(ls); got != nil {
		t.Errorf("nil sem CaptureFactOf = %+v, want nil", got)
	}

	// ② 真實語料：明確非 option 的綁定不得登記（副表不得污染既有路徑）。
	p := New(lexer.New("main = (b i64) {\n\ta i64 = b\n}\n"))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	p.LowerSurface(prog)
	if got := prog.Sem.CaptureFactsLen(); got != 0 {
		t.Errorf("plain binding CaptureFactsLen = %d, want 0", got)
	}

	// ③ 真實語料：走到結構捕獲的綁定必須登記 —— 且**必須真的注入了合成節點**
	//    （對偶的「有登記」側）。
	//
	// 只能斷言**條數**，不能斷言內容：maybeCaptureOptionAssign 會把原 ls
	// 整條替換成 BlockStatement（預宣告 + match 三臂），原指針已不在樹裡，
	// 無法用 findLetByName 查回做 CaptureFactOf。內容形狀由 ① 的往返覆蓋。
	captured := []struct {
		name string
		src  string
	}{
		// 算術根含 option 運算元：`inferOptionArith` 推不出 ?T
		// （signedIntOperandType(o) 對 `?i64` 失敗）⇒ 走結構捕獲。
		{"opt-operand", "main = () {\n\to ?i64 = 1\n\tx = o + 1\n}\n"},
		// 巢狀安全索引：根是算術，索引在運算元位置被就地捕獲。
		{"nested-safe-index", "main = () {\n\tp []i64 = [1, 2, 3]\n\ti i64 = 0\n\tx = p[i] + 1\n}\n"},
	}
	for _, c := range captured {
		p := New(lexer.New(c.src))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("[%s] parse errors: %v", c.name, p.Errors())
		}
		p.LowerSurface(prog)
		if got := prog.Sem.CaptureFactsLen(); got != 1 {
			t.Errorf("[%s] CaptureFactsLen = %d, want 1", c.name, got)
		}
		// 對偶的另一半：登記了就必須真的做了結構 desugar。缺這一側的斷言，
		// 副表可能記錄到「其實沒注入合成節點」的綁定，S3-b 切換後 tohir
		// 會為它們憑空建 KLet + KIf 鏈。
		if caps, lets := syntheticInjection(prog); caps == 0 || lets == 0 {
			t.Errorf("[%s] CaptureFactsLen=1 but no synthetic AST injection (caps=%d lets=%d); "+
				"the side table must record exactly the V4-violating family", c.name, caps, lets)
		}
	}

	// ④ 對偶的「無登記」側：被上游閘門短路的綁定不得登記，且**必須沒有**
	//    合成注入 —— 否則代表有 V4 違規面漏記錄（S3-b 切換會漏掉它）。
	notCaptured := []struct {
		name string
		src  string
	}{
		// inferOptionArith 成功 ⇒ InferredByOptionArith 閘門短路；
		// 整條算術根由 codegen 逐層包裹，不需要也不該有 __cap_N。
		{"literal-arith", "main = () {\n\tx = 1 + 4 / 2\n}\n"},
		// 安全索引根 ⇒ 索引分支提前 return nil，只合成 `?elem`
		// （由 sem.InferredType 承載），不注入 AST 節點 ⇒ 不在 S3 範圍。
		{"safe-index-root", "main = () {\n\tp []i64 = [1, 2, 3]\n\ti i64 = 0\n\tx = p[i]\n}\n"},
	}
	for _, c := range notCaptured {
		p := New(lexer.New(c.src))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("[%s] parse errors: %v", c.name, p.Errors())
		}
		p.LowerSurface(prog)
		if got := prog.Sem.CaptureFactsLen(); got != 0 {
			t.Errorf("[%s] CaptureFactsLen = %d, want 0 (short-circuited before the write point)", c.name, got)
		}
		if caps, lets := syntheticInjection(prog); caps != 0 || lets != 0 {
			t.Errorf("[%s] CaptureFactsLen=0 but synthetic AST injection happened (caps=%d lets=%d); "+
				"a V4 violation would be missed by the S3-b switch", c.name, caps, lets)
		}
	}
}

// TestCaptureFactsHIRTrackFreezesAST 驗證 S3-b 雙軌開關的前半段：開關打開時
// lowerer **只記錄、不注入合成節點** ⇒ ParseProgram 的輸出保持凍結（V4 合規），
// 且副表帶齊了 H0 產出結構所需的全部資訊。
//
// 這是 S3-b 的核心收益，也是唯一能單獨驗證的一半（另一半——H0 讀表產出
// `KLet` + `KIf` 鏈——尚未實作，故本測試不碰端到端編譯）。
//
// ⚠️ 開關為 true 時捕獲語義缺失，任一項斷言都不得依賴產物可執行。
func TestCaptureFactsHIRTrackFreezesAST(t *testing.T) {
	HIRCaptureEnabled = true
	defer func() { HIRCaptureEnabled = false }()

	src := "main = () {\n\to ?i64 = 1\n\tx = o + 1\n}\n"
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	p.LowerSurface(prog)

	// ① AST 凍結：無合成 let、無 `__cap_` 標識 —— S3-b 的全部意義所在。
	if caps, lets := syntheticInjection(prog); caps != 0 || lets != 0 {
		t.Errorf("HIR track injected synthetic AST nodes (caps=%d lets=%d); "+
			"the point of S3-b is that it must not", caps, lets)
	}
	// `x` 必須仍是原始綁定：沒被替換成 BlockStatement，也沒被寫上 `?i64`。
	x := findLetByName(t, prog, "x")
	if x.Type != nil {
		t.Errorf("x.Type = %s, want nil (inferred ?T belongs in Program.Sem, not on the AST)",
			typeString(x.Type))
	}

	// ② 型別仍進副表 —— H0 產出 `?T` 的唯一來源，開關打開時不得丟。
	if got := prog.Sem.InferredType(x); got != "?i64" {
		t.Fatalf("InferredType(x) = %q, want \"?i64\"", got)
	}

	// ③ 副表內容完整：inner / binds / wasDeclared 齊備。
	f := prog.Sem.CaptureFactOf(x)
	if f == nil {
		t.Fatal("no CaptureFact recorded for x")
	}
	if f.Inner != "i64" {
		t.Errorf("CaptureFact.Inner = %q, want \"i64\"", f.Inner)
	}
	if len(f.Binds) != 1 {
		t.Fatalf("len(Binds) = %d, want 1", len(f.Binds))
	}
	if f.Binds[0].Name != "__cap_1" || f.Binds[0].Inner != "i64" {
		t.Errorf("Binds[0] = %+v, want name=__cap_1 inner=i64", f.Binds[0])
	}
	if f.WasDeclared {
		t.Error("WasDeclared = true, want false (x is first declared by this very statement)")
	}
	// Binds[i].Expr 必須指向**原始**表達式：語義副表以原始指針為鍵，副本
	// 節點會讓 tohir 查不到型別，產出的 HIR 與今日不一致。
	id, ok := f.Binds[0].Expr.(*Identifier)
	if !ok || id.Value != "o" {
		t.Errorf("Binds[0].Expr = %#v, want the original `o` identifier", f.Binds[0].Expr)
	}
}

// TestHIRCaptureMatchesASTDesugar 是 S3-b 的**驗收門禁**：同一份語料在兩條
// 軌道下必須產出**逐字節相同**的 HIR。
//
//   - AST 軌（默認）：parser 走查期就地改寫用戶的 AST（V4 違規，今日現狀）；
//   - H0 軌（HIRCaptureEnabled）：AST 凍結，由 tohir 讀副表直產 KLet + KIf 鏈。
//
// 兩軌共用 buildCaptureChain，本測試是「共用」這一保證的唯一驗證——少了它，
// 任何一側的漂移都只會在 MIR/codegen 端以離現場很遠的症狀爆出來。
func TestHIRCaptureMatchesASTDesugar(t *testing.T) {
	defer func() { HIRCaptureEnabled = false }()
	// ⚠️ 覆蓋面說明：corpus 門禁（dump 包，623 份語料）實際只有 2 份走到結構
	// 捕獲（多數源碼用 `?=` 或 inferOptionArith 已成功 ⇒ 走不到 desugar），
	// 所以**這裡的手造語料才是等價性的主證**。下列每條都刻意對準一種捕獲形態。
	srcs := []struct {
		name string
		src  string
		// extraWant 非空時額外要求兩軌都出現該標識：用來斷言語料真的觸發了
		// 預期的捕獲形態（例如多綁定必須有 __cap_2，否則案例名是假的）。
		extraWant string
	}{
		// 單綁定：算術根含 option 運算元。
		{"opt-operand", "main = () {\n\to ?i64 = 1\n\tx = o + 1\n}\n", ""},
		// 巢狀安全索引：根是算術，索引在運算元位置被就地捕獲。
		{"nested-safe-index", "main = () {\n\tp []i64 = [1, 2, 3]\n\ti i64 = 0\n\tx = p[i] + 1\n}\n", ""},
		// 多綁定（Binds > 1）：驗證巢狀順序（bind_0 在最外層）。
		// `o2 / o3` 兩側皆 ?i64 ⇒ captureInfixInner 判不出 ⇒ 遞迴各捕獲一次。
		{"multi-bind", "main = () {\n\to1 ?i64 = 1\n\to2 ?i64 = 2\n\to3 ?i64 = 3\n\tx = o1 + o2 / o3\n}\n", "__cap_2"},
		// 浮點：inner 由 commonCaptureInner 補正為 f64（非預設 i64）。
		{"float-inner", "main = () {\n\tf1 ?f64 = 1.0\n\tf2 ?f64 = 2.0\n\tx = f1 * f2\n}\n", ""},
		// str 拼接：inner 補正為 str（`+` 運算子 + commonCaptureInner=="str"）。
		{"str-inner", "main = () {\n\ts1 ?str = 'a'\n\ts2 ?str = 'b'\n\tx = s1 + s2\n}\n", ""},
		// WasDeclared=true：目標已宣告為 ?T ⇒ H0 不得產出預宣告。
		{"was-declared", "main = () {\n\to ?i64 = 1\n\tx ?i64 = nil\n\tx = o + 1\n}\n", ""},
		// 呼叫引數內捕獲：根是呼叫（回傳 plain），可錯源在引數裡。
		// 回傳值用具名結果參數（Nolang 語法：結果寫在第二組括號裡再賦值）。
		{"call-arg", "g = (v i64) (r i64) {\n\tr = v\n}\nmain = () {\n\to ?i64 = 1\n\tx = g(o + 1)\n}\n", ""},
	}
	for _, c := range srcs {
		HIRCaptureEnabled = false
		astTrack := dumpHIR(t, c.src)
		HIRCaptureEnabled = true
		hirTrack := dumpHIR(t, c.src)
		HIRCaptureEnabled = false
		// 自校驗：兩側若都沒產出捕獲鏈，字串相等是假綠。
		if c.extraWant != "" &&
			(!strings.Contains(astTrack, c.extraWant) || !strings.Contains(hirTrack, c.extraWant)) {
			t.Fatalf("[%s] expected %s in both tracks but it is missing; the fixture does "+
				"not exercise the shape its name claims\nAST track:\n%s\nH0 track:\n%s",
				c.name, c.extraWant, astTrack, hirTrack)
		}
		if !strings.Contains(astTrack, "__cap_1") || !strings.Contains(hirTrack, "__cap_1") {
			t.Fatalf("[%s] a track produced no capture chain; the equality assertion would be vacuous\n"+
				"AST track:\n%s\nH0 track:\n%s", c.name, astTrack, hirTrack)
		}
		if astTrack != hirTrack {
			t.Errorf("[%s] HIR differs between tracks:\n--- AST desugar ---\n%s\n--- H0 direct ---\n%s",
				c.name, astTrack, hirTrack)
		}
	}
}

// dumpHIR 走完整 ParseProgram → LowerSurface → ASTToHIR 並回傳 HIR dump。
func dumpHIR(t *testing.T, src string) string {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse errors: %v", p.Errors())
	}
	p.LowerSurface(prog)
	pkg, _ := ASTToHIRWithMap(prog)
	return pkg.Dump()
}

// TestCaptureFactWasDeclared 覆蓋 CaptureFact.WasDeclared —— H0 產不產預宣告
// 全靠它，而它活在 lowerer 的走查期作用域裡，tohir 無法自行判定。
//
// ⚠️ 為什麼要單獨斷言、而不是靠「產物差異」：目標若已宣告為 `?T`，合成預宣告
// `x ?T = nil` 與使用者自己寫的 `x ?T = nil` **等價** ⇒ 兩軌 MIR 位元組相同，
// 看不出 WasDeclared 有沒有生效（實測 was-declared 與 opt-operand 的 MIR 皆
// 1155 bytes）。所以這裡直接讀副表，是唯一能證明該欄位正確的斷言。
func TestCaptureFactWasDeclared(t *testing.T) {
	HIRCaptureEnabled = true
	defer func() { HIRCaptureEnabled = false }()

	// ① 目標首次由本陳述宣告 ⇒ WasDeclared=false（H0 須產預宣告）。
	// ② 目標已宣告為 ?T ⇒ WasDeclared=true（H0 不得產預宣告，否則是把已
	//    初始化的變數清成 nil）。
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"first-declared", "main = () {\n\to ?i64 = 1\n\tx = o + 1\n}\n", false},
		{"pre-declared", "main = () {\n\to ?i64 = 1\n\tx ?i64 = nil\n\tx = o + 1\n}\n", true},
	}
	for _, c := range cases {
		p := New(lexer.New(c.src))
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("[%s] parse errors: %v", c.name, p.Errors())
		}
		p.LowerSurface(prog)
		// H0 軌下原 LetStatement 不被替換、仍在樹裡 ⇒ 可由副表查回。
		var got *CaptureFact
		walkNodes(prog, func(v interface{}) {
			ls, ok := v.(*LetStatement)
			if !ok || ls == nil {
				return
			}
			if f := prog.Sem.CaptureFactOf(ls); f != nil {
				got = f
			}
		})
		if got == nil {
			t.Fatalf("[%s] no CaptureFact recorded", c.name)
		}
		if got.WasDeclared != c.want {
			t.Errorf("[%s] WasDeclared = %v, want %v", c.name, got.WasDeclared, c.want)
		}
	}
}

// syntheticInjection 回報 prog 中的 V4 違規面：`__cap_N` 標識出現次數與
// IsSynthetic 的 LetStatement 數量。
//
// 兩者皆 0 代表該語料未注入合成 AST 節點，不屬結構捕獲族（如安全索引根
// `x = p[i]`）⇒ 不應進 CaptureFacts 副表。這是 ③④ 兩組斷言的對偶基礎：
// 副表的記錄面必須與「真正注入合成節點的那一族」完全重合。
func syntheticInjection(prog *Program) (capIdents int, synthLets int) {
	walkNodes(prog, func(v interface{}) {
		switch n := v.(type) {
		case *Identifier:
			if strings.HasPrefix(n.Value, "__cap_") {
				capIdents++
			}
		case *LetStatement:
			if n.IsSynthetic {
				synthLets++
			}
		}
	})
	return
}
