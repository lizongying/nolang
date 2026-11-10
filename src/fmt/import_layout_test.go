package fmt

import "testing"

// 導入區段的排版契約（`no fmt`）：
//  1. 模組註釋（Program.Doc 那段，以 `; [mod](#mod)` 收尾）與導入區段之間固定
//     空一行；
//  2. 導入區段與其後的第一個節點之間固定空一行；
//  3. 導入之間不留空行（源碼寫了也壓掉）；
//  4. 導入上方帶註釋 / `#{...}` 註解時，註釋塊與上一條導入之間空一行，註釋
//     本身緊貼它註釋的導入；
//  5. 導入只允許在文件頂部（搬移規則見 import_hoist.go 與 imports_test.go）。
func TestFormatImportSectionSpacing(t *testing.T) {
	// 1. 模組註釋 → 空行 → 導入區段。源碼緊貼也補上。
	fmtCheck(t, "module doc and import run separated by one blank line",
		"; mod — 說明\n; [mod](#mod)\n# std/io\n\nmain = () {\n    print('hi')\n}",
		"; mod — 說明\n; [mod](#mod)\n\n# std/io\n\nmain = () {\n    print('hi')\n}")

	// 2. 導入區段 → 空行 → 後續節點；導入之間無空行（源碼空行被壓掉）。
	fmtCheck(t, "blank lines between imports are collapsed",
		"# std/io\n\n# std/vec\n\n\nmain = () {\n    print('hi')\n}",
		"# std/io\n# std/vec\n\nmain = () {\n    print('hi')\n}")

	// 4. 分組註釋：註釋前空一行，註釋緊貼導入。
	fmtCheck(t, "comment group before an import gets one leading blank line",
		"# std/io\n; 第三方\n# npm/lodash\n# std/vec",
		"# std/io\n\n; 第三方\n# npm/lodash\n# std/vec")

	// 4b. 源碼在註釋與導入之間留了空行 → 壓掉（註釋緊貼它註釋的導入）。
	fmtCheck(t, "blank line between import and its own comment is collapsed",
		"# std/io\n\n; 第三方\n\n# npm/lodash",
		"# std/io\n\n; 第三方\n# npm/lodash")

	// 1+2 一起：模組註釋、導入、代碼三段各空一行。
	fmtCheck(t, "module doc, import run and code each split by one blank line",
		"; util — 工具\n; [util](#util)\n# std/io\n# std/vec\nmain = () {\n    print('hi')\n}",
		"; util — 工具\n; [util](#util)\n\n# std/io\n# std/vec\n\nmain = () {\n    print('hi')\n}")

	// 4c. `#{...}` 註解（附着在導入上）同樣讓整組與上一條導入分隔。
	fmtCheck(t, "attached annotation group is separated from the previous import",
		"# aaa/lib\n#{bind=ffi}\n# /src/sqlite\n",
		"# aaa/lib\n\n#{bind=ffi}\n# /src/sqlite")
}
