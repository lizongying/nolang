package build

import "testing"

// TestStructLiteralQualifiedTypeLoadsModule 釘住一個「只在有標註時才成立」的
// 巧合：跨模組結構體字面量用**推導型別**（`w = zip.writer{}`）時，該模組必須
// 和顯式標註（`w zip.writer = zip.writer{}`）一樣被按需載入。
//
// 缺陷形態（2026-11-12 修，HEAD 基線同樣復現）：
//
//	w = zip.writer{}
//	d = w.data            ; ⇒ EmitLLVM: builtin str-len: receiver has no slot
//	d = w.data.slice(0, len(w.data))   ; ⇒ unknown callee d.to-hex
//
// 根因不在 MIR 的欄位解析（`w.data` 沒撞上 `%vec.data`），而在模組載入：
// collectReferencedStdModules 掃 StructLiteral 時只有 `addRef(ex.Type)`，而
// lookup 的鍵是模組 ShortName（"zip"），`zip.writer` 因此永遠匹配不上 ⇒
// zip 模組體不進 merged ⇒ 結構體定義不進 HIR ⇒ MIR 的 StructFields 沒有
// "zip.writer" ⇒ 頂層 let 走 synthesizeMainForTopLevel 的「非內聯」分支
// （isInlineableLetType 對含點的 raw 一律 false）時 StructFields 落空、整個
// `continue` 掉，綁定從未 materialize —— 於是 `w` 是 void。
//
// 顯式標註之所以正常，是 walkType 的 NamedType 分支早就把前綴拆出來 addRef
// 了。兩條路徑（型別註解 / 字面量的類型名）必須同口徑。
//
// 不變量：同一份程式，去掉型別標註與加上型別標註必須都能編譯，且都能量到
// `data` 欄位的 vec 長度（= 0，新建的空 writer）。
func TestStructLiteralQualifiedTypeLoadsModule(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "inferred",
			src: `w = zip.writer{}
n = len(w.data)
m = len(w.cd-data)
print('n=' - n - ' m=' - m)
`,
		},
		{
			name: "annotated",
			src: `w zip.writer = zip.writer{}
n = len(w.data)
m = len(w.cd-data)
print('n=' - n - ' m=' - m)
`,
		},
		{
			// 字面量的 vec 欄位要能真的當 vec 用：slice + to-hex + push。
			// 這是「欄位型別被誤認成 str」時最先崩的三個使用者可見形態。
			name: "field-is-real-vec",
			src: `w = zip.writer{}
d = w.data.slice(0, len(w.data))
h = d.to-hex()
w.data.push(0x41)
w.data.push(0x42)
print('h=' - h - ' n=' - len(w.data) - ' hex=' - w.data.to-hex())
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// compileWith 會在失敗時 t.Fatalf，涵蓋我們要的全部斷言：
			// 模組載入失敗/欄位型別錯誤都會以編譯錯誤的形式暴露。
			ir := compileWith(t, tc.src, "main.no")
			if ir == "" {
				t.Fatal("empty LLVM IR")
			}
			// 反向校驗：型別若真被誤認成 str，`len()` 會落到 @_mir_str_len
			// 家族；空的 zip.writer 沒有任何 str 欄位，出現即是回歸。
			if containsFn(ir, "_mir_str_len") {
				t.Fatalf("zip.writer 的 vec 欄位被當成 str（IR 出現 _mir_str_len）:")
			}
		})
	}
}

// containsFn 回報 LLVM IR 中是否出現某個符號（@name 或 call ... @name）。
func containsFn(ir, name string) bool {
	for i := 0; i+len(name) <= len(ir); i++ {
		if ir[i:i+len(name)] == name {
			return true
		}
	}
	return false
}
