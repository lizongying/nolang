; ModuleID = 'nolang'
source_filename = "nolang"
target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

declare void @llvm.lifetime.start.p0i8(i64, i8* nocapture)
declare void @llvm.lifetime.end.p0i8(i64, i8* nocapture)

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

@dummy_sink = global i64 0

define i32 @main() {
	entry:
		%result = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %result)
		%iter = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %iter)
		%n_val = alloca i64
		call void @llvm.lifetime.start.p0i8(i64 8, i8* %n_val)
		store i64 0, i64* %iter
		br label %for.cond
for.cond:
			%iter.val = load i64, i64* %iter
			%cmp = icmp slt i64 %iter.val, 10000000
			br i1 %cmp, label %for.body, label %for.end
for.body:
			%iter.val.again = load i64, i64* %iter
			%cmp.5m = icmp slt i64 %iter.val.again, 5000000
			br i1 %cmp.5m, label %use40, label %use41
use40:
			store i64 40, i64* %n_val
			br label %continue
use41:
			store i64 41, i64* %n_val
			br label %continue
continue:
			call void @fib(i64* %n_val, i64* %result)
			; 寫入全域變數 — 編譯器無法優化掉
			%res = load i64, i64* %result
			store volatile i64 %res, i64* @dummy_sink
			%iter.val.inc = load i64, i64* %iter
			%inc = add i64 %iter.val.inc, 1
			store i64 %inc, i64* %iter
			br label %for.cond
for.end:
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %result)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %iter)
		call void @llvm.lifetime.end.p0i8(i64 8, i8* %n_val)
		ret i32 0
}
