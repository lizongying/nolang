# Tasks

- [x] Task 1: 添加防御性检查（防止空操作数 IR 生成）
  - [x] SubTask 1.1: 在 `src/build/llvm/call_stdlib.go` 的 `emitArgAsStrLong` 函数中，于 `v := g.generateExprWithSB(sb, expr)` 之后添加 `v == ""` 检查，输出编译错误并返回零值占位指针或 panic。
  - [x] SubTask 1.2: 在 `src/build/llvm/expr.go` 的 `extractStrLen` 函数入口添加 `strPtr == ""` 检查。
  - [x] SubTask 1.3: 在 `src/build/llvm/expr.go` 的 `extractStrCap` 函数入口添加 `strPtr == ""` 检查。
  - [x] SubTask 1.4: 在 `src/build/llvm/expr.go` 的 `extractStrDataPtr` 函数入口添加 `strPtr == ""` 检查。

- [x] Task 2: 修复根因（generateExprWithSB 对有输出参数的 CallExpression 不应返回空字符串）
  - [x] SubTask 2.1: 分析 `src/build/llvm/call.go` 中 `generateCallExpression` 的 `voidSingleOutput` 与 `funcHeuristicOutput` 判定逻辑（约 1850-1864 行），确认有输出参数的函数是否正确走 `voidSingleOutput` 路径返回 SSA 寄存器。
  - [x] SubTask 2.2: 修复 `src/build/llvm/expr.go` 中 `generateExprWithSB` 的 `CallExpression` 分支（约 336-392 行），确保当 `generateCallExpression` 返回 `"call void @..."` 且函数有输出参数时，不再返回空字符串，而是走 `voidSingleOutput` 路径获取返回值。
  - [x] SubTask 2.3: 确认 `sprintf`/`format` 等内置函数在 `callFmt`（`call_stdlib.go`）中未被正确处理时（如多参数 C 风格 `sprintf('%d', x)`）不会静默返回空字符串，需返回错误或走默认路径。

- [x] Task 3: 重建编译器并验证修复
  - [x] SubTask 3.1: 执行 `make no` 重建编译器二进制。
  - [x] SubTask 3.2: 验证 B1 修复（`http.http-get` 调用路径，使用 `tests/test-net-http.no` 测试用例，`it.status-code` 路径）。
  - [x] SubTask 3.3: 验证 B19 修复（`no vet src/std` 完整构建，确认无空操作数 IR 与防御性诊断触发）。
  - [x] SubTask 3.4: 执行 `no vet` 验证标准库无回归错误。

# Task Dependencies
- [Task 2] 依赖 [Task 1]（防御性检查先落地，便于根因修复过程中暴露问题位置）
- [Task 3] 依赖 [Task 1] 和 [Task 2]（修复完成后方可重建验证）
