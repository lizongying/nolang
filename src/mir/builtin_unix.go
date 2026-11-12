package mir

import (
	"fmt"

	"github.com/lizongying/nolang/i18n"
)

// Unix-domain-socket builtins (`unix-listen` / `unix-dial`).
//
// Background: both builtins are registered in src/builtin/net.go
// (ForwardFunc "unix-listen" / "unix-dial"), but the hand-written lowering
// lived in the legacy LLVM backend (src/build/llvm/call_stdlib.go) and was
// deleted together with it in c7febdb1 ("test(llvm): remove obsolete LLVM IR
// code generation tests"). Nothing replaced it, so EVERY `net/unix` entry
// point failed with `unsupported builtin unix-listen [2kqboupo]` — the whole
// module was uncallable (and therefore untestable) for months.
//
// They cannot go through the generic C-call table (forward_call.go) because
// neither maps to a single libc function: both must build a `struct
// sockaddr_un` on the stack first. Hence bespoke emitters here, modelled on
// emitBuiltinNetListen / emitBuiltinNetDial.
//
// struct sockaddr_un layout (offset of `sun_path` is 2 on BOTH platforms,
// which is the only reason one code path can serve both):
//
//	linux:  sa_family_t sun_family (2) ; char sun_path[108]   -> 110 bytes
//	darwin: uint8_t sun_len (1) ; sa_family_t sun_family (1) ; char sun_path[104]
//
// On darwin sun_len must hold the whole address length, and the family is a
// single byte at offset 1 — the same quirk emitBuiltinNetListen handles for
// sockaddr_in.
const (
	// afUnix is AF_UNIX on both linux and darwin.
	afUnix = 1
	// unixAddrBuf is one byte more than the largest sockaddr_un (linux, 110):
	// keeps the stack slot aligned-ish and lets darwin's 106-byte form fit.
	unixAddrBuf = 112
	// unixMaxPath is the longest path that still leaves a NUL inside sun_path
	// on darwin (104 - 1). std's UNIX-MAX-PATH is 103, so this is not a
	// narrowing for well-behaved callers.
	unixMaxPath = 103
)

// emitUnixSockaddr materialises a zeroed `struct sockaddr_un` for `pathPtr` and
// returns
//
//	addrp   — i8* pointing at the struct,
//	addrLen — i64 register holding the byte length to pass to bind/connect,
//	tooLong — i1 register that is true when the path does not fit sun_path,
//
// The caller is responsible for branching on tooLong BEFORE doing anything
// with addrp (a truncated path must not be unlinked/bound), and for freeing
// pathPtr with @nolang_free once the syscalls are done.
func (c *codegen) emitUnixSockaddr(pathPtr string) (addrp, addrLen, tooLong string) {
	darwin := targetGOOS() == "darwin"

	// strlen is already declared globally by codegen.go — re-declaring it here
	// makes clang reject the module with "invalid redefinition of function".
	plen := c.treg("ux.plen")
	c.sb.WriteString(fmt.Sprintf("  %s = call i64 @strlen(i8* %s)\n", plen, pathPtr))

	tooLong = c.treg("ux.toolong")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp ugt i64 %s, %d\n", tooLong, plen, unixMaxPath))

	addr := c.treg("ux.addr")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca [%d x i8]\n", addr, unixAddrBuf))
	addrp = c.treg("ux.addrp")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds [%d x i8], [%d x i8]* %s, i64 0, i64 0\n",
		addrp, unixAddrBuf, unixAddrBuf, addr))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memset.p0i8.i64(i8* %s, i8 0, i64 %d, i1 0)\n", addrp, unixAddrBuf))

	// sun_path starts at offset 2 on both platforms.
	pathDst := c.treg("ux.pathp")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 2\n", pathDst, addrp))
	c.sb.WriteString(fmt.Sprintf("  call void @llvm.memcpy.p0i8.p0i8.i64(i8* %s, i8* %s, i64 %s, i1 0)\n",
		pathDst, pathPtr, plen))

	// sun_family: byte 0 on linux, byte 1 on darwin (byte 0 is sun_len there).
	famOff := int64(0)
	if darwin {
		famOff = 1
	}
	famp := c.treg("ux.famp")
	c.sb.WriteString(fmt.Sprintf("  %s = getelementptr inbounds i8, i8* %s, i64 %d\n", famp, addrp, famOff))
	c.sb.WriteString(fmt.Sprintf("  store i8 %d, i8* %s\n", afUnix, famp))

	// addrlen = 2 (family prefix) + strlen + 1 (NUL), which the zeroed buffer
	// already provides.
	addrLen = c.treg("ux.alen")
	c.sb.WriteString(fmt.Sprintf("  %s = add i64 %s, 3\n", addrLen, plen))
	if darwin {
		len8 := c.treg("ux.len8")
		c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i8\n", len8, addrLen))
		c.sb.WriteString(fmt.Sprintf("  store i8 %s, i8* %s\n", len8, addrp))
	}
	return addrp, addrLen, tooLong
}

