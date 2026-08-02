package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

func TestProcessEmbeds(t *testing.T) {
	// 創建臨時目錄結構
	tmpDir := t.TempDir()
	// 創建 package.jsonc
	modContent := `{
  "name": "test-embed",
  "version": "1.0.0",
  "description": "test"
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.jsonc"), []byte(modContent), 0644); err != nil {
		t.Fatal(err)
	}
	// 創建嵌入資源文件
	assetContent := []byte("HELLO")
	assetPath := filepath.Join(tmpDir, "asset.txt")
	if err := os.WriteFile(assetPath, assetContent, 0644); err != nil {
		t.Fatal(err)
	}
	// 創建源碼文件路徑（不需要實際創建文件，processEmbeds 只需要 sourcePath 用於路徑解析）
	sourcePath := filepath.Join(tmpDir, "main.no")

	t.Run("success", func(t *testing.T) {
		src := "#{embed='asset.txt'}\nDATA []byte"
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		trans := NewTranspiler(nil)
		if err := trans.processEmbeds(prog, sourcePath); err != nil {
			t.Fatalf("processEmbeds error: %v", err)
		}
		// 找到 LetStatement 並檢查 side-table 中的 EmbedData
		for _, stmt := range prog.Statements {
			if ls, ok := stmt.(*parser.LetStatement); ok {
				embedData := prog.Sem.EmbedDataOf(ls)
				if len(embedData) != len(assetContent) {
					t.Errorf("EmbedData length = %d, want %d", len(embedData), len(assetContent))
				}
				for i, b := range embedData {
					if b != assetContent[i] {
						t.Errorf("EmbedData[%d] = %d, want %d", i, b, assetContent[i])
					}
				}
				return
			}
		}
		t.Fatal("no LetStatement found")
	})

	t.Run("file_not_found", func(t *testing.T) {
		src := "#{embed='nonexistent.txt'}\nDATA []byte"
		l := lexer.New(src)
		p := parser.New(l)
		prog := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parse errors: %v", p.Errors())
		}
		trans := NewTranspiler(nil)
		err := trans.processEmbeds(prog, sourcePath)
		if err == nil {
			t.Fatal("expected error for nonexistent file, got nil")
		}
		if !strings.Contains(err.Error(), "file not found") {
			t.Errorf("error should contain 'file not found', got: %v", err)
		}
	})
}
