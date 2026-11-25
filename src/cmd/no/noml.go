package main

import (
	"sort"
	"strings"

	pkg "github.com/lizongying/nolang/package"

	"github.com/lizongying/nolang/i18n"
)

// This file hosts the noml (Nolang Markup Language) helpers used by the
// `no` CLI for workspace/package configuration files. The format itself is
// implemented in src/package/noml.go (parser, marshaler, 'key:version'
// dependency specs); the .jsonc readers/writers that used to live here were
// removed when the toolchain hard-cut to .noml.

// ─── Public API ───────────────────────────────────────────────────────────────

// nomlParseMap parses noml data that is expected to be a flat object whose
// values are all strings (e.g. workspace.noml). Returns map[string]string.
func nomlParseMap(data []byte) (map[string]string, error) {
	m, err := pkg.ParseNomlMap(data)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		s, ok := val.(string)
		if !ok {
			return nil, i18n.Errorf("dns4nguy", "noml: value for key %q is not a string (got %T)", k, val)
		}
		result[k] = s
	}
	return result, nil
}

// nomlMarshalMap serialises a map[string]string to noml text with 2-space
// indentation. Keys are sorted for deterministic output; object members are
// newline-separated (no commas).
func nomlMarshalMap(m map[string]string) string {
	anyMap := make(map[string]any, len(m))
	for k, v := range m {
		anyMap[k] = v
	}
	text, err := pkg.MarshalNoml(anyMap)
	if err != nil {
		// map[string]string 不可能序列化失敗；保底返回空物件。
		return "{\n}\n"
	}
	return text
}

// nomlParseProjectConfig parses noml data into a ProjectConfig.
func nomlParseProjectConfig(data []byte) (*ProjectConfig, error) {
	m, err := pkg.ParseNomlMap(data)
	if err != nil {
		return nil, err
	}
	// 舊清單沒有 publish 時沿用 nohub 的缺省 true；DecodeInto 只會在鍵存在時覆寫。
	cfg := ProjectConfig{Publish: true}
	if err := pkg.DecodeInto(m, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ─── package.noml template ───────────────────────────────────────────────────

// packageCategoryComment 列出 category 欄位的建議取值（英文小寫），以 ;
// 註解形式寫在產生出的 package.noml 最上方供使用者參考。順序與官方分類一致。
const packageCategoryComment = `; category (optional, lowercase), one of:
;   books
;   business
;   developer tools
;   education
;   entertainment
;   finance
;   food & drink
;   graphics & design
;   health & fitness
;   lifestyle
;   magazines & newspapers
;   medical
;   music
;   navigation
;   news
;   photo & video
;   productivity
;   reference
;   safari extensions
;   shopping
;   social networking
;   sports
;   travel
;   utilities
;   weather
`

// renderPackageNoml renders a package.noml document from ProjectConfig.
// 只寫 pkg.RequiredManifestFields()：可選欄位留給內建預設，所以新生成的清單
// 一出生就不會被 no fmt / LoadPackage 警告缺欄位。
// dependencies 以 'key:version' 字串陣列落地（排序後輸出求確定性）。
func renderPackageNoml(config ProjectConfig) string {
	var sb strings.Builder
	sb.WriteString(packageCategoryComment)
	sb.WriteString("{\n")

	quoted := func(v string) string { return pkg.NomlQuote(v) }
	strArray := func(items []string) string {
		if len(items) == 0 {
			return "[]"
		}
		var b strings.Builder
		b.WriteString("[")
		for i, item := range items {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(quoted(item))
		}
		b.WriteString("]")
		return b.String()
	}
	values := map[string]string{
		"name":         quoted(config.Name),
		"description":  quoted(config.Description),
		"category":     quoted(config.Category),
		"keywords":     strArray(config.Keywords),
		"author":       quoted(config.Author),
		"email":        quoted(config.Email),
		"organization": quoted(config.Organization),
		"repository":   quoted(config.Repository),
		"homepage":     quoted(config.Homepage),
		"license":      quoted(config.License),
	}

	for _, f := range pkg.RequiredManifestFields() {
		switch f.Key {
		case "compiler":
			sb.WriteString("  compiler: {\n")
			for _, sub := range pkg.CompilerManifestFields() {
				// 可選欄位不寫進新清單，讓 compiler 自己用內建預設。
				if pkg.IsNonRequiredManifestField("compiler." + sub.Key) {
					continue
				}
				if sub.Key == "version" {
					sb.WriteString("    version: " + quoted(config.Compiler.Version) + "\n")
				}
			}
			sb.WriteString("  }\n")
		case "dependencies":
			sb.WriteString(formatDependencies(config.Dependencies))
		default:
			sb.WriteString("  " + nomlFormatKeyLocal(f.Key) + ": " + values[f.Key] + "\n")
		}
	}
	sb.WriteString("}\n")
	return sb.String()
}

func nomlFormatKeyLocal(k string) string {
	if pkg.IsNomlIdentifierKey(k) {
		return k
	}
	return pkg.NomlQuote(k)
}

// formatDependencies renders the dependencies member as a noml string array
// of 'key:version' elements (sorted for deterministic output).
func formatDependencies(deps map[string]string) string {
	if len(deps) == 0 {
		return "  dependencies: []\n"
	}
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("  dependencies: [\n")
	for _, k := range keys {
		sb.WriteString("    ")
		sb.WriteString(pkg.NomlQuote(pkg.FormatDepSpec(k, deps[k])))
		sb.WriteString(",\n") // 尾隨逗號：陣列元素一律帶逗號
	}
	sb.WriteString("  ]\n")
	return sb.String()
}
