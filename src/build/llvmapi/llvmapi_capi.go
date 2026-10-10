//go:build llvm_capi && cgo

package llvmapi

/*
#include <stdlib.h>
#include <llvm-c/Analysis.h>
#include <llvm-c/Core.h>
#include <llvm-c/Error.h>
#include <llvm-c/IRReader.h>
#include <llvm-c/Target.h>
#include <llvm-c/TargetMachine.h>
#include <llvm-c/Transforms/PassBuilder.h>
*/
import "C"

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

// 本檔案只在 `-tags llvm_capi` 且 CGO_ENABLED=1 時參與構建（見 Makefile 的 no-capi）。
// 構建參數取自 llvm-config：
//
//	CGO_CFLAGS  = $(llvm-config --cflags)
//	CGO_LDFLAGS = $(llvm-config --ldflags --libs --link-shared)
const available = true

// Available 報告本二進位是否帶有 LLVM C API 後端。
func Available() bool { return available }

var (
	initOnce sync.Once
	// compileMu 序列化所有進程內編譯：LLVM 的 target 註冊表與部分全局狀態不是並發
	// 安全的，而 `no build` 在 workspace 模式下會並行構建多個專案。
	compileMu sync.Mutex
)

func ensureInit() {
	initOnce.Do(func() {
		C.LLVMInitializeAllTargetInfos()
		C.LLVMInitializeAllTargets()
		C.LLVMInitializeAllTargetMCs()
		C.LLVMInitializeAllAsmPrinters()
	})
}

// fnAttrIndex 是 LLVMAttributeFunctionIndex（-1）的位元等價寫法。
// C 頭檔把它定義成 enum { LLVMAttributeFunctionIndex = -1 }，Go 端無法把 -1 這個
// 常數直接轉成無符號的 LLVMAttributeIndex，故用 ~0U 表示「函式本身」這個索引。
const fnAttrIndex = C.LLVMAttributeIndex(0xFFFFFFFF)

