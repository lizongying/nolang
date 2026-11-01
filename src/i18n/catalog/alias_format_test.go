package catalog_test

import (
	"strings"
	"testing"

	"github.com/lizongying/nolang/i18n"
	"github.com/lizongying/nolang/i18n/catalog"
)

// TestAliasRowLayoutPinsTheColumns: a row is a positional array, so the order is
// the whole contract. This test states it once, in the same shape a human sees
// in the file, and proves the round-trip is byte-stable.
func TestAliasRowLayoutPinsTheColumns(t *testing.T) {
	const doc = `{
  'args.get': ['获取参数', 'g', 'hqcs', 'huoqucanshu']
  'arr.arr-stack-pop': ['弹出栈顶', 'asp', 'tczd']
}
`
	rows, err := catalog.ParseAliasCatalogData("test.noml", []byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	zh := rows["args.get"]
	if zh.Alias != "获取参数" || zh.EnKeys != "g" || zh.Keys != "hqcs" || zh.Full != "huoqucanshu" {
		t.Errorf("args.get decoded as %+v, want alias/enkeys/keys/full in that order", zh)
	}
	noFull := rows["arr.arr-stack-pop"]
	if noFull.Alias != "弹出栈顶" || noFull.EnKeys != "asp" || noFull.Keys != "tczd" || noFull.Full != "" {
		t.Errorf("arr.arr-stack-pop decoded as %+v, want an absent fourth column", noFull)
	}

	// Re-rendering must reproduce the canonical form, and parsing that must
	// reproduce the same rows: the freshness gate compares bytes, so a
	// one-way-consistent renderer would still fail it.
	rendered := catalog.RenderAlias("zh-CN", rows)
	again, err := catalog.ParseAliasCatalogData("rendered.noml", []byte(rendered))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for anchor, want := range rows {
		if got := again[anchor]; got != want {
			t.Errorf("%s: round-trip changed the row\n got %+v\nwant %+v", anchor, got, want)
		}
	}
	if second := catalog.RenderAlias("zh-CN", again); second != rendered {
		t.Error("render is not idempotent")
	}
}

// TestAliasRowRejectsWrongShapes: every rejection here would otherwise fail
// silently at runtime — a shifted column shows a keystroke string as the hover
// name, and a non-ASCII cell simply never matches.
func TestAliasRowRejectsWrongShapes(t *testing.T) {
	cases := map[string]string{
		"too few elements":    `{ 'a.b': ['名字', 'g'] }`,
		"too many elements":   `{ 'a.b': ['名字', 'g', 'hq', 'x', 'y'] }`,
		"scalar value":        `{ 'a.b': '名字' }`,
		"empty alias":         `{ 'a.b': ['', 'g', 'hq'] }`,
		"non-string element":  `{ 'a.b': ['名字', 1, 'hq'] }`,
		"space in keystrokes": `{ 'a.b': ['名字', 'g h', 'hq'] }`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := catalog.ParseAliasCatalogData("test.noml", []byte(doc)); err == nil {
				t.Fatalf("parsed %s; want a loud rejection", doc)
			}
		})
	}
}

// TestAliasFullColumnAcceptsBopomofoPunctuation: ㄝ/ㄡ/ㄤ/ㄥ/ㄦ live on , . ; / -
// in the Dachen layout, so those five characters are legal in column 3 only.
func TestAliasFullColumnAcceptsBopomofoPunctuation(t *testing.T) {
	const doc = `{ 'a.b': ['名字', 'g', 'hq', '0;8/-,.'] }`
	rows, err := catalog.ParseAliasCatalogData("test.noml", []byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := rows["a.b"].Full; got != "0;8/-,." {
		t.Fatalf("Full = %q, want the punctuation keys preserved", got)
	}
	if _, err := catalog.ParseAliasCatalogData("test.noml", []byte(`{ 'a.b': ['名字', 'g;h', 'hq'] }`)); err == nil {
		t.Fatal("enkeys accepted a punctuation key; only column 3 may carry one")
	}
}

// TestRenderAliasSkipsRowsWithoutDisplayName: such a row cannot be parsed back,
// so rendering it would produce a file the loader rejects.
func TestRenderAliasSkipsRowsWithoutDisplayName(t *testing.T) {
	out := catalog.RenderAlias("en", map[string]i18n.AliasEntry{
		"a.b": {Keys: "hq", EnKeys: "g"},
	})
	if strings.Contains(out, "a.b") {
		t.Fatalf("rendered a row with no alias:\n%s", out)
	}
}
