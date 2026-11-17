package checker

import (
	"sort"
	"testing"
)

// 這一組測試守的是「預烘焙的 std 索引 ≡ 現場全量掃描」這條不變式。
//
// 動機：`no` 每次冷啟動都要判斷「任何 `math.max(...)` 這種模組限定呼叫是否合法」，
// 原本的做法是把**全部 119 個 std 模組**各解析一遍再收集導出 / union 別名（實測
// 冷建置 ~3s，是 playground 單筆編譯的主要成本）。現在改成：genstdsig 把
// 「哪幾個模組有 union 別名」和「每個模組的導出符號」烤進 stdsig_gen.go。
//
// 烘焙表一旦與 src/std 漂移就會靜默改變語義（少一條 union 別名 ⇒ 跨模組 union
// 形參被誤報；少一條導出 ⇒ 合法符號被判未定義），所以：
//   - 導出表的等價性由 hir.TestModuleExportsMatchChecker 逐模組逐欄位比對（它拿
//     HIR 現場重算的結果當 oracle）；
//   - union 別名索引沒有天然的 oracle，由本檔的測試比對「窄掃描 vs 全量掃描」。

// TestUnionAliasNarrowingMatchesFullScan pins 窄掃描（只解析烘焙表列出的模組）
// 與全量掃描（解析 knownStdModules 全部模組）產生完全相同的 union 別名表。
// 兩邊不一致時 CollectStdUnionAliases 會自癒退回全量，但那意味著烘焙表過期：
// 應該重新 `make gen`，不是靠自癒兜著。
func TestUnionAliasNarrowingMatchesFullScan(t *testing.T) {
	if len(embeddedStdUnionAliasMods) == 0 {
		t.Fatal("embeddedStdUnionAliasMods 是空的：genstdsig 沒有烘焙 union 別名索引，CollectStdUnionAliases 會退回全量掃描（慢但不錯）")
	}

	narrow, narrowMod := scanStdUnionAliases(unionAliasScanTargets())
	full, fullMod := scanStdUnionAliases(knownStdModules())

	if len(narrow) == 0 {
		t.Fatal("窄掃描一條 union 別名都沒找到")
	}
	if got, want := sortedKeys(narrowMod), sortedKeys(fullMod); !equalStrings(got, want) {
		t.Fatalf("別名集合不一致：窄掃描 %v vs 全量 %v（烘焙表過期？重跑 make gen）", got, want)
	}
	for name, short := range fullMod {
		if narrowMod[name] != short {
			t.Errorf("別名 %q 的定義模組不一致：窄掃描 %q vs 全量 %q", name, narrowMod[name], short)
		}
		if narrow[name] == nil || full[name] == nil {
			t.Errorf("別名 %q 的 *parser.TypeAlias 為 nil", name)
			continue
		}
		if got, want := narrow[name].Union.String(), full[name].Union.String(); got != want {
			t.Errorf("別名 %q 的成員不一致：窄掃描 %q vs 全量 %q", name, got, want)
		}
	}
}

// TestBakedModuleExportsCoverEveryStdModule 確認每個 std 模組都有烘焙導出表條目。
// parseModuleExports 對「表裡沒有」的模組會回退到源碼解析（慢但不錯），所以這條
// 只斷言「覆蓋完整」，不是正確性本身——正確性在 hir.TestModuleExportsMatchChecker。
func TestBakedModuleExportsCoverEveryStdModule(t *testing.T) {
	var missing []string
	for _, full := range knownStdModules() {
		if _, ok := embeddedStdModuleExports[full]; !ok {
			missing = append(missing, full)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("%d 個 std 模組沒有烘焙導出表（重跑 make gen）：%v", len(missing), missing)
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
