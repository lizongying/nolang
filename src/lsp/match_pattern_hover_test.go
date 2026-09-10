package lsp

import (
	"strings"
	"testing"
)

// REGRESSION GUARD: hovering the option match patterns `ok` / `nil` / `err`
// in a match arm must show the **option type** of the matched expression
// (e.g. `?i64`) plus the meaning of that specific variant — not fall through
// to a same-named symbol (or show nothing at all).
func TestHoverMatchOptionPatternShowsOptionType(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/match_pattern_hover.no"
	text := "main = () (content ?i64) {\n" +
		"    fd = open-read('/tmp/probe_x')\n" +
		"    size ?= fstat-size(fd)\n" +
		"    size: {\n" +
		"        ok -> content = it\n" +
		"        nil -> content = 0\n" +
		"        err -> content = 0\n" +
		"    }\n" +
		"}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, _, err := dm.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	idx := dm.GetIndex(uri)
	if idx == nil {
		t.Fatal("no index")
	}
	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	hp := NewHoverProvider(doc, idx)

	cases := []struct {
		name    string
		line    uint32
		char    uint32
		want    []string
		notWant []string
	}{
		// `ok` 顯示解箱後的內層型別 i64（不是 ?i64）；`nil` / `err` 顯示各自的哨兵型別。
		{"ok", 4, 8, []string{"option 模式", "**Option type**: `?i64`", "**Type**: `i64`", "**Matched**: `size`"}, []string{"**Type**: `?i64`"}},
		{"nil", 5, 8, []string{"option 模式", "**Option type**: `?i64`", "**Type**: `nil`"}, []string{"**Type**: `?i64`"}},
		{"err", 6, 8, []string{"option 模式", "**Option type**: `?i64`", "**Type**: `err`"}, []string{"**Type**: `?i64`"}},
	}
	for _, c := range cases {
		h, ok := hp.GetHover(Position{Line: c.line, Character: c.char})
		if !ok {
			t.Fatalf("%s: no hover returned", c.name)
		}
		mc, ok := h.Contents.(MarkupContent)
		if !ok {
			t.Fatalf("%s: hover contents is not MarkupContent: %T", c.name, h.Contents)
		}
		for _, w := range c.want {
			if !strings.Contains(mc.Value, w) {
				t.Errorf("%s: hover missing %q:\n%s", c.name, w, mc.Value)
			}
		}
		for _, w := range c.notWant {
			if strings.Contains(mc.Value, w) {
				t.Errorf("%s: hover should not contain %q:\n%s", c.name, w, mc.Value)
			}
		}
	}
}

// The `ok` on the **right side** of `->` (e.g. `err -> ok = err('...')`) is a
// variable, not a pattern — it must NOT get the option-pattern doc.
func TestHoverMatchOptionPatternIgnoresRHS(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/match_pattern_rhs.no"
	text := "main = () (ok ?i64) {\n" +
		"    fd = open-read('/tmp/probe_x')\n" +
		"    size ?= fstat-size(fd)\n" +
		"    size: {\n" +
		"        err -> ok = 0\n" +
		"    }\n" +
		"}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, _, err := dm.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	idx := dm.GetIndex(uri)
	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	hp := NewHoverProvider(doc, idx)

	// `ok` at line 4 column 15 (right of `->`) — a variable assignment target.
	h, ok := hp.GetHover(Position{Line: 4, Character: 15})
	if !ok {
		return // no hover at all is fine; only assert it is not the pattern doc
	}
	if mc, ok := h.Contents.(MarkupContent); ok && strings.Contains(mc.Value, "option 模式") {
		t.Errorf("RHS `ok` wrongly shows option-pattern doc:\n%s", mc.Value)
	}
}

// Combined patterns (`nil || err -> ...`) and same-line matches
// (`size: { ok -> ... }`) must also resolve.
func TestHoverMatchOptionPatternCombinedAndSameLine(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/match_pattern_combined.no"
	text := "main = () (content ?i64) {\n" +
		"    fd = open-read('/tmp/probe_x')\n" +
		"    size ?= fstat-size(fd)\n" +
		"    size: {\n" +
		"        nil || err -> content = 0\n" +
		"        -> content = size\n" +
		"    }\n" +
		"    size: { ok -> content = it }\n" +
		"}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, _, err := dm.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	idx := dm.GetIndex(uri)
	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	hp := NewHoverProvider(doc, idx)

	// `err` of `nil || err ->` on line 4.
	h, ok := hp.GetHover(Position{Line: 4, Character: 15})
	if !ok {
		t.Fatal("no hover for `err` in combined pattern")
	}
	if mc, ok := h.Contents.(MarkupContent); !ok || !strings.Contains(mc.Value, "`?i64`") {
		t.Errorf("combined pattern hover missing option type: %+v", h.Contents)
	}

	// `ok` of the same-line match on line 7 (`    size: { ok -> content = it }`).
	h, ok = hp.GetHover(Position{Line: 7, Character: 13})
	if !ok {
		t.Fatal("no hover for `ok` in same-line match")
	}
	if mc, ok := h.Contents.(MarkupContent); !ok || !strings.Contains(mc.Value, "`?i64`") {
		t.Errorf("same-line match hover missing option type: %+v", h.Contents)
	}
}

// `ok(name) ->` 析構綁定形式：懸停 `ok` 仍須識別為 option 模式並顯示
// option 型別（括號內的 name 只是綁定名，不是條件）。
func TestHoverMatchOptionPatternWithBindingName(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/match_pattern_bind.no"
	text := "main = () (content ?i64) {\n" +
		"    fd = open-read('/tmp/probe_x')\n" +
		"    size ?= fstat-size(fd)\n" +
		"    size: {\n" +
		"        ok(it1) -> content = it1\n" +
		"        nil -> content = 0\n" +
		"        err -> content = 0\n" +
		"    }\n" +
		"}\n"

	if _, err := dm.OpenDocument(uri, text); err != nil {
		t.Fatalf("OpenDocument failed: %v", err)
	}
	if _, _, err := dm.ParseDocument(uri); err != nil {
		t.Fatalf("ParseDocument failed: %v", err)
	}
	idx := dm.GetIndex(uri)
	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	hp := NewHoverProvider(doc, idx)

	// `ok` of `ok(it1) ->` on line 4, column 8.
	h, ok := hp.GetHover(Position{Line: 4, Character: 8})
	if !ok {
		t.Fatal("no hover for `ok` in `ok(it1) ->`")
	}
	mc, ok := h.Contents.(MarkupContent)
	if !ok {
		t.Fatalf("hover contents is not MarkupContent: %T", h.Contents)
	}
	if !strings.Contains(mc.Value, "option 模式") {
		t.Errorf("`ok(it1)` not recognized as option pattern:\n%s", mc.Value)
	}
	if !strings.Contains(mc.Value, "`?i64`") {
		t.Errorf("`ok(it1)` hover missing option type:\n%s", mc.Value)
	}
	if !strings.Contains(mc.Value, "**Type**: `i64`") {
		t.Errorf("`ok(it1)` hover missing inner type `i64`:\n%s", mc.Value)
	}
}
