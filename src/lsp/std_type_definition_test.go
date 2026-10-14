package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/checker"
)

// init 在 test 主體執行前設定 NOLANG_STD_SRC 指向本倉庫的 src/std，避免
// package.GetStdSourceDir 的 sync.Once 被其它測試以錯誤目錄抢先快取，讓下方
// 「char → std/char.no」的正向斷言可穩定命中（僅在未顯式設定該變數時才覆蓋）。
func init() {
	if os.Getenv("NOLANG_STD_SRC") != "" {
		return
	}
	if abs, err := filepath.Abs(filepath.Join("..", "std")); err == nil {
		if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
			_ = os.Setenv("NOLANG_STD_SRC", abs)
		}
	}
}

// TestStdTypeDefinitionGates 驗證跳檔只对「內建型別名」觸發：非型別詞、以及
// 沒有同名 std 檔的數值型別（i64）都不得跳檔。這不依賴 std 是否可解析。
func TestStdTypeDefinitionGates(t *testing.T) {
	if _, ok := stdTypeDefinition("definitely_not_a_type"); ok {
		t.Error("unknown identifier must not link to a std module")
	}
	if _, ok := stdTypeDefinition("i64"); ok {
		t.Error("numeric type without a std file (i64) must not link")
	}
}

// TestStdTypeDefinitionChar 驗證 `char`（內建型別，方法實作在 std/char.no）
// 能導航到該模組檔頂部。
func TestStdTypeDefinitionChar(t *testing.T) {
	if checker.ResolveStdModulePath("char") == "" {
		t.Skip("std source dir not reachable in this env")
	}
	loc, ok := stdTypeDefinition("char")
	if !ok {
		t.Fatal("`char` should resolve to std/char.no")
	}
	if !strings.HasSuffix(loc.URI, "/std/char.no") {
		t.Errorf("URI = %q, want .../std/char.no", loc.URI)
	}
	// 定位到模組頂部（第 0 行）。
	if loc.Range.Start.Line != 0 || loc.Range.Start.Character != 0 {
		t.Errorf("expected start at line0 col0, got %d:%d", loc.Range.Start.Line, loc.Range.Start.Character)
	}
}

// TestDefinitionCharTypeAnnotation 端到端驗證：游標落在 `cs []char` 的 `char` 上，
// GetDefinition 回傳指向 std/char.no 的 Location（即便符號表沒有 char 的宣告節點）。
func TestDefinitionCharTypeAnnotation(t *testing.T) {
	if checker.ResolveStdModulePath("char") == "" {
		t.Skip("std source dir not reachable in this env")
	}
	text := `txt.from-chars = (cs []char) (t ?txt) {
    t = val
}
`
	doc := createTestDocument(text)
	program := createTestProgram(text)
	dp := NewDefinitionProvider(doc, createTestIndex(doc, program))

	// 游標落在獨立型別標註 `[]char` 的 `char` 上——注意 "from-chars" 也含 "char"
	// 子串，必須用 "[]char" 定位真正的型別名（偏移 2 跳過 "["、"]"）。
	idx := strings.Index(text, "[]char")
	if idx < 0 {
		t.Fatal("test fixture missing `[]char`")
	}
	loc, found := dp.GetDefinition(Position{Line: 0, Character: uint32(idx + 2)})
	if !found {
		t.Fatal("go-to-definition on `char` should resolve via std module fallback")
	}
	if !strings.HasSuffix(loc.URI, "/std/char.no") {
		t.Errorf("URI = %q, want .../std/char.no", loc.URI)
	}
}

// TestStdTypeDefinitionOtherTypes 驗證「其他類型也同樣處理」：所有有同名 std
// 模組檔的內建型別（str/txt/byte/bool）都能跳檔到 std/<type>.no；沒有 std 檔的
// 型別（fd）不跳檔。无法解析的型別（ResolveStdModulePath 回空）则跳过。
func TestStdTypeDefinitionOtherTypes(t *testing.T) {
	for _, tc := range []struct {
		word   string
		wantOK bool
	}{
		{"str", true},
		{"txt", true},
		{"byte", true},
		{"bool", true},
		{"fd", false}, // fd 無 std/fd.no
	} {
		// 正向用例需要 std 可解析；不可达则跳过该 case。fd（期望不跳档）无需跳过。
		if tc.wantOK && checker.ResolveStdModulePath(tc.word) == "" {
			t.Skipf("std source dir not reachable for %q in this env", tc.word)
		}
		loc, ok := stdTypeDefinition(tc.word)
		if ok != tc.wantOK {
			t.Errorf("%q: ok = %v, want %v", tc.word, ok, tc.wantOK)
			continue
		}
		if tc.wantOK {
			want := "/std/" + tc.word + ".no"
			if !strings.HasSuffix(loc.URI, want) {
				t.Errorf("%q: URI = %q, want ...%s", tc.word, loc.URI, want)
			}
		}
	}
}
