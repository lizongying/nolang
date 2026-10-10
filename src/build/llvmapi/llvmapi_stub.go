//go:build !llvm_capi

package llvmapi

// 默認構建（make no）：純 Go、不引入 cgo、不鏈接任何 LLVM 開發庫。
// 這樣 wasip1 交叉構建（playground 的 no.wasm / lsp.wasm）與 CI 都不受影響。

// available 恆為 false：本二進位不含 LLVM C API 後端。
const available = false

// Available 報告本二進位是否帶有 LLVM C API 後端。默認構建下為 false。
func Available() bool { return available }

// CompileToObject 在默認構建下不可用：呼叫方應據 ErrNotCompiledIn 回退到外部 clang 路徑。
func CompileToObject(opts Options, objPath string) error {
	return ErrNotCompiledIn
}
