package llvmapi

import (
	"errors"
	"os"
	"testing"
)

// 默認關閉是本改動的核心不變量：開關未開時後端管線必須與今日逐字相同。
func TestEnabledDefaultOff(t *testing.T) {
	for _, v := range []string{"", "0", "off", "false", "no", "OFF", "  off  "} {
		t.Setenv(EnvSwitch, v)
		if Enabled() {
			t.Fatalf("Enabled() = true for %s=%q, want false (default off)", EnvSwitch, v)
		}
	}
}

func TestEnabledOn(t *testing.T) {
	for _, v := range []string{"1", "on", "true", "yes", "ON", "  1  "} {
		t.Setenv(EnvSwitch, v)
		if !Enabled() {
			t.Fatalf("Enabled() = false for %s=%q, want true", EnvSwitch, v)
		}
	}
}

// 未知拼寫一律按 off，避免拼錯就悄悄換後端（與 mir.MIROptLevel 的容錯口徑一致）。
func TestEnabledUnknownSpellingTreatedAsOff(t *testing.T) {
	for _, v := range []string{"2", "maybe", "enable", "llvm"} {
		t.Setenv(EnvSwitch, v)
		if Enabled() {
			t.Fatalf("Enabled() = true for unknown %s=%q, want false", EnvSwitch, v)
		}
	}
}

// 內置 LLVM 的發布變體（make no-llvm）用 -X 注入 defaultOn=1：
// 未設環境變數時默認開，但顯式 NOLANG_LLVM_CAPI=0 仍要能關回 clang 路徑。
func TestEnabledBuildDefaultOn(t *testing.T) {
	old := defaultOn
	defaultOn = "1"
	t.Cleanup(func() { defaultOn = old })

	prev, had := os.LookupEnv(EnvSwitch)
	os.Unsetenv(EnvSwitch)
	if !Enabled() {
		t.Fatal("defaultOn=1 with env unset: Enabled() = false, want true")
	}
	t.Setenv(EnvSwitch, "0")
	if Enabled() {
		t.Fatalf("defaultOn=1 with %s=0: Enabled() = true, want false (explicit override wins)", EnvSwitch)
	}
	if had {
		t.Setenv(EnvSwitch, prev)
	} else {
		os.Unsetenv(EnvSwitch)
	}
}

func TestSetEnabledRoundTrip(t *testing.T) {
	SetEnabled(true)
	if !Enabled() {
		t.Fatal("SetEnabled(true) then Enabled() = false")
	}
	SetEnabled(false)
	if Enabled() {
		t.Fatal("SetEnabled(false) then Enabled() = true")
	}
}

func TestParseOptLevel(t *testing.T) {
	cases := map[string]int{
		"-O0": 0, "0": 0, "-O1": 1, "-O2": 2, "": 2, "garbage": 2, "-O3": 3, "3": 3,
	}
	for in, want := range cases {
		if got := ParseOptLevel(in); got != want {
			t.Fatalf("ParseOptLevel(%q) = %d, want %d", in, got, want)
		}
	}
}

// 默認構建（無 llvm_capi tag）必須是純 Go：不含 LLVM C API 後端，且 CompileToObject
// 回 ErrNotCompiledIn 讓呼叫方回退到 clang 路徑——而不是把構建判失敗。
func TestDefaultBuildHasNoCAPIBackend(t *testing.T) {
	if Available() {
		t.Skip("built with -tags llvm_capi: real backend present")
	}
	err := CompileToObject(Options{IR: "define i32 @main() { ret i32 0 }"}, "/tmp/nolang-capi-should-not-exist.o")
	if !errors.Is(err, ErrNotCompiledIn) {
		t.Fatalf("CompileToObject() error = %v, want ErrNotCompiledIn", err)
	}
}
