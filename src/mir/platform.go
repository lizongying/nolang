package mir

import (
	"runtime"
	"sync/atomic"

	"github.com/lizongying/nolang/hir"
	nopkg "github.com/lizongying/nolang/package"
)

// mirPlatform is a (goos, goarch) pair set by the driver to declare the
// compilation target. Fields mirror the runtime.GOOS/GOARCH vocabulary so the
// same string comparisons work for host and target alike.
type mirPlatform struct{ goos, goarch string }

// mirTarget holds the -target platform. nil = not set → lower for the host.
var mirTarget atomic.Pointer[mirPlatform]

// SetTargetPlatform declares the (GOOS, GOARCH) pair the MIR backend compiles
// for. The build driver calls it from Transpiler.SetTargetPlatform with the
// values parsed from `-target <triple>`. Empty strings reset it to the host
// fallback (native builds, and any target the triple parser could not fully
// resolve), which is also what an unset target means.
//
// Before this existed the MIR backend lowered EVERYTHING for the host:
// nodeMatchesPlatform picked platform annotation variants by runtime.GOOS, and
// the C shims in builtin_call.go / forward_call.go hardcoded host symbols
// (glibc's __errno_location, Linux's stdin). Cross-compiling from a Linux
// runner to darwin then linked a macOS binary against ___errno_location and
// _stdin — symbols that only exist in glibc — and the link died on undefined
// symbols even though the #{mac-*}/#{linux-*} annotations were ALSO filtered
// by the wrong platform.
func SetTargetPlatform(goos, goarch string) {
	if goos == "" || goarch == "" {
		mirTarget.Store(nil)
		return
	}
	mirTarget.Store(&mirPlatform{goos: goos, goarch: goarch})
}

// mirTargetPlatform returns the (goos, goarch) pair the MIR backend lowers
// for: the -target platform when SetTargetPlatform declared one, the host
// otherwise.
func mirTargetPlatform() (string, string) {
	if p := mirTarget.Load(); p != nil {
		return p.goos, p.goarch
	}
	return runtime.GOOS, runtime.GOARCH
}

// targetGOOS / targetGOARCH are the single source of truth for every
// platform-dependent decision in lowering and codegen: annotation filtering,
// libc symbol names (__errno_location vs __error, stdin vs __stdinp),
// struct layouts, errno/fcntl constants. They must be used INSTEAD OF
// runtime.GOOS/GOARCH so a -target build emits code for the TARGET, not the
// machine running the compiler.
func targetGOOS() string {
	goos, _ := mirTargetPlatform()
	return goos
}

func targetGOARCH() string {
	_, goarch := mirTargetPlatform()
	return goarch
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
// SAME parameter list. build.mangleOverloads must therefore leave these
// platform alternatives under their source name; LowerHIR filters the HIR
// nodes before registering the target body. Otherwise the alternatives either
// collapse onto one symbol or bare calls lose the qualified module name.
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
