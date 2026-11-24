package fmt

import (
	"sort"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// 導入語句（`# path[.fn] [alias]`）的自動整理：formatProgram 輸出前，對文件
// 頂部**連續的 UseStatement 區段**做一次規範化，四條規則：
//
//  1. 去重：完全相同（Path+Function+Alias 逐字相同）的導入只保留第一條 ——
//     重複導入不改變任何綁定，刪掉不改變語義。
//  2. 移除無別名的純 std 模組導入（path 以 `std/` 開頭，如 `# std/noml`、
//     `# std/database/sql`）：標準庫由編譯器自動載入（見語法文件
//     syntax/50-modules-imports.md〈Module Prefix Rules〉），這類導入不改變
//     任何綁定，純屬冗餘。寫了別名（`# std/noml n`）視為用戶刻意保留該名稱
//     綁定，不動；函數導入（`# std/math.sqrt`）承載裸名 sqrt 綁定，也不動。
//  3. 移除未使用的無別名模組導入：`# nogit` 綁定 `nogit`，`# nogit/remote`
//     綁定 `remote`。若正文沒有用到該名稱（含 `nogit.remote(...)` 的根標識符
//     與 `remote` 這種點號屬性），導入只是噪音。`std/`、`js/`、`npm/` 與
//     以 `/` 開頭的原始碼模組不走這條規則（前者由規則 2 / 生成器管理，
//     後者可能承載 side effect）。
//  4. 排序：按 Path → Function → Alias 字典序穩定排序（同鍵保持相對次序）。
//
// 保守邊界（任一命中即整個區段保留源序、且不做移除——用戶寫下的附着內容
// 不可靜默丟棄，見 LSP formatter non-silence 慣例）：
//   - 任一導入帶 Doc 註釋或同行註釋：排序會拆散註釋與語句的視覺對應，
//     移除會連註釋一起消失。
//   - 任一導入帶附着 `#{...}` 註解（如 `#{bind=ffi}` 原生綁定策略）：註解與
//     導入的配對承載語義。
//
// 即便如此，「無任何附着內容的完全重複項」在任何區段都會被去重（重複行是
// 首行的逐字複印，刪掉不丟失資訊）。

// normalizeImportRun 就地重排/刪減 program 頂部連續導入區段。
func normalizeImportRun(p *parser.Program) {
	if p == nil {
		return
	}
	end := 0
	for end < len(p.Statements) {
		if _, ok := p.Statements[end].(*parser.UseStatement); !ok {
			break
		}
		end++
	}
	if end == 0 {
		return
	}
	run := make([]*parser.UseStatement, 0, end)
	for _, s := range p.Statements[:end] {
		run = append(run, s.(*parser.UseStatement))
	}

	// 保守閘：任一導入帶註釋或附着 `#{...}` 註解 → 區段保留源序、不做移除。
	structured := false
	f := &formatter{sem: p.Sem}
	for _, u := range run {
		if d := u.GetDoc(); d != nil && len(d.List) > 0 {
			structured = true
			break
		}
		if c := u.GetComment(); c != nil && len(c.List) > 0 {
			structured = true
			break
		}
		if len(f.attachedAnnotations(u)) > 0 {
			structured = true
			break
		}
	}

	// 規則 3 需要知道正文用到了哪些名字。只從導入區段之後的語句取樣，
	// 避免把 `# aaa/lib` 自己的 Path 當成「已使用」。
	used := importNamesUsed(p, end)

	// 規則 1（去重）+ 規則 2/3（移除冗餘導入）。
	kept := make([]*parser.UseStatement, 0, len(run))
	seen := map[string]bool{}
	for _, u := range run {
		key := strings.Join([]string{u.Path, u.Function, u.Alias}, "\x00")
		if seen[key] {
			continue // 完全重複：首行已保留
		}
		seen[key] = true
		if !structured && u.Alias == "" && u.Function == "" {
			// std 為隱式載入：純模組導入不改變任何綁定，可安全移除。
			if strings.HasPrefix(u.Path, "std/") || u.Path == "std" {
				continue
			}
			// 包別名 / 模組導入：正文沒引用綁定名就清掉。
			if removablePackageImport(u.Path) && !used[moduleBindingName(u.Path)] {
				continue
			}
		}
		kept = append(kept, u)
	}

	// 規則 4（排序）。
	if !structured {
		sort.SliceStable(kept, func(i, j int) bool {
			a, b := kept[i], kept[j]
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			if a.Function != b.Function {
				return a.Function < b.Function
			}
			return a.Alias < b.Alias
		})
	}

	out := make([]parser.Statement, 0, len(p.Statements)-end+len(kept))
	for _, u := range kept {
		out = append(out, u)
	}
	out = append(out, p.Statements[end:]...)
	p.Statements = out
}

// removablePackageImport 判斷是否適用「未使用則清理」規則。
// 只處理純包名 / 包內模組路徑（nogit、nogit/remote）；std/js/npm 與原始碼
// 路徑（/src/foo）各有自己的載入語義，不因正文未直接引用就刪除。
func removablePackageImport(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") {
		return false
	}
	first := path
	if i := strings.IndexByte(path, '/'); i >= 0 {
		first = path[:i]
	}
	if first == "std" || first == "js" || first == "npm" {
		return false
	}
	// URL 風格（github.com/o/r/tool）留給依賴解析器，fmt 不猜它的綁定名。
	return !strings.Contains(first, ".")
}

// moduleBindingName 回傳無別名模組導入在正文裡的綁定名：路徑最後一段。
// `# nogit` → nogit；`# nogit/remote` → remote。
func moduleBindingName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// importNamesUsed 收集正文（導入區段之後）出現過的標識符與點號屬性。
// 連點號屬性一起收集是保守策略：`nogit.remote(...)` 裡 `remote` 只是
// DotExpression.Property，但 `# nogit/remote` 的綁定名正是它——不能誤刪。
func importNamesUsed(p *parser.Program, from int) map[string]bool {
	used := map[string]bool{}
	if p == nil || from >= len(p.Statements) {
		return used
	}
	rest := &parser.Program{Statements: p.Statements[from:]}
	parser.WalkProgramNodes(rest, func(n parser.Node) {
		switch v := n.(type) {
		case *parser.Identifier:
			used[v.Value] = true
		case *parser.DotExpression:
			used[v.Property] = true
		}
	})
	return used
}
