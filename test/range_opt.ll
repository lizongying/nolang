; ModuleID = '/var/folders/66/rv9s0g5n7bd908c8g7lvf98r0000gn/T/nolang3379156777/range.ll'
source_filename = "nolang"
target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

@.str.0 = private unnamed_addr constant [12 x i8] c"range test:\00"
@.pfmt.13 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.str.7 = private unnamed_addr constant [4 x i8] c"---\00"
@.str.8 = private unnamed_addr constant [5 x i8] c"done\00"

; Function Attrs: nofree nounwind
declare noundef i32 @printf(ptr noundef readonly captures(none), ...) local_unnamed_addr #0

; Function Attrs: nofree nounwind
define noundef i32 @main() local_unnamed_addr #0 {
entry:
  %puts = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.0)
  %0 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 1)
  %1 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 2)
  %2 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 3)
  %3 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 4)
  %4 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 5)
  %puts14 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.7)
  %5 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 1)
  %6 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 2)
  %7 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 3)
  %8 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 4)
  %puts15 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.7)
  %9 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 2)
  %10 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 3)
  %11 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 4)
  %12 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 5)
  %puts16 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.7)
  %13 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 2)
  %14 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 3)
  %15 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 4)
  %puts17 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.7)
  %16 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 5)
  %puts18 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.7)
  %puts19 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.7)
  %17 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 5)
  %18 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 4)
  %19 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 3)
  %20 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 2)
  %21 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 1)
  %22 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.13, i64 0)
  %puts20 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.7)
  %puts21 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.8)
  ret i32 0
}

; Function Attrs: nofree nounwind
declare noundef i32 @puts(ptr noundef readonly captures(none)) local_unnamed_addr #0

attributes #0 = { nofree nounwind }
