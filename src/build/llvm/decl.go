package llvm

import (
	"runtime"
	"strings"
)

func (g *Generator) writeDeclarations(sb *strings.Builder) {
	sb.WriteString("declare i32 @printf(i8*, ...)\n")
	sb.WriteString("declare double @llvm.fabs.f64(double)\n")
	sb.WriteString("declare double @llvm.sqrt.f64(double)\n")
	sb.WriteString("declare double @llvm.sin.f64(double)\n")
	sb.WriteString("declare double @llvm.cos.f64(double)\n")
	sb.WriteString("declare double @llvm.pow.f64(double, double)\n")
	sb.WriteString("declare double @llvm.ceil.f64(double)\n")
	sb.WriteString("declare double @llvm.floor.f64(double)\n")
	sb.WriteString("declare double @llvm.round.f64(double)\n")
	sb.WriteString("declare double @llvm.trunc.f64(double)\n")
	sb.WriteString("declare double @llvm.exp.f64(double)\n")
	sb.WriteString("declare double @llvm.log.f64(double)\n")
	sb.WriteString("declare double @llvm.log10.f64(double)\n")
	sb.WriteString("declare double @llvm.log2.f64(double)\n")
	sb.WriteString("declare double @llvm.atan.f64(double)\n")
	sb.WriteString("declare double @llvm.atan2.f64(double, double)\n")
	sb.WriteString("declare double @llvm.maxnum.f64(double, double)\n")
	sb.WriteString("declare double @llvm.minnum.f64(double, double)\n")
	sb.WriteString("declare double @llvm.asin.f64(double)\n")
	sb.WriteString("declare double @llvm.acos.f64(double)\n")
	sb.WriteString("declare double @llvm.sinh.f64(double)\n")
	sb.WriteString("declare double @llvm.cosh.f64(double)\n")
	sb.WriteString("declare double @llvm.tanh.f64(double)\n")
	// strtod/strtoll replaced by internal @nolang.strtod/@nolang.strtoll
	// (FFI ffi-cstr-at-float/ffi-cstr-at-int use these internal implementations).
	sb.WriteString("declare i32 @sprintf(i8*, i8*, ...)\n")
	sb.WriteString("declare i8* @malloc(i64)\n")
	sb.WriteString("declare void @free(i8*)\n")
	sb.WriteString("declare void @llvm.memcpy.p0i8.p0i8.i64(i8* nocapture writeonly, i8* nocapture readonly, i64, i1 immarg)\n")
	sb.WriteString("declare void @llvm.memset.p0i8.i64(i8* nocapture writeonly, i8, i64, i1 immarg)\n")
	sb.WriteString("declare i8* @getenv(i8*)\n")
	sb.WriteString("declare i32 @setenv(i8*, i8*, i32)\n")
	sb.WriteString("declare i8* @getcwd(i8*, i64)\n")
	sb.WriteString("declare i32 @chdir(i8*)\n")
	sb.WriteString("declare void @exit(i32)\n")
	sb.WriteString("declare i32 @getpid()\n")
	sb.WriteString("declare i32 @gethostname(i8*, i64)\n")
	sb.WriteString("declare i32 @mkdir(i8*, i32)\n")
	sb.WriteString("declare i32 @chmod(i8*, i32)\n")
	sb.WriteString("declare i32 @chown(i8*, i32, i32)\n")
	sb.WriteString("declare i32 @getuid()\n")
	sb.WriteString("declare i32 @getgid()\n")
	sb.WriteString("declare i32 @unlink(i8*)\n")
	sb.WriteString("declare i32 @rename(i8*, i8*)\n")
	sb.WriteString("declare i32 @stat(i8*, i8*)\n")
	// Directory operations (from <dirent.h>)
	sb.WriteString("declare i8* @opendir(i8*)\n")
	sb.WriteString("declare i8* @readdir(i8*)\n")
	sb.WriteString("declare i32 @closedir(i8*)\n")
	// File timestamp update (from <sys/stat.h>)
	sb.WriteString("declare i32 @utimensat(i32, i8*, i8*, i32)\n")
	// libc @time 已移除：now 內建改用內部 @nolang.now_s（gettimeofday）
	// libc @sleep 已移除：sleep 內建改用內部 @nolang.sleep_s（nanosleep）
	sb.WriteString("declare i32 @open(i8*, i32, ...)\n")
	sb.WriteString("declare i32 @gettimeofday(i8*, i8*)\n")
	sb.WriteString("declare i32 @usleep(i32)\n")
	sb.WriteString("declare i32 @nanosleep(i8*, i8*)\n")
	sb.WriteString("declare i32 @clock_gettime(i32, i8*)\n")
	// errno access: macOS uses __error(), Linux uses __errno_location()
	if runtime.GOOS == "darwin" {
		sb.WriteString("declare i32* @__error()\n")
	} else {
		sb.WriteString("declare i32* @__errno_location()\n")
	}
	sb.WriteString("declare i32 @dup2(i32, i32)\n")
	sb.WriteString("declare i32 @kill(i32, i32)\n")
	sb.WriteString("declare i32 @getppid()\n")
	sb.WriteString("declare i32 @system(i8*)\n")
	sb.WriteString("declare i32 @execlp(i8*, ...)\n")
	sb.WriteString("declare i32 @fork()\n")
	sb.WriteString("declare i32 @pipe(i32*)\n")
	sb.WriteString("declare i32 @waitpid(i32, i32*, i32)\n")
	// zlib @compress2 / @uncompress 已移除：archive/gzip.no 以純 Nolang 實現
	// gzip-compress / gzip-decompress，不再使用 zlib 高階 API。

	// sincos: provide an implementation for platforms where libm does not
	// export sincos (e.g. macOS). llc's DAG combiner merges sin(x)+cos(x)
	// with the same argument into a single sincos call; without this
	// definition the linker fails with "undefined symbol: _sincos".
	// Marked optnone+noinline to prevent llc from re-combining sin/cos
	// inside this very function into another sincos call (infinite recursion).
	sb.WriteString("define void @sincos(double %x, double* %sin_out, double* %cos_out) #0 {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%s = call double @llvm.sin.f64(double %x)\n")
	sb.WriteString("\t%c = call double @llvm.cos.f64(double %x)\n")
	sb.WriteString("\tstore double %s, double* %sin_out\n")
	sb.WriteString("\tstore double %c, double* %cos_out\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n")
	sb.WriteString("attributes #0 = { optnone noinline }\n\n")

	// nolang.strlen: runtime C-string length (replaces libc @strlen).
	// Loops over i8* until null terminator. Used when converting C strings
	// from libc functions (getenv, readdir, fgets, etc.) to %str-long.
	// Handles NULL input by returning 0 (avoids UB from libc strlen(NULL)).
	sb.WriteString("define internal i64 @nolang.strlen(i8* %s) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%null.cond = icmp eq i8* %s, null\n")
	sb.WriteString("\tbr i1 %null.cond, label %done, label %loop\n")
	sb.WriteString("loop:\n")
	sb.WriteString("\t%i = phi i64 [0, %entry], [%i.next, %loop]\n")
	sb.WriteString("\t%ptr = getelementptr i8, i8* %s, i64 %i\n")
	sb.WriteString("\t%c = load i8, i8* %ptr\n")
	sb.WriteString("\t%cond = icmp eq i8 %c, 0\n")
	sb.WriteString("\t%i.next = add i64 %i, 1\n")
	sb.WriteString("\tbr i1 %cond, label %done, label %loop\n")
	sb.WriteString("done:\n")
	sb.WriteString("\t%len = phi i64 [0, %entry], [%i, %loop]\n")
	sb.WriteString("\tret i64 %len\n")
	sb.WriteString("}\n\n")

	// nolang.strtoll: parse C string as base-10 i64 (replaces libc @strtoll).
	// Used by FFI ffi-cstr-at-int to parse C string arrays.
	// Skips leading whitespace, handles optional sign, reads decimal digits.
	sb.WriteString("define internal i64 @nolang.strtoll(i8* %s) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%ptr.addr = alloca i8*\n")
	sb.WriteString("\tstore i8* %s, i8** %ptr.addr\n")
	sb.WriteString("\t%neg.flag = alloca i1\n")
	sb.WriteString("\tstore i1 false, i1* %neg.flag\n")
	sb.WriteString("\t%acc = alloca i64\n")
	sb.WriteString("\tstore i64 0, i64* %acc\n")
	sb.WriteString("\tbr label %skip.ws\n")
	// skip whitespace
	sb.WriteString("skip.ws:\n")
	sb.WriteString("\t%p = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%c = load i8, i8* %p\n")
	sb.WriteString("\t%is.sp = icmp eq i8 %c, 32\n")
	sb.WriteString("\t%is.ht = icmp eq i8 %c, 9\n")
	sb.WriteString("\t%is.lf = icmp eq i8 %c, 10\n")
	sb.WriteString("\t%is.cr = icmp eq i8 %c, 13\n")
	sb.WriteString("\t%a1 = or i1 %is.sp, %is.ht\n")
	sb.WriteString("\t%a2 = or i1 %a1, %is.lf\n")
	sb.WriteString("\t%a3 = or i1 %a2, %is.cr\n")
	sb.WriteString("\tbr i1 %a3, label %ws.next, label %check.sign\n")
	sb.WriteString("ws.next:\n")
	sb.WriteString("\t%p.next = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.next, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %skip.ws\n")
	// check sign
	sb.WriteString("check.sign:\n")
	sb.WriteString("\t%is.minus = icmp eq i8 %c, 45\n")
	sb.WriteString("\tbr i1 %is.minus, label %set.neg, label %check.plus\n")
	sb.WriteString("set.neg:\n")
	sb.WriteString("\tstore i1 true, i1* %neg.flag\n")
	sb.WriteString("\t%p.am = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.am, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %digit.loop\n")
	sb.WriteString("check.plus:\n")
	sb.WriteString("\t%is.plus = icmp eq i8 %c, 43\n")
	sb.WriteString("\tbr i1 %is.plus, label %skip.plus, label %digit.loop\n")
	sb.WriteString("skip.plus:\n")
	sb.WriteString("\t%p.ap = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.ap, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %digit.loop\n")
	// digit loop
	sb.WriteString("digit.loop:\n")
	sb.WriteString("\t%p2 = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%c2 = load i8, i8* %p2\n")
	sb.WriteString("\t%ge.0 = icmp sge i8 %c2, 48\n")
	sb.WriteString("\t%le.9 = icmp sle i8 %c2, 57\n")
	sb.WriteString("\t%is.dig = and i1 %ge.0, %le.9\n")
	sb.WriteString("\tbr i1 %is.dig, label %dig.proc, label %done\n")
	sb.WriteString("dig.proc:\n")
	sb.WriteString("\t%cur = load i64, i64* %acc\n")
	sb.WriteString("\t%mul10 = mul i64 %cur, 10\n")
	sb.WriteString("\t%dv = sext i8 %c2 to i64\n")
	sb.WriteString("\t%dv.sub = sub i64 %dv, 48\n")
	sb.WriteString("\t%new.acc = add i64 %mul10, %dv.sub\n")
	sb.WriteString("\tstore i64 %new.acc, i64* %acc\n")
	sb.WriteString("\t%p3 = getelementptr i8, i8* %p2, i64 1\n")
	sb.WriteString("\tstore i8* %p3, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %digit.loop\n")
	sb.WriteString("done:\n")
	sb.WriteString("\t%final = load i64, i64* %acc\n")
	sb.WriteString("\t%is.neg = load i1, i1* %neg.flag\n")
	sb.WriteString("\tbr i1 %is.neg, label %negate, label %ret.pos\n")
	sb.WriteString("negate:\n")
	sb.WriteString("\t%negated = sub i64 0, %final\n")
	sb.WriteString("\tret i64 %negated\n")
	sb.WriteString("ret.pos:\n")
	sb.WriteString("\tret i64 %final\n")
	sb.WriteString("}\n\n")

	// nolang.strtod: parse C string as f64 (replaces libc @strtod).
	// Used by FFI ffi-cstr-at-float to parse C string arrays.
	// Handles: optional sign, integer digits, optional fractional part,
	// optional exponent (e/E + sign + digits).
	// Uses @llvm.pow.f64 for exponentiation.
	sb.WriteString("define internal double @nolang.strtod(i8* %s) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%ptr.addr = alloca i8*\n")
	sb.WriteString("\tstore i8* %s, i8** %ptr.addr\n")
	sb.WriteString("\t%neg.flag = alloca i1\n")
	sb.WriteString("\tstore i1 false, i1* %neg.flag\n")
	sb.WriteString("\t%acc = alloca double\n")
	sb.WriteString("\tstore double 0.0, double* %acc\n")
	sb.WriteString("\t%frac.div = alloca double\n")
	sb.WriteString("\tstore double 1.0, double* %frac.div\n")
	sb.WriteString("\t%has.dot = alloca i1\n")
	sb.WriteString("\tstore i1 false, i1* %has.dot\n")
	sb.WriteString("\tbr label %skip.ws\n")
	// skip whitespace
	sb.WriteString("skip.ws:\n")
	sb.WriteString("\t%p = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%c = load i8, i8* %p\n")
	sb.WriteString("\t%is.sp = icmp eq i8 %c, 32\n")
	sb.WriteString("\t%is.ht = icmp eq i8 %c, 9\n")
	sb.WriteString("\t%is.lf = icmp eq i8 %c, 10\n")
	sb.WriteString("\t%is.cr = icmp eq i8 %c, 13\n")
	sb.WriteString("\t%a1 = or i1 %is.sp, %is.ht\n")
	sb.WriteString("\t%a2 = or i1 %a1, %is.lf\n")
	sb.WriteString("\t%a3 = or i1 %a2, %is.cr\n")
	sb.WriteString("\tbr i1 %a3, label %ws.next, label %check.sign\n")
	sb.WriteString("ws.next:\n")
	sb.WriteString("\t%p.next = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.next, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %skip.ws\n")
	// check sign
	sb.WriteString("check.sign:\n")
	sb.WriteString("\t%is.minus = icmp eq i8 %c, 45\n")
	sb.WriteString("\tbr i1 %is.minus, label %set.neg, label %check.plus\n")
	sb.WriteString("set.neg:\n")
	sb.WriteString("\tstore i1 true, i1* %neg.flag\n")
	sb.WriteString("\t%p.am = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.am, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %main.loop\n")
	sb.WriteString("check.plus:\n")
	sb.WriteString("\t%is.plus = icmp eq i8 %c, 43\n")
	sb.WriteString("\tbr i1 %is.plus, label %skip.plus, label %main.loop\n")
	sb.WriteString("skip.plus:\n")
	sb.WriteString("\t%p.ap = getelementptr i8, i8* %p, i64 1\n")
	sb.WriteString("\tstore i8* %p.ap, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %main.loop\n")
	// main loop: digits and dot
	sb.WriteString("main.loop:\n")
	sb.WriteString("\t%p2 = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%c2 = load i8, i8* %p2\n")
	sb.WriteString("\t%ge.0 = icmp sge i8 %c2, 48\n")
	sb.WriteString("\t%le.9 = icmp sle i8 %c2, 57\n")
	sb.WriteString("\t%is.dig = and i1 %ge.0, %le.9\n")
	sb.WriteString("\tbr i1 %is.dig, label %dig.proc, label %check.dot\n")
	sb.WriteString("dig.proc:\n")
	sb.WriteString("\t%dv = sext i8 %c2 to i64\n")
	sb.WriteString("\t%dv.sub = sub i64 %dv, 48\n")
	sb.WriteString("\t%dv.f = sitofp i64 %dv.sub to double\n")
	sb.WriteString("\t%in.frac = load i1, i1* %has.dot\n")
	sb.WriteString("\tbr i1 %in.frac, label %frac.dig, label %int.dig\n")
	sb.WriteString("int.dig:\n")
	sb.WriteString("\t%cur.i = load double, double* %acc\n")
	sb.WriteString("\t%mul10 = fmul double %cur.i, 10.0\n")
	sb.WriteString("\t%add.i = fadd double %mul10, %dv.f\n")
	sb.WriteString("\tstore double %add.i, double* %acc\n")
	sb.WriteString("\tbr label %next.char\n")
	sb.WriteString("frac.dig:\n")
	sb.WriteString("\t%cur.f = load double, double* %acc\n")
	sb.WriteString("\t%cur.div = load double, double* %frac.div\n")
	sb.WriteString("\t%new.div = fmul double %cur.div, 10.0\n")
	sb.WriteString("\tstore double %new.div, double* %frac.div\n")
	sb.WriteString("\t%divided = fdiv double %dv.f, %new.div\n")
	sb.WriteString("\t%add.f = fadd double %cur.f, %divided\n")
	sb.WriteString("\tstore double %add.f, double* %acc\n")
	sb.WriteString("\tbr label %next.char\n")
	sb.WriteString("next.char:\n")
	sb.WriteString("\t%p3 = getelementptr i8, i8* %p2, i64 1\n")
	sb.WriteString("\tstore i8* %p3, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %main.loop\n")
	// check for decimal point
	sb.WriteString("check.dot:\n")
	sb.WriteString("\t%is.dot = icmp eq i8 %c2, 46\n")
	sb.WriteString("\tbr i1 %is.dot, label %set.dot, label %check.exp\n")
	sb.WriteString("set.dot:\n")
	sb.WriteString("\tstore i1 true, i1* %has.dot\n")
	sb.WriteString("\t%p.ad = getelementptr i8, i8* %p2, i64 1\n")
	sb.WriteString("\tstore i8* %p.ad, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %main.loop\n")
	// check for exponent
	sb.WriteString("check.exp:\n")
	sb.WriteString("\t%is.e = icmp eq i8 %c2, 101\n")
	sb.WriteString("\t%is.E = icmp eq i8 %c2, 69\n")
	sb.WriteString("\t%is.exp = or i1 %is.e, %is.E\n")
	sb.WriteString("\tbr i1 %is.exp, label %exp.setup, label %done\n")
	// exponent parsing
	sb.WriteString("exp.setup:\n")
	sb.WriteString("\t%p.ae = getelementptr i8, i8* %p2, i64 1\n")
	sb.WriteString("\tstore i8* %p.ae, i8** %ptr.addr\n")
	sb.WriteString("\t%exp.neg = alloca i1\n")
	sb.WriteString("\tstore i1 false, i1* %exp.neg\n")
	sb.WriteString("\t%exp.acc = alloca i64\n")
	sb.WriteString("\tstore i64 0, i64* %exp.acc\n")
	sb.WriteString("\t%pe0 = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%ce0 = load i8, i8* %pe0\n")
	sb.WriteString("\t%exp.is.minus = icmp eq i8 %ce0, 45\n")
	sb.WriteString("\tbr i1 %exp.is.minus, label %exp.set.neg, label %exp.check.plus\n")
	sb.WriteString("exp.set.neg:\n")
	sb.WriteString("\tstore i1 true, i1* %exp.neg\n")
	sb.WriteString("\t%p.aem = getelementptr i8, i8* %pe0, i64 1\n")
	sb.WriteString("\tstore i8* %p.aem, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %exp.dig.loop\n")
	sb.WriteString("exp.check.plus:\n")
	sb.WriteString("\t%exp.is.plus = icmp eq i8 %ce0, 43\n")
	sb.WriteString("\tbr i1 %exp.is.plus, label %exp.skip.plus, label %exp.dig.loop\n")
	sb.WriteString("exp.skip.plus:\n")
	sb.WriteString("\t%p.aep = getelementptr i8, i8* %pe0, i64 1\n")
	sb.WriteString("\tstore i8* %p.aep, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %exp.dig.loop\n")
	sb.WriteString("exp.dig.loop:\n")
	sb.WriteString("\t%pe2 = load i8*, i8** %ptr.addr\n")
	sb.WriteString("\t%ce2 = load i8, i8* %pe2\n")
	sb.WriteString("\t%ege.0 = icmp sge i8 %ce2, 48\n")
	sb.WriteString("\t%ele.9 = icmp sle i8 %ce2, 57\n")
	sb.WriteString("\t%eis.dig = and i1 %ege.0, %ele.9\n")
	sb.WriteString("\tbr i1 %eis.dig, label %exp.dig.proc, label %exp.apply\n")
	sb.WriteString("exp.dig.proc:\n")
	sb.WriteString("\t%ec = load i64, i64* %exp.acc\n")
	sb.WriteString("\t%emul = mul i64 %ec, 10\n")
	sb.WriteString("\t%edv = sext i8 %ce2 to i64\n")
	sb.WriteString("\t%edv.sub = sub i64 %edv, 48\n")
	sb.WriteString("\t%enew = add i64 %emul, %edv.sub\n")
	sb.WriteString("\tstore i64 %enew, i64* %exp.acc\n")
	sb.WriteString("\t%pe3 = getelementptr i8, i8* %pe2, i64 1\n")
	sb.WriteString("\tstore i8* %pe3, i8** %ptr.addr\n")
	sb.WriteString("\tbr label %exp.dig.loop\n")
	// apply exponent: result = acc * 10^exp (or / 10^exp if negative)
	sb.WriteString("exp.apply:\n")
	sb.WriteString("\t%exp.val = load i64, i64* %exp.acc\n")
	sb.WriteString("\t%exp.f = sitofp i64 %exp.val to double\n")
	sb.WriteString("\t%pow10 = call double @llvm.pow.f64(double 10.0, double %exp.f)\n")
	sb.WriteString("\t%exp.is.neg = load i1, i1* %exp.neg\n")
	sb.WriteString("\tbr i1 %exp.is.neg, label %exp.div, label %exp.mul\n")
	sb.WriteString("exp.div:\n")
	sb.WriteString("\t%before.div = load double, double* %acc\n")
	sb.WriteString("\t%after.div = fdiv double %before.div, %pow10\n")
	sb.WriteString("\tstore double %after.div, double* %acc\n")
	sb.WriteString("\tbr label %done\n")
	sb.WriteString("exp.mul:\n")
	sb.WriteString("\t%before.mul = load double, double* %acc\n")
	sb.WriteString("\t%after.mul = fmul double %before.mul, %pow10\n")
	sb.WriteString("\tstore double %after.mul, double* %acc\n")
	sb.WriteString("\tbr label %done\n")
	// done: apply sign and return
	sb.WriteString("done:\n")
	sb.WriteString("\t%final = load double, double* %acc\n")
	sb.WriteString("\t%is.neg = load i1, i1* %neg.flag\n")
	sb.WriteString("\tbr i1 %is.neg, label %negate, label %ret.pos\n")
	sb.WriteString("negate:\n")
	sb.WriteString("\t%negated = fneg double %final\n")
	sb.WriteString("\tret double %negated\n")
	sb.WriteString("ret.pos:\n")
	sb.WriteString("\tret double %final\n")
	sb.WriteString("}\n\n")

	// nolang.now_ms: gettimeofday → sec*1000 + usec/1000
	sb.WriteString("define internal i64 @nolang.now_ms() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%tv = alloca [16 x i8]\n")
	sb.WriteString("\t%tv.ptr = bitcast [16 x i8]* %tv to i8*\n")
	sb.WriteString("\tcall i32 @gettimeofday(i8* %tv.ptr, i8* null)\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %tv to i64*\n")
	sb.WriteString("\t%sec = load i64, i64* %sec.ptr\n")
	sb.WriteString("\t%usec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\t%usec = load i64, i64* %usec.ptr\n")
	sb.WriteString("\t%sec.ms = mul i64 %sec, 1000\n")
	sb.WriteString("\t%usec.ms = sdiv i64 %usec, 1000\n")
	sb.WriteString("\t%result = add i64 %sec.ms, %usec.ms\n")
	sb.WriteString("\tret i64 %result\n")
	sb.WriteString("}\n\n")

	// nolang.now_s: gettimeofday → sec（取代 libc @time(NULL)）
	// 使用 gettimeofday 取得 struct timeval { tv_sec, tv_usec }，直接回傳 tv_sec。
	// 與 @time(NULL) 等價，但避免依賴 libc @time。
	sb.WriteString("define internal i64 @nolang.now_s() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%tv = alloca [16 x i8]\n")
	sb.WriteString("\t%tv.ptr = bitcast [16 x i8]* %tv to i8*\n")
	sb.WriteString("\tcall i32 @gettimeofday(i8* %tv.ptr, i8* null)\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %tv to i64*\n")
	sb.WriteString("\t%sec = load i64, i64* %sec.ptr\n")
	sb.WriteString("\tret i64 %sec\n")
	sb.WriteString("}\n\n")

	// nolang.now_us: gettimeofday → sec*1000000 + usec
	sb.WriteString("define internal i64 @nolang.now_us() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%tv = alloca [16 x i8]\n")
	sb.WriteString("\t%tv.ptr = bitcast [16 x i8]* %tv to i8*\n")
	sb.WriteString("\tcall i32 @gettimeofday(i8* %tv.ptr, i8* null)\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %tv to i64*\n")
	sb.WriteString("\t%sec = load i64, i64* %sec.ptr\n")
	sb.WriteString("\t%usec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\t%usec = load i64, i64* %usec.ptr\n")
	sb.WriteString("\t%sec.us = mul i64 %sec, 1000000\n")
	sb.WriteString("\t%result = add i64 %sec.us, %usec\n")
	sb.WriteString("\tret i64 %result\n")
	sb.WriteString("}\n\n")

	// nolang.now_ns: clock_gettime(CLOCK_REALTIME=0) → sec*1000000000 + nsec
	sb.WriteString("define internal i64 @nolang.now_ns() {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%ts = alloca [16 x i8]\n")
	sb.WriteString("\t%ts.ptr = bitcast [16 x i8]* %ts to i8*\n")
	sb.WriteString("\tcall i32 @clock_gettime(i32 0, i8* %ts.ptr)\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %ts to i64*\n")
	sb.WriteString("\t%sec = load i64, i64* %sec.ptr\n")
	sb.WriteString("\t%nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\t%nsec = load i64, i64* %nsec.ptr\n")
	sb.WriteString("\t%sec.ns = mul i64 %sec, 1000000000\n")
	sb.WriteString("\t%result = add i64 %sec.ns, %nsec\n")
	sb.WriteString("\tret i64 %result\n")
	sb.WriteString("}\n\n")

	// nolang.sleep_us: usleep(i32)
	sb.WriteString("define internal void @nolang.sleep_us(i64 %us) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%us.trunc = trunc i64 %us to i32\n")
	sb.WriteString("\tcall i32 @usleep(i32 %us.trunc)\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")

	// nolang.sleep_s: nanosleep(sec, 0)（取代 libc @sleep(i32)）
	// 使用 nanosleep 系統呼叫，避免依賴 libc @sleep。
	// 與 @sleep(sec) 等價，不關心剩餘時間（被中斷時不重試）。
	// 返回 i64 0（成功）以符合 CLibCall LLVMI64 返回型別；
	// Nolang time.sleep-s 丟棄返回值，固定 0 不影響行為。
	sb.WriteString("define internal i64 @nolang.sleep_s(i64 %sec) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%req = alloca [16 x i8]\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %req to i64*\n")
	sb.WriteString("\tstore i64 %sec, i64* %sec.ptr\n")
	sb.WriteString("\t%nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\tstore i64 0, i64* %nsec.ptr\n")
	sb.WriteString("\t%req.ptr = bitcast [16 x i8]* %req to i8*\n")
	sb.WriteString("\tcall i32 @nanosleep(i8* %req.ptr, i8* null)\n")
	sb.WriteString("\tret i64 0\n")
	sb.WriteString("}\n\n")

	// nolang.sleep_ns: nanosleep
	sb.WriteString("define internal void @nolang.sleep_ns(i64 %ns) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%req = alloca [16 x i8]\n")
	sb.WriteString("\t%sec = sdiv i64 %ns, 1000000000\n")
	sb.WriteString("\t%nsec = srem i64 %ns, 1000000000\n")
	sb.WriteString("\t%sec.ptr = bitcast [16 x i8]* %req to i64*\n")
	sb.WriteString("\tstore i64 %sec, i64* %sec.ptr\n")
	sb.WriteString("\t%nsec.ptr = getelementptr i64, i64* %sec.ptr, i64 1\n")
	sb.WriteString("\tstore i64 %nsec, i64* %nsec.ptr\n")
	sb.WriteString("\t%req.ptr = bitcast [16 x i8]* %req to i8*\n")
	sb.WriteString("\tcall i32 @nanosleep(i8* %req.ptr, i8* null)\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")

	sb.WriteString("declare i64 @read(i32, i8*, i64)\n")
	sb.WriteString("declare i64 @write(i32, i8*, i64)\n")
	sb.WriteString("declare i32 @close(i32)\n")
	sb.WriteString("declare i8* @fopen(i8*, i8*)\n")
	sb.WriteString("declare i8* @fgets(i8*, i32, i8*)\n")
	sb.WriteString("declare i32 @fclose(i8*)\n")
	sb.WriteString("declare void @llvm.lifetime.start.p0i8(i64, i8* nocapture)\n")
	sb.WriteString("declare void @llvm.lifetime.end.p0i8(i64, i8* nocapture)\n\n")

	sb.WriteString("@.os-buf = private global [1024 x i8] zeroinitializer\n")
	sb.WriteString("@.str.true = private unnamed_addr constant [5 x i8] c\"true\\00\"\n")
	sb.WriteString("@.str.false = private unnamed_addr constant [6 x i8] c\"false\\00\"\n")
	sb.WriteString("@.str.empty = private unnamed_addr constant [1 x i8] c\"\\00\"\n")
	sb.WriteString("@.str.r = private unnamed_addr constant [2 x i8] c\"r\\00\"\n")
	sb.WriteString("@.str.oob = private unnamed_addr constant [36 x i8] c\"runtime error: index out of bounds\\0A\\00\"\n\n")

	// Global storage for argc/argv, set by @main and read by args-count/args-get builtins.
	// These must be globals (not local allocas) because builtins are called from
	// user functions, not from @main itself.
	sb.WriteString("@.argc.addr = global i32 0\n")
	sb.WriteString("@.argv.addr = global i8** null\n\n")

	// nolang.bounds_check: runtime array/slice/string bounds check
	// If idx < 0 || idx >= len, writes error to stderr and exits.
	sb.WriteString("define internal void @nolang.bounds_check(i64 %idx, i64 %len) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\t%lo = icmp slt i64 %idx, 0\n")
	sb.WriteString("\t%hi = icmp sge i64 %idx, %len\n")
	sb.WriteString("\t%oob = or i1 %lo, %hi\n")
	sb.WriteString("\tbr i1 %oob, label %err, label %ok\n")
	sb.WriteString("err:\n")
	sb.WriteString("\tcall i64 @write(i32 2, i8* getelementptr inbounds ([36 x i8], [36 x i8]* @.str.oob, i64 0, i64 0), i64 36)\n")
	sb.WriteString("\tcall void @exit(i32 1)\n")
	sb.WriteString("\tunreachable\n")
	sb.WriteString("ok:\n")
	sb.WriteString("\tret void\n")
	sb.WriteString("}\n\n")

	// nolang.memcmp: runtime byte-by-byte comparison (replaces @llvm.memcmp intrinsic
	// which is not expanded by opt in LLVM 21). Returns -1, 0, or 1 (like C memcmp).
	sb.WriteString("define internal i32 @nolang.memcmp(i8* %a, i8* %b, i64 %n) {\n")
	sb.WriteString("entry:\n")
	sb.WriteString("\tbr label %loop\n")
	sb.WriteString("loop:\n")
	sb.WriteString("\t%i = phi i64 [0, %entry], [%i.next, %loop.cont]\n")
	sb.WriteString("\t%done = icmp eq i64 %i, %n\n")
	sb.WriteString("\tbr i1 %done, label %equal, label %body\n")
	sb.WriteString("body:\n")
	sb.WriteString("\t%a.ptr = getelementptr i8, i8* %a, i64 %i\n")
	sb.WriteString("\t%b.ptr = getelementptr i8, i8* %b, i64 %i\n")
	sb.WriteString("\t%a.byte = load i8, i8* %a.ptr\n")
	sb.WriteString("\t%b.byte = load i8, i8* %b.ptr\n")
	sb.WriteString("\t%cmp.eq = icmp eq i8 %a.byte, %b.byte\n")
	sb.WriteString("\tbr i1 %cmp.eq, label %loop.cont, label %diff\n")
	sb.WriteString("diff:\n")
	sb.WriteString("\t%lt = icmp ult i8 %a.byte, %b.byte\n")
	sb.WriteString("\t%result = select i1 %lt, i32 -1, i32 1\n")
	sb.WriteString("\tret i32 %result\n")
	sb.WriteString("loop.cont:\n")
	sb.WriteString("\t%i.next = add i64 %i, 1\n")
	sb.WriteString("\tbr label %loop\n")
	sb.WriteString("equal:\n")
	sb.WriteString("\tret i32 0\n")
	sb.WriteString("}\n\n")

	// zlib @inflateInit2_ / @inflate / @inflateEnd / @.zlib_ver / @nolang.inflate_raw
	// 已全部移除：archive/gzip.no 的 inflate-decompress 改為純 Nolang 實現
	// （RFC 1951 DEFLATE 解壓縮），不再依賴 zlib 串流 API。

	// POSIX socket functions for net module
	sb.WriteString("declare i32 @socket(i32, i32, i32)\n")
	sb.WriteString("declare i32 @setsockopt(i32, i32, i32, i8*, i32)\n")
	sb.WriteString("declare i32 @bind(i32, i8*, i32)\n")
	sb.WriteString("declare i32 @listen(i32, i32)\n")
	sb.WriteString("declare i32 @accept(i32, i8*, i32*)\n")
	sb.WriteString("declare i32 @connect(i32, i8*, i32)\n")
	sb.WriteString("declare i64 @send(i32, i8*, i64, i32)\n")
	sb.WriteString("declare i64 @recv(i32, i8*, i64, i32)\n")
	sb.WriteString("declare i64 @sendto(i32, i8*, i64, i32, i8*, i32)\n")
	sb.WriteString("declare i64 @recvfrom(i32, i8*, i64, i32, i8*, i32*)\n")
	sb.WriteString("declare i32 @inet_pton(i32, i8*, i8*)\n")
	// DNS resolution (getaddrinfo fallback for net-dial)
	// macOS addrinfo layout: ai_addr at offset 32
	sb.WriteString("declare i32 @getaddrinfo(i8*, i8*, i8*, i8**)\n")
	sb.WriteString("declare void @freeaddrinfo(i8*)\n")

	// TLS 已由純 Nolang 實現（std/net/tls.no），不需要 OpenSSL 宣告。

	// pthread declarations for async/await (run/awy)
	sb.WriteString("declare i32 @pthread_create(i64*, i8*, i8* (i8*)*, i8*)\n")
	sb.WriteString("declare i32 @pthread_join(i64, i8**)\n")
}

