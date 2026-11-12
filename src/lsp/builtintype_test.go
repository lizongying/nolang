package lsp

import (
	"os"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
	"github.com/lizongying/nolang/parser"
)

// TestBuiltinTypesTable 釘住登記表自身的形狀：四條佈局型別、名字唯一、
// 模組短名與錨點就是型別名（`txt` ⇒ src/std/txt.no 的 `; [txt](#txt)`）。
//
// 這張表是 hover / F12 / 補全的唯一來源，任何一條寫錯都會讓對應工具靜默失真。
func TestBuiltinTypesTable(t *testing.T) {
	all := parser.BuiltinTypes()
	if len(all) != 4 {
		t.Fatalf("BuiltinTypes() has %d entries, want 4 (str / txt / vec / arr)", len(all))
	}

	seen := make(map[string]bool, len(all))
	for _, info := range all {
		if seen[info.Name] {
			t.Errorf("duplicate builtin type %q", info.Name)
		}
		seen[info.Name] = true

		if info.Module != info.Name {
			t.Errorf("%q: Module = %q, want %q (module short name is the type name today)",
				info.Name, info.Module, info.Name)
		}
		if info.Anchor != info.Name {
			t.Errorf("%q: Anchor = %q, want %q", info.Name, info.Anchor, info.Name)
		}
		if info.Summary == "" || info.Layout == "" {
			t.Errorf("%q: Summary / Layout must be non-empty (%q / %q)",
				info.Name, info.Summary, info.Layout)
		}

		wantRuntime := info.Name != "arr"
		if info.Runtime != wantRuntime {
			t.Errorf("%q: Runtime = %v, want %v (arr has no runtime descriptor: [n]t is a bare [N x T])",
				info.Name, info.Runtime, wantRuntime)
		}
		if info.Name == "arr" {
			if info.Size != 0 {
				t.Errorf("arr: Size = %d, want 0 (no fixed size)", info.Size)
			}
		} else if info.Size <= 0 {
			t.Errorf("%q: Size = %d, want > 0", info.Name, info.Size)
		}

		if !parser.IsBuiltinLayoutStructName(info.Name) {
			t.Errorf("IsBuiltinLayoutStructName(%q) = false, want true (it must delegate to the table)",
				info.Name)
		}
	}

	if parser.IsBuiltinLayoutStructName("int") {
		t.Error("IsBuiltinLayoutStructName(\"int\") = true, want false (int is a std union alias, not a layout type)")
	}
}

// TestBuiltinTypesMatchStdDocs 是本輪的防漂移測試：表裡的每一條都必須指到
// 一個真實存在的 std 模組檔，而且那個檔案裡必須有一行模組級文檔錨點
// `; [<anchor>](#<anchor>)` —— F12 的落點就是靠掃這一行。
//
// 兩邊任何一側改名（表改了、或 std 的錨點改了）都會在這裡紅。
func TestBuiltinTypesMatchStdDocs(t *testing.T) {
	for _, info := range parser.BuiltinTypes() {
		path := checker.ResolveStdModulePath(info.Module)
		if path == "" {
			t.Fatalf("%q: std module %q not found (set NOLANG_STD_SRC or run via make test)",
				info.Name, info.Module)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%q: read %s: %v", info.Name, path, err)
		}
		anchor := "[" + info.Anchor + "](#" + info.Anchor + ")"
		if !strings.Contains(string(data), anchor) {
			t.Errorf("%q: %s has no module doc anchor %q; F12 would silently fall back to line 0",
				info.Name, path, anchor)
		}
	}
}

// TestStdTypeDefinitionLandsOnDocAnchor 驗證 F12 落在模組文檔錨點行，而不是
// 文件第 0 行 —— 使用者要讀的是這個型別的文檔，不是檔案開頭。
func TestStdTypeDefinitionLandsOnDocAnchor(t *testing.T) {
	for _, info := range parser.BuiltinTypes() {
		loc, ok := stdTypeDefinition(info.Name)
		if !ok {
			t.Errorf("%q: stdTypeDefinition returned false", info.Name)
			continue
		}
		path := strings.TrimPrefix(loc.URI, "file://")
		if path == loc.URI {
			t.Errorf("%q: URI %q is not a file:// URI", info.Name, loc.URI)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%q: read %s: %v", info.Name, path, err)
			continue
		}
		lines := strings.Split(string(data), "\n")
		line := int(loc.Range.Start.Line)
		if line == 0 {
			t.Errorf("%q: F12 lands on line 0 (file head), want the module doc anchor line", info.Name)
			continue
		}
		if line >= len(lines) {
			t.Errorf("%q: F12 line %d out of range (%s has %d lines)", info.Name, line, path, len(lines))
			continue
		}
		anchor := "[" + info.Anchor + "](#" + info.Anchor + ")"
		if !strings.Contains(lines[line], anchor) {
			t.Errorf("%q: line %d of %s is %q, want it to contain %q",
				info.Name, line, path, strings.TrimSpace(lines[line]), anchor)
		}
		if loc.Range.End.Character == 0 {
			t.Errorf("%q: F12 range has zero width", info.Name)
		}
	}
}

// TestHoverRendersBuiltinLayoutTypes：四個佈局型別都要有 hover，而且文案要
// 說出佈局（vec / arr 先前完全沒有 hover —— 它們不在 builtinTypeDoc 裡）。
func TestHoverRendersBuiltinLayoutTypes(t *testing.T) {
	for _, info := range parser.BuiltinTypes() {
		text := "t " + info.Name + " = 'z'"
		doc := createTestDocument(text)
		program := createTestProgram(text)
		hp := NewHoverProvider(doc, createTestIndex(doc, program))

		hover, found := hp.GetHover(Position{Line: 0, Character: 2})
		if !found || hover == nil {
			t.Errorf("%q: no hover on the type annotation", info.Name)
			continue
		}
		mc, ok := hover.Contents.(MarkupContent)
		if !ok {
			t.Errorf("%q: hover contents is %T, want MarkupContent", info.Name, hover.Contents)
			continue
		}
		if !strings.Contains(mc.Value, "`"+info.Name+"`") {
			t.Errorf("%q: hover does not name the type: %q", info.Name, mc.Value)
		}
		if !strings.Contains(mc.Value, "Layout") {
			t.Errorf("%q: hover does not show the layout: %q", info.Name, mc.Value)
		}
	}
}

// TestCompletionIncludesBuiltinLayoutTypes：補全清單含 txt / vec / arr
// （舊清單只有 str），且不重複。
func TestCompletionIncludesBuiltinLayoutTypes(t *testing.T) {
	cp := &CompletionProvider{}
	items := cp.getTypeCompletions()

	count := map[string]int{}
	for _, it := range items {
		count[it.Label]++
	}
	for _, name := range []string{"str", "txt", "vec", "arr"} {
		if count[name] == 0 {
			t.Errorf("completion is missing builtin type %q", name)
		}
		if count[name] > 1 {
			t.Errorf("completion lists %q %d times", name, count[name])
		}
	}
}
