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
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.0, i64 0, i64 0), i8* getelementptr inbounds ([12 x i8], [12 x i8]* @.str.0, i64 0, i64 0))
		%rng.init.2 = add i64 1, 0
		store i64 %rng.init.2, i64* %i
		br label %rng.cond.1
rng.cond.1:
			%rng.i.5 = load i64, i64* %i
			%rng.asc.6 = icmp sle i64 %rng.i.5, 5
			%rng.desc.7 = icmp sge i64 %rng.i.5, 5
			%rng.cmp.3 = icmp sle i64 1, 5
			%rng.sel.4 = select i1 %rng.cmp.3, i1 %rng.asc.6, i1 %rng.desc.7
			br i1 %rng.sel.4, label %rng.body.1, label %rng.end.1
rng.body.1:
			%i.val.8 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.1, i64 0, i64 0), i64 %i.val.8)
			%rng.up.9 = add i64 %rng.i.5, 1
			%rng.down.10 = sub i64 %rng.i.5, 1
			%rng.next.11 = select i1 %rng.cmp.3, i64 %rng.up.9, i64 %rng.down.10
			store i64 %rng.next.11, i64* %i
			br label %rng.cond.1
rng.end.1:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.2, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.1, i64 0, i64 0))
		%rng.init.13 = add i64 1, 0
		store i64 %rng.init.13, i64* %i
		br label %rng.cond.12
rng.cond.12:
			%rng.i.16 = load i64, i64* %i
			%rng.asc.17 = icmp slt i64 %rng.i.16, 5
			%rng.desc.18 = icmp sgt i64 %rng.i.16, 5
			%rng.cmp.14 = icmp sle i64 1, 5
			%rng.sel.15 = select i1 %rng.cmp.14, i1 %rng.asc.17, i1 %rng.desc.18
			br i1 %rng.sel.15, label %rng.body.12, label %rng.end.12
rng.body.12:
			%i.val.19 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.3, i64 0, i64 0), i64 %i.val.19)
			%rng.up.20 = add i64 %rng.i.16, 1
			%rng.down.21 = sub i64 %rng.i.16, 1
			%rng.next.22 = select i1 %rng.cmp.14, i64 %rng.up.20, i64 %rng.down.21
			store i64 %rng.next.22, i64* %i
			br label %rng.cond.12
rng.end.12:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.4, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.2, i64 0, i64 0))
		%rng.init.24 = add i64 1, 1
		store i64 %rng.init.24, i64* %i
		br label %rng.cond.23
rng.cond.23:
			%rng.i.28 = load i64, i64* %i
			%rng.asc.29 = icmp sle i64 %rng.i.28, 5
			%rng.desc.30 = icmp sge i64 %rng.i.28, 5
			%rng.cmp.26 = icmp sle i64 1, 5
			%rng.sel.27 = select i1 %rng.cmp.26, i1 %rng.asc.29, i1 %rng.desc.30
			br i1 %rng.sel.27, label %rng.body.23, label %rng.end.23
rng.body.23:
			%i.val.31 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.5, i64 0, i64 0), i64 %i.val.31)
			%rng.up.32 = add i64 %rng.i.28, 1
			%rng.down.33 = sub i64 %rng.i.28, 1
			%rng.next.34 = select i1 %rng.cmp.26, i64 %rng.up.32, i64 %rng.down.33
			store i64 %rng.next.34, i64* %i
			br label %rng.cond.23
rng.end.23:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.6, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.3, i64 0, i64 0))
		%rng.init.36 = add i64 1, 1
		store i64 %rng.init.36, i64* %i
		br label %rng.cond.35
rng.cond.35:
			%rng.i.40 = load i64, i64* %i
			%rng.asc.41 = icmp slt i64 %rng.i.40, 5
			%rng.desc.42 = icmp sgt i64 %rng.i.40, 5
			%rng.cmp.38 = icmp sle i64 1, 5
			%rng.sel.39 = select i1 %rng.cmp.38, i1 %rng.asc.41, i1 %rng.desc.42
			br i1 %rng.sel.39, label %rng.body.35, label %rng.end.35
rng.body.35:
			%i.val.43 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.7, i64 0, i64 0), i64 %i.val.43)
			%rng.up.44 = add i64 %rng.i.40, 1
			%rng.down.45 = sub i64 %rng.i.40, 1
			%rng.next.46 = select i1 %rng.cmp.38, i64 %rng.up.44, i64 %rng.down.45
			store i64 %rng.next.46, i64* %i
			br label %rng.cond.35
rng.end.35:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.8, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.4, i64 0, i64 0))
		%rng.init.48 = add i64 5, 0
		store i64 %rng.init.48, i64* %i
		br label %rng.cond.47
