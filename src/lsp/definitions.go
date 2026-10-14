package lsp

import (
	"path/filepath"

	"github.com/lizongying/nolang/checker"
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

// stdTypeDefinition 為「內建型別名」提供 go-to-definition：若 word 是 LSP 已知的
// 內建型別（見 hover.go 的 builtinTypeDoc），且存在同名 std 模組檔，則回傳指向該
// 模組檔開首（型別方法實作處）的 Location。數值型別（i64/u8/f64…）沒有對應 std 檔，
// 回傳 false（其 hover 已由 builtinTypeDoc 覆蓋，无需跳檔）。
func stdTypeDefinition(word string) (Location, bool) {
	if _, ok := builtinTypeDoc[word]; !ok {
		return Location{}, false
	}
	stdFile := checker.ResolveStdModulePath(word)
	if stdFile == "" {
		return Location{}, false
	}
	abs := stdFile
	if a, err := filepath.Abs(stdFile); err == nil {
		abs = a
	}
	// 定位到模組頂部標頭（第 0 行），即該型別方法實作的檔案開首。
	return Location{
		URI: "file://" + abs,
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 0, Character: uint32(len(word))},
		},
	}, true
}
