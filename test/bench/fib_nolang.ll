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

define void @fib(i64* %n, i64* %o) {
	entry:
		%i = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %i)
		%c = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %c)
		%a = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %a)
		%b = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %b)
		store i64 0, i64* %a
		store i64 1, i64* %b
		store i64 2, i64* %i
		br label %for.cond.1
for.cond.1:
			%i.val.2 = load i64, i64* %i
			%n.val.3 = load i64, i64* %n
			%cmp.i1.4 = icmp sle i64 %i.val.2, %n.val.3
			br i1 %cmp.i1.4, label %for.body.1, label %for.end.1
for.body.1:
			%a.val.5 = load i64, i64* %a
			%b.val.6 = load i64, i64* %b
			%add.tmp.7 = add i64 %a.val.5, %b.val.6
			store i64 %add.tmp.7, i64* %c
			%b.val.8 = load i64, i64* %b
			store i64 %b.val.8, i64* %a
			%c.val.9 = load i64, i64* %c
			store i64 %c.val.9, i64* %b
			%i.val.10 = load i64, i64* %i
			%add.tmp.11 = add i64 %i.val.10, 1
			store i64 %add.tmp.11, i64* %i
			br label %for.cond.1
for.end.1:
		%b.val.12 = load i64, i64* %b
		store i64 %b.val.12, i64* %o
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %i)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %c)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %a)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %b)
		ret void
}

define i32 @main() {
	entry:
		%b = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %b)
		%i = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %i)
		%c = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %c)
		%o = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %o)
		%iter = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %iter)
		%result = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %result)
		%a = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %a)
		store i64 0, i64* %iter
		br label %for.cond.13
for.cond.13:
			%iter.val.14 = load i64, i64* %iter
			%cmp.i1.15 = icmp slt i64 %iter.val.14, 10000000
			br i1 %cmp.i1.15, label %for.body.13, label %for.end.13
for.body.13:
			store i64 0, i64* %result
			%result.val.16 = load i64, i64* %result
			%ref.tmp.17 = alloca i64
			store i64 40, i64* %ref.tmp.17
			call void @fib(i64* %ref.tmp.17, i64* %result)
			%result.val.18 = load i64, i64* %result
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.0, i64 0, i64 0), i64 %result.val.18)
			%iter.val.19 = load i64, i64* %iter
			%inc.ld.20 = load i64, i64* %iter
			%inc.21 = add i64 %inc.ld.20, 1
			store i64 %inc.21, i64* %iter
			br label %for.cond.13
for.end.13:
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %b)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %i)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %c)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %o)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %iter)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %result)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %a)
		ret i32 0
}


; Format string constants
@.pfmt.0 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
