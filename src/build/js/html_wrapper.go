package js

import "strings"

// htmlTemplate 是瀏覽器模式下的 HTML wrapper 模板。
// {{TITLE}} 為頁面標題，{{JS_FILE}} 為引用的 JS 檔名。
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{TITLE}}</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
            margin: 16px;
            color: #333;
        }
        h1 {
            font-size: 1.5em;
            margin-bottom: 16px;
        }
        #nolang-output {
            border: 1px solid #ddd;
            padding: 12px;
            min-height: 120px;
            white-space: pre-wrap;
            font-family: "SF Mono", Monaco, Consolas, monospace;
            font-size: 14px;
            background: #fafafa;
            border-radius: 4px;
        }
        #nolang-output div {
            padding: 2px 0;
        }
        canvas {
            border: 1px solid #ddd;
            display: block;
            margin: 8px 0;
        }
        button {
            padding: 8px 16px;
            margin: 8px 8px 8px 0;
            cursor: pointer;
        }
    </style>
</head>
<body>
    <h1>{{TITLE}}</h1>
    <div id="nolang-output"></div>
    <script src="{{JS_FILE}}"></script>
</body>
</html>`

// RenderHTML 生成包含輸出區的 HTML wrapper，引用同名的 .js 檔案。
// title 為頁面標題，jsFileName 為引用的 JS 檔名（僅檔名，不含路徑）。
func RenderHTML(title string, jsFileName string) string {
	html := htmlTemplate
	html = strings.ReplaceAll(html, "{{TITLE}}", title)
	html = strings.ReplaceAll(html, "{{JS_FILE}}", jsFileName)
	return html
}

// DefaultESMCDN is the CDN base used for browser import maps when the package
// manifest (compiler.esm-cdn) does not override it.
const DefaultESMCDN = "https://esm.sh"

// RenderHTMLModule is RenderHTML for ESM output: the JS is referenced as a
// module script and bare package specifiers are resolved through an import
// map (one exact key per specifier, plus a trailing-slash key for subpaths).
// Relative specifiers are left out of the map — they resolve without it.
// cdnBase empty means DefaultESMCDN.
func RenderHTMLModule(title string, jsFileName string, imports []ESMImport, cdnBase string) string {
	if cdnBase == "" {
		cdnBase = DefaultESMCDN
	}
	cdnBase = strings.TrimRight(cdnBase, "/")
	html := htmlTemplate
	html = strings.ReplaceAll(html, "{{TITLE}}", title)
	// Module script + import map (must precede the first module script).
	// Relative specifiers are skipped (see below); when that leaves no map key
	// at all the import map is omitted entirely.
	var b strings.Builder
	bares := make([]ESMImport, 0, len(imports))
	for _, imp := range imports {
		// A relative specifier resolves against the importing module /
		// document URL and is never remapped by an import map, so emitting a
		// CDN entry for it ("https://esm.sh/./x.mjs") would be dead and
		// misleading. Bare npm specifiers are the only map keys.
		if IsRelativeSpecifier(imp.Specifier) {
			continue
		}
		bares = append(bares, imp)
	}
	if len(bares) > 0 {
		b.WriteString("    <script type=\"importmap\">\n")
		b.WriteString("    {\"imports\": {")
		for i, imp := range bares {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(jsLiteral(imp.Specifier) + ": " + jsLiteral(cdnBase+"/"+imp.Specifier))
			if strings.Contains(imp.Specifier, "/") {
				b.WriteString(", " + jsLiteral(imp.Specifier+"/") + ": " + jsLiteral(cdnBase+"/"+imp.Specifier+"/"))
			}
		}
		b.WriteString("}}\n")
		b.WriteString("    </script>\n")
	}
	b.WriteString("    <script type=\"module\" src=\"" + jsFileName + "\"></script>\n")
	html = strings.ReplaceAll(html, "    <script src=\"{{JS_FILE}}\"></script>", b.String())
	html = strings.ReplaceAll(html, "{{JS_FILE}}", jsFileName)
	return html
}
