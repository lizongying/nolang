; ModuleID = 'nolang'
source_filename = "nolang"
target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

declare i32 @printf(i8*, ...)
declare double @llvm.fabs.f64(double)
declare double @llvm.sqrt.f64(double)
declare double @llvm.sin.f64(double)
declare double @llvm.cos.f64(double)
declare double @llvm.pow.f64(double, double)
declare double @llvm.ceil.f64(double)
declare double @llvm.floor.f64(double)
declare double @llvm.round.f64(double)
declare double @llvm.trunc.f64(double)
declare double @llvm.exp.f64(double)
declare double @llvm.log.f64(double)
declare double @llvm.log10.f64(double)
declare double @llvm.log2.f64(double)
declare double @llvm.atan.f64(double)
declare double @llvm.atan2.f64(double, double)
declare double @llvm.maxnum.f64(double, double)
declare double @llvm.minnum.f64(double, double)
declare double @llvm.asin.f64(double)
declare double @llvm.acos.f64(double)
declare double @llvm.sinh.f64(double)
declare double @llvm.cosh.f64(double)
declare double @llvm.tanh.f64(double)
declare double @fmod(double, double)
declare double @hypot(double, double)
declare double @cbrt(double)
declare i32 @atoi(i8*)
declare i64 @strtoull(i8*, i8**, i32)
declare double @strtod(i8*, i8**)
declare i32 @sprintf(i8*, i8*, ...)
declare i32 @strcmp(i8*, i8*)
declare i8* @getenv(i8*)
declare i32 @setenv(i8*, i8*, i32)
declare i8* @getcwd(i8*, i64)
declare i32 @chdir(i8*)
declare void @exit(i32)
declare i32 @getpid()
declare i32 @gethostname(i8*, i64)
declare i32 @mkdir(i8*, i32)
declare i32 @unlink(i8*)
declare i32 @rename(i8*, i8*)
declare i32 @stat(i8*, i8*)
declare i64 @time(i8*)
declare i32 @sleep(i32)
declare i32 @open(i8*, i32, i32)
declare i64 @read(i32, i8*, i64)
declare i64 @write(i32, i8*, i64)
declare i32 @close(i32)
declare void @llvm.lifetime.start.p0i8(i64, i8* nocapture)
declare void @llvm.lifetime.end.p0i8(i64, i8* nocapture)

@.strconv_buf = private global [64 x i8] zeroinitializer
@.os_buf = private global [1024 x i8] zeroinitializer
@.str.true = private unnamed_addr constant [5 x i8] c"true\00"
@.str.false = private unnamed_addr constant [6 x i8] c"false\00"

define i32 @main() {
	entry:
		%i = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %i)
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.0, i64 0, i64 0), i8* getelementptr inbounds ([14 x i8], [14 x i8]* @.str.0, i64 0, i64 0))
		%stridx.2 = add i64 0, 0
		store i64 %stridx.2, i64* %i
		%strptr.3 = add i64 0, 0
		br label %str.cond.1
str.cond.1:
			%stri.4 = load i64, i64* %i
			%strcmp.5 = icmp slt i64 %stri.4, 3
			br i1 %strcmp.5, label %str.body.1, label %str.end.1
str.body.1:
			%strch.6 = getelementptr inbounds [4 x i8], [4 x i8]* @.str.1, i64 0, i64 %stri.4
			%strchz.7 = load i8, i8* %strch.6
			%strcv.8 = zext i8 %strchz.7 to i64
			store i64 %strcv.8, i64* %i
			%i.val.9 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.1, i64 0, i64 0), i64 %i.val.9)
			%strnext.10 = add i64 %stri.4, 1
			store i64 %strnext.10, i64* %i
			br label %str.cond.1
str.end.1:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.2, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.2, i64 0, i64 0))
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.3, i64 0, i64 0), i8* getelementptr inbounds ([11 x i8], [11 x i8]* @.str.3, i64 0, i64 0))
		%rng.init.12 = add i64 1, 0
		store i64 %rng.init.12, i64* %i
		br label %rng.cond.11
rng.cond.11:
			%rng.i.15 = load i64, i64* %i
			%rng.asc.16 = icmp sle i64 %rng.i.15, 5
			%rng.desc.17 = icmp sge i64 %rng.i.15, 5
			%rng.cmp.13 = icmp sle i64 1, 5
			%rng.sel.14 = select i1 %rng.cmp.13, i1 %rng.asc.16, i1 %rng.desc.17
			br i1 %rng.sel.14, label %rng.body.11, label %rng.end.11
rng.body.11:
			%i.val.18 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.4, i64 0, i64 0), i64 %i.val.18)
			%rng.up.19 = add i64 %rng.i.15, 1
			%rng.down.20 = sub i64 %rng.i.15, 1
			%rng.next.21 = select i1 %rng.cmp.13, i64 %rng.up.19, i64 %rng.down.20
			store i64 %rng.next.21, i64* %i
			br label %rng.cond.11
rng.end.11:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.5, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.4, i64 0, i64 0))
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %i)
		ret i32 0
}


; Format string constants
@.pfmt.0 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.0 = private unnamed_addr constant [14 x i8] c"string range:\00"
@.str.1 = private unnamed_addr constant [4 x i8] c"abc\00"
@.pfmt.1 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.2 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.2 = private unnamed_addr constant [4 x i8] c"---\00"
@.pfmt.3 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.3 = private unnamed_addr constant [11 x i8] c"int range:\00"
@.pfmt.4 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.5 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.4 = private unnamed_addr constant [4 x i8] c"---\00"
