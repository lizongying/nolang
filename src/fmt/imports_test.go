package fmt

import (
	"strings"
	"testing"
)

// 導入區段規範化（imports.go）：`no fmt` 對文件頂部連續導入做三件事——
//  1. 完全重複的導入自動去重；
//  2. 無別名的 `# std/fmt` 自動移除（print/format 等為編譯器內建）；函數導入
//     或寫了別名（`# std/fmt.sprintf fm`）視為刻意綁定名稱，保留；
//  3. 導入按 Path→Function→Alias 字典序自動排序。
// 任一導入帶註釋或 `#{...}` 附着註解時保守不動（排序會拆散註釋配對、
// `#{bind=ffi}` 等策略承載語義）。這裡釘住每條規則 + 冪等性。

func fmtCheck(t *testing.T, name, input, expected string) {
	t.Helper()
	got, ok, errs := formatProgram(input)
	if !ok {
		t.Fatalf("%s: format failed: %v", name, errs)
	}
	if got != expected {
		t.Errorf("%s:\n got: %q\nwant: %q", name, got, expected)
	}
	// 冪等：第二遍不再改。
	again, ok2, errs2 := formatProgram(got)
	if !ok2 {
		t.Fatalf("%s: reformat failed: %v", name, errs2)
	}
	if again != got {
		t.Errorf("%s: not idempotent:\n 1st: %q\n 2nd: %q", name, got, again)
	}
}

func TestFormatDedupsIdenticalImports(t *testing.T) {
	fmtCheck(t, "identical imports collapse to one",
		"# std/hash/md5\n# std/hash/md5\n# std/hash/sha1",
		"# std/hash/md5\n# std/hash/sha1")
	fmtCheck(t, "distinct function aliases of the same path kept",
		"# std/math.sqrt s1\n# std/math.abs a1\n# std/math.sqrt s2",
		"# std/math.abs a1\n# std/math.sqrt s1\n# std/math.sqrt s2")
	fmtCheck(t, "fn import and module import are distinct",
		"# std/fmt.sprintf\n# std/io",
		"# std/fmt.sprintf\n# std/io")
}

func TestFormatRemovesStdFmtImport(t *testing.T) {
	fmtCheck(t, "bare # std/fmt is removed",
		"# std/fmt\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
	fmtCheck(t, "duplicate # std/fmt removed with them",
		"# std/fmt\n# std/fmt\n# std/io",
		"# std/io")
	// 函數導入承載裸名綁定（# std/fmt.sprintf → 裸 sprintf），不移除。
	fmtCheck(t, "function import from fmt kept",
		"# std/fmt.sprintf sp\n# std/io",
		"# std/fmt.sprintf sp\n# std/io")
	// std/fmt 是唯一被自動移除的模組；其它 std 導入只排序/去重。
	fmtCheck(t, "other std imports untouched by removal",
		"# std/str\n# std/math",
		"# std/math\n# std/str")
}

func TestFormatSortsImports(t *testing.T) {
	fmtCheck(t, "imports sorted by path then function then alias",
		"# std/vec\n# std/user/prefs\n# std/io\n# std/math.sqrt\n# std/math.abs",
		"# std/io\n# std/math.abs\n# std/math.sqrt\n# std/user/prefs\n# std/vec")
	fmtCheck(t, "same path+fn sorted by alias",
		"# std/math.sqrt s2\n# std/math.sqrt s1",
		"# std/math.sqrt s1\n# std/math.sqrt s2")
	fmtCheck(t, "existing sorted order unchanged",
		"# std/hash/md5\n# std/hash/sha1\n# std/io",
		"# std/hash/md5\n# std/hash/sha1\n# std/io")
}

func TestFormatImportsConservativeGate(t *testing.T) {
	// 附着 `#{bind=ffi}` 的導入：不排序、不移除（native 綁定策略承載語義）。
	fmtCheck(t, "annotation-attached run keeps source order",
		"#{bind=ffi}\n# /src/sqlite\n# aaa/lib",
		"#{bind=ffi}\n# /src/sqlite\n# aaa/lib")
	// Doc 註釋在導入上方：同樣保守不動。
	fmtCheck(t, "doc-commented run keeps source order",
		"; second\n# zzz/b\n; first\n# aaa/a",
		"; second\n# zzz/b\n; first\n# aaa/a")
	// 同行註釋：不動（格式化为既有的 `;` 緊跟形式）。
	fmtCheck(t, "inline-commented run keeps source order",
		"# zzz/b ; note\n# aaa/a",
		"# zzz/b; note\n# aaa/a")
	// std/fmt 帶註釋 → 不移除（不可靜默丟棄用戶註釋）。
	fmtCheck(t, "commented std/fmt is kept",
		"# std/fmt ; 顯式導入保留\n# std/io",
		"# std/fmt; 顯式導入保留\n# std/io")
}

func TestFormatImportsBlankLineAroundSurvives(t *testing.T) {
	// 導入與其他語句之間固定一個空行的既有規則不受區段改寫影響。
	fmtCheck(t, "blank after import block",
		"# std/vec\n# std/fmt\n\nmain = () {\n    print('hi')\n}",
		"# std/vec\n\nmain = () {\n    print('hi')\n}")
	// 只有 # std/fmt 一條導入、被移除後文件以代碼開頭。
	fmtCheck(t, "lone std/fmt removed, code starts clean",
		"# std/fmt\n\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
}

func TestFormatImportsBareAliasKeptOnSameLine(t *testing.T) {
	// 裸別名（無 `as`）：`# std/err error` 的 `error` 是別名，必須與路徑同行。
	// 解析器曾把裸別名當成緊跟的獨立陳述，formatter 隨之把別名印到新行（regress）。
	fmtCheck(t, "bare module alias stays on the import line",
		"# std/err error\n\nmain = () {\n    print('hi')\n}",
		"# std/err error\n\nmain = () {\n    print('hi')\n}")
	// `as` 寫法仍保留（往返不變）。
	fmtCheck(t, "as-style module alias preserved",
		"# std/err as error\n\nmain = () {\n    print('hi')\n}",
		"# std/err as error\n\nmain = () {\n    print('hi')\n}")
	// 函數導入的裸別名（`.fn` 形式）本就同行，確認未被破壞。
	fmtCheck(t, "fn import bare alias stays on the line",
		"# std/err.new e\n\nmain = () {\n    print('hi')\n}",
		"# std/err.new e\n\nmain = () {\n    print('hi')\n}")
	// 多條裸別名導入：各自同行、整體仍按 path 排序。
	fmtCheck(t, "multiple bare aliases each on their line",
		"# std/str s\n# std/err error\n# std/io",
		"# std/err error\n# std/io\n# std/str s")
}

func TestFormatImportsMidFileRunNotTouched(t *testing.T) {
	// 非文件頂部的導入區段（出現在代碼之後）保守不動。
	input := "x = 1\n# zzz/b\n# aaa/a\n"
	got, ok, errs := formatProgram(input)
	if !ok {
		t.Fatalf("format failed: %v", errs)
	}
	if !strings.Contains(got, "# zzz/b\n# aaa/a") {
		t.Errorf("mid-file import run should keep source order, got:\n%s", got)
	}
}