rng.cond.47:
			%rng.i.51 = load i64, i64* %i
			%rng.asc.52 = icmp sle i64 %rng.i.51, 5
			%rng.desc.53 = icmp sge i64 %rng.i.51, 5
			%rng.cmp.49 = icmp sle i64 5, 5
			%rng.sel.50 = select i1 %rng.cmp.49, i1 %rng.asc.52, i1 %rng.desc.53
			br i1 %rng.sel.50, label %rng.body.47, label %rng.end.47
rng.body.47:
			%i.val.54 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.9, i64 0, i64 0), i64 %i.val.54)
			%rng.up.55 = add i64 %rng.i.51, 1
			%rng.down.56 = sub i64 %rng.i.51, 1
			%rng.next.57 = select i1 %rng.cmp.49, i64 %rng.up.55, i64 %rng.down.56
			store i64 %rng.next.57, i64* %i
			br label %rng.cond.47
rng.end.47:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.10, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.5, i64 0, i64 0))
		%rng.init.59 = add i64 5, 1
		store i64 %rng.init.59, i64* %i
		br label %rng.cond.58
rng.cond.58:
			%rng.i.63 = load i64, i64* %i
			%rng.asc.64 = icmp slt i64 %rng.i.63, 5
			%rng.desc.65 = icmp sgt i64 %rng.i.63, 5
			%rng.cmp.61 = icmp sle i64 5, 5
			%rng.sel.62 = select i1 %rng.cmp.61, i1 %rng.asc.64, i1 %rng.desc.65
			br i1 %rng.sel.62, label %rng.body.58, label %rng.end.58
rng.body.58:
			%i.val.66 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.11, i64 0, i64 0), i64 %i.val.66)
			%rng.up.67 = add i64 %rng.i.63, 1
			%rng.down.68 = sub i64 %rng.i.63, 1
			%rng.next.69 = select i1 %rng.cmp.61, i64 %rng.up.67, i64 %rng.down.68
			store i64 %rng.next.69, i64* %i
			br label %rng.cond.58
rng.end.58:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.12, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.6, i64 0, i64 0))
		%rng.init.71 = add i64 5, 0
		store i64 %rng.init.71, i64* %i
		br label %rng.cond.70
rng.cond.70:
			%rng.i.74 = load i64, i64* %i
			%rng.asc.75 = icmp sle i64 %rng.i.74, 0
			%rng.desc.76 = icmp sge i64 %rng.i.74, 0
			%rng.cmp.72 = icmp sle i64 5, 0
			%rng.sel.73 = select i1 %rng.cmp.72, i1 %rng.asc.75, i1 %rng.desc.76
			br i1 %rng.sel.73, label %rng.body.70, label %rng.end.70
rng.body.70:
			%i.val.77 = load i64, i64* %i
			call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([6 x i8], [6 x i8]* @.pfmt.13, i64 0, i64 0), i64 %i.val.77)
			%rng.up.78 = add i64 %rng.i.74, 1
			%rng.down.79 = sub i64 %rng.i.74, 1
			%rng.next.80 = select i1 %rng.cmp.72, i64 %rng.up.78, i64 %rng.down.79
			store i64 %rng.next.80, i64* %i
			br label %rng.cond.70
rng.end.70:
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.14, i64 0, i64 0), i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.str.7, i64 0, i64 0))
		call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.pfmt.15, i64 0, i64 0), i8* getelementptr inbounds ([5 x i8], [5 x i8]* @.str.8, i64 0, i64 0))
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %i)
		ret i32 0
}


; Format string constants
@.pfmt.0 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.0 = private unnamed_addr constant [12 x i8] c"range test:\00"
@.pfmt.1 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.2 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.1 = private unnamed_addr constant [4 x i8] c"---\00"
@.pfmt.3 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.4 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.2 = private unnamed_addr constant [4 x i8] c"---\00"
@.pfmt.5 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.6 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.3 = private unnamed_addr constant [4 x i8] c"---\00"
@.pfmt.7 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.8 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.4 = private unnamed_addr constant [4 x i8] c"---\00"
@.pfmt.9 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.10 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.5 = private unnamed_addr constant [4 x i8] c"---\00"
@.pfmt.11 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.12 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.6 = private unnamed_addr constant [4 x i8] c"---\00"
@.pfmt.13 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.14 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.7 = private unnamed_addr constant [4 x i8] c"---\00"
@.pfmt.15 = private unnamed_addr constant [4 x i8] c"%s\0A\00"
@.str.8 = private unnamed_addr constant [5 x i8] c"done\00"
