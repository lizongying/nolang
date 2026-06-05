; ModuleID = '/var/folders/66/rv9s0g5n7bd908c8g7lvf98r0000gn/T/nolang1861377481/test_byte.ll'
source_filename = "nolang"
target datalayout = "e-m:o-i64:64-i128:128-n32:64-S128"
target triple = "arm64-apple-macosx15.0.0"

@.pfmt.0 = private unnamed_addr constant [6 x i8] c"%lld\0A\00"

; Function Attrs: nofree nounwind
declare noundef i32 @printf(ptr noundef readonly captures(none), ...) local_unnamed_addr #0

; Function Attrs: nofree nounwind
define void @test() local_unnamed_addr #0 {
entry:
  %0 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.0, i64 255)
  ret void
}

; Function Attrs: nofree nounwind
define noundef i32 @main() local_unnamed_addr #0 {
entry:
  %0 = tail call i32 (ptr, ...) @printf(ptr nonnull dereferenceable(1) @.pfmt.0, i64 255)
  ret i32 0
}

attributes #0 = { nofree nounwind }
