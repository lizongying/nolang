package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsPathIgnored(t *testing.T) {
	root := t.TempDir()
	// 標記為倉庫根，讓向上收集在 root 停下，避免讀到外部 .gitignore。
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitignore := "tmp\ndist/\n*.log\nnogit/tmp-*\n!keep.log\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatal(err)
	}

	files := loadGitIgnoreFiles(root)
	if len(files) == 0 {
		t.Fatal("expected at least one gitignore file")
	}

	mustPath := func(parts ...string) string {
		p := filepath.Join(append([]string{root}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{mustPath("tmp"), true, true},                         // 暫存目錄本身
		{mustPath("tmp", "a", "t.no"), false, true},            // tmp 下的檔案
		{mustPath("dist"), true, true},                         // 目錄專用規則命中目錄
		{mustPath("app.log"), false, true},                     // *.log glob
		{mustPath("keep.log"), false, false},                   // !keep.log 重新納入
		{mustPath("nogit", "tmp-x.no"), false, true},           // 錨定含 / 的規則
		{mustPath("nogit", "index.no"), false, false},          // 未命中
		{mustPath("src", "main.no"), false, false},             // 正常源碼
	}
	for _, c := range cases {
		if got := isPathIgnored(files, c.path, c.isDir); got != c.want {
			t.Errorf("isPathIgnored(%q, isDir=%v) = %v, want %v", c.path, c.isDir, got, c.want)
		}
	}
}
