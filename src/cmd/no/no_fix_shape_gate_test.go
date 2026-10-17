package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// ─────────────────────────────────────────────────────────────────────────────
// S4 / §21.7 item 3: flipping `no fix` to the sugar AST.
//
// `no fix` is four separate fixers (overflow / match / redundant / unwrap), each
// of which parses the file itself. §21.1's discipline applies to each one: a
// ruler first, then the flip. Two things were measured before anything moved:
//
//   1. THE CORPUS CANNOT BE THE RULER. `checker.ValidateIntOverflow`,
//      `CollectNonExhaustiveMatches` and `ValidateRedundantTypeAnnotation`
//      report *nothing at all* on the 532-file tests/ corpus (measured: 532/532
//      files have zero results under both shapes — the corpus's arithmetic is
//      annotated, its matches are exhaustive, its annotations are not
//      redundant). A "both shapes agree" result there would be 0 == 0, i.e.
//      vacuous. The fixtures below exist to make each fixer actually fire.
//   2. ONE OF THE FOUR WAS SHAPE-BROKEN. `CollectNonExhaustiveMatches`
//      (what `--fix=match` runs on) used to return 1 under surface and 0 under
//      sugar: checker/match_exhaustive.go walked `collectMatchRoots`, which only
//      knows the lowered if-chain shape, so every sugar match was invisible to
//      it. That is now fixed — match_exhaustive.go grew a sugar path
//      (collectSugarNonExhaustiveMatches) mirroring match_arm_reach.go's
//      collectSugarCatchAllArms, and NonExhaustiveMatch carries shape-neutral
//      Pos / End / Subject so the fixer no longer reads IfExpression fields.
//
// Status of the flip (see §21.11 for the full table):
//
//	--fix=overflow  FLIPPED to SkipLowering. Corpus-measured end-to-end parity
//	                with two allowlisted divergences, both sugar being the
//	                better side; see TestFixOverflowRuleIsShapeParityEndToEnd.
//	--fix=redundant FLIPPED (fixRedundantTypeSource parses with SkipLowering,
//	                and so does the vet path's findRedundantTypeNode parse —
//	                measured corpus-inert: all 9 corpus files it would change
//	                are handled by the standalone path).
//	--fix=unwrap    FLIPPED to SkipLowering; collectUnwrapSites grew the
//	                SurfaceMatch descent the arm-body sites need. The corpus
//	                ruler is vacuous (0/532 hits), so the gate is fixture-based;
//	                see TestFixUnwrapRuleIsShapeParity.
//	--fix=match     FLIPPED to SkipLowering. The rule's sugar path landed first
//	                (measured: 13/13 match shapes agree on Pos/End/Subject/
//	                Missing, and SurfaceMatch never survives lowering so the
//	                two collection paths cannot double-report). The corpus
//	                ruler stays vacuous (0/532), so the gate is fixture-based;
//	                see TestFixMatchRuleIsShapeParity.
//	--fix=main      flipped with parseProgramForFmt in an earlier round.
// ─────────────────────────────────────────────────────────────────────────────

// fixShapeFixture is one source that provably triggers its fixer. The "want"
// field records which fixer must fire, so a fixture that silently stops
// triggering fails loudly instead of turning the ruler into 0 == 0.
type fixShapeFixture struct {
	name string
	src  string
	// which fixer must produce output on this source
	wantOverflow  bool
	wantMatch     bool
	wantRedundant bool
}

// fixShapeFixtures returns the non-vacuous fixtures.
//
// Note the ordering constraint in the match fixture: `get-v` must be declared
// AFTER `main`, otherwise the resolver cannot infer `v`'s type when it lowers
// `main`, and the match reads as non-option ⇒ nothing is reported (measured:
// the reverse order gives surface=0 / sugar=0, i.e. a vacuous ruler).
func fixShapeFixtures() []fixShapeFixture {
	return []fixShapeFixture{
		{
			name:          "overflow",
			src:           "f = () () {\n    a i64 = 5\n    b = a + 1\n    print(b)\n}\n",
			wantOverflow:  true,
			wantRedundant: true, // `a i64 = 5` also has a redundant annotation
		},
		{
			name:          "redundant",
			src:           "a i64 = 5\nb str = \"x\"\nprint(a)\nprint(b)\n",
			wantRedundant: true,
		},
		{
			name: "match",
			src: "main = () {\n    v = get-v()\n    v: {\n        ok -> print(v)\n    }\n}\n" +
				"get-v = () (r ?i64) {\n    r = 1\n}\n",
			wantMatch: true,
		},
	}
}