// emitBuiltinUnixListen lowers `unix.unix-listen(path)` -> fd: unlink any stale
// socket file, then socket(AF_UNIX, SOCK_STREAM, 0) + setsockopt(SO_REUSEADDR)
// + bind + listen(128). Returns -1 (and never touches the filesystem) when the
// path does not fit sun_path or any syscall fails.
func (c *codegen) emitBuiltinUnixListen(inst *Inst) error {
	if len(inst.Args) < 1 {
		return i18n.Errorf("ux1lstn0", "unix-listen: needs (path)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return i18n.Errorf("ux1lstn1", "unix-listen: no result slot")
	}
	pathPtr := c.cstrOf(inst.Args[0])
	if pathPtr == "" {
		return i18n.Errorf("ux1lstn2", "unix-listen: cannot marshal path as C string")
	}

	c.decl("declare i32 @socket(i32, i32, i32)")
	c.decl("declare i32 @setsockopt(i32, i32, i32, i8*, i32)")
	c.decl("declare i32 @bind(i32, i8*, i32)")
	c.decl("declare i32 @listen(i32, i32)")
	c.decl("declare i32 @unlink(i8*)")

	solSocket := int32(1) // linux SOL_SOCKET
	if targetGOOS() == "darwin" {
		solSocket = 65535
	}

	addrp, addrLen, tooLong := c.emitUnixSockaddr(pathPtr)
	aLen32 := c.treg("ux.alen32")
	c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i32\n", aLen32, addrLen))

	failBlk := c.label("uxl.fail")
	setupBlk := c.label("uxl.setup")
	doneBlk := c.label("uxl.done")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", tooLong, failBlk, setupBlk))

	c.sb.WriteString(setupBlk + ":\n")
	// A stale socket file makes bind() fail with EADDRINUSE forever; the std
	// contract documents the unlink (see src/std/net/unix.no).
	c.sb.WriteString(fmt.Sprintf("  call i32 @unlink(i8* %s)\n", pathPtr))

	sock := c.treg("uxl.sock")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @socket(i32 %d, i32 1, i32 0)\n", sock, afUnix))

	reuse := c.treg("uxl.reuse")
	c.sb.WriteString(fmt.Sprintf("  %s = alloca i32\n", reuse))
	c.sb.WriteString(fmt.Sprintf("  store i32 1, i32* %s\n", reuse))
	reuseP := c.treg("uxl.reusep")
	c.sb.WriteString(fmt.Sprintf("  %s = bitcast i32* %s to i8*\n", reuseP, reuse))
	c.sb.WriteString(fmt.Sprintf("  call i32 @setsockopt(i32 %s, i32 %d, i32 4, i8* %s, i32 4)\n",
		sock, solSocket, reuseP))

	bindRet := c.treg("uxl.bind")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @bind(i32 %s, i8* %s, i32 %s)\n", bindRet, sock, addrp, aLen32))
	listenRet := c.treg("uxl.listen")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @listen(i32 %s, i32 128)\n", listenRet, sock))

	sockOk := c.treg("uxl.sok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", sockOk, sock))
	sock64 := c.treg("uxl.s64")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", sock64, sock))
	s1 := c.treg("uxl.s1")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", s1, sockOk, sock64))
	bindOk := c.treg("uxl.bok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", bindOk, bindRet))
	s2 := c.treg("uxl.s2")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", s2, bindOk, s1))
	listenOk := c.treg("uxl.lok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", listenOk, listenRet))
	fd := c.treg("uxl.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", fd, listenOk, s2))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", doneBlk))

	c.sb.WriteString(failBlk + ":\n")
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", doneBlk))

	c.sb.WriteString(doneBlk + ":\n")
	res := c.treg("uxl.res")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i64 [ %s, %%%s ], [ -1, %%%s ]\n", res, fd, setupBlk, failBlk))
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", res, dstSlot))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", pathPtr))
	return nil
}

