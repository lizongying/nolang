package lsp

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/i18n"
)

// hoverText renders the hover body for the identifier at (line, col) and fails
// the test when there is no hover at all.
func hoverText(t *testing.T, text string, line, col uint32) string {
	t.Helper()
	doc := createTestDocument(text)
	program := createTestProgram(text)
	hp := NewHoverProvider(doc, createTestIndex(doc, program))
	hover, found := hp.GetHover(Position{Line: line, Character: col})
	if !found || hover == nil {
		t.Fatalf("no hover at %d:%d", line, col)
	}
	mc, ok := hover.Contents.(MarkupContent)
	if !ok {
		t.Fatalf("contents = %T, want MarkupContent", hover.Contents)
	}
	return mc.Value
}

// linkedDocSource: line 0-1 are prose, line 2 is the link, line 3 is the
// declaration.
const linkedDocSource = `; get: 獲取第 i 個參數
; 委派至 os.arg(i) builtin；越界回傳 nil
; [get](#args.get)
get = (i i64) (val ?str) {
    val = os.arg(i)
}
`

const translatedDoc = "get: the i-th argument\ndelegates to the os.arg(i) builtin; an out-of-range i returns nil"

// TestHoverTranslatesLinkedDoc is the end-to-end property of the doc link: a doc
// comment authored in Chinese is shown in English when the toolchain runs in
// English, and the whole block is translated as one unit.
func TestHoverTranslatesLinkedDoc(t *testing.T) {
	i18n.SetDocCatalog(map[string]string{"args.get": translatedDoc})
	defer i18n.SetDocCatalog(nil)

	got := hoverText(t, linkedDocSource, 3, 0)
	if !strings.Contains(got, "get: the i-th argument") {
		t.Errorf("hover does not carry the English doc: %q", got)
	}
	if !strings.Contains(got, "delegates to the os.arg(i) builtin") {
		t.Errorf("hover does not carry the translated second line: %q", got)
	}
	if strings.Contains(got, "獲取第 i 個參數") {
		t.Errorf("hover still shows the source text: %q", got)
	}
	if strings.Contains(got, "#args.get") || strings.Contains(got, "[get]") {
		t.Errorf("hover leaked the link line into the doc body: %q", got)
	}
}

// TestHoverOnLinkShowsContent is the "hover the link itself" behaviour: the link
// resolves to the entry it points at, in the active language.
func TestHoverOnLinkShowsContent(t *testing.T) {
	i18n.SetDocCatalog(map[string]string{"args.get": translatedDoc})
	defer i18n.SetDocCatalog(nil)

	// Column 2 is inside `get` in `; [get](#args.get)` — an ordinary identifier,
	// which must NOT win over the anchor.
	got := hoverText(t, linkedDocSource, 2, 2)
	if !strings.Contains(got, "args.get") {
		t.Errorf("hover on the link does not name the anchor: %q", got)
	}
	if !strings.Contains(got, "get: the i-th argument") {
		t.Errorf("hover on the link does not resolve to the entry: %q", got)
	}
	if strings.Contains(got, "獲取第 i 個參數") {
		t.Errorf("hover on the link shows the source text: %q", got)
	}
}

// TestHoverOnLinkFallsBackToSource: an anchor with no translation still resolves,
// to the comment text the author wrote. A link must never hover to nothing.
func TestHoverOnLinkFallsBackToSource(t *testing.T) {
	i18n.SetDocCatalog(nil)

	got := hoverText(t, linkedDocSource, 2, 2)
	if !strings.Contains(got, "args.get") {
		t.Errorf("hover on the link does not name the anchor: %q", got)
	}
	if !strings.Contains(got, "get: 獲取第 i 個參數") {
		t.Errorf("hover on the link does not fall back to the source: %q", got)
	}
}

// TestHoverStripsDocLinkWithoutCatalog covers the untranslated case for the
// symbol hover: the link line must never leak into the doc body.
func TestHoverStripsDocLinkWithoutCatalog(t *testing.T) {
	i18n.SetDocCatalog(nil)

	got := hoverText(t, linkedDocSource, 3, 0)
	if strings.Contains(got, "#args.get") || strings.Contains(got, "[get]") {
		t.Errorf("hover leaked the link line: %q", got)
	}
	if !strings.Contains(got, "get: 獲取第 i 個參數") {
		t.Errorf("hover lost the source doc: %q", got)
	}
}

// TestHoverUnmarkedDocUntouched proves the marker is opt-in: an ordinary comment
// is not looked up, so a coincidence between a comment's text and a catalog key
// can never silently rewrite it.
func TestHoverUnmarkedDocUntouched(t *testing.T) {
	i18n.SetDocCatalog(map[string]string{"args.plain": "SHOULD NOT APPEAR"})
	defer i18n.SetDocCatalog(nil)

	src := `; plain: 未標記的說明
plain = () (n i64) {
    n = 1
}
`
	got := hoverText(t, src, 1, 0)
	if strings.Contains(got, "SHOULD NOT APPEAR") {
		t.Errorf("an unmarked doc comment was translated: %q", got)
	}
	if !strings.Contains(got, "plain: 未標記的說明") {
		t.Errorf("hover lost the doc: %q", got)
	}
}

// TestHoverLinkMustBeLastLine: a link in the middle of a doc group is prose, not a
// marker. Treating it as one would silently drop the lines below it.
func TestHoverLinkMustBeLastLine(t *testing.T) {
	i18n.SetDocCatalog(map[string]string{"args.get": "SHOULD NOT APPEAR"})
	defer i18n.SetDocCatalog(nil)

	src := `; [get](#args.get)
; still prose
get = () (n i64) {
    n = 1
}
`
	got := hoverText(t, src, 2, 0)
	if strings.Contains(got, "SHOULD NOT APPEAR") {
		t.Errorf("a non-trailing link was treated as a marker: %q", got)
	}
	if !strings.Contains(got, "still prose") {
		t.Errorf("hover lost the trailing prose: %q", got)
	}
}
