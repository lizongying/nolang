package fmt

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TEMPORARY PROBE — not for commit.
func TestZZProbeMarkdownParseRate(t *testing.T) {
	if os.Getenv("PROBE_MD") == "" {
		t.Skip("set PROBE_MD=1")
	}
	openRe := regexp.MustCompile(`^(\s*)(` + "`" + `{3,})(\s*)([A-Za-z0-9_+-]*)`)
	var total, okCnt int
	langs := map[string]int{}
	var sample []string
	_ = filepath.WalkDir("../../docs", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.Contains(path, "node_modules") || strings.Contains(path, ".docusaurus") {
			return nil
		}
		if !strings.HasSuffix(path, ".md") && !strings.HasSuffix(path, ".mdx") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i := 0; i < len(lines); i++ {
			m := openRe.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			lang := strings.ToLower(strings.TrimSpace(m[4]))
			if lang != "no" && lang != "nolang" {
				continue
			}
			langs[lang]++
			j := i + 1
			var body []string
			for j < len(lines) {
				if strings.HasPrefix(strings.TrimSpace(lines[j]), "`") {
					break
				}
				body = append(body, lines[j])
				j++
			}
			i = j
			total++
			src := strings.Join(body, "\n")
			if _, ok, _ := formatProgram(src); ok {
				okCnt++
			} else if len(sample) < 12 {
				first := strings.TrimSpace(src)
				if len(first) > 70 {
					first = first[:70]
				}
				sample = append(sample, path+":"+m[0]+" FAIL "+first)
			}
		}
		return nil
	})
	t.Logf("total=%d parse-ok=%d rate=%.1f%% langs=%v", total, okCnt, float64(okCnt)*100/float64(total), langs)
	for _, s := range sample {
		t.Logf("  %s", s)
	}
}
