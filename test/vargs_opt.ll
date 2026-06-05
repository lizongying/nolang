; ModuleID = '/var/folders/66/rv9s0g5n7bd908c8g7lvf98r0000gn/T/nolang450431306/vargs.ll'
source_filename = "nolang"
target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

@.str.1 = private unnamed_addr constant [6 x i8] c"hello\00"
@.str.2 = private unnamed_addr constant [6 x i8] c"world\00"
@.pfmt.4 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.pfmt.5 = private unnamed_addr constant [4 x i8] c"%g\0A\00"
@.str.3 = private unnamed_addr constant [11 x i8] c"no newline\00"
@.pfmt.7 = private unnamed_addr constant [3 x i8] c"%s\00"
@.str.5 = private unnamed_addr constant [16 x i8] c"int=%d str=%s\\n\00"
@.str.6 = private unnamed_addr constant [5 x i8] c"test\00"
@.str.7 = private unnamed_addr constant [11 x i8] c"float=%g\\n\00"

; Function Attrs: nofree nounwind
declare noundef i32 @printf(ptr noundef readonly captures(none), ...) local_unnamed_addr #0

; Function Attrs: nofree nounwind
define noundef i32 @main() local_unnamed_addr #0 {
entry:
  %puts = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.1)
  %0 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.7, ptr nonnull @.str.1)
  %putchar = tail call i32 @putchar(i32 32)
  %puts1 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.2)
  %1 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 42)
  %2 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.5, double 3.140000e+00)
  %putchar2 = tail call i32 @putchar(i32 10)
  %3 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.7, ptr nonnull @.str.3)
  %putchar4 = tail call i32 @putchar(i32 10)
  %4 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.str.5, i64 42, ptr nonnull @.str.6)
  %5 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.str.7, double 3.140000e+00)
  ret i32 0
}

; Function Attrs: nofree nounwind
declare noundef i32 @puts(ptr noundef readonly captures(none)) local_unnamed_addr #0

; Function Attrs: nofree nounwind
declare noundef i32 @putchar(i32 noundef) local_unnamed_addr #0

attributes #0 = { nofree nounwind }
