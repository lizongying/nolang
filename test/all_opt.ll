; ModuleID = '/var/folders/66/rv9s0g5n7bd908c8g7lvf98r0000gn/T/nolang1648661228/all.ll'
source_filename = "nolang"
target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

@.str.0 = private unnamed_addr constant [14 x i8] c"=== array ===\00"
@.str.1 = private unnamed_addr constant [7 x i8] c"arr ok\00"
@.str.2 = private unnamed_addr constant [14 x i8] c"=== range ===\00"
@.str.3 = private unnamed_addr constant [9 x i8] c"range ok\00"
@.str.4 = private unnamed_addr constant [21 x i8] c"=== string range ===\00"
@.str.6 = private unnamed_addr constant [13 x i8] c"str range ok\00"
@.str.7 = private unnamed_addr constant [23 x i8] c"=== variadic print ===\00"
@.str.8 = private unnamed_addr constant [6 x i8] c"hello\00"
@.str.9 = private unnamed_addr constant [6 x i8] c"world\00"
@.pfmt.9 = private unnamed_addr constant [3 x i8] c"%s\00"
@.pfmt.11 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.str.10 = private unnamed_addr constant [9 x i8] c"vargs ok\00"
@.str.11 = private unnamed_addr constant [15 x i8] c"=== printf ===\00"
@.str.12 = private unnamed_addr constant [16 x i8] c"int=%d str=%s\\n\00"
@.str.13 = private unnamed_addr constant [5 x i8] c"test\00"
@.str.14 = private unnamed_addr constant [10 x i8] c"printf ok\00"
@.str.15 = private unnamed_addr constant [17 x i8] c"all tests passed\00"

; Function Attrs: nofree nounwind
declare noundef i32 @printf(ptr noundef readonly captures(none), ...) local_unnamed_addr #0

; Function Attrs: nofree nounwind
define noundef i32 @main() local_unnamed_addr #0 {
entry:
  %puts = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.0)
  %puts2 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.1)
  %puts3 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.2)
  %puts4 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.3)
  %puts5 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.4)
  %puts6 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.6)
  %puts7 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.7)
  %0 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.9, ptr nonnull @.str.8)
  %putchar = tail call i32 @putchar(i32 32)
  %1 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.9, ptr nonnull @.str.9)
  %putchar8 = tail call i32 @putchar(i32 32)
  %2 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.11, i64 42)
  %puts9 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.10)
  %puts10 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.11)
  %3 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.str.12, i64 42, ptr nonnull @.str.13)
  %puts11 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.14)
  %puts12 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.15)
  ret i32 0
}

; Function Attrs: nofree nounwind
declare noundef i32 @puts(ptr noundef readonly captures(none)) local_unnamed_addr #0

; Function Attrs: nofree nounwind
declare noundef i32 @putchar(i32 noundef) local_unnamed_addr #0

attributes #0 = { nofree nounwind }
