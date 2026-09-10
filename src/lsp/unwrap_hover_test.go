package lsp

import (
	"strings"
	"testing"
)

// REGRESSION GUARD: `size ?= fstat-size(.fd)` must hover `size` as `i64`
// (the unwrapped inner type), not `?i64` / `(?i64, bool)` / `call fstat-size`.
//
// Chain of requirements:
//  1. AddBuiltinSymbols must populate ResultParams, folding the registry's raw
//     C pair (i64, bool) into the language-level single `?i64` for
//     option-return builtins (stat-size / fstat-size / file-size).
//  2. The module-exports loop in ParseDocument must not clobber that entry
//     with a bare Type:"fn" placeholder.
//  3. The ASTWalker's UnwrapAssignStatement branch strips the leading "?".
func TestHoverUnwrapAssignFromBuiltinOptionReturn(t *testing.T) {
	dm := NewDocumentManager()
	uri := "file:///test/unwrap_hover.no"
	text := "main = () (content ?i64) {\n    fd = open-read('/tmp/probe_x')\n    size ?= fstat-size(.fd)\n    content = size\n}\n"

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

	// 1+2: builtin entry keeps its folded signature.
	fn, ok := idx.functions["fstat-size"]
	if !ok {
		t.Fatal("fstat-size not indexed")
	}
	if len(fn.ResultParams) != 1 || fn.ResultParams[0].Type != "?i64" {
		t.Errorf("fstat-size ResultParams = %v, want [?i64]", fn.ResultParams)
	}
	if !strings.Contains(fn.Type, "?i64") || strings.Contains(fn.Type, "bool") {
		t.Errorf("fstat-size Type = %q, want fn(fd) ?i64", fn.Type)
	}

	// 3: walker stores the stripped inner type for the ?= target.
	sym, ok := idx.symbols["size"]
	if !ok {
		t.Fatal("symbols[size] missing")
	}
	if sym.Type != "i64" {
		t.Errorf("symbols[size].Type = %q, want i64", sym.Type)
	}

	// Hover on a later reference resolves through the same entry.
	doc, err := dm.GetDocument(uri)
	if err != nil {
		t.Fatalf("GetDocument failed: %v", err)
	}
	hp := NewHoverProvider(doc, idx)
	h, ok := hp.GetHover(Position{Line: 3, Character: 14}) // `size` in `content = size`
	if !ok {
		t.Fatal("no hover on later reference")
	}
	if mc, ok := h.Contents.(MarkupContent); ok {
		if strings.Contains(mc.Value, "?i64") || strings.Contains(mc.Value, "call fstat-size") {
			t.Errorf("hover shows wrong type: %s", mc.Value)
		}
	}
}
