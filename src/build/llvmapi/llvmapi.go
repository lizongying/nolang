// Package llvmapi 封裝「以 LLVM C API 在進程內把 LLVM IR 編譯成目標檔（.o）」的路徑。
//
// 動機：非 debug 模式（未加 -v）目前是「把 .ll 寫盤 → spawn `clang -x ir`，由 clang
// 在進程內完成 optimize + codegen + link」。改成進程內呼叫 LLVM C API 可省掉一次
// 進程 spawn 與 .ll 的落盤/重解析，也是後續 nocg/nold 原生後端的過渡臺階。
//
// 約束：專案默認構建必須保持純 Go（無 cgo），否則 wasip1 交叉構建（playground 的
// no.wasm / lsp.wasm）與 CI 都會被 LLVM 開發庫綁死。因此本套件拆成兩個互斥檔案：
//
//	llvmapi_stub.go  //go:build !llvm_capi   → Available() == false（默認構建）
//	llvmapi_capi.go  //go:build llvm_capi    → cgo + LLVM C API 真身
//
// 也就是說：環境開關開啟、但二進位不是以 `-tags llvm_capi` 構建時，Available() 為
// false，呼叫方應回退到既有的 clang 後端（見 build.buildLLVMInternal）。
package llvmapi

import "errors"

// ErrNotCompiledIn 表示本二進位未包含 LLVM C API 後端（缺少 llvm_capi build tag）。
// 呼叫方應據此回退到外部 clang 路徑，而不是把構建判失敗。
var ErrNotCompiledIn = errors.New("LLVM C API backend not compiled into this binary (rebuild with: make no-capi)")

// Options 描述一次「IR 文本 → 目標檔」的進程內編譯。
type Options struct {
	// IR 是完整的 LLVM IR 文本（.ll 的內容），非空時由 C API 直接從內存解析。
	IR string
	// Triple 是目標 triple；空字串表示沿用 LLVM 的默認（宿主）triple。
	Triple string
	// OptLevel 是 0..3，對應 -O0..-O3。
	OptLevel int
	// FPContract 為 "off" 時盡力關閉浮點收縮（對齊 clang -ffp-contract=off）；
	// "fast" 或空字串表示不干預、走 LLVM 默認。
	FPContract string
}

// CompileToObject(opts, objPath) 與 Available() 由下列兩個互斥檔案之一提供：
//
//	llvmapi_stub.go（默認構建）  → Available() == false，CompileToObject 回 ErrNotCompiledIn
//	llvmapi_capi.go（llvm_capi） → cgo 真身
