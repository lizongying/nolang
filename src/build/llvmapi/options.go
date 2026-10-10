package llvmapi

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// ─── 開關：非 debug 模式改用 LLVM C API 進程內編譯 ──────────────────────────────
//
// 約定沿用專案既有的三級開關範式（見 docs/design/nolang-native-backend.md DR-6）：
//
//	CLI flag  →  環境變數  →  單一讀取器（本檔案的 Enabled）
//
// 默認關閉：未開啟時後端管線與今日逐字相同（寫 .ll → spawn clang -x ir）。

// EnvSwitch 是控制本後端的環境變數名。
const EnvSwitch = "NOLANG_LLVM_CAPI"

var warnOnce sync.Once

// Enabled 報告本次構建是否要用 LLVM C API 路徑。
// 值域（大小寫與空白皆不敏感）：
//   - 未設定 / "" / "0" / "off" / "false" / "no" → 關閉（默認）
//   - "1" / "on" / "true" / "yes"               → 開啟
//   - 其他未知拼寫 → 一律按關閉處理（並在 stderr 提示一次）
//     （與 mir.MIROptLevel 的容錯口徑一致，避免拼錯就悄悄換後端）
func Enabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvSwitch)))
	switch v {
	case "", "0", "off", "false", "no":
		return false
	case "1", "on", "true", "yes":
		return true
	}
	warnOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "[warn] unknown %s=%q, treating as off (use 1/on/true/yes to enable)\n", EnvSwitch, v)
	})
	return false
}

// SetEnabled 供命令層（--llvm-capi）寫回環境變數，使 build / run / test 共用同一開關。
func SetEnabled(on bool) {
	if on {
		os.Setenv(EnvSwitch, "1")
	} else {
		os.Setenv(EnvSwitch, "0")
	}
}

// ParseOptLevel 把 "-O0".."-O3" 形式的旗標轉成 0..3。
// 空值或非預期拼寫一律回 2（與 builder.go 的 NOLANG_OPT_LEVEL 默認 -O2 對齊）。
func ParseOptLevel(flag string) int {
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case "-o0", "0", "o0":
		return 0
	case "-o1", "1", "o1":
		return 1
	case "-o3", "3", "o3":
		return 3
	case "-os", "s", "os":
		return 2
	case "-oz", "z", "oz":
		return 2
	}
	return 2
}
