# Checklist

- [x] `emitArgAsStrLong` 在 `generateExprWithSB` 返回空字符串时输出编译错误，不再生成 `store i64 , i64* ...` 形式的损坏 IR。
- [x] `extractStrLen` 在 `strPtr` 为空时输出编译错误，不再生成 `getelementptr ... %str-long* , ...` 形式的损坏 IR。
- [x] `extractStrCap` 在 `strPtr` 为空时输出编译错误，不再生成损坏 IR。
- [x] `extractStrDataPtr` 在 `strPtr` 为空时输出编译错误，不再生成损坏 IR。
- [x] `generateExprWithSB` 对有输出参数的 `CallExpression`（`voidSingleOutput` 路径）返回有效的 SSA 寄存器名，不再返回空字符串。
- [x] B1 重现用例（`http.http-get` 路径）编译通过，生成的 IR 中无空操作数指令。
- [x] B19 重现用例（`no vet src/std` 完整构建）编译通过，生成的 IR 中无空操作数指令。
- [x] `make no` 成功重建编译器二进制。
- [x] `no vet` 通过，标准库无新增错误（已知 pre-existing `src/std/net/quic.no:354` 的 `%data-len` bug 与本次修复无关，可忽略）。
- [x] 未修改与本次修复无关的 valid 代码。
- [x] 未自行设计 Nolang 语法，严格遵循现有语法规则。
