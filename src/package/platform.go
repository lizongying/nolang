package pkg

// PlatformKeys maps flattened platform annotation keys to (GOOS, GOARCH) pairs.
// 供 llvm 代碼生成的平台過濾與 checker 的 #{platform=...} 註解校驗共用，
// 兩者必須使用同一張表，避免鍵名漂移。
var PlatformKeys = map[string]struct{ GOOS, GOARCH string }{
	"linux-amd64": {"linux", "amd64"},
	"linux-arm64": {"linux", "arm64"},
	"win-amd64":   {"windows", "amd64"},
	"win-arm64":   {"windows", "arm64"},
	"mac-amd64":   {"darwin", "amd64"},
	"mac-arm64":   {"darwin", "arm64"},
	"wasi-wasm32": {"wasi", "wasm32"},
	"js":          {"js", ""},
}

// PlatformKeyFor performs a reverse lookup on PlatformKeys: given a (goos, goarch)
// pair, it returns the corresponding platform annotation key (e.g. ("darwin","arm64")
// → "mac-arm64"). Returns "" if no key matches.
func PlatformKeyFor(goos, goarch string) string {
	for key, matcher := range PlatformKeys {
		if matcher.GOOS == goos && matcher.GOARCH == goarch {
			return key
		}
	}
	return ""
}