// StatLayout 描述各平台的 struct stat 佈局（大小與欄位位移）。
// 用於取代硬編碼的 macOS arm64 位移，使 is-dir/is-file/stat-* 等內建跨平台相容。
//
// 佈局來源：
//   macOS (arm64/amd64): <sys/stat.h> struct stat (st_mtimespec)
//   Linux x86_64:        glibc <bits/struct_stat.h> struct stat (st_mtim)
//   Linux arm64:         glibc aarch64 struct stat (st_mtim)
type StatLayout struct {
	Size      int64 // struct stat 總大小（bytes）
	ModeOff   int64 // st_mode 位移
	UidOff    int64 // st_uid 位移
	GidOff    int64 // st_gid 位移
	MtimeOff  int64 // st_mtime.tv_sec 位移
	SizeOff   int64 // st_size 位移
}

// statLayout 返回當前編譯目標平台的 struct stat 佈局。
// 若平台未知，回退到 macOS arm64 佈局（既有行為）。
func statLayout() StatLayout {
	switch runtime.GOOS {
	case "darwin":
		// macOS arm64/amd64: struct stat (st_mtimespec at offset 48)
		// dev_t st_dev@0, mode_t st_mode@4, nlink_t st_nlink@6, ino_t st_ino@8,
		// uid_t st_uid@16, gid_t st_gid@20, dev_t st_rdev@24,
		// st_atimespec@32, st_mtimespec@48, st_ctimespec@64, st_birthtimespec@80,
		// off_t st_size@96
		return StatLayout{Size: 144, ModeOff: 4, UidOff: 16, GidOff: 20, MtimeOff: 48, SizeOff: 96}
	case "linux":
		if runtime.GOARCH == "arm64" {
			// Linux arm64 (aarch64): glibc struct stat
			// dev_t st_dev@0, ino_t st_ino@8, mode_t st_mode@16, nlink_t st_nlink@20,
			// uid_t st_uid@24, gid_t st_gid@28, dev_t st_rdev@32, __pad1@40,
			// off_t st_size@48, st_atim@72, st_mtim@88, st_ctim@104
			return StatLayout{Size: 128, ModeOff: 16, UidOff: 24, GidOff: 28, MtimeOff: 88, SizeOff: 48}
		}
		// Linux x86_64/amd64: glibc struct stat
		// dev_t st_dev@0, ino_t st_ino@8, nlink_t st_nlink@16,
		// mode_t st_mode@24, uid_t st_uid@28, gid_t st_gid@32, __pad0@36,
		// dev_t st_rdev@40, off_t st_size@48, st_atim@72, st_mtim@88, st_ctim@104
		return StatLayout{Size: 144, ModeOff: 24, UidOff: 28, GidOff: 32, MtimeOff: 88, SizeOff: 48}
	default:
		// 未知平台：回退到 macOS arm64 佈局
		return StatLayout{Size: 144, ModeOff: 4, UidOff: 16, GidOff: 20, MtimeOff: 48, SizeOff: 96}
	}
}

// openWriteFlags 返回當前平台的 O_WRONLY|O_CREAT|O_TRUNC 組合值。
//   macOS: 1 | 512 | 1024 = 1537
//   Linux: 1 | 64  | 512  = 577
// 用於 open-write 與 write-file 內建，取代硬編碼的 1537。
func openWriteFlags() int {
	switch runtime.GOOS {
	case "darwin":
		return 1537 // O_WRONLY(1) | O_CREAT(0x200) | O_TRUNC(0x400)
	case "linux":
		return 577 // O_WRONLY(1) | O_CREAT(0x40) | O_TRUNC(0x200)
	default:
		return 1537 // 回退到 macOS 值
	}
}
