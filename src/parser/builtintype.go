package parser

// builtintype.go — 編譯器內建型別的登記表。
//
// 這張表是「內建型別名 → 文檔所在 std 模組 → 模組文檔錨點 → 佈局」的唯一真相。
// 消費方：
//   - LSP 的 hover / go-to-definition / 補全（src/lsp：hover.go、definitions.go、
//     completion.go）。此前這三處各自硬編碼一份清單，口徑還不一致 ——
//     `builtinTypeDoc` 有 str/txt 沒有 vec/arr，`stdTypeDefinition` 拿它當准入
//     白名單，於是 vec/arr 既沒有 hover 也 F12 不了；
//   - 保留名判斷 IsBuiltinLayoutStructName（build 的模組前綴、checker 的
//     ValidateBuiltinStructDecls、MIR 的 collectStructFields 都問它）。
//
// 這張表**不承擔佈局**：真實佈局在 MIR 手寫描述符（%str-long / %vec / %txt）或
// 編譯期常量（arr 的 `.len` 是 n）。src/std/{str,vec,txt,arr}.no 開頭那幾份
// `#{buildin} X { … }` 只剩一個職責 —— 給 checker 一份欄位型別（`t.len` 是 u8），
// 它們不參與鏈接，刪掉也不會讓 hover / F12 失效（只要這張表還在）。

// BuiltinType 描述一個編譯器內建型別。
type BuiltinType struct {
	// Name 是型別名，也是它出現在型別位置上的寫法（`t txt = 'x'`）。
	Name string
	// Module 是文檔所在 std 模組的**短名**（`txt` ⇒ src/std/txt.no）。
	// 必須能餵給 checker.ResolveStdModulePath。
	Module string
	// Anchor 是該模組的模組級文檔錨點 label（`; [txt](#txt)` 裡的 `txt`）。
	Anchor string
	// Summary 是一句話說明，供補全的 detail 使用。
	Summary string
	// Layout 是人類可讀的佈局，供 hover 與 docs 使用（不是語法，不解析）。
	Layout string
	// Size 是佈局大小（位元組）；0 表示沒有固定大小。
	Size int
	// Runtime 表示是否有 MIR 手寫的運行時描述符。arr 沒有：`[n]t` 在 LLVM 層
	// 就是裸聚合 `[N x T]`，`.len` 由編譯器替換成常量。
	Runtime bool
}

// builtinTypes 的順序即 LSP 補全的呈現順序。
//
// 加一條之前先確認兩件事：① src/std/<module>.no 裡真的有一行
// `; [<anchor>](#<anchor>)`（TestBuiltinTypesMatchStdDocs 會檢查）；
// ② MIR 側的佈局與這裡的 Layout / Size 一致（str/vec 在 codegen 的 prelude，
// txt 見 emitStructTypes，arr 沒有描述符）。
var builtinTypes = []BuiltinType{
	{
		Name:    "str",
		Module:  "str",
		Anchor:  "str",
		Summary: "string (immutable byte sequence)",
		Layout:  "{ i32 len, i32 cap, i8* data }",
		Size:    16,
		Runtime: true,
	},
	{
		Name:    "txt",
		Module:  "txt",
		Anchor:  "txt",
		Summary: "fixed 256-byte string (max 255 bytes of data)",
		Layout:  "{ [255 x i8] data, i8 len }",
		Size:    256,
		Runtime: true,
	},
	{
		Name:    "vec",
		Module:  "vec",
		Anchor:  "vec",
		Summary: "variable-length slice []T",
		Layout:  "{ i32 len, i32 cap, i64 data }",
		Size:    16,
		Runtime: true,
	},
	{
		Name:    "arr",
		Module:  "arr",
		Anchor:  "arr",
		Summary: "fixed-size array [N]T",
		Layout:  "[N x T] (elements inline, no len/cap field)",
		Size:    0,
		Runtime: false,
	},
}

// BuiltinTypes 回傳全部內建型別的登記項（含標量以外的佈局型別）。
// 回傳的切片是內部資料，呼叫方只讀。
func BuiltinTypes() []BuiltinType { return builtinTypes }

// LookupBuiltinType 依型別名取登記項。
func LookupBuiltinType(name string) (BuiltinType, bool) {
	for _, t := range builtinTypes {
		if t.Name == name {
			return t, true
		}
	}
	return BuiltinType{}, false
}
