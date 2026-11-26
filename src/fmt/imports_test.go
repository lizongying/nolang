package fmt

import (
	"testing"
)

// 導入區段規範化（imports.go）：`no fmt` 對文件頂部連續導入做三件事——
//  1. 完全重複的導入自動去重；
//  2. 無別名的純 std 模組導入（`# std/noml`、`# std/math` 等）自動移除——
//     標準庫由編譯器隱式載入，這類導入不綁定任何名稱；函數導入或寫了別名
//     （`# std/noml n`、`# std/fmt.sprintf fm`）視為刻意綁定名稱，保留；
//  3. 導入按 Path→Function→Alias 字典序自動排序。
// 任一導入帶註釋或 `#{...}` 附着註解時保守不動（排序會拆散註釋配對、
// `#{bind=ffi}` 等策略承載語義）。別名拼寫的正規化（`as` → 裸別名，
// tgutu5g0）在 stmt.go 的輸出層，不受此保守閘影響。這裡釘住每條規則 + 冪等性。

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
		"# aaa/md5\n# aaa/md5\n# bbb/sha1\n\nmain = () {\n    md5.go()\n    sha1.go()\n}",
		"# aaa/md5\n# bbb/sha1\n\nmain = () {\n    md5.go()\n    sha1.go()\n}")
	fmtCheck(t, "distinct function aliases of the same path kept",
		"# std/math.sqrt s1\n# std/math.abs a1\n# std/math.sqrt s2",
		"# std/math.abs a1\n# std/math.sqrt s1\n# std/math.sqrt s2")
	fmtCheck(t, "fn import and module import are distinct",
		"# std/fmt.sprintf\n# npm/lodash",
		"# npm/lodash\n# std/fmt.sprintf")
}

