// add_modprefix: 為 .no 檔案中的跨模組函數調用添加 ShortPath 前綴
//
// 用法: go run ./tools/add_modprefix <root-dir>
// 例如: go run ./tools/add_modprefix /path/to/no
//
// 跨模組調用前綴規則：
//   需要前綴：
//   - 其他模組定義的模組級函數（如 hash.sha256.sha256()、fs.open()）
//   - 其他模組定義的常量（如 net.NET-BUF-SIZE、math.PI）
//
//   不需要前綴：
//   - printf / sprintf / print（依規定免除，非因 builtin）
//   - 同檔案內定義的函數、常量、方法
//   - 內置類型的方法調用（str/i64/vec/arr/byte/char 等，如 s.starts-with()）
//   - 結構體實例的方法調用（如 f.read()，方法已通過型別解析）
//   - 字串字面量與註釋中的內容（工具不處理，僅修改程式碼）
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// funcDefRe matches top-level function definitions: `funcname = (`
var funcDefRe = regexp.MustCompile(`^([a-z][a-zA-Z0-9_-]*)\s*=\s*\(`)

// constDefRe matches top-level constant definitions: `CONSTNAME = value`
var constDefRe = regexp.MustCompile(`^([A-Z][A-Z0-9_-]*)\s*=\s`)

// methodDefRe matches method definitions: `type.method = (`
var methodDefRe = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_-]*)\.([a-zA-Z][a-zA-Z0-9_-]*)\s*=\s*\(`)

// commentRe matches comment lines
var commentRe = regexp.MustCompile(`^\s*//`)

// builtinFuncs are functions that don't need a module prefix
var builtinFuncs = map[string]bool{
	"printf":  true,
	"sprintf": true,
	"print":   true,
}

// funcToPath maps function name → ShortPath (dotted form, e.g. "hash.sha256")
var funcToPath = make(map[string]string)

// constToPath maps constant name → ShortPath (dotted form)
var constToPath = make(map[string]string)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: add_modprefix <root-dir>")
		os.Exit(1)
	}
	rootDir := os.Args[1]
	stdDir := filepath.Join(rootDir, "src/std")

	// 1. Build function → ShortPath map from all std .no files
	buildFuncMap(stdDir)
	fmt.Fprintf(os.Stderr, "Built map: %d functions, %d constants\n", len(funcToPath), len(constToPath))

	// 2. Process all .no files in std/, example/, tests/
	var dirs []string
	dirs = append(dirs, stdDir)
	dirs = append(dirs, filepath.Join(rootDir, "example"))
	dirs = append(dirs, filepath.Join(rootDir, "tests"))

	totalChanged := 0
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		changed := processDir(dir, stdDir)
		totalChanged += changed
	}
	fmt.Fprintf(os.Stderr, "Total files changed: %d\n", totalChanged)
}

// buildFuncMap reads all std .no files and builds function/constant → ShortPath maps
func buildFuncMap(stdDir string) {
	files, err := filepath.Glob(filepath.Join(stdDir, "*.no"))
	if err != nil {
		return
	}
	// Also glob subdirectories
	subdirs, _ := os.ReadDir(stdDir)
	for _, sd := range subdirs {
		if sd.IsDir() {
			subFiles, _ := filepath.Glob(filepath.Join(stdDir, sd.Name(), "*.no"))
			files = append(files, subFiles...)
		}
	}

	for _, file := range files {
		rel, _ := filepath.Rel(stdDir, file)
		full := strings.TrimSuffix(rel, ".no")
		shortName := filepath.Base(full)
		dir := filepath.Dir(full)
		if dir == "." {
			// Top-level file
		} else if dir == shortName {
			full = shortName // ShortPath: omit redundant dir
		}
		// Convert ShortPath to dotted form
		dottedPath := strings.ReplaceAll(full, "/", ".")

		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if commentRe.MatchString(line) {
				continue
			}
			// Function definitions: `funcname = (`
			if m := funcDefRe.FindStringSubmatch(line); m != nil {
				fn := m[1]
				if !builtinFuncs[fn] {
					funcToPath[fn] = dottedPath
				}
			}
			// Constant definitions: `CONSTNAME = value`
			if m := constDefRe.FindStringSubmatch(line); m != nil {
				cn := m[1]
				constToPath[cn] = dottedPath
			}
		}
	}
}

// processDir processes all .no files in a directory (recursively)
func processDir(dir, stdDir string) int {
	changed := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".no") {
			return nil
		}
		if processFile(path, stdDir) {
			changed++
		}
		return nil
	})
	return changed
}

