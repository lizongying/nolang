package checker

// Stage 6B：糖形态诊断三锚点覆盖验证。
//
// 三锚点（§2.B）：
//   ① 解析期形态无关类（warnUnreachableMatchArms）→ ParseProgram 收尾，三通道全覆盖
//   ② H0 降糖期发现类（h0Diags）→ 仅 build 路径，类型依赖
//   ③ 需糖形状的 lint 类 → RunSurfaceLints（H0 前跑 L0）
//
// 本测试验证：RunSurfaceLints 在典型糖语料上确实产出诊断（非空洞），
// 且与 runTHIRStageChecks 的并集覆盖 RunAllLints 的全部诊断。

import (
	"testing"

	"github.com/lizongying/nolang/lexer"
	"github.com/lizongying/nolang/parser"
)

// TestSurfaceLintsCoverSugarDiagnostics 验证 RunSurfaceLints 在糖语料上产出诊断。
func TestSurfaceLintsCoverSugarDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		// expectInSurface: RunSurfaceLints 必须产出至少一条诊断
		expectInSurface bool
	}{
		{"non-exhaustive-match", "f = (a ?i64) (r i64) {\n\ta: {\n\t\tok -> return it\n\t}\n}\n", true},
		{"catch-all-arm", "f = (a ?i64) (r i64) {\n\ta: {\n\t\tnil -> return 0\n\t\t-> return 1\n\t}\n}\n", true},
		{"unused-var", "f = () {\n\t_unused i64 = 42\n}\n", true},
		{"naming", "BadName = 1\n", true},
		{"str-ordering", "f = (a str) {\n\tif a >= \"x\" { }\n}\n", true},
		{"div-zero", "f = (a i64) (r i64) {\n\treturn 1 / 0\n}\n", true},
	}

	for _, tc := range cases {
		p := parser.New(lexer.New(tc.src))
		prog := p.ParseProgram()
		if prog == nil {
			t.Fatalf("%s: ParseProgram returned nil", tc.name)
		}
		opts := LintOptions{SourcePath: "test.no", RootDir: "."}
		surface := RunSurfaceLints(prog, opts)
		thir := runTHIRStageChecks(prog, opts)
		all := RunAllLints(prog, opts)

		if tc.expectInSurface && len(surface) == 0 {
			t.Errorf("%s: RunSurfaceLints produced 0 diagnostics (expected >0)", tc.name)
		}
		if len(all) == 0 {
			t.Errorf("%s: RunAllLints produced 0 diagnostics (expected >0)", tc.name)
		}
		// 并集覆盖验证（多重集包含关系）
		surfaceKeys := make(map[string]int)
		for _, r := range surface {
			surfaceKeys[lintDiagKey(r)]++
		}
		thirKeys := make(map[string]int)
		for _, r := range thir {
			thirKeys[lintDiagKey(r)]++
		}
		for _, r := range all {
			k := lintDiagKey(r)
			if surfaceKeys[k] > 0 {
				surfaceKeys[k]--
			} else if thirKeys[k] > 0 {
				thirKeys[k]--
			} else {
				t.Errorf("%s: diagnostic %q in RunAllLints but not in surface⊎thir", tc.name, k)
			}
		}
	}
}

// TestSurfaceTrackHasSugarSteps 验证 lintSteps 表中糖相关步骤在 surface 轨道。
func TestSurfaceTrackHasSugarSteps(t *testing.T) {
	required := map[string]bool{
		"non-exhaustive-match": true,
		"catch-all-arms":       true,
		"range-index-loops":    true,
		"range-self-ellipsis":  true,
		"list-dir-unfiltered":  true,
		"parser-warnings":      true,
	}
	found := make(map[string]bool)
	for _, s := range lintSteps {
		if required[s.name] {
			found[s.name] = true
			if !s.surface {
				t.Errorf("step %q should be surface track but is THIR", s.name)
			}
		}
	}
	for name := range required {
		if !found[name] {
			t.Errorf("required sugar step %q not found in lintSteps", name)
		}
	}
}
