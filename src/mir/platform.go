package mir

import (
	"runtime"

	"github.com/lizongying/nolang/hir"
	nopkg "github.com/lizongying/nolang/package"
)

// mirTargetPlatform returns the (goos, goarch) pair the MIR backend lowers for.
//
// The MIR pipeline has no target-platform plumbing of its own: it lowers for
// the HOST, exactly like the runtime shims in builtin_call.go which branch on
// runtime.GOOS. Cross-compilation under NOLANG_MIR=3 is therefore unsupported
// (the C prelude and the syscall shims are host-specific too), so the host
// triple is the honest answer here rather than a silently wrong one.
func mirTargetPlatform() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}

// nodeMatchesPlatform reports whether a HIR node carrying platform annotations
// (#{mac-arm64}, #{win-amd64}, ...) participates in the current compilation.
//
// Why the MIR backend needs its own copy of this check: the platform filter
// (build/llvm.filterByPlatformG) runs at LLVM-emission time over the
// RECONSTRUCTED AST. The HIR handed to LowerHIR still contains EVERY platform
// variant of a declaration, and the name tables keep whichever variant is
// visited last.
//
// `process.cmd` is the case that made this visible: it has a POSIX definition
// (#{mac-*, linux-*, wasi-wasm32}) and a Win32 definition (#{win-*}) with the
// SAME parameter list, so build.mangleOverloads collapses both onto one symbol,
// `process.cmd_str_slice.str_str_str_slice.str_i64_bool`. funcNames kept the
// Win32 body, and calling it bailed out with "unsupported builtin
// win-create-pipe" instead of running the POSIX implementation.
//
// A node with no platform annotation is always kept, mirroring
// build/llvm.matchesPlatform.
func nodeMatchesPlatform(pk *hir.Package, id int32) bool {
	if pk == nil || id == hir.NoID {
		return true
	}
	goos, goarch := mirTargetPlatform()
	keys, hasValue := pk.AnnotationKeys(id)
	hasPlatform := false
	for i, k := range keys {
		// Only a bare boolean entry can be a platform annotation.
		if hasValue[i] {
			continue
		}
		matcher, ok := nopkg.PlatformKeys[k]
		if !ok {
			continue
		}
		hasPlatform = true
		if matcher.GOOS == goos && matcher.GOARCH == goarch {
			return true
		}
	}
	return !hasPlatform
}
