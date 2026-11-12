package checker

// builtin_struct_decl.go — 「`#{buildin}` 佈局聲明」與「內建型別名」的雙向門禁。
//
// 背景（2026-11-12）：`src/std/{str,vec,txt,arr}.no` 各有一份
//
//	#{buildin}
//	txt { data [255]byte; len u8 }
//
// 形式的 struct 聲明。它的用途只剩一個：**給 checker 一份欄位型別**（讓
// `t.len` 被推成 `u8`、`15w45dqk` 那道「?i64 不能直接寫 u8 欄位」的閘門有效）。
// LSP 的 hover / 跳轉定義 / 補全**不讀它** —— 那些走 parser.BuiltinTypes()
// 登記表（名字 → std 模組 → 模組文檔錨點 → 佈局）。真實佈局也不在這裡 ——
// str / vec / txt 由 MIR 手寫描述符提供（hir2mir.go 的
// `mod.StructFields["str"|"vec"|"txt"]`），arr 的 `.len` 是編譯期常量。
//
// 也就是說這份聲明在**語義上不存在**：它不改名、不註冊接收者、不覆蓋佈局。
// 這需要三處配合，任何一處漏掉都會立刻炸掉整個構建：
//   - build 的模組前綴階段：不能把它當「本模組定義的型別」加前綴，否則聲明被
//     改名成 `txt.txt`（⇒ `%txt_txt`）、方法被改名成 `txt.txt.to-str`
//     （⇒ `unknown callee txt.to-str`）；
//   - MIR 的 collectStructFields：不能用 HIR 的欄位覆蓋手寫描述符；
//   - **本檔**：反過來要求「內建型別名」與「`#{buildin}` 標記」必須成對出現。
//
// 為什麼反向也要管：上面兩處的跳過條件是「`#{buildin}` **且** 內建佈局名」。
// 若使用者能自由地用 `str` 這個名字宣告普通 struct，它就會繞過所有機制 ——
// 編譯器會生成一份 `%str` 的影子佈局，與 `%str-long` 的真實布局打架；反之，
// 若使用者能給任意 struct 貼 `#{buildin}`，那份聲明就會被 MIR 跳過，欄位全部
// 消失，只留下一個 `unknown field` 的謎題。所以兩端都必須報錯。

import (
	"strings"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/parser"
)

const (
	// builtinStructRedeclTraceID — 用內建型別名宣告 struct（缺 `#{buildin}`）。
	builtinStructRedeclTraceID = "1a8m445h"
	// builtinStructAnnotTraceID — 給非內建佈局名貼 `#{buildin}`。
	builtinStructAnnotTraceID = "3lu6n4np"
)

// reservedStructName 報告 name 是否為「編譯器持有的型別名」—— 使用者不得用它
// 宣告 struct。
//
// 兩個集合的併集：
//   - IsBuiltinLayoutStructName（str / vec / txt / arr）：這四個是 src/std 真的
//     用 `#{buildin}` 聲明過的佈局 stub；
//   - IsBuiltinTypeName（str / txt / 各標量 / bool / byte / char）：可以出現在
//     型別位置的內建詞彙。標量名在 parser 層就進不了 struct 名（不是 IDENT），
//     並進來純屬縱深防禦。
//
// 刻意**不含** err / option / any / hashmap-*：它們是本模組定義的真實型別
// （std/err.no 的 `struct err`、global.no 的 `#{buildin} option` 標籤列舉、
// 泛型模板 hashmap-*），佈局由自己的欄位決定，名字也不保留給編譯器。
func reservedStructName(name string) bool {
	return parser.IsBuiltinLayoutStructName(name) || parser.IsBuiltinTypeName(name)
}

// bareTypeName 取點分名的最後一段：模組前綴階段之後，使用者宣告的
// `struct str` 可能已變成 `m.str`，但「是否內建名」的判定只看裸名。
func bareTypeName(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// ValidateBuiltinStructDecls 檢查 struct 聲明與 `#{buildin}` 標記的配對：
//
//	① 裸名是內建名卻沒帶 `#{buildin}` ⇒ ERROR：使用者想宣告一個與編譯器
//	   佈局同名的型別，這在任何路徑上都會產生兩份互相矛盾的佈局。
//	② 帶了 `#{buildin}` 但裸名不是內建佈局名 ⇒ ERROR：這份聲明會被 MIR 當
//	   「佈局由編譯器持有」而跳過，欄位全部不生效，留給使用者的只是一堆
//	   `unknown field`。
//
// 只掃 struct；`#{buildin}` 的另外兩個載體（函數樁、標籤列舉）各有自己的
// 語義與既有檢查，不在本檔範圍。
func ValidateBuiltinStructDecls(program *parser.Program) []ValidateResult {
	if program == nil {
		return nil
	}
	var results []ValidateResult
	for _, stmt := range program.Statements {
		sd, ok := stmt.(*parser.StructDefinition)
		if !ok {
			continue
		}
		bare := bareTypeName(sd.Name)
		pos := sd.Pos()
		file := program.Sem.SourceFileOf(sd)
		switch {
		case sd.Builtin && !parser.IsBuiltinLayoutStructName(bare):
			// The ID is written twice on purpose: msgscan only accepts a literal
			// at the T call site, while the printed TraceID must stay the const
			// so that severity mapping and docs keep referring to the same code.
			results = append(results, ValidateResult{
				Line: pos.Line, Column: pos.Column, File: file,
				Message: i18n.T("3lu6n4np", "'#{buildin}' only applies to the compiler-owned layout types (str / vec / txt / arr); it does nothing on '%s'. Remove it.", bare),
				TraceID: builtinStructAnnotTraceID,
			})
		case !sd.Builtin && reservedStructName(bare):
			results = append(results, ValidateResult{
				Line: pos.Line, Column: pos.Column, File: file,
				Message: i18n.T("1a8m445h", "'%s' is a compiler-owned builtin type and cannot be declared as a struct: its layout is fixed by the compiler. Rename the type.", bare),
				TraceID: builtinStructRedeclTraceID,
			})
		}
	}
	return results
}
