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
