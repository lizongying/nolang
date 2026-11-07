package fmt

// 「先宣告型別、緊接一行賦值」的合併（no fmt 常規規則，非 --fix 子類）。
//
// 形態：兩條相鄰陳述，第一條只寫了「名字 + 型別」，第二條才是賦值——
//
//	v i64
//	v = .
//
// 這在 AST 上本來就是「一次宣告 + 一次賦值」，只是被拆成兩行寫。合併後：
//
//	v i64 = .
//
// 為什麼可以合併：`v i64` 定義的是 v 的**型別**，`v = .` 只是對 v 賦值，
// 兩者合起來正好是「帶初始值的宣告」這一種語義。在此之前不行——編譯器把
// 綁定的型別取自初始化式、而非宣告（`v i64 = b`（b: byte）實際分配 i8 槽、
// 按 8 位做算術，200*200 得 64；兩行寫法才是 64 位、得 40000）。該缺陷已在
// src/mir/hir2mir.go 的 KLet 綁定處修好：宣告型別就地做整數拓寬，於是兩種
// 寫法產生的 MIR 一致，合併成為純排版變化。
//
// ── 安全性判據（全部必要） ──────────────────────────────────
//  1. 第一條必須是**源碼顯式**寫下的型別。parser 會為了正確解析 map 字面量
//     給 `x = { … }` 補一個 inferred 型別（markInferred），那不是用戶宣告。
//  2. 第一條不得帶初始化式、不得是合成陳述，名字為簡單識別字且不是 `_`/`it`。
//  3. 第二條必須是同一名字、不帶（顯式）型別的賦值，且兩者之間沒有空行——
//     空行是「宣告與初始化刻意分離」的排版意圖，保留原樣。
//  4. 型別限於 NamedType（i64 / str / char / 結構名 …）。`?T` 是 NullableType、
//     `[]T` 是 SliceType、`[K]V` 是 MapType，一律不折疊：這些型別的「先零值
//     宣告再賦值」與「帶初始值宣告」在 clone/包裝路徑上並不對稱，超出排版範疇。
//  5. 兩條陳述都不得帶行內註解或 `#{...}` 註解條目：合併後第二條的註解無處
//     安放，第一條的註解條目在複製節點上會失去 sem 關聯（attachedAnnotations
//     按節點查 side-table）。doc 註解只限制第二條——第一條的 doc 隨節點複製、
//     仍印在合併後那行上方。
//  6. 僅作用於區塊內陳述（formatBlockInner），不碰頂層：`x i64` + `x = 5` 在
//     頂層是「全域 + 寫入」兩個獨立語義，合併會改變全域初始化的求值時機。

import (
	"github.com/lizongying/nolang/parser"
)

// foldDeclAssign 合併相鄰的「宣告 + 賦值」陳述對。輸入是已過濾過的陳述切片，
// 回傳的新切片中每個可折疊的對已合成為單條 LetStatement（型別取第一條、值取
// 第二條），並保持其餘陳述與順序不變。
func (f *formatter) foldDeclAssign(statements []parser.Statement) []parser.Statement {
	if len(statements) < 2 {
		return statements
	}
	out := make([]parser.Statement, 0, len(statements))
	for i := 0; i < len(statements); i++ {
		if i+1 < len(statements) {
			if merged := f.mergeDeclAssign(statements[i], statements[i+1]); merged != nil {
				out = append(out, merged)
				i++ // 第二條已被吸入
				continue
			}
		}
		out = append(out, statements[i])
	}
	return out
}

// mergeDeclAssign 判定 decl + asg 是否構成可合併的「宣告 + 賦值」對，
// 可合併時回傳合成後的節點（decl 的淺拷貝，Value 換成 asg 的初始化式）。
func (f *formatter) mergeDeclAssign(decl, asg parser.Statement) parser.Statement {
	d, ok := decl.(*parser.LetStatement)
	if !ok || d == nil || d.IsSynthetic || d.Name == nil || d.Value != nil {
		return nil
	}
	// (1)(4) 只有源碼顯式寫下的具名型別。
	if d.Type == nil || typeIsInferred(d.Type) {
		return nil
	}
	if _, isNamed := d.Type.(*parser.NamedType); !isNamed {
		return nil
	}
	// (2)
	if n := d.Name.Value; n == "" || n == "_" || n == "it" {
		return nil
	}
	a, ok := asg.(*parser.LetStatement)
	if !ok || a == nil || a.IsSynthetic || a.Name == nil || a.Value == nil {
		return nil
	}
	// (3)
	if a.Name.Value != d.Name.Value || !typeIsInferred(a.Type) {
		return nil
	}
	if a.Comment != nil || f.hasDocComment(a) {
		return nil
	}
	// 第一條的 doc 註解隨節點複製過去、仍印在合併後那行上方，不丟資訊；
	// 行內註解與 `#{...}` 條目則無處安放（前者在 Value 之後、後者按節點查
	// side-table），一律放棄。
	if d.Comment != nil || len(f.attachedAnnotations(d)) > 0 {
		return nil
	}
	// (5)
	if len(f.attachedAnnotations(a)) > 0 {
		return nil
	}
	// (3) 空行：宣告與初始化被刻意分開。
	dEnd, aStart := stmtTokenEndLine(d), stmtTokenLine(a)
	if dEnd <= 0 || aStart <= 0 || aStart <= dEnd {
		return nil
	}
	if f.hasBlankLineBetween(dEnd, aStart) {
		return nil
	}
	merged := *d
	merged.Value = a.Value
	return &merged
}
