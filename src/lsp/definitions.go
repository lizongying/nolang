package lsp

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

type DefinitionProvider struct {
	index *SymbolIndex
	doc   *TextDocument
}

func NewDefinitionProvider(doc *TextDocument, index *SymbolIndex) *DefinitionProvider {
	return &DefinitionProvider{
		index: index,
		doc:   doc,
	}
}

func (dp *DefinitionProvider) GetDefinition(position Position) (Location, bool) {
	word := getWordAtPosition(dp.doc.Text, position)
	if word == "" {
		return Location{}, false
	}

	if dp.index == nil {
		// 無符號表時仍需支援內建型別跳檔（如 `[]char` 的 char → std/char.no）。
		if loc, ok := stdTypeDefinition(word); ok {
			return loc, true
		}
		return Location{}, false
	}

	// Check if the word is part of a module-qualified call (e.g. "fs.read"
	// where the cursor is on "read" and "fs." precedes it). If so, try the
	// qualified name first so that `fs.read` resolves to the `fs` module's
	// definition, not to any unrelated symbol named `read` in the index.
	qualified := getQualifiedWordAtPosition(dp.doc.Text, position)
	if qualified != "" {
		if entry, ok := dp.index.GetDefinition(qualified); ok {
			return entry.Location, true
		}
	}

	entry, ok := dp.index.GetDefinition(word)
	if !ok {
		entry, ok = dp.index.Lookup(word)
		if !ok {
			// 內建型別名（char/str/txt/byte/bool…）在 AST 裡沒有宣告節點——它们是
			// 編譯器內建型別，其方法定義在 std/<type>.no。跳到該模組檔，讓
			// `cs []char`、`t ?txt`、`val txt` 之类的型別標註可 F12 導航。
			if loc, ok := stdTypeDefinition(word); ok {
				return loc, true
			}
			return Location{}, false
		}
	}

	// 索引命中的可能是內建符號的佔位項（AddBuiltinSymbols 不寫 Location，URI 為空），
	// 對跳檔毫無意義。此時仍嘗試 std 模組檔兜底，使型別名能導航到實作檔。
	if entry.Location.URI == "" {
		if loc, ok := stdTypeDefinition(word); ok {
			return loc, true
		}
	}

	return entry.Location, true
}

// stdTypeDefinition 為「內建型別名」提供 go-to-definition。
//
// 兩類走法：
//   - 佈局型別（str / txt / vec / arr）：模組與文檔錨點取自 parser.BuiltinTypes()，
//     落點是該模組的模組級文檔錨點行（`; [txt](#txt)`）—— 使用者想讀的是型別文檔，
//     不是檔案第 0 行；
//   - 其餘內建型別（char / byte / bool…）：名字即模組名，落點退回文件開首。
//
// 數值型別（i64/u8/f64…）沒有對應 std 檔（ResolveStdModulePath 回空），回傳 false。
func stdTypeDefinition(word string) (Location, bool) {
	if info, ok := parser.LookupBuiltinType(word); ok {
		return stdModuleLocation(info.Module, info.Anchor)
	}
	if _, ok := builtinTypeDoc[word]; !ok {
		return Location{}, false
	}
	return stdModuleLocation(word, "")
}

// stdModuleLocation 把「std 模組短名 + 可選的模組文檔錨點」解析成 Location。
// 錨點命中時落在 `; [anchor](#anchor)` 那一行；否則退回文件開首。
func stdModuleLocation(module, anchor string) (Location, bool) {
	stdFile := checker.ResolveStdModulePath(module)
	if stdFile == "" {
		return Location{}, false
	}
	abs := stdFile
	if a, err := filepath.Abs(stdFile); err == nil {
		abs = a
	}
	line, width := docAnchorLine(stdFile, anchor)
	return Location{
		URI: "file://" + abs,
		Range: Range{
			Start: Position{Line: line, Character: 0},
			End:   Position{Line: line, Character: width},
		},
	}, true
}

// docAnchorLine 在 std 模組檔裡找模組級文檔錨點 `; [anchor](#anchor)`，回傳
// 該行行號（0 起算）與行寬；找不到（或 anchor 為空）回 (0, 0)，即退回文件開首。
func docAnchorLine(path, anchor string) (uint32, uint32) {
	if anchor == "" {
		return 0, 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	want := "[" + anchor + "](#" + anchor + ")"
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimSpace(strings.TrimPrefix(line, ";"))
		if line == want {
			return uint32(i), uint32(len(strings.TrimRight(raw, " \t\r")))
		}
	}
	return 0, 0
}
