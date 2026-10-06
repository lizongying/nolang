package main

import (
	"os"
	"path/filepath"
	"strings"
)

// 轻量 .gitignore 支援：讓 `no fmt -w <dir>` 跳過被 .gitignore 排除的路徑，
// 避免把 tmp/、dist/、node_modules/ 等暫存／產物目錄裡的殘檔當成源碼去格式化，
// 從而出現大量假的 parse error 誤導他人。僅實作日常常見的規則子集。

// gitIgnoreRule 對應 .gitignore 中一行有效規則。
type gitIgnoreRule struct {
	pattern  string // glob 主體（已移除前導 '!' 與前導 '/'）
	negation bool   // '!' 前綴：把先前被忽略的路徑重新納入
	dirOnly  bool   // 結尾 '/'：只匹配目錄
	anchored bool   // 規則含分隔符：相對該 .gitignore 所在目錄錨定匹配
}

// gitIgnoreFile 記錄一個 .gitignore 及其所在目錄。
type gitIgnoreFile struct {
	base  string // 含此 .gitignore 的目錄（絕對路徑）
	rules []gitIgnoreRule
}

// loadGitIgnoreFiles 從 startDir 向上收集 .gitignore，直到倉庫根（含 `.git`
// 者）或檔案系統根為止。回傳順序為外層→內層，呼叫端依序套用、內層覆蓋外層，
// 以符合 git 的優先級語義。
func loadGitIgnoreFiles(startDir string) []*gitIgnoreFile {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil
	}
	var files []*gitIgnoreFile
	dir := abs
	for {
		if content, err := os.ReadFile(filepath.Join(dir, ".gitignore")); err == nil {
			if gf := parseGitIgnore(dir, string(content)); len(gf.rules) > 0 {
				files = append([]*gitIgnoreFile{gf}, files...)
			}
		}
		if isRepoRoot(dir) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return files
}

// isRepoRoot 判斷 dir 是否為 git 倉庫根（含有 `.git` 條目）。
func isRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// parseGitIgnore 解析 .gitignore 內容為規則列表（跳過空行與註解）。
func parseGitIgnore(base, content string) *gitIgnoreFile {
	gf := &gitIgnoreFile{base: filepath.Clean(base)}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := gitIgnoreRule{}
		if strings.HasPrefix(line, "!") {
			r.negation = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			r.anchored = true
			line = strings.TrimPrefix(line, "/")
		} else if strings.Contains(line, "/") {
			r.anchored = true
		}
		r.pattern = line
		gf.rules = append(gf.rules, r)
	}
	return gf
}

// isPathIgnored 回報 path 是否被任一已收集的 .gitignore 排除。
func isPathIgnored(files []*gitIgnoreFile, path string, isDir bool) bool {
	if len(files) == 0 {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	ignored := false
	for _, gf := range files { // 外層→內層，內層優先
		if !isUnderDir(abs, gf.base) {
			continue
		}
		rel := strings.TrimPrefix(abs[len(gf.base):], "/")
		if rel == "" {
			continue
		}
		for _, r := range gf.rules {
			if r.dirOnly && !isDir {
				continue
			}
			if matchRule(r, rel) {
				ignored = !r.negation
			}
		}
	}
	return ignored
}

// isUnderDir 判斷 path 是否位於 dir 之下（含 dir 本身）。
func isUnderDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, dir+string(os.PathSeparator))
}

// matchRule 以 git 語義將規則套用到相對路徑 rel。
func matchRule(r gitIgnoreRule, rel string) bool {
	rel = filepath.ToSlash(rel)
	if r.anchored {
		return globMatch(r.pattern, rel)
	}
	// 未錨定：規則可匹配路徑中任一節段（相當於「任意層級下的該名稱」）。
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			continue
		}
		if globMatch(r.pattern, seg) {
			return true
		}
	}
	return false
}

// globMatch 用有限的 glob（* 不跨越 /、? 匹配單字元）比對。
func globMatch(pattern, name string) bool {
	ok, err := filepath.Match(pattern, name)
	if err != nil {
		return pattern == name
	}
	return ok
}
