package lsp

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/i18n"
)

// aliasFixture installs a two-row alias catalog mirroring a zh-CN slice, and
// restores the process-wide table afterwards. Both rows deliberately expose the
// same identifier (`get`) under different modules, which is the dedupe case.
func aliasFixture(t *testing.T) {
	t.Helper()
	i18n.SetAliasCatalog(map[string]i18n.AliasEntry{
		"args.get": {
			Alias:  "获取参数",
			Keys:   "hqcs",
			EnKeys: "g",
			Full:   "huoqucanshu",
		},
		"map.get": {
			Alias:  "获取参数",
			Keys:   "hqcs",
			EnKeys: "g",
			Full:   "huoqucanshu",
		},
		"arr.arr-stack-pop": {
			Alias:  "弹出栈顶",
			Keys:   "tczd",
			EnKeys: "asp",
			Full:   "tanchuzhanding",
		},
	})
	t.Cleanup(func() { i18n.SetAliasCatalog(nil) })
}

func aliasCompletionFor(t *testing.T, text string, line, char uint32) []CompletionItem {
	t.Helper()
	doc := createTestDocument(text)
	cp := NewCompletionProvider(doc, nil)
	return cp.getAliasCompletions(Position{Line: line, Character: char})
}

// TestAliasCompletionByInitials: typing the display name's initials proposes the
// English identifier, and the edit REPLACES the typed letters (the whole point
// of the feature — otherwise `hqcs` would end up glued to `get`).
func TestAliasCompletionByInitials(t *testing.T) {
	aliasFixture(t)
	items := aliasCompletionFor(t, "hq", 0, 2)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 after dedupe: %+v", len(items), items)
	}
	item := items[0]
	if item.Label != "获取参数" {
		t.Errorf("Label = %q, want the localized display name", item.Label)
	}
	if item.TextEdit == nil || item.TextEdit.NewText != "get" {
		t.Fatalf("TextEdit = %+v, want insertion of the identifier `get`", item.TextEdit)
	}
	if r := item.TextEdit.Range; r.Start.Character != 0 || r.End.Character != 2 {
		t.Errorf("replace range = %+v, want [0,2) so the typed letters are consumed", r)
	}
	if !strings.Contains(item.FilterText, "hqcs") || !strings.Contains(item.FilterText, "get") {
		t.Errorf("FilterText = %q, want it to carry both the keystrokes and the identifier", item.FilterText)
	}
}

// TestAliasCompletionByFullPinyin: the pinyin column is full spelling, not
// initials, so a longer query must work too.
func TestAliasCompletionByFullPinyin(t *testing.T) {
	aliasFixture(t)
	items := aliasCompletionFor(t, "huoqu", 0, 5)
	if len(items) != 1 || items[0].TextEdit.NewText != "get" {
		t.Fatalf("items = %+v, want one `get` suggestion", items)
	}
}

// TestAliasCompletionByEnKeys: the identifier's own initials match in every
// language, which is what makes the column worth storing per locale.
func TestAliasCompletionByEnKeys(t *testing.T) {
	aliasFixture(t)
	items := aliasCompletionFor(t, "asp", 0, 3)
	if len(items) != 1 || items[0].TextEdit.NewText != "arr-stack-pop" {
		t.Fatalf("items = %+v, want one `arr-stack-pop` suggestion", items)
	}
}

// TestAliasCompletionSkipsUnspellableQuery: a CJK query is not something any
// column can match, and the byte-indexed word range would mis-slice it.
func TestAliasCompletionSkipsUnspellableQuery(t *testing.T) {
	aliasFixture(t)
	doc := createTestDocument("获取")
	cp := NewCompletionProvider(doc, nil)
	if items := cp.getAliasCompletions(Position{Line: 0, Character: 6}); len(items) != 0 {
		t.Fatalf("items = %+v, want none for a non-ASCII query", items)
	}
}

// TestAliasCompletionWithoutCatalog: the provider is installed in builds that
// never load a catalog (tests, `no` CLI), so it must be a no-op, not a panic.
func TestAliasCompletionWithoutCatalog(t *testing.T) {
	i18n.SetAliasCatalog(nil)
	if items := aliasCompletionFor(t, "hq", 0, 2); len(items) != 0 {
		t.Fatalf("items = %+v, want none without a catalog", items)
	}
}

// TestAliasCompletionInComment: a `;` comment is prose, and suggesting code
// there is noise.
func TestAliasCompletionInComment(t *testing.T) {
	aliasFixture(t)
	doc := createTestDocument("; 切换 lang 时 hq")
	cp := NewCompletionProvider(doc, nil)
	if items := cp.getAliasCompletions(Position{Line: 0, Character: 15}); len(items) != 0 {
		t.Fatalf("items = %+v, want none inside a comment", items)
	}
}

// TestAliasCompletionEmptyPrefix: an empty word must not enumerate the catalog.
func TestAliasCompletionEmptyPrefix(t *testing.T) {
	aliasFixture(t)
	doc := createTestDocument("")
	cp := NewCompletionProvider(doc, nil)
	if items := cp.getAliasCompletions(Position{Line: 0, Character: 0}); len(items) != 0 {
		t.Fatalf("items = %+v, want none for an empty prefix", items)
	}
}

// TestAliasCompletionAtEndOfLine covers the range that getWordRange gets wrong:
// with the cursor past the last character it returns an empty range, which would
// insert instead of replace.
func TestAliasCompletionAtEndOfLine(t *testing.T) {
	aliasFixture(t)
	items := aliasCompletionFor(t, "  hq", 0, 4)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want one suggestion", items)
	}
	r := items[0].TextEdit.Range
	if r.Start.Character != 2 || r.End.Character != 4 {
		t.Errorf("range = %+v, want [2,4) covering `hq`", r)
	}
}
