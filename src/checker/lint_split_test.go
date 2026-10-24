package checker

// Stage 6A 切分等价测试：驗證 RunAllLints ≡ RunSurfaceLints ⊎ runTHIRStageChecks
// （多重集相等），以及 lintSteps 表的完整性（步驟名不重不漏）。
// 此測試為永久性門禁（6E 收口後為 lint 等價性的唯一自動驗證）。

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// lintDiagKey 返回診斷的多重集歸一化鍵。
func lintDiagKey(r LintResult) string {
	var b strings.Builder
	b.WriteString(r.File)
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(r.Line))
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(r.Column))
	b.WriteByte('|')
	b.WriteString(string(r.Severity))
	b.WriteByte('|')
	b.WriteString(r.TraceID)
	b.WriteByte('|')
	b.WriteString(r.Message)
	return b.String()
}

// lintMultiset 將結果歸一化為排序後的鍵切片（多重集表示）。
func lintMultiset(results []LintResult) []string {
	keys := make([]string, len(results))
	for i, r := range results {
		keys[i] = lintDiagKey(r)
	}
	sort.Strings(keys)
	return keys
}

// lintMultisetEqual 比較兩個多重集是否相等。
func lintMultisetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// parseNolang 解析一段 Nolang 源碼。
func parseNolang(t *testing.T, name, src string) *parser.Program {
	t.Helper()
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	if prog == nil {
		t.Fatalf("%s: ParseProgram returned nil", name)
	}
	return prog
}

// buildSplitTestPrograms 構建切分等價測試用語料（合成靶向 + tests/*.no）。
func buildSplitTestPrograms(t *testing.T) []struct {
	name string
	prog *parser.Program
	opts LintOptions
} {
	t.Helper()
	type entry struct {
		name string
		prog *parser.Program
		opts LintOptions
	}
	var entries []entry

	// 合成语料（覆蓋糖節點/option/命名/注解/Strict 等）
	synth := []struct {
		name string
		src  string
		opts LintOptions
	}{
		{"match-sugar", "f = (a i64) (r i64) {\n\ta: {\n\t\ti64 -> return a\n\t\t-> return 0\n\t}\n}\n", LintOptions{}},
		{"multi-assign", "f = (a ?i64) (r ?i64) {\n\tb, c = 1, 2\n\treturn b\n}\n", LintOptions{}},
		{"option-lifecycle", "f = () (r ?i64) {\n\tx = 1 + 2\n}\n", LintOptions{}},
		{"naming", "BadName = 1\n", LintOptions{}},
		{"unused-var", "_unused i64 = 1\n", LintOptions{}},
		{"div-zero", "f = (a i64) (r i64) {\n\tv i64 = 0 / 0\n\treturn v\n}\n", LintOptions{}},
		{"str-concat", "f = (a str) {\n\tb = a + \"x\"\n}\n", LintOptions{}},
		{"str-ordering", "f = (a str) {\n\tif a >= \"x\" { }\n}\n", LintOptions{}},
		{"index-write", "f = (a [3]i64) {\n\ta[0] = 1\n}\n", LintOptions{}},
		{"overflow-annot", "#{overflow = wrap}\nf = (a i64) (r i64) {\n\treturn a + 1\n}\n", LintOptions{}},
		{"compound-assign", "f = (a i64) (r i64) {\n\tx = a\n\tx += 1\n\treturn x\n}\n", LintOptions{}},
		{"catch-all-arm", "f = (a ?i64) (r i64) {\n\ta: {\n\t\t-> return 0\n\t}\n}\n", LintOptions{LightweightMode: true}},
		{"skip-type", "f = (a i64) (r i64) {\n\treturn a\n}\n", LintOptions{SkipTypeChecks: true, Strict: true}},
		{"simple", "x i64 = 1\n", LintOptions{}},
		{"func-empty", "f = () {\n\tv i64 = 1\n}\n", LintOptions{}},
	}

	for _, s := range synth {
		opts := s.opts
		opts.SourcePath = filepath.Join("synth", s.name+".no")
		entries = append(entries, entry{s.name, parseNolang(t, s.name, s.src), opts})
	}

	// tests/*.no 真語料
	matches, err := filepath.Glob(filepath.Join("..", "..", "tests", "*.no"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) < 50 {
		t.Fatalf("too few test files: %d", len(matches))
	}
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		prog := parseNolang(t, m, string(b))
		entries = append(entries, entry{m, prog, LintOptions{
			SourcePath: m,
			RootDir:    filepath.Join("..", ".."),
		}})
	}

	// 轉為匿名結構體切片以匹配返回型別
	result := make([]struct {
		name string
		prog *parser.Program
		opts LintOptions
	}, len(entries))
	for i, e := range entries {
		result[i].name = e.name
		result[i].prog = e.prog
		result[i].opts = e.opts
	}
	return result
}

// TestLintStepsTableComplete 驗證 lintSteps 表不重不漏。
func TestLintStepsTableComplete(t *testing.T) {
	seen := make(map[string]bool)
	surfaceCount := 0
	thirCount := 0
	for _, s := range lintSteps {
		if seen[s.name] {
			t.Errorf("duplicate step name: %q", s.name)
		}
		seen[s.name] = true
		if s.run == nil {
			t.Errorf("step %q has nil run func", s.name)
		}
		if s.surface {
			surfaceCount++
		} else {
			thirCount++
		}
	}
	total := len(lintSteps)
	if total < 35 {
		t.Errorf("too few lint steps: %d (expected ≥35)", total)
	}
	t.Logf("lintSteps: total=%d surface=%d thir=%d", total, surfaceCount, thirCount)
}

// TestLintSplitEquivalence 驗證多重集 RunAllLints ≡ RunSurfaceLints ⊎ runTHIRStageChecks。
func TestLintSplitEquivalence(t *testing.T) {
	entries := buildSplitTestPrograms(t)

	for _, e := range entries {
		opts := e.opts
		all := lintMultiset(RunAllLints(e.prog, opts))
		surface := RunSurfaceLints(e.prog, opts)
		thir := runTHIRStageChecks(e.prog, opts)
		combined := lintMultiset(append(append([]LintResult{}, surface...), thir...))

		if !lintMultisetEqual(all, combined) {
			t.Errorf("%s: multiset mismatch (all=%d combined=%d)", e.name, len(all), len(combined))
			// 打印差異
			allSet := make(map[string]int)
			for _, k := range all {
				allSet[k]++
			}
			combSet := make(map[string]int)
			for _, k := range combined {
				combSet[k]++
			}
			for k, cnt := range allSet {
				if combSet[k] != cnt {
					t.Errorf("  all-only: %q (all=%d combined=%d)", k, cnt, combSet[k])
				}
			}
			for k, cnt := range combSet {
				if allSet[k] != cnt {
					t.Errorf("  combined-only: %q (all=%d combined=%d)", k, allSet[k], cnt)
				}
			}
		}
	}
	t.Logf("split equivalence OK: %d programs", len(entries))
}
