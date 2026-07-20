package llvm

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestPlatformVariantResolution 驗證 FilterByPlatform 依目標平台過濾宣告，
// 以及 PlatformKeyFor 的反向查找行為。
func TestPlatformVariantResolution(t *testing.T) {
	// 測試 FilterByPlatform 依目標平台過濾
	t.Run("filter_by_target_platform", func(t *testing.T) {
		src := `#{mac-arm64}
O-EXCL = 2048
#{win-arm64}
O-EXCL = 1024
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}

		// 過濾到 windows/arm64 — 應保留 win-arm64 變體
		filtered := FilterByPlatform(prog.Statements, "windows", "arm64")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 statement for windows/arm64, got %d", len(filtered))
		}
		letStmt, ok := filtered[0].(*parser.LetStatement)
		if !ok {
			t.Fatalf("expected *LetStatement, got %T", filtered[0])
		}
		// 驗證保留的是 win-arm64 變體（值為 1024）
		if intLit, ok := letStmt.Value.(*parser.IntegerLiteral); !ok || intLit.Value != 1024 {
			t.Errorf("expected win-arm64 variant with value 1024, got %v", letStmt.Value)
		}

		// 過濾到 darwin/arm64 — 應保留 mac-arm64 變體
		filtered = FilterByPlatform(prog.Statements, "darwin", "arm64")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 statement for darwin/arm64, got %d", len(filtered))
		}
		letStmt, ok = filtered[0].(*parser.LetStatement)
		if !ok {
			t.Fatalf("expected *LetStatement, got %T", filtered[0])
		}
		if intLit, ok := letStmt.Value.(*parser.IntegerLiteral); !ok || intLit.Value != 2048 {
			t.Errorf("expected mac-arm64 variant with value 2048, got %v", letStmt.Value)
		}
	})

	// 測試 PlatformKeyFor 反向查找
	t.Run("platform_key_for", func(t *testing.T) {
		cases := []struct {
			goos, goarch, expected string
		}{
			{"darwin", "arm64", "mac-arm64"},
			{"darwin", "amd64", "mac-amd64"},
			{"linux", "arm64", "linux-arm64"},
			{"linux", "amd64", "linux-amd64"},
			{"windows", "arm64", "win-arm64"},
			{"windows", "amd64", "win-amd64"},
			{"freebsd", "arm64", ""}, // 不支援的平台
		}
		for _, c := range cases {
			got := PlatformKeyFor(c.goos, c.goarch)
			if got != c.expected {
				t.Errorf("PlatformKeyFor(%q, %q) = %q, want %q", c.goos, c.goarch, got, c.expected)
			}
		}
	})

	// 測試無平台註解的宣告在所有平台下都保留
	t.Run("generic_declaration_kept", func(t *testing.T) {
		src := `O-RDONLY = 0
`
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}

		// 在所有平台下都應保留
		for _, tc := range []struct{ goos, goarch string }{
			{"darwin", "arm64"},
			{"linux", "amd64"},
			{"windows", "arm64"},
		} {
			filtered := FilterByPlatform(prog.Statements, tc.goos, tc.goarch)
			if len(filtered) != 1 {
				t.Errorf("expected 1 statement for %s/%s, got %d", tc.goos, tc.goarch, len(filtered))
			}
		}
	})
}
