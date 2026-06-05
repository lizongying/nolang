package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Package 表示 nolang.jsonc 定義的專案套件
type Package struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Description     string            `json:"description,omitempty"`
	Keywords        []string          `json:"keywords,omitempty"`
	Author          string            `json:"author,omitempty"`
	Email           string            `json:"email,omitempty"`
	Organization    string            `json:"organization,omitempty"`
	Repository      string            `json:"repository,omitempty"`
	Homepage        string            `json:"homepage,omitempty"`
	License         string            `json:"license,omitempty"`
	Main            string            `json:"main,omitempty"`
	Dependencies    map[string]string `json:"dependencies,omitempty"`
	DevDependencies map[string]string `json:"dev-dependencies,omitempty"`
	Ignore          []string          `json:"ignore,omitempty"`
	Alias           map[string]string `json:"alias,omitempty"`
	RootDir         string            // 套件根目錄（含 nolang.jsonc）
}

// stripJSONC 移除 JSONC 中的 // 和 /* */ 註解
func stripJSONC(raw []byte) []byte {
	s := string(raw)
	var out strings.Builder
	inStr := false
	inLine := false
	inBlock := false
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if inStr {
			out.WriteRune(ch)
			if ch == '"' && (i == 0 || runes[i-1] != '\\') {
				inStr = false
			}
			continue
		}

		if inLine {
			if ch == '\n' {
				inLine = false
				out.WriteRune(ch)
			}
			continue
		}

		if inBlock {
			if ch == '*' && i+1 < len(runes) && runes[i+1] == '/' {
				inBlock = false
				i++
			}
			continue
		}

		if ch == '"' {
			inStr = true
			out.WriteRune(ch)
			continue
		}

		if ch == '/' && i+1 < len(runes) {
			next := runes[i+1]
			if next == '/' {
				inLine = true
				i++
				continue
			}
			if next == '*' {
				inBlock = true
				i++
				continue
			}
		}

		out.WriteRune(ch)
	}

	return []byte(out.String())
}

// LoadPackage 從 dir 目錄尋找並解析 nolang.jsonc
func LoadPackage(dir string) (*Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	// 向上尋找 nolang.jsonc
	root := abs
	for {
		candidate := filepath.Join(root, "nolang.jsonc")
		if _, err := os.Stat(candidate); err == nil {
			raw, err := os.ReadFile(candidate)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", candidate, err)
			}
			cleaned := stripJSONC(raw)
			var pkg Package
			if err := json.Unmarshal(cleaned, &pkg); err != nil {
				return nil, fmt.Errorf("parsing %s: %w", candidate, err)
			}
			pkg.RootDir = root

			// 補上預設 alias
			if pkg.Alias == nil {
				pkg.Alias = make(map[string]string)
			}
			if _, ok := pkg.Alias["@"]; !ok {
				pkg.Alias["@"] = "./"
			}

			return &pkg, nil
		}

		parent := filepath.Dir(root)
		if parent == root { // 到根目錄了
			break
		}
		root = parent
	}

	return nil, nil // 沒有找到套件
}

// ResolvePath 根據 alias 解析路徑
func (p *Package) ResolvePath(inputPath string) string {
	if p == nil {
		return inputPath
	}

	// 嘗試所有 alias 前綴
	for prefix, alias := range p.Alias {
		prefixStr := prefix
		if strings.HasPrefix(inputPath, prefixStr) {
			rel := strings.TrimPrefix(inputPath, prefixStr)
			aliasPath := filepath.Join(p.RootDir, alias, rel)
			return filepath.Clean(aliasPath)
		}
	}

	// 沒有匹配 alias，相對於套件根目錄
	return filepath.Clean(filepath.Join(p.RootDir, inputPath))
}