func TestFormatRemovesUnaliasedStdImports(t *testing.T) {
	fmtCheck(t, "bare # std/fmt is removed",
		"# std/fmt\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
	fmtCheck(t, "bare # std/noml is removed",
		"# std/noml\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
	fmtCheck(t, "bare nested std paths are removed",
		"# std/database/sql\n# std/crypto/sha3\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
	fmtCheck(t, "duplicates and unaliased std imports removed together",
		"# std/number\n# std/number\n# std/os\n# aaa/lib\n\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
	// 別名視為刻意綁定名稱，保留（含 `as` 寫法，輸出正規化為裸別名）。
	fmtCheck(t, "aliased std import kept",
		"# std/noml n\n# aaa/lib",
		"# std/noml n")
	// 函數導入承載裸名綁定（# std/fmt.sprintf → 裸 sprintf），不移除。
	fmtCheck(t, "function import from fmt kept",
		"# std/fmt.sprintf sp\n# aaa/lib",
		"# std/fmt.sprintf sp")
	// 非 std 導入：正文有用到的話照常排序/去重，不因來自外部包就另設規則。
	fmtCheck(t, "used non-std imports kept and sorted",
		"# zzz/fmt\n# aaa/lib\n\nmain = () {\n    fmt.go()\n    lib.go()\n}",
		"# aaa/lib\n# zzz/fmt\n\nmain = () {\n    fmt.go()\n    lib.go()\n}")
}

func TestFormatSortsImports(t *testing.T) {
	// 純 std 導入會先被移除，排序測試改用非 std 路徑 + std 函數導入墊住四個層級。
	fmtCheck(t, "imports sorted by path then function then alias",
		"# zzz/vec\n# npm/prefs\n# aaa/lib\n# std/math.sqrt\n# std/math.abs\n\nmain = () {\n    vec.go()\n    lib.go()\n}",
		"# aaa/lib\n# npm/prefs\n# std/math.abs\n# std/math.sqrt\n# zzz/vec\n\nmain = () {\n    vec.go()\n    lib.go()\n}")
	fmtCheck(t, "same path+fn sorted by alias",
		"# std/math.sqrt s2\n# std/math.sqrt s1",
		"# std/math.sqrt s1\n# std/math.sqrt s2")
	fmtCheck(t, "existing sorted order unchanged",
		"# aaa/md5\n# aaa/sha1\n# zzz/io\n\nmain = () {\n    md5.go()\n    sha1.go()\n    io.go()\n}",
		"# aaa/md5\n# aaa/sha1\n# zzz/io\n\nmain = () {\n    md5.go()\n    sha1.go()\n    io.go()\n}")
}

func TestFormatImportsConservativeGate(t *testing.T) {
	// 附着 `#{bind=ffi}` 的導入：不排序、不移除（native 綁定策略承載語義）。
	fmtCheck(t, "annotation-attached run keeps source order",
		"#{bind=ffi}\n# /src/sqlite\n# aaa/lib",
		"#{bind=ffi}\n# /src/sqlite\n# aaa/lib")
	// Doc 註釋在導入上方：保守不動排序。首條導入的文件頭註釋段與導入之間
	// 固定空一行，區段內其它分組註釋則緊貼它所註釋的導入。
	fmtCheck(t, "doc-commented run keeps source order",
		"; second\n# zzz/b\n; first\n# aaa/a",
		"; second\n\n# zzz/b\n\n; first\n# aaa/a")
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
		"# aaa/vec\n# zzz/fmt\n\nmain = () {\n    vec.go()\n    fmt.go()\n}",
		"# aaa/vec\n# zzz/fmt\n\nmain = () {\n    vec.go()\n    fmt.go()\n}")
	// 只有一條無別名 std 導入、被移除後文件以代碼開頭。
	fmtCheck(t, "lone std import removed, code starts clean",
		"# std/fmt\n\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
	fmtCheck(t, "lone # std/noml removed, code starts clean",
		"# std/noml\n\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
}

func TestFormatImportsBareAliasKeptOnSameLine(t *testing.T) {
	// 裸別名（無 `as`）：`# std/err error` 的 `error` 是別名，必須與路徑同行。
	// 解析器曾把裸別名當成緊跟的獨立陳述，formatter 隨之把別名印到新行（regress）。
	fmtCheck(t, "bare module alias stays on the import line",
		"# std/err error\n\nmain = () {\n    print('hi')\n}",
		"# std/err error\n\nmain = () {\n    print('hi')\n}")
	// `as` 寫法正規化為裸別名（checker tgutu5g0 建議的形式）。
	fmtCheck(t, "as-style module alias normalized",
		"# std/err as error\n\nmain = () {\n    print('hi')\n}",
		"# std/err error\n\nmain = () {\n    print('hi')\n}")
	// 函數導入的裸別名（`.fn` 形式）本就同行，確認未被破壞。
	fmtCheck(t, "fn import bare alias stays on the line",
		"# std/err.new e\n\nmain = () {\n    print('hi')\n}",
		"# std/err.new e\n\nmain = () {\n    print('hi')\n}")
	// 多條裸別名導入：各自同行、整體仍按 path 排序（無別名 std 導入會被移除）。
	fmtCheck(t, "multiple bare aliases each on their line",
		"# std/str s\n# std/err error\n# aaa/lib\n\nmain = () {\n    lib.go()\n}",
		"# aaa/lib\n# std/err error\n# std/str s\n\nmain = () {\n    lib.go()\n}")
}

func TestFormatUseAliasKeywordNormalized(t *testing.T) {
	// `no fmt` 直接輸出 tgutu5g0 建議的裸別名，讓 `no fmt -w` 一次消除 HINT。
	// 三種邊界：函數導入、`_` 佔位別名、.noml 模組別名；帶連字號的別名也
	// 必須仍是合法 path-ident（否則正規化會產生無法重新解析的源碼）。
	fmtCheck(t, "as-style aliases normalized (and sorted)",
		"# npm/lodash.get as deep-get\n# npm/lodash.default as _\n# /cfg/server as cfg",
		"# /cfg/server cfg\n# npm/lodash.default _\n# npm/lodash.get deep-get")
	// 已是裸別名的輸入不能再被改動。
	fmtCheck(t, "bare aliases untouched",
		"# std/err error\n# std/err.new e\n",
		"# std/err error\n# std/err.new e")
}

// TestFormatPreservesUnderscoreImportAlias pins the `# path _` spelling: `_`
// is the "deliberately unused / side-effect only" marker (same convention as
// `_`-prefixed unused variables). `no fmt` must never strip or rewrite it —
// these imports are load-bearing for the entry-file-direct-import workaround
// (bug #85), and the checker emits the `unimpuse` hint when it is missing.
func TestFormatPreservesUnderscoreImportAlias(t *testing.T) {
	fmtCheck(t, "underscore alias on workspace path is kept",
		"# /glossary/src/nav _\n\nprint('hi')",
		"# /glossary/src/nav _\n\nprint('hi')")
	fmtCheck(t, "underscore alias on package path is kept",
		"# nogit _\n\nprint('hi')",
		"# nogit _\n\nprint('hi')")
	fmtCheck(t, "as-underscore normalizes to bare underscore",
		"# /glossary/src/nav as _\n\nprint('hi')",
		"# /glossary/src/nav _\n\nprint('hi')")
	fmtCheck(t, "underscore aliases participate in sorting",
		"# /glossary/src/drv\n# /glossary/src/nav _\n# /glossary/src/db _\n\nprint('hi')",
		"# /glossary/src/db _\n# /glossary/src/drv\n# /glossary/src/nav _\n\nprint('hi')")
}

func TestFormatHoistsMidFileImports(t *testing.T) {
	// 導入只允許出現在文件頂部：代碼之後的導入被搬到頂部區段（保持源碼
	// 相對順序），搬完照常參與排序/去重，原位只留下代碼。
	fmtCheck(t, "mid-file import run is hoisted to the top",
		"main = () {\n    a.go()\n    b.go()\n}\n# zzz/b\n# aaa/a\n",
		"# aaa/a\n# zzz/b\n\nmain = () {\n    a.go()\n    b.go()\n}")
	// 塊內（函式體、for 體、臂體）的導入同樣搬出；正文需真的用到，
	// 否則會被「未使用則清理」規則拿掉。
	fmtCheck(t, "import inside a function body is hoisted",
		"main = () {\n    # aaa/lib\n    lib.go()\n}",
		"# aaa/lib\n\nmain = () {\n    lib.go()\n}")
	fmtCheck(t, "import inside a for body is hoisted",
		"f = () {\n    3 * {\n        # aaa/lib\n        lib.go()\n    }\n}",
		"# aaa/lib\n\nf = () {\n    3 * {\n        lib.go()\n    }\n}")
	fmtCheck(t, "import inside a match arm is hoisted",
		"f = (x i64) {\n    x: {\n        0 -> {\n            # aaa/lib\n            lib.go()\n        }\n\n        -> print('other')\n    }\n}",
		"# aaa/lib\n\nf = (x i64) {\n    x: {\n        0 -> {\n            lib.go()\n        }\n\n        -> print('other')\n    }\n}")
	// 搬來的導入與原有頂部區段合併、一起去重排序。
	fmtCheck(t, "hoisted imports merge into the existing top run",
		"# zzz/vec\n\nmain = () {\n    vec.go()\n    io.go()\n}\n# aaa/io\n",
		"# aaa/io\n# zzz/vec\n\nmain = () {\n    vec.go()\n    io.go()\n}")
	// 註釋隨導入一起搬；搬到頂部後它成為文件頭說明段，與導入之間空一行。
	fmtCheck(t, "hoisted import carries its doc comment",
		"x = 1\n\n; lib\n# aaa/lib\n",
		"; lib\n\n# aaa/lib\n\nx = 1")
}

func TestFormatRemovesUnusedPackageImports(t *testing.T) {
	fmtCheck(t, "unused bare package import removed",
		"# nogit\n\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
	fmtCheck(t, "bare package used as nogit.<module> kept",
		"# nogit\n\nmain = () {\n    nogit.remote.greet('bob')\n}",
		"# nogit\n\nmain = () {\n    nogit.remote.greet('bob')\n}")
	fmtCheck(t, "unused package submodule import removed",
		"# nogit/remote\n\nmain = () {\n    print('hi')\n}",
		"main = () {\n    print('hi')\n}")
	fmtCheck(t, "package submodule used via its short name kept",
		"# nogit/remote\n\nmain = () {\n    remote.greet('bob')\n}",
		"# nogit/remote\n\nmain = () {\n    remote.greet('bob')\n}")
	fmtCheck(t, "nogit.remote property keeps the submodule import",
		"# nogit\n# nogit/remote\n\nmain = () {\n    nogit.remote.greet('bob')\n}",
		"# nogit\n# nogit/remote\n\nmain = () {\n    nogit.remote.greet('bob')\n}")
}
