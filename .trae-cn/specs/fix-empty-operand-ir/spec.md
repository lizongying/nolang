# 修复 Codegen 空操作数 IR Bug (B1/B19) Spec

## Why
Nolang 编译器在生成 LLVM IR 时，对某些 `CallExpression` 会产生空操作数指令，导致 notools 项目无法构建。两个具体表现：
- **B1**：`store i64 , i64* %fmtval.39`（store 的值操作数为空），触发于 `http.http-get` 调用路径中 `emitArgAsStrLong` 使用了空返回值。
- **B19**：`getelementptr inbounds %str-long, %str-long* , i32 0, i32 0`（GEP 的指针操作数为空），触发于 notools `main.no` 构建中的 `du-print`/`http-do` 模块路径中 `extractStrLen`/`extractStrCap`/`extractStrDataPtr` 使用了空返回值。

根因：`generateExprWithSB`（`src/build/llvm/expr.go`）在处理 `CallExpression` 时，当 `generateCallExpression` 返回 `"call void @..."` 字符串时，直接返回空字符串 `""`。下游使用该空值的代码未做检查，导致生成的 IR 指令操作数为空。

## What Changes
- 在 `call_stdlib.go` 的 `emitArgAsStrLong` 函数中，对 `generateExprWithSB` 返回值添加空值防御性检查，输出编译错误而非生成损坏 IR。
- 在 `expr.go` 的 `extractStrLen`、`extractStrCap`、`extractStrDataPtr` 函数入口添加 `strPtr == ""` 防御性检查，输出编译错误而非生成损坏 IR。
- 修复 `generateExprWithSB` 处理 `CallExpression` 的根因：当被调用函数有输出参数（`voidSingleOutput` 路径）时，确保 `generateCallExpression` 返回有效的 SSA 寄存器名而非 `"call void @..."`，从而使 `generateExprWithSB` 不再返回空字符串。

## Impact
- Affected specs: 无（编译器内部 codegen 修复，不影响语言规范）
- Affected code:
  - `src/build/llvm/expr.go`：`generateExprWithSB` 的 `CallExpression` 分支（约 336-392 行）；`extractStrLen`（约 1922 行）、`extractStrCap`（约 1935 行）、`extractStrDataPtr`（约 1904 行）
  - `src/build/llvm/call_stdlib.go`：`emitArgAsStrLong`（约 268 行 `v := g.generateExprWithSB(sb, expr)` 之后）
  - `src/build/llvm/call.go`：`generateCallExpression` 中 `voidSingleOutput` 与 `funcHeuristicOutput` 的判定逻辑（约 1850-1864 行），确认有输出参数的函数走 `voidSingleOutput` 路径返回 SSA 寄存器

## ADDED Requirements

### Requirement: 空操作数防御性检查
当 `generateExprWithSB` 对任意表达式求值返回空字符串时，下游 IR 生成函数 SHALL 输出明确的编译错误并终止该指令的生成，而不是继续生成操作数为空的 IR 指令。

#### Scenario: emitArgAsStrLong 收到空返回值
- **WHEN** `emitArgAsStrLong` 调用 `generateExprWithSB(sb, expr)` 返回空字符串 `""`
- **THEN** 输出编译错误信息（包含表达式位置信息），并返回一个零值占位指针（避免后续指令级联损坏），或直接 panic 终止编译以暴露问题。

#### Scenario: extractStrLen/Cap/DataPtr 收到空指针
- **WHEN** `extractStrLen`/`extractStrCap`/`extractStrDataPtr` 的 `strPtr` 参数为空字符串 `""`
- **THEN** 输出编译错误信息，并返回零值占位寄存器名，或直接 panic 终止编译。

## MODIFIED Requirements

### Requirement: generateExprWithSB 对 CallExpression 的返回值
`generateExprWithSB` 处理 `*parser.CallExpression` 时 SHALL 区分以下情况：
1. **纯 void 调用（无输出参数）**：返回空字符串 `""`（保持现有行为，仅用于语句上下文）。
2. **有输出参数的 void 调用（`voidSingleOutput` 路径）**：返回 `generateCallExpression` 生成的 SSA 寄存器名（如 `%call.tmp.N`），不得返回空字符串。
3. **非 void 调用**：返回 SSA 寄存器名（保持现有行为）。

调用方（如 `emitArgAsStrLong`、`extractStrLen` 等）在使用返回值前 SHALL 进行空值检查，作为防御性兜底。

## REMOVED Requirements
无。
