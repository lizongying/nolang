; ModuleID = '/var/folders/66/rv9s0g5n7bd908c8g7lvf98r0000gn/T/nolang3000474891/range2.ll'
source_filename = "nolang"
target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

@.str.0 = private unnamed_addr constant [14 x i8] c"string range:\00"
@.str.3 = private unnamed_addr constant [11 x i8] c"int range:\00"
@.pfmt.4 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"
@.str.4 = private unnamed_addr constant [4 x i8] c"---\00"

; Function Attrs: nofree nounwind
declare noundef i32 @printf(ptr noundef readonly captures(none), ...) local_unnamed_addr #0

; Function Attrs: nofree nounwind
define noundef i32 @main() local_unnamed_addr #0 {
entry:
  %puts = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.0)
  %0 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 97)
  %1 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 98)
  %2 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 99)
  %puts4 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.4)
  %puts5 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.3)
  %3 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 1)
  %4 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 2)
  %5 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 3)
  %6 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 4)
  %7 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.4, i64 5)
  %puts6 = tail call i32 @puts(ptr nonnull dereferenceable(1) @.str.4)
  ret i32 0
}

; Function Attrs: nofree nounwind
declare noundef i32 @puts(ptr noundef readonly captures(none)) local_unnamed_addr #0

attributes #0 = { nofree nounwind }
