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
		%a = alloca [3 x i64]
		call void @llvm.lifetime.start.p0i8(i64 64, i8* %a)
		store [3 x i64] [i64 1, i64 2, i64 3], [3 x i64]* %a
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.0, i64 0, i64 0), i8* getelementptr inbounds ([14 x i8], [14 x i8]* @.str.0, i64 0, i64 0))
		call void @llvm.lifetime.end.p0i8(i64 64, i8* %a)
		ret i32 0
}


; Format string constants
@.str.0 = private unnamed_addr constant [14 x i8] c"array test ok\00"
@.pfmt.0 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