// processFile processes a single .no file
func processFile(path, stdDir string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	original := string(data)
	lines := strings.Split(original, "\n")

	// Find local function and constant definitions to exclude
	localFuncs := make(map[string]bool)
	localConsts := make(map[string]bool)
	for _, line := range lines {
		if m := funcDefRe.FindStringSubmatch(line); m != nil {
			localFuncs[m[1]] = true
		}
		if m := methodDefRe.FindStringSubmatch(line); m != nil {
			// Register method names as local to avoid prefixing
			localFuncs[m[2]] = true
		}
		if m := constDefRe.FindStringSubmatch(line); m != nil {
			localConsts[m[1]] = true
		}
	}

	// Determine this file's own ShortPath (to skip self-references)
	rel, _ := filepath.Rel(stdDir, path)
	if !strings.HasPrefix(rel, "..") {
		// This is a std file, compute its ShortPath
		thisFull := strings.TrimSuffix(rel, ".no")
		thisShort := filepath.Base(thisFull)
		thisDir := filepath.Dir(thisFull)
		if thisDir == "." {
			// top-level
		} else if thisDir == thisShort {
			thisFull = thisShort
		}
		thisDotted := strings.ReplaceAll(thisFull, "/", ".")
		// Remove this file's own functions and constants from the map (they're local)
		for fn, p := range funcToPath {
			if p == thisDotted {
				localFuncs[fn] = true
			}
		}
		for cn, p := range constToPath {
			if p == thisDotted {
				localConsts[cn] = true
			}
		}
	}

	// Process each line
	changed := false
	for i, line := range lines {
		if commentRe.MatchString(line) {
			continue
		}
		newLine := prefixLine(line, localFuncs, localConsts)
		if newLine != line {
			lines[i] = newLine
			changed = true
		}
	}

	if changed {
		result := strings.Join(lines, "\n")
		if result != original {
			os.WriteFile(path, []byte(result), 0644)
			fmt.Fprintf(os.Stderr, "  modified: %s\n", path)
			return true
		}
	}
	return false
}

// prefixLine adds ShortPath prefix to bare function calls in a line
func prefixLine(line string, localFuncs map[string]bool, localConsts map[string]bool) string {
	// Protect string literals from replacement
	// Split line into segments: code and string parts
	segments := splitCodeStrings(line)

	// Process functions: replace bare funcname( with dotted.funcname(
	for fn, path := range funcToPath {
		if localFuncs[fn] {
			continue
		}
		pattern := regexp.MustCompile(`(^|[^.\w-])` + regexp.QuoteMeta(fn) + `\(`)
		replacement := "${1}" + path + "." + fn + "("
		for i := range segments {
			if segments[i].isString {
				continue
			}
			segments[i].text = pattern.ReplaceAllString(segments[i].text, replacement)
		}
	}

	// Process constants: replace bare CONSTNAME with dotted.CONSTNAME
	for cn, path := range constToPath {
		if localConsts[cn] {
			continue
		}
		// Skip if this line is a definition of this constant
		if strings.Contains(line, cn+" = ") && strings.HasPrefix(strings.TrimSpace(line), cn+" ") {
			continue
		}
		pattern := regexp.MustCompile(`(^|[^.\w-])` + regexp.QuoteMeta(cn) + `([^A-Z0-9_-]|$)`)
		replacement := "${1}" + path + "." + cn + "${2}"
		for i := range segments {
			if segments[i].isString {
				continue
			}
			segments[i].text = pattern.ReplaceAllString(segments[i].text, replacement)
		}
	}

	// Reassemble
	var result strings.Builder
	for _, seg := range segments {
		result.WriteString(seg.text)
	}
	return result.String()
}

// lineSegment represents a part of a line that is either code or string content
type lineSegment struct {
	text     string
	isString bool
}

// splitCodeStrings splits a line into alternating code and string segments.
// String literals in Nolang use single quotes '...' or double quotes "...".
func splitCodeStrings(line string) []lineSegment {
	var segments []lineSegment
	var current strings.Builder
	inString := false
	var quoteChar byte

	for i := 0; i < len(line); i++ {
		ch := line[i]

		if !inString {
			if ch == '\'' || ch == '"' {
				// Flush current code segment
				if current.Len() > 0 {
					segments = append(segments, lineSegment{text: current.String(), isString: false})
					current.Reset()
				}
				inString = true
				quoteChar = ch
				current.WriteByte(ch)
			} else {
				current.WriteByte(ch)
			}
		} else {
			current.WriteByte(ch)
			if ch == quoteChar {
				// Check for escaped quote (preceded by backslash)
				if i > 0 && line[i-1] == '\\' {
					// Escaped, stay in string
					continue
				}
				// End of string
				segments = append(segments, lineSegment{text: current.String(), isString: true})
				current.Reset()
				inString = false
			}
		}
	}
	// Flush remaining
	if current.Len() > 0 {
		segments = append(segments, lineSegment{text: current.String(), isString: inString})
	}
	return segments
}