// CompileToObject 以 LLVM C API 在進程內把 IR 文本編譯成目標檔 objPath。
// 流程對齊 llc 的默認行為：parse → verify → 新 PassManager（default<O{n}>）→
// TargetMachine 寫出 .o。後續連結仍交由外部 cc 完成（LLVM C API 不提供 lld）。
func CompileToObject(opts Options, objPath string) error {
	if strings.TrimSpace(opts.IR) == "" {
		return fmt.Errorf("LLVM C API: empty IR")
	}
	compileMu.Lock()
	defer compileMu.Unlock()
	ensureInit()

	ctx := C.LLVMContextCreate()
	defer C.LLVMContextDispose(ctx)

	nameC := C.CString("<nolang-ir>")
	defer C.free(unsafe.Pointer(nameC))
	irC := C.CString(opts.IR)
	defer C.free(unsafe.Pointer(irC))
	// 注意：LLVMParseIRInContext 無論成功或失敗都會接管 buf 的所有權，不要再 Dispose。
	buf := C.LLVMCreateMemoryBufferWithMemoryRange(irC, C.size_t(len(opts.IR)), nameC, C.LLVMBool(1))
	if buf == nil {
		return fmt.Errorf("LLVM C API: creating memory buffer for IR failed")
	}

	var mod C.LLVMModuleRef
	var msg *C.char
	if C.LLVMParseIRInContext(ctx, buf, &mod, &msg) != 0 {
		return capiError("parsing LLVM IR", msg)
	}
	defer C.LLVMDisposeModule(mod)

	if C.LLVMVerifyModule(mod, C.LLVMReturnStatusAction, &msg) != 0 {
		return capiError("verifying LLVM IR", msg)
	}

	// module 自帶的 triple 由 codegen 寫出（宿主構建時即真實宿主 triple），
	// 交給 pickTriple 與請求的目標一起裁決。
	triple := pickTriple(strings.TrimSpace(opts.Triple), C.GoString(C.LLVMGetTarget(mod)))
	tripleC := C.CString(triple)
	defer C.free(unsafe.Pointer(tripleC))

	var target C.LLVMTargetRef
	if C.LLVMGetTargetFromTriple(tripleC, &target, &msg) != 0 {
		return capiError("resolving target for triple "+triple, msg)
	}

	// CPU 留空 = 用該 target 的默認 CPU（與 llc 未指定 -mcpu 時的行為一致）；
	// 顯式帶入宿主 CPU 會讓同一份 IR 在不同機器上產出不同程式碼，非我們要的。
	cpuC := C.CString("")
	defer C.free(unsafe.Pointer(cpuC))
	featC := C.CString("")
	defer C.free(unsafe.Pointer(featC))

	tm := C.LLVMCreateTargetMachine(target, tripleC, cpuC, featC, codegenLevel(opts.OptLevel), C.LLVMRelocDefault, C.LLVMCodeModelDefault)
	if tm == nil {
		return fmt.Errorf("LLVM C API: creating target machine for triple %q failed", triple)
	}
	defer C.LLVMDisposeTargetMachine(tm)

	// llc 會以 target machine 的 triple / data layout 覆蓋 module 自身的設定，這裡照做，
	// 避免 IR 裡寫死的（可能是假的）triple 與實際目標不一致。
	C.LLVMSetTarget(mod, tripleC)
	if dl := C.LLVMCreateTargetDataLayout(tm); dl != nil {
		if dlStr := C.LLVMCopyStringRepOfTargetData(dl); dlStr != nil {
			C.LLVMSetDataLayout(mod, dlStr)
			C.LLVMDisposeMessage(dlStr)
		}
		C.LLVMDisposeTargetData(dl)
	}

	if opts.FPContract == "off" {
		applyFPContractAttr(ctx, mod, "off")
	}

	pbOpts := C.LLVMCreatePassBuilderOptions()
	defer C.LLVMDisposePassBuilderOptions(pbOpts)
	passes := fmt.Sprintf("default<O%d>", clampOptLevel(opts.OptLevel))
	passesC := C.CString(passes)
	defer C.free(unsafe.Pointer(passesC))
	if errRef := C.LLVMRunPasses(mod, passesC, tm, pbOpts); errRef != nil {
		// LLVMGetErrorMessage 會一併消費掉 error 本身，故只需釋放訊息字串，
		// 不能再呼叫 LLVMConsumeError / LLVMDisposeError（error 已失效）。
		errMsg := C.LLVMGetErrorMessage(errRef)
		what := C.GoString(errMsg)
		C.LLVMDisposeErrorMessage(errMsg)
		return fmt.Errorf("LLVM C API: running passes %q: %s", passes, what)
	}

	objC := C.CString(objPath)
	defer C.free(unsafe.Pointer(objC))
	if C.LLVMTargetMachineEmitToFile(tm, mod, objC, C.LLVMObjectFile, &msg) != 0 {
		return capiError("emitting object file "+objPath, msg)
	}
	return nil
}

// applyFPContractAttr 給模組內每個函式加上 "fp-contract"="..." 字串屬性。
// 這是 clang -ffp-contract=off 的對齊手段（LLVM C API 無法直接設 TargetOptions）；
// 若當前 LLVM 版本不認這個屬性，它只是被忽略，不影響正確性。
func applyFPContractAttr(ctx C.LLVMContextRef, mod C.LLVMModuleRef, val string) {
	key := "fp-contract"
	keyC := C.CString(key)
	defer C.free(unsafe.Pointer(keyC))
	valC := C.CString(val)
	defer C.free(unsafe.Pointer(valC))
	attr := C.LLVMCreateStringAttribute(ctx, keyC, C.unsigned(len(key)), valC, C.unsigned(len(val)))
	if attr == nil {
		return
	}
	for fn := C.LLVMGetFirstFunction(mod); fn != nil; fn = C.LLVMGetNextFunction(fn) {
		C.LLVMAddAttributeAtIndex(fn, fnAttrIndex, attr)
	}
}