// emitBuiltinUnixDial lowers `unix.unix-dial(path)` -> fd: socket(AF_UNIX,
// SOCK_STREAM, 0) + connect. Returns -1 when the path does not fit sun_path or
// connect fails; the socket is closed again on the failure path so a retry loop
// cannot leak descriptors (net-dial leaks its socket; this one does not).
func (c *codegen) emitBuiltinUnixDial(inst *Inst) error {
	if len(inst.Args) < 1 {
		return i18n.Errorf("ux1dial0", "unix-dial: needs (path)")
	}
	dstSlot := c.valSlot[inst.Dst]
	if dstSlot == "" {
		return i18n.Errorf("ux1dial1", "unix-dial: no result slot")
	}
	pathPtr := c.cstrOf(inst.Args[0])
	if pathPtr == "" {
		return i18n.Errorf("ux1dial2", "unix-dial: cannot marshal path as C string")
	}

	c.decl("declare i32 @socket(i32, i32, i32)")
	c.decl("declare i32 @connect(i32, i8*, i32)")
	c.decl("declare i32 @close(i32)")

	addrp, addrLen, tooLong := c.emitUnixSockaddr(pathPtr)
	aLen32 := c.treg("ux.alen32")
	c.sb.WriteString(fmt.Sprintf("  %s = trunc i64 %s to i32\n", aLen32, addrLen))

	failBlk := c.label("uxd.fail")
	setupBlk := c.label("uxd.setup")
	doneBlk := c.label("uxd.done")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", tooLong, failBlk, setupBlk))

	c.sb.WriteString(setupBlk + ":\n")
	sock := c.treg("uxd.sock")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @socket(i32 %d, i32 1, i32 0)\n", sock, afUnix))
	conn := c.treg("uxd.conn")
	c.sb.WriteString(fmt.Sprintf("  %s = call i32 @connect(i32 %s, i8* %s, i32 %s)\n", conn, sock, addrp, aLen32))

	sockOk := c.treg("uxd.sok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", sockOk, sock))
	sock64 := c.treg("uxd.s64")
	c.sb.WriteString(fmt.Sprintf("  %s = sext i32 %s to i64\n", sock64, sock))
	s1 := c.treg("uxd.s1")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", s1, sockOk, sock64))
	connOk := c.treg("uxd.cok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", connOk, conn))
	fd := c.treg("uxd.fd")
	c.sb.WriteString(fmt.Sprintf("  %s = select i1 %s, i64 %s, i64 -1\n", fd, connOk, s1))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", doneBlk))

	c.sb.WriteString(failBlk + ":\n")
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", doneBlk))

	c.sb.WriteString(doneBlk + ":\n")
	// Both phis must merge the same two predecessors: the failure block never
	// defined `sock`, so it needs its own phi (-1) rather than a dangling use.
	sockPhi := c.treg("uxd.sockphi")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i32 [ %s, %%%s ], [ -1, %%%s ]\n", sockPhi, sock, setupBlk, failBlk))
	res := c.treg("uxd.res")
	c.sb.WriteString(fmt.Sprintf("  %s = phi i64 [ %s, %%%s ], [ -1, %%%s ]\n", res, fd, setupBlk, failBlk))

	// Close the socket when the connect failed (sock >= 0 && fd < 0).
	fdOk := c.treg("uxd.fdok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i64 %s, 0\n", fdOk, res))
	keepBlk := c.label("uxd.keep")
	relBlk := c.label("uxd.release")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", fdOk, keepBlk, relBlk))

	c.sb.WriteString(relBlk + ":\n")
	// Second guard: close(-1) is only EBADF, but a conditional call keeps the
	// listing honest and avoids a pointless syscall when socket() itself failed.
	relSockOk := c.treg("uxd.rok")
	c.sb.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, 0\n", relSockOk, sockPhi))
	closeBlk := c.label("uxd.close")
	skipBlk := c.label("uxd.skip")
	c.sb.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", relSockOk, closeBlk, skipBlk))

	c.sb.WriteString(closeBlk + ":\n")
	c.sb.WriteString(fmt.Sprintf("  call i32 @close(i32 %s)\n", sockPhi))
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", skipBlk))

	c.sb.WriteString(keepBlk + ":\n")
	c.sb.WriteString(fmt.Sprintf("  br label %%%s\n", skipBlk))

	c.sb.WriteString(skipBlk + ":\n")
	c.sb.WriteString(fmt.Sprintf("  store i64 %s, i64* %s\n", res, dstSlot))
	c.sb.WriteString(fmt.Sprintf("  call void @nolang_free(i8* %s)\n", pathPtr))
	return nil
}