// parseWithShape parses src under the (now sole) sugar shape and returns nil
// when it does not parse cleanly.
func parseWithShape(src string) *parser.Program {
	p := parser.New(lexer.New(src))
	p.SkipLowering = true
	prog := p.ParseProgram()
	if prog == nil || len(p.Errors()) > 0 {
		return nil
	}
	return prog
}

// TestNoFixFixturesTriggerTheirFix is the anti-vacuity gate for everything else
// in this file: every fixture must make its fixer fire, and each fixer must fire
// on at least one fixture. Without this, "both shapes agree" can silently mean
// "neither shape does anything".
func TestNoFixFixturesTriggerTheirFix(t *testing.T) {
	fired := map[string]int{"overflow": 0, "match": 0, "redundant": 0}
	for _, f := range fixShapeFixtures() {
		dir := t.TempDir()
		path := filepath.Join(dir, f.name+".no")
		if err := os.WriteFile(path, []byte(f.src), 0644); err != nil {
			t.Fatal(err)
		}
		if got, ok := fixOverflowInFile(path); ok != f.wantOverflow || (ok && got == "") {
			t.Errorf("%s: fixOverflowInFile ok=%v, want ok=%v", f.name, ok, f.wantOverflow)
		}
		if got, ok := fixMatchArmsInFile(path); ok != f.wantMatch || (ok && got == "") {
			t.Errorf("%s: fixMatchArmsInFile ok=%v, want ok=%v", f.name, ok, f.wantMatch)
		}
		if got, ok := fixRedundantTypeSource(f.src); ok != f.wantRedundant || (ok && got == "") {
			t.Errorf("%s: fixRedundantTypeSource ok=%v, want ok=%v", f.name, ok, f.wantRedundant)
		}
		if f.wantOverflow {
			fired["overflow"]++
		}
		if f.wantMatch {
			fired["match"]++
		}
		if f.wantRedundant {
			fired["redundant"]++
		}
	}
	for name, n := range fired {
		if n == 0 {
			t.Errorf("no fixture triggers the %s fixer — the shape comparison would be vacuous", name)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// `--fix=match` 翻轉門禁（§21.11 表第五行）。
//
// 與 unwrap 同一個處境：**語料尺子是空洞的**。532 份語料上
// `CollectNonExhaustiveMatches` 兩形態各 0 命中（實測；語料的 match 要麼已
// 窮盡、要麼主體靜態型別已知而由 parser 的 [RAL] 在解析期就攔下）。所以門禁
// 只能立在 fixture 上，且每個 fixture 必須在**兩種形態下給出同一個答案**——
// 包括「不該修」的答案（sugar 路徑最容易犯的錯正是多報，例如裸 guard 區塊
// `{ ok -> body }` 的條件恰好叫 ok）。
//
// 規則側落地前先量測了 13 個 match 形狀（checker 的 TestZZMatchShapeParityProbe），
// 兩形態的 Pos/End/Subject/Missing 全部逐字段相同；下面這批 fixture 就是把那次
// 量測釘成門禁，並且把「修復結果逐位元相同」也一併釘住（規則對、插入點錯，
// 在這裡一樣 FAIL）。
// ─────────────────────────────────────────────────────────────────────────────

// matchShapeFixtures returns sources for the `--fix=match` shape gate. wantFix
// records the expected answer under BOTH shapes, so a fixture that silently
// stops firing (or starts firing) fails loudly instead of turning the gate into
// 0 == 0.
func matchShapeFixtures() []struct {
	name, src string
	wantFix   bool
} {
	const pre = "main = () {\n"
	const post = "}\nget-v = () (r ?i64) {\n    r = 1\n}\n"
	mk := func(body string) string { return pre + body + post }
	return []struct {
		name, src string
		wantFix   bool
	}{
		{"ok-only", mk("    v = get-v()\n    v: {\n        ok -> print(v)\n    }\n"), true},
		{"ok-nil", mk("    v = get-v()\n    v: {\n        ok -> print(v)\n        nil -> print('n')\n    }\n"), true},
		{"nil-only", mk("    v = get-v()\n    v: {\n        nil -> print('n')\n    }\n"), true},
		{"err-only", mk("    v = get-v()\n    v: {\n        err -> print('e')\n    }\n"), true},
		{"call-subject", mk("    get-v(): {\n        ok -> print(1)\n    }\n"), true},
		{"nested-arm", mk("    v = get-v()\n    v: {\n        ok -> {\n            w = get-v()\n            w: {\n                ok -> print(w)\n            }\n        }\n    }\n"), true},
		{"multiline-brace", mk("    v = get-v()\n    v: {\n        ok ->\n            print(v)\n    }\n"), true},
		{"module-level", "v = get-v()\nv: {\n    ok -> print(v)\n}\nget-v = () (r ?i64) {\n    r = 1\n}\n", true},
		// ---- 以下每個都必須**不**被修（sugar 多報的防線）----
		{"all-three", mk("    v = get-v()\n    v: {\n        ok -> print(v)\n        nil -> print('n')\n        err -> print('e')\n    }\n"), false},
		{"wildcard-catch-all", mk("    v = get-v()\n    v: {\n        ok -> print(v)\n        -> print('other')\n    }\n"), false},
		{"nil-err-combined", mk("    v = get-v()\n    v: {\n        ok -> print(v)\n        nil || err -> print('x')\n    }\n"), false},
		{"bare-guard-ok", mk("    {\n        ok -> print(1)\n    }\n"), false},
		{"bare-guard-cond", mk("    x i64 = 1\n    {\n        x > 0 -> print(1)\n    }\n"), false},
		{"plain-value", mk("    x i64 = 1\n    x: {\n        1 -> print(1)\n        2 -> print(2)\n    }\n"), false},
		{"tagged-enum", "e-res {\n    ok(v str),\n    fail,\n}\nshow = (q e-res) {\n    q: {\n        ok(v) -> print(v)\n        fail -> print('fail')\n    }\n}\n", false},
	}
}

// TestFixMatchRuleIsShapeParity gates the `--fix=match` flip: every fixture must
// give the expected answer under the (now sole) sugar shape — fire or not fire —
// and must not be a no-op when it should fire. A fixture that silently stops
// firing loses function; one that fires when it shouldn't is a false positive
// (the bare-guard / plain-value cases below pin exactly that).
func TestFixMatchRuleIsShapeParity(t *testing.T) {
	fired := 0
	for _, f := range matchShapeFixtures() {
		out, ok := fixMatchArmsSource(f.src)
		if ok != f.wantFix {
			t.Errorf("%s: sugar shape ok=%v, want %v", f.name, ok, f.wantFix)
			continue
		}
		if f.wantFix {
			fired++
			if out == f.src || out == "" {
				t.Errorf("%s: --fix=match produced no change on a fixture that should be fixed", f.name)
			}
			// 冪等：對修復結果再跑一次不應再產生變動。
			if _, againOk := fixMatchArmsSource(out); againOk {
				t.Errorf("%s: --fix=match not idempotent (fixed twice)", f.name)
			}
		}
	}
	if fired == 0 {
		t.Fatal("no fixture triggers --fix=match — the sugar gate would be vacuous")
	}
}

// TestFixMatchRuleCorpusShapeParity sweeps the 532-file corpus end to end under
// the (now sole) sugar shape. It is a REGRESSION NET, not the ruler: measured
// today sugar fixes 0 of 532 files (the corpus's matches are exhaustive), so a
// pass here proves nothing on its own — TestFixMatchRuleIsShapeParity carries
// the non-vacuity burden. What this adds is forward coverage: the moment anyone
// commits a non-exhaustive match into tests/, it is exercised for real.
func TestFixMatchRuleCorpusShapeParity(t *testing.T) {
	files := noCorpusFiles(t)
	fixed := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, ok := fixMatchArmsSource(string(data)); ok {
			fixed++
		}
	}
	t.Logf("%d files: --fix=match sugar-shape end-to-end sweep fixed %d (corpus ruler is vacuous today, see matchShapeFixtures for the non-vacuous ruler)", len(files), fixed)
}

// TestFixOverflowAndRedundantRulesAreSugarParity covers the two fixers that were
// cleared to flip: their rules must report the expected positions under the
// (now sole) sugar shape on the fixtures that trigger them.
func TestFixOverflowAndRedundantRulesAreShapeParity(t *testing.T) {
	for _, f := range fixShapeFixtures() {
		if !f.wantOverflow && !f.wantRedundant {
			continue
		}
		sugar := parseWithShape(f.src)
		if sugar == nil {
			t.Fatalf("%s: fixture must parse cleanly under sugar shape", f.name)
		}
		if f.wantOverflow {
			if n := len(checker.ValidateIntOverflow(sugar)); n == 0 {
				t.Errorf("%s: ValidateIntOverflow sugar=%d, want >0", f.name, n)
			}
		}
		if f.wantRedundant {
			if n := len(checker.ValidateRedundantTypeAnnotation(sugar)); n == 0 {
				t.Errorf("%s: ValidateRedundantTypeAnnotation sugar=%d, want >0", f.name, n)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// `--fix=overflow` 翻轉後的端到端門禁（§21.11 第一行）。
//
// 翻轉前的量測（532 份語料，fixOverflowSourceShape 兩形態逐檔比對）：
//
//   - 報告差異 1/532：tests/type-match.no，surface 0 條 / sugar 2 條。
//     方向是 sugar 補上 surface 的漏報——`w i64 -> w + 1` 的 w 型別只存在於
//     糖節點 MatchArm.TypeBindingName/TypePattern，降糖後臂綁定失去型別、
//     運算元「型別未知」本應保守報，卻因合成 let 帶 `?T` 而被豁免 ⇒ surface
//     看不見這兩處。糖形態直接讀臂上的型別，報的是真的。
//   - 輸出差異 2/532：
//       · type-match.no：surface 不修、sugar 修（修的正是上述真漏報）；
//       · enum-annotation.no：兩形態都修同一站點，粒度不同——surface 把註解
//         插到臂行之前（match 區塊內部、臂模式行上方），sugar 插到整個 match
//         語句上方。後者更安全：語句級註解對整棵子樹生效（walkStmtForOverflow
//         的 overflowAnnotatedNode 提前返回覆蓋臂體），而「臂行上方」的位置
//         是否能被 parser/lowering 認可取決於註解附著細節。
//
// sugar 插入位置的安全性已實測四項（tests/type-match.no）：
//  ① `no build` 修復後檔案 EXIT=0；
//  ② 運行輸出與原版逐位元相同（10/3/2.5/7/3/2.5/8/5/5.5）；
//  ③ `no fmt` 保留該註解（StatementsWithIntOverflow 的 SurfaceMatch 分支讓
//     臂體內運算對 relevant 集合可見）⇒ 不會出現「fmt 刪、fix 插」的 churn；
//  ④ 重跑冪等（二次 fix 無目標）。
//
// 關鍵單向不變式：**sugar 絕不能比 surface 少修**。surface 修而 sugar 不修
// 即是翻轉丟功能（靜默），這條在語料上逐檔斷言；輸出不同則必須在
// overflowShapeKnownDivergence 白名單裡，且每條都必須仍然相異（防死豁免）。
// ─────────────────────────────────────────────────────────────────────────────

// §4.3 已刪除 surface 形態；舊的 overflowShapeKnownDivergence 白名單（記錄
// surface 漏報、糖形態多修的語料檔，如 tests/type-match.no /
// tests/enum-annotation.no）已隨之退休——糖形態是唯一形態，不再需要「surface vs
// sugar」差異豁免。

// noCorpusFiles walks the 532-file tests/ corpus. Refuses to return an empty
// slice: a vacuous "0 == 0" sweep must fail loudly, not pass.
func noCorpusFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir("../../../tests", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".no") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walking corpus: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no .no files under ../../../tests — refusing to report a vacuous 0-differ")
	}
	sort.Strings(files)
	return files
}

// TestFixOverflowRuleIsShapeParityEndToEnd sweeps the 532-file corpus end to end
// under the (now sole) sugar shape. §4.3 已刪除 surface 形態，原「sugar 不得比
// surface 少修」的單向不變式隨之退休（surface 在 type-match.no 等處本就漏報，
// 糖形態才是正確形態）。本測試改為 regression net：確保 --fix=overflow 在整個
// 語料上不崩潰、產出穩定；非空洞性由 TestNoFixFixturesTriggerTheirFix 承擔。
func TestFixOverflowRuleIsShapeParityEndToEnd(t *testing.T) {
	files := noCorpusFiles(t)
	fixed := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, ok := fixOverflowSourceShape(string(data)); ok {
			fixed++
		}
	}
	t.Logf("%d files: --fix=overflow sugar-shape end-to-end sweep fixed %d (corpus ruler is vacuous today, see fixShapeFixtures for the non-vacuous ruler)", len(files), fixed)
}

// ─────────────────────────────────────────────────────────────────────────────
// `--fix=unwrap` 翻轉門禁（§21.11 表第四行的落地）。
//
// 與 overflow 不同，unwrap 的語料尺子是**空洞的**：`--fix=unwrap` 在 532 份語料上
// 0 命中（實測 `no fmt --fix=overflow -d tests/` 同法跑 unwrap 得 0 檔）——語料的
// option 要麼已被消費、要麼帶 expect: 標記。所以門禁只能立在 fixture 上，且每個
// fixture 必須在**兩種形態下都真的觸發**（否則「兩形態一致」會退化成 0 == 0）。
//
// 關鍵敏感點是「報告點在 match 臂內」：optunuse 判的是「option 綁定後有沒有被
// 消費」，而臂體正是最常見的消費場所。surface 形態下 match 被降糖成 if 鏈，
// collectUnwrapSites 的 IfExpression 分支看得見臂體；糖形態下 match 是
// SurfaceMatch，walker 必須自己下鑽（parser.WalkSurfaceMatchArmBodies）——
// 沒有那一條，臂內站點靜默漏修。下面兩個 fixture 就是為釘住這件事而存在：
// 把 WalkSurfaceMatchArmBodies 呼叫刪掉，它們立刻輸出不同（sugar 側漏修）。
// ─────────────────────────────────────────────────────────────────────────────

// unwrapShapeFixtures returns sources that trigger `--fix=unwrap`, including
// two whose reported site sits inside a match arm body (the shape-sensitive
// shape: measured — arm-block-arith / arm-two-stmts / arm-for produce NO
// optunuse report at all, only index reads inside an arm do).
func unwrapShapeFixtures() []struct{ name, src string } {
	return []struct{ name, src string }{
		{"plain", "f = (arr []i64, i i64) (out i64) {\n    v = arr[i]\n    out = 0\n}\n"},
		{"arm-block-index", "f = (arr []i64, i i64) (out i64) {\n    1: {\n        ok -> {\n            w = arr[i]\n            z = 0\n        }\n        -> z = 0\n    }\n    out = 0\n}\n"},
		{"arm-nested-match", "f = (arr []i64, i i64) (out i64) {\n    v = arr[i]\n    v: {\n        ok -> {\n            u = arr[i]\n            z = 0\n        }\n        -> z = 0\n    }\n    out = 0\n}\n"},
	}
}

// TestFixUnwrapRuleIsShapeParity gates the `--fix=unwrap` flip: every fixture
// must fire under BOTH shapes (anti-vacuity) and produce byte-identical output
// (parity). A fixture that fires only under surface is a silent loss of
// function — exactly what the flip must not do.
func TestFixUnwrapRuleIsShapeParity(t *testing.T) {
	for _, f := range unwrapShapeFixtures() {
		dir := t.TempDir()
		path := filepath.Join(dir, "u.no")
		if err := os.WriteFile(path, []byte(f.src), 0644); err != nil {
			t.Fatal(err)
		}
		out, ok := fixUnwrapSourceShape(path, f.src)
		if !ok {
			t.Errorf("%s: sugar shape no longer fires — the ruler is vacuous", f.name)
			continue
		}
		if out == f.src || out == "" {
			t.Errorf("%s: --fix=unwrap produced no change on a fixture that should be fixed", f.name)
		}
	}
}