// pickTriple 裁決最終用於 codegen 的 triple：
//
//   - 未指定目標（req == ""）→ 用 module 自帶的 triple（IR 作者的意圖），
//     若 module 也沒有才退回 LLVM 的默認（宿主）triple。
//   - 指定目標且與 module 指向同一個目標（同 arch + 同 vendor/OS 家族，即宿主構建）
//     → 沿用 module 的 triple。原因：請求的 triple 常是不帶 OS 版本的
//     "arm64-apple-darwin"，而缺版本的 Apple triple 會讓 LLVM 寫不出
//     LC_BUILD_VERSION，連結器只能「假設」目標平台
//     （ld: warning: no platform load command ... assuming: macOS）；
//     若用 LLVM 默認宿主版本又可能比 SDK 新，變成
//     "built for newer 'macOS' version ... than being linked"。
//     module 的 triple 帶版本且與宿主一致，兩頭都不會踩。
//   - 指定目標與 module 不同（交叉構建，如 wasm32-wasi / x86_64-linux-gnu）
//     → 以指定目標為準；Apple 目標仍補上宿主版本號（同上的 LC_BUILD_VERSION 理由）。
func pickTriple(req, mod string) string {
	req = strings.TrimSpace(req)
	mod = strings.TrimSpace(mod)
	if req == "" {
		if mod != "" {
			return mod
		}
		return defaultTriple()
	}
	if mod != "" && sameTarget(req, mod) && osVersionOf(osComponent(mod)) != "" {
		return mod
	}
	if !strings.Contains(req, "apple-") {
		return req
	}
	parts := strings.Split(req, "-")
	if len(parts) < 3 || osVersionOf(parts[2]) != "" {
		return req
	}
	ver := osVersionOf(osComponent(defaultTriple()))
	if ver == "" {
		return req
	}
	parts[2] += ver
	return strings.Join(parts, "-")
}

// sameTarget 判斷兩個 triple 是否指向同一個目標（arch 相同且 OS 家族相同）。
// Apple 的 darwin / macosx / macos 視為同一家族（只有版本寫法不同）。
func sameTarget(a, b string) bool {
	pa, pb := strings.Split(a, "-"), strings.Split(b, "-")
	if len(pa) < 3 || len(pb) < 3 {
		return false
	}
	if normArch(pa[0]) != normArch(pb[0]) {
		return false
	}
	return osFamily(pa[2]) == osFamily(pb[2])
}

// normArch 統一架構別名：DetectTarget() 用的是 LLVM 拼寫（aarch64 / x86_64），
// 而 IR 裡的 module triple 可能是 Go/Apple 拼寫（arm64 / amd64）。
func normArch(arch string) string {
	switch strings.ToLower(arch) {
	case "aarch64", "arm64":
		return "arm64"
	case "x86_64", "amd64":
		return "x86_64"
	}
	return strings.ToLower(arch)
}

func osFamily(osPart string) string {
	switch {
	case strings.HasPrefix(osPart, "darwin"):
		return "darwin"
	case strings.HasPrefix(osPart, "macos"):
		return "darwin"
	}
	return osPart
}

// osComponent 取 triple 的第三段（OS 組件，可能含版本）。
func osComponent(triple string) string {
	parts := strings.Split(triple, "-")
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// defaultTriple 返回 LLVM 的默認（宿主）triple，如 "arm64-apple-darwin25.0.0"。
func defaultTriple() string {
	t := C.LLVMGetDefaultTargetTriple()
	if t == nil {
		return ""
	}
	s := C.GoString(t)
	C.LLVMDisposeMessage(t)
	return s
}

// osVersionOf 取得 OS 組件裡的數字後綴（"darwin25.0.0" → "25.0.0"），無版本則為空。
func osVersionOf(osPart string) string {
	for i := 0; i < len(osPart); i++ {
		if osPart[i] >= '0' && osPart[i] <= '9' {
			return osPart[i:]
		}
	}
	return ""
}

func codegenLevel(n int) C.LLVMCodeGenOptLevel {
	switch {
	case n <= 0:
		return C.LLVMCodeGenLevelNone
	case n == 1:
		return C.LLVMCodeGenLevelLess
	case n >= 3:
		return C.LLVMCodeGenLevelAggressive
	default:
		return C.LLVMCodeGenLevelDefault
	}
}

func clampOptLevel(n int) int {
	if n < 0 {
		return 0
	}
	if n > 3 {
		return 3
	}
	return n
}

// capiError 把 LLVM C API 的 char* 錯誤訊息轉成 Go error（並釋放訊息）。
func capiError(what string, msg *C.char) error {
	if msg == nil {
		return fmt.Errorf("LLVM C API: %s failed", what)
	}
	m := C.GoString(msg)
	C.LLVMDisposeMessage(msg)
	return fmt.Errorf("LLVM C API: %s failed: %s", what, m)
}
