package checker

import (
	"strings"

	"github.com/lizongying/nolang/i18n"
)

// namingMessage 回報命名違規的診斷文字。能推出合法的 kebab-case 寫法時，
// 附上「可以寫成 X」的具體建議（canvasEl -> canvas-el）；推不出安全建議時
// （例如以數字開頭、含非 ASCII 字元）退回原本不帶建議的 2dhoris2 文案。
//
// 建議只是同一條診斷的補充說明：呼叫端的 TraceID 一律維持 2dhoris2，
// 既有測試、lint 分級與工具鏈不需跟著改。
func namingMessage(name string) string {
	if suggestion := kebabSuggestion(name); suggestion != "" {
		return i18n.T("2dhoris3", "'%s' should use only lowercase letters and hyphens; write '%s' instead", name, suggestion)
	}
	return i18n.T("2dhoris2", "'%s' should use only lowercase letters and hyphens", name)
}

// kebabSuggestion 把違反命名規範的識別符轉成「小寫字母 + 數字 + 連字號」寫法。
// 回空字串代表給不出建議，呼叫端應退回不帶建議的文案。
//
// 轉換規則（逐段處理，點分名保留點號）：
//   - 大寫轉小寫，並在 camel/Pascal 邊界補連字號：canvasEl -> canvas-el、
//     MyData -> my-data；縮寫詞後接小寫時也斷開：HTTPServer -> http-server。
//   - 底線與既有連字號折成單一連字號：tmp__1 -> tmp-1。
//   - 其餘字元（非 ASCII、`[]` 等）無法定義安全的改寫，直接放棄建議。
//   - 結果仍須過 isValidVarName，且要與原名不同；`1foo` 這類「改了也還是
//     違規」的名字不給誤導性建議。
func kebabSuggestion(name string) string {
	if name == "" {
		return ""
	}
	segs := strings.Split(name, ".")
	changed := false
	for i, seg := range segs {
		out, ok := kebabSegment(seg)
		if !ok || !isValidVarName(out) {
			return ""
		}
		if out != seg {
			changed = true
		}
		segs[i] = out
	}
	if !changed {
		return ""
	}
	return strings.Join(segs, ".")
}

// kebabSegment 轉換單一段；ok=false 代表遇到無法改寫的字元。
func kebabSegment(seg string) (string, bool) {
	var b strings.Builder
	var last byte
	for i := 0; i < len(seg); i++ {
		ch := seg[i]
		switch {
		case ch >= 'A' && ch <= 'Z':
			var prev, next byte
			if i > 0 {
				prev = seg[i-1]
			}
			if i+1 < len(seg) {
				next = seg[i+1]
			}
			// 邊界：前面是小寫/數字（camelCase），或大寫縮寫詞後接小寫
			// （HTTPServer 的 S）。
			boundary := b.Len() > 0 && last != '-' &&
				((prev >= 'a' && prev <= 'z') ||
					(prev >= '0' && prev <= '9') ||
					(prev >= 'A' && prev <= 'Z' && next >= 'a' && next <= 'z'))
			if boundary {
				b.WriteByte('-')
				last = '-'
			}
			last = ch + ('a' - 'A')
			b.WriteByte(last)
		case (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9'):
			last = ch
			b.WriteByte(ch)
		case ch == '_' || ch == '-':
			if b.Len() > 0 && last != '-' {
				b.WriteByte('-')
				last = '-'
			}
		default:
			return "", false
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "", false
	}
	return out, true
}
