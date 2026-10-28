package fmt

import (
	"sort"
	"strings"

	"github.com/lizongying/nolang/parser"
)

// 導入語句（`# path[.fn] [alias]`）的自動整理：formatProgram 輸出前，對文件
// 頂部**連續的 UseStatement 區段**做一次規範化，三條規則：
//
//  1. 去重：完全相同（Path+Function+Alias 逐字相同）的導入只保留第一條 ——
//     重複導入不改變任何綁定，刪掉不改變語義。
//  2. 移除 `# std/fmt`：print/eprint/format/sprintf 是編譯器內建（ForwardFunc，
//     見 src/builtin/fmt.go），std 模組又隱式可載入，`# std/fmt` 從不必需。
//     寫了別名（`# std/fmt fm`）視為用戶刻意保留該名稱綁定，不動。
//  3. 排序：按 Path → Function → Alias 字典序穩定排序（同鍵保持相對次序）。
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

	// 規則 1（去重）+ 規則 2（移除無別名的 `# std/fmt`）。
	kept := make([]*parser.UseStatement, 0, len(run))
	seen := map[string]bool{}
	for _, u := range run {
		key := strings.Join([]string{u.Path, u.Function, u.Alias}, "\x00")
		if seen[key] {
			continue // 完全重複：首行已保留
		}
		seen[key] = true
		if !structured && u.Alias == "" && u.Function == "" && u.Path == "std/fmt" {
			continue
		}
		kept = append(kept, u)
	}

	// 規則 3（排序）。
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
