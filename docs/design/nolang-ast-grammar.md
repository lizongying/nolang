# Nolang AST 節點清單、文法與錯誤訊息

> 由 `scripts/ast_inventory.py` **直接從 Go 原始碼機械抽取**，無人工轉錄。
> 來源：`src/parser/ast.go`（節點定義）、`src/parser/*.go`（建構點、錯誤點）、
> `src/parser/dump/dump.go`（L2 oracle 的封閉型別 switch）。

## 0. 總覽

| 項目 | 數量 |
|---|---|
| `ast.go` 內 struct 總數 | 78 |
| 實作 `Node`（Pos+EndPos） | 71 |
| 　└ `Statement` | 18 |
| 　└ `Expression` | 31 |
| 　└ `Type` | 9 |
| 　└ 純 `Node`（無 marker） | 13 |
| 非 `Node` 輔助 struct | 7 |
| `dumpNode()` case 數 | 71 |
| ❌ **oracle 未覆蓋的 Node** | **0** |
| ⚠️ 只印「殼」的節點（無遞迴） | 22 |
| `nodeString()` case 數 | 7 |
| 錯誤訊息站點（`saveError` + 結構化 API） | 168 |

## 1. 未被 oracle 覆蓋的節點（預期為空 ⇒ 空表就是綠燈）

> 這一節由 `dumpNode` 的 case 清單與 `ast.go` 的 Node 清單相減而得。
> **空 = `no ast` 對每個節點型別都有 case**（2026-10-10 補齊，見設計文檔 §5.3.1）。
> 非空 ⇒ 那些節點的**整個子樹**在 L2 差分中不可見。

| # | 節點 | ast.go 行 | 種類 | 欄位 | 建構函式 |
|---|---|---|---|---|---|

## 2. 只印「殼」的節點（dump 有 case，但無遞迴欄位 ⇒ 子樹不可見）

| 節點 | 行 | 種類 | dump 引用到的欄位 | **未印出的欄位** |
|---|---|---|---|---|
| `UseStatement` | 516 | Statement | Alias, AsKeyword, Function, Path | — |
| `ExportStatement` | 530 | Statement | Alias, AsKeyword, Function, Path | — |
| `AnnotationStatement` | 1132 | Statement | Entries | — |
| `BreakStatement` | 1700 | Statement | Label, LabelKind | — |
| `ContinueStatement` | 1713 | Statement | Label, LabelKind | — |
| `InterfaceDefinition` | 1971 | Statement | Implements, Methods, Name | — |
| `Identifier` | 666 | Expression | IsImplicitGeneric, Value | — |
| `IntegerLiteral` | 1304 | Expression | Raw, Value | — |
| `FloatLiteral` | 1317 | Expression | Raw, Value | — |
| `StringLiteral` | 1327 | Expression | Raw, Value | — |
| `CharLiteral` | 1337 | Expression | Raw, Value | — |
| `RegexLiteral` | 1357 | Expression | Flags, Pattern | — |
| `BooleanLiteral` | 1367 | Expression | Value | — |
| `NilLiteral` | 1376 | Expression | — | — |
| `EnumValue` | 1812 | Node | Explicit, Name, Value | — |
| `NamedType` | 36 | Type | IsInferred, Value | — |
| `AnnotationBoolValue` | 911 | Node | Value | — |
| `AnnotationIntValue` | 927 | Node | Value | — |
| `AnnotationStringValue` | 938 | Node | Value | — |
| `AnnotationIdentValue` | 949 | Node | Value | — |
| `AnnotationStructValue` | 987 | Node | Fields, Type | — |
| `AnnotationEntry` | 1053 | Node | — | `Key`, `Value`, `Trailing`, `Propagated` |

## 3. 遞迴覆蓋完整的節點

| 節點 | 遞迴欄位 |
|---|---|
| `Program` | `Statements` |
| `MultiAssignStatement` | `Targets`, `Value` |
| `UnwrapAssignStatement` | `Target`, `Value` |
| `LetStatement` | `Type`, `Value` |
| `ReturnStatement` | `ReturnValue` |
| `ExpressionStatement` | `Expression` |
| `BlockStatement` | `Statements` |
| `FunctionDefinition` | `Body` |
| `ExternStatement` | `Parameters`, `Results` |
| `TypeAlias` | `Type`, `Union` |
| `ForStatement` | `Body`, `Condition`, `CountExpr`, `Init`, `IterRange`, `Update` |
| `EnumDefinition` | `Values` |
| `TaggedEnumDefinition` | `Variants` |
| `StructDefinition` | `Fields` |
| `FunctionLiteral` | `Body` |
| `CallExpression` | `Arguments`, `Function` |
| `DotExpression` | `Receiver` |
| `PrefixExpression` | `Right` |
| `InfixExpression` | `Left`, `Right` |
| `RunExpression` | `Call` |
| `CoExpression` | `Call` |
| `AwaitExpression` | `Right` |
| `IfExpression` | `Alternative`, `Condition`, `Consequence` |
| `RangeExpression` | `End`, `Start` |
| `IterationExpr` | `Range`, `RangeExpr` |
| `SliceExpression` | `Left`, `Range` |
| `IndexExpression` | `Index`, `Left` |
| `AssignExpression` | `Left`, `Value` |
| `ConditionalExpression` | `Alternative`, `Condition`, `Consequence` |
| `CastExpression` | `Expr`, `Type` |
| `ArrayLiteral` | `Elements`, `Size` |
| `SliceLiteral` | `Elements` |
| `MapLiteral` | `MapType`, `Pairs` |
| `MapPair` | `Key`, `Value` |
| `StructLiteral` | `Fields` |
| `StructField` | `Type`, `Value` |
| `TaggedEnumVariant` | `Fields`, `Type` |
| `GroupedExpression` | `Expression` |
| `Parameter` | `DefaultExpr`, `Type` |
| `ArrayType` | `Elem`, `Size` |
| `SliceType` | `Elem` |
| `MapType` | `Key`, `Value` |
| `FunctionType` | `Params`, `Results` |
| `UnionType` | `Types` |
| `NullableType` | `Type` |
| `PointerType` | `Type` |
| `ViewType` | `Type` |
| `AnnotationArrayValue` | `Elements` |
| `AnnotationRangeValue` | `End`, `Start` |

## 4. 非 Node 輔助 struct（被節點欄位引用，本身不進 AST 走訪）

| struct | 行 | 欄位 |
|---|---|---|
| `Comment` | 300 | `Pos`:lexer.Position, `End`:lexer.Position, `Kind`:CommentKind, `Text`:string, `Marker`:string |
| `CommentGroup` | 310 | `List`:[]*Comment, `Start`:lexer.Position, `End`:lexer.Position |
| `CommentedNode` | 317 | `Doc`:*CommentGroup, `Comment`:*CommentGroup, `SourceFile`:string, `ModuleOwner`:string |
| `FuncSignature` | 751 | `Parameters`:[]*Parameter, `Results`:[]*Parameter, `GenericParams`:[]*Identifier, `IsVariadic`:bool, `VariadicUnion`:string, `GenericUnion`:string |
| `AnnotationStructField` | 994 | `Name`:string, `Value`:AnnotationValue |
| `TaggedEnumField` | 1896 | `Token`:lexer.Token, `Name`:string, `Type`:Type |
| `InterfaceMethod` | 1961 | `Token`:lexer.Token, `Name`:string, `Parameters`:[]*Parameter, `IsVariadic`:bool, `Results`:[]*Parameter, `Receiver`:string, `IsGenericReceiver`:bool |

## 5. 節點 ← 建構函式（文法歸屬）

| 節點 | 種類 | 建構它的函式 |
|---|---|---|
| `Comment` | - | commentGroupFromTokens, parseBareMatchExpr, parseBlockStatement |
| `CommentGroup` | - | commentGroupFromTokens, parseBareMatchExpr, parseBlockStatement |
| `InterfaceMethod` | - | parseInterfaceDefinition |
| `TaggedEnumField` | - | parseTaggedEnumDefinition |
| `ArrayLiteral` | Expression | defaultLiteralFor, looseLiteralFromAnnotationValue, parseArrayLiteral, parseLetStatement, zeroValueLiteralFor |
| `AssignExpression` | Expression | lowerUnwrapAssign, maybeIndexOutAssign, parseExpression |
| `AwaitExpression` | Expression | parseExpression |
| `BooleanLiteral` | Expression | defaultLiteralFor, looseLiteralFromAnnotationValue, parseBangLoop, parseBoolLiteralLoop, parseCondLoopBlockFirst, parseCondLoopPrefix, parseExpression, parseForStatement, parseMatchExprFrom, scalarZeroLiteral |
| `CallExpression` | Expression | parseCallExpression, parseExpression |
| `CastExpression` | Expression | parseExpression |
| `CharLiteral` | Expression | parseExpression, parseLetStatement |
| `CoExpression` | Expression | parseExpression |
| `ConditionalExpression` | Expression | parseConditionalExpression |
| `DotExpression` | Expression | buildMatchDesugar, parseExpression |
| `FloatLiteral` | Expression | defaultLiteralFor, looseLiteralFromAnnotationValue, parseFloatLiteral, scalarZeroLiteral |
| `FunctionLiteral` | Expression | parseFunctionLiteral |
| `GroupedExpression` | Expression | parseGroupedExpression |
| `Identifier` | Expression | ResolveProgram, addCaptureNode, addImplicitGeneric, bindAndGuard, buildBareMatchDesugar, buildItBinding, buildItBindingForArm, buildMatchDesugar, buildType, lowerUnwrapAssign, makePropagateGuard, maybeCaptureOptionAssign, maybeIndexOutAssign, namedItBinding, parseAnnotationFFIDeclaration, parseAssignTarget, parseBareMatchExpr, parseElifBlock, parseExpression, parseFunctionDefinition, parseIdentifier, parseLetStatement, parseMatchExprFrom, parseMatchExpression, parseTildeMatchStatement, parseTypeExpression, parseUnwrapAssignStatement |
| `IfExpression` | Expression | buildBareMatchDesugar, buildMatchDesugar, parseElifBlock, parseExpressionStatement, parseIfExpression, parseLabeledStatement, parseLetStatement, parseMatchExpression, wrapStandaloneChainDepth |
| `IndexExpression` | Expression | parseAssignTarget, parseSliceExpression |
| `InfixExpression` | Expression | buildMatchDesugar, desugarRangeCondition, parseExpression, parseForStatement, parseInfixExpression, parseMatchExpression |
| `IntegerLiteral` | Expression | buildBareMatchDesugar, buildMatchDesugar, buildType, defaultLiteralFor, desugarRangeCondition, looseLiteralFromAnnotationValue, maybeIndexOutAssign, parseCountLoopPrefix, parseCountedLoopBlockFirst, parseExpressionStatement, parseIntegerLiteral, parseLetStatement, parseMatchExpression, parseTypeExpression, scalarZeroLiteral, zeroValueLiteralFor |
| `IterationExpr` | Expression | parseForStatement |
| `MapLiteral` | Expression | parseMapLiteral, zeroValueLiteralFor |
| `NilLiteral` | Expression | looseLiteralFromAnnotationValue, maybeCaptureOptionAssign, parseBareMatchExpr, parseExpression, parseNilLiteral |
| `Parameter` | Expression | parseArrayTypeMethodDefinition, parseColonMethodDefinition, parseExternParamList, parseFunctionBody, parseFunctionLiteral, parseFunctionTypeEntry, parseInterfaceDefinition, parseMethodDefinition |
| `PrefixExpression` | Expression | parsePrefixExpression |
| `RangeExpression` | Expression | parseForRange, parseGroupedExpression, parseRangeBody, parseSliceExpression, parseSliceLiteral |
| `RegexLiteral` | Expression | parseExpression |
| `RunExpression` | Expression | parseExpression |
| `SliceExpression` | Expression | parseSliceExpression |
| `SliceLiteral` | Expression | parseSliceLiteral |
| `StringLiteral` | Expression | defaultLiteralFor, looseLiteralFromAnnotationValue, parseStringLiteral, scalarZeroLiteral |
| `StructLiteral` | Expression | defaultLiteralFor, looseLiteralFromAnnotationValue, parseStructLiteral, zeroValueLiteralFor |
| `UnwrapAssignStatement` | Expression | parseExpression, parseUnwrapAssignStatement |
| `AnnotationArrayValue` | Node | parseAnnotationArray |
| `AnnotationBoolValue` | Node | parseAnnotationSimpleValue |
| `AnnotationEntry` | Node | parseAnnotationBody |
| `AnnotationIdentValue` | Node | carryOverflowAnnotation, parseAnnotationSimpleValue |
| `AnnotationIntValue` | Node | parseAnnotationSimpleValue |
| `AnnotationRangeValue` | Node | parseAnnotationRange, parseAnnotationValue |
| `AnnotationStringValue` | Node | parseAnnotationSimpleValue |
| `AnnotationStructValue` | Node | parseAnnotationSimpleValue |
| `EnumValue` | Node | parseEnumDefinition |
| `Program` | Node | ParseProgram |
| `StructField` | Node | defaultLiteralFor, looseLiteralFromAnnotationValue, parseStructDefinition, parseStructLiteral |
| `TaggedEnumVariant` | Node | parseTaggedEnumDefinition |
| `AnnotationStatement` | Statement | parseAnnotationStatement |
| `BlockStatement` | Statement | buildBareMatchDesugar, buildMatchDesugar, lowerUnwrapAssign, makePropagateGuard, maybeCaptureOptionAssign, maybeIndexOutAssign, parseBareMatchExpr, parseBlockStatement, parseElifBlock, parseLabeledStatement, parseLoopBody, parseMatchExprFrom, parseMatchExpression, parseStandaloneBody, parseSwitchStatement, parseTildeMatchStatement, prependStmt, wrapStandaloneChainDepth |
| `BreakStatement` | Statement | parseBreakStatement, parseStarBreakOrExpr |
| `ContinueStatement` | Statement | parseContinueStatement, parseStarStarContinue |
| `EnumDefinition` | Statement | parseEnumDefinition |
| `ExportStatement` | Statement | parseExportStatement |
| `ExpressionStatement` | Statement | buildBareMatchDesugar, buildMatchDesugar, lowerUnwrapAssign, makePropagateGuard, maybeCaptureOptionAssign, maybeIndexOutAssign, parseElifBlock, parseExpressionStatement, parseLabeledStatement, parseMatchExpression, parseStandaloneBody, parseStatement, parseSwitchStatement, parseTildeMatchStatement, wrapStandaloneChainDepth |
| `ExternStatement` | Statement | parseAnnotationFFIDeclaration |
| `ForStatement` | Statement | parseBangLoop, parseBoolLiteralLoop, parseCondLoopBlockFirst, parseCondLoopPrefix, parseCountLoopPrefix, parseCountedLoopBlockFirst, parseExpressionStatement, parseForStatement, parseLabeledStatement |
| `FunctionDefinition` | Statement | parseArrayTypeMethodDefinition, parseColonFunctionDefinition, parseColonMethodDefinition, parseFunctionDefinition |
| `InterfaceDefinition` | Statement | parseInterfaceDefinition |
| `LetStatement` | Statement | bindAndGuard, buildItBinding, buildItBindingForArm, buildMatchDesugar, lowerUnwrapAssign, makePropagateGuard, maybeCaptureOptionAssign, maybeIndexOutAssign, parseLetStatement |
| `MultiAssignStatement` | Statement | parseExpressionStatement, parseMultiAssignStatement |
| `ReturnStatement` | Statement | lowerUnwrapAssign, makePropagateGuard, parseReturnStatement |
| `StructDefinition` | Statement | parseStructDefinition |
| `TaggedEnumDefinition` | Statement | parseTaggedEnumDefinition |
| `TypeAlias` | Statement | parseFunctionTypeAlias, parseTypeAlias |
| `UseStatement` | Statement | parseUseStatement |
| `ArrayType` | Type | buildType, parseLetStatement, parseTypeExpression |
| `FunctionType` | Type | parseFunctionType |
| `MapType` | Type | buildType, parseLetStatement, parseMapTypeString, parseTypeExpression |
| `NamedType` | Type | bindAndGuard, buildItBindingForArm, buildMatchDesugar, buildType, exprToType, inferOptionDivMod, lowerUnwrapAssign, maybeCaptureOptionAssign, maybeIndexOutAssign, parseFunctionTypeEntry, parseLetStatement, parseMapTypeString, parseTypeExpression, synthesizeNullableType |
| `NullableType` | Type | bindAndGuard, buildMatchDesugar, buildType, inferOptionDivMod, lowerUnwrapAssign, maybeCaptureOptionAssign, maybeIndexOutAssign, parseLetStatement, parseNullableType, parseTypeExpression, synthesizeNullableType |
| `PointerType` | Type | buildType, parseExternType, parsePointerType, parseTypeExpression |
| `SliceType` | Type | buildType, parseLetStatement, parseTypeExpression |
| `UnionType` | Type | parseTypeAlias |
| `ViewType` | Type | buildType |

## 6. 錯誤訊息目錄（供 `noc` 逐字重現）

| # | 函式 | 位置 | 通道 | 錯誤碼 | 訊息格式 |
|---|---|---|---|---|---|
| 1 | `parseTrailingAnnotation` | annotation.go:70 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close annotation, got %s instead` |
| 2 | `errPrefixAnnotation` | annotation.go:104 | saveError | `E_GENERAL` | `line %d, column %d: `#{...}` 註解不能寫在目標前方同一行；請獨立成行置於目標上方，或寫在目標同一行尾隨` |
| 3 | `parseAnnotationStatement` | annotation.go:152 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close annotation, got %s instead` |
| 4 | `parseAnnotationStatement` | annotation.go:204 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close annotation, got %s instead` |
| 5 | `parseAnnotationFFIDeclaration` | annotation.go:948 | saveError | `E_GENERAL` | `line %d, column %d: expected identifier after #{%s} directive, got %s instead` |
| 6 | `parseAnnotationFFIDeclaration` | annotation.go:958 | saveError | `E_GENERAL` | `line %d, column %d: expected '=' after FFI name, got %s instead` |
| 7 | `parseAnnotationBody` | annotation.go:1007 | saveError | `E_GENERAL` | `line %d, column %d: expected annotation key, got %s instead` |
| 8 | `parseAnnotationBody` | annotation.go:1051 | saveError | `E_GENERAL` | `line %d, column %d: expected ',' or '}' in annotation, got %s instead` |
| 9 | `parseAnnotationValue` | annotation.go:1101 | saveError | `E_GENERAL` | `line %d, column %d: expected ']' or ')' to close range, got %s instead` |
| 10 | `parseAnnotationSimpleValue` | annotation.go:1221 | saveError | `E_GENERAL` | `line %d, column %d: expected field name in struct literal, got %s instead` |
| 11 | `parseAnnotationSimpleValue` | annotation.go:1229 | saveError | `E_GENERAL` | `line %d, column %d: expected ':' after struct field name, got %s instead` |
| 12 | `parseAnnotationSimpleValue` | annotation.go:1240 | saveError | `E_GENERAL` | `line %d, column %d: expected ',' or '}' in struct literal, got %s instead` |
| 13 | `parseAnnotationSimpleValue` | annotation.go:1259 | saveError | `E_GENERAL` | `line %d, column %d: expected annotation value, got %s instead` |
| 14 | `parseAnnotationArray` | annotation.go:1302 | saveError | `E_GENERAL` | `line %d, column %d: expected ',' or ']' in array, got %s instead` |
| 15 | `parseAnnotationRange` | annotation.go:1330 | saveError | `E_GENERAL` | `line %d, column %d: expected '..' in range, got %s instead` |
| 16 | `parseAnnotationRange` | annotation.go:1348 | saveError | `E_GENERAL` | `line %d, column %d: expected ']' or ')' to close range, got %s instead` |
| 17 | `parseArrayTypeMethodDefinition` | decl.go:219 | saveError | `E_GENERAL` | `line %d, column %d: expected ']' in nullable slice type, got %s` |
| 18 | `parseArrayTypeMethodDefinition` | decl.go:234 | saveError | `E_GENERAL` | `line %d, column %d: expected ']' in array type, got %s` |
| 19 | `parseArrayTypeMethodDefinition` | decl.go:249 | saveError | `E_GENERAL` | `line %d, column %d: expected '.' after type, got %s` |
| 20 | `parseArrayTypeMethodDefinition` | decl.go:258 | saveError | `E_GENERAL` | `line %d, column %d: expected method name, got %s` |
| 21 | `parseArrayTypeMethodDefinition` | decl.go:275 | saveError | `E_GENERAL` | `line %d, column %d: expected '('` |
| 22 | `parseArrayTypeMethodDefinition` | decl.go:289 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter name, got %s instead` |
| 23 | `parseArrayTypeMethodDefinition` | decl.go:347 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter type, got %s instead` |
| 24 | `parseArrayTypeMethodDefinition` | decl.go:376 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis, got %s instead` |
| 25 | `parseArrayTypeMethodDefinition` | decl.go:386 | saveError | `E_GENERAL` | `line %d, column %d: expected ')'` |
| 26 | `parseArrayTypeMethodDefinition` | decl.go:422 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter name, got %s instead` |
| 27 | `parseArrayTypeMethodDefinition` | decl.go:480 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter type, got %s instead` |
| 28 | `parseArrayTypeMethodDefinition` | decl.go:500 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or ')'` |
| 29 | `parseArrayTypeMethodDefinition` | decl.go:510 | saveError | `E_GENERAL` | `line %d, column %d: expected ')'` |
| 30 | `parseArrayTypeMethodDefinition` | decl.go:528 | saveError | `E_GENERAL` | `line %d, column %d: expected '{'` |
| 31 | `parseEnumDefinition` | decl.go:783 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close enum value annotation, got %s instead` |
| 32 | `parseEnumDefinition` | decl.go:796 | saveError | `E_GENERAL` | `line %d, column %d: expected enum value name, got %s` |
| 33 | `parseEnumDefinition` | decl.go:816 | saveError | `E_GENERAL` | `line %d, column %d: expected integer value after '=', got %s` |
| 34 | `parseEnumDefinition` | decl.go:839 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close enum value annotation, got %s instead` |
| 35 | `parseInterfaceDefinition` | decl.go:891 | saveError | `E_GENERAL` | `line %d, column %d: expected method name in interface, got %s` |
| 36 | `parseInterfaceDefinition` | decl.go:916 | saveError | `E_GENERAL` | `line %d, column %d: expected '(' after interface method name, got %s` |
| 37 | `parseInterfaceDefinition` | decl.go:933 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter name in interface method, got %s` |
| 38 | `parseInterfaceDefinition` | decl.go:1033 | saveError | `E_GENERAL` | `line %d, column %d: expected result name in interface method, got %s` |
| 39 | `parseTaggedEnumDefinition` | decl.go:1112 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close variant annotation, got %s instead` |
| 40 | `parseTaggedEnumDefinition` | decl.go:1125 | saveError | `E_GENERAL` | `line %d, column %d: expected variant name in tagged enum, got %s instead` |
| 41 | `parseTaggedEnumDefinition` | decl.go:1178 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close variant annotation, got %s instead` |
| 42 | `parseStructDefinition` | decl.go:1287 | saveError | `E_GENERAL` | `line %d, column %d: expected '{' after struct name, got %s` |
| 43 | `parseStructDefinition` | decl.go:1312 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close field annotation, got %s instead` |
| 44 | `parseStructDefinition` | decl.go:1333 | saveError | `E_GENERAL` | `line %d, column %d: expected field name in struct definition, got %s instead` |
| 45 | `parseStructDefinition` | decl.go:1399 | saveError | `E_GENERAL` | `line %d, column %d: expected field type after %q, got %s instead` |
| 46 | `parseStructDefinition` | decl.go:1468 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close field annotation, got %s instead` |
| 47 | `parseFunctionDefinition` | decl.go:1527 | saveError | `E_GENERAL` | `line %d, column %d: expected generic parameter name, got %s instead` |
| 48 | `parseFunctionDefinition` | decl.go:1539 | saveError | `E_GENERAL` | `line %d, column %d: expected ',' or '>' in generic parameters, got %s instead` |
| 49 | `parseFunctionDefinition` | decl.go:1550 | saveError | `E_GENERAL` | `line %d, column %d: expected '=' after function name, got %s instead` |
| 50 | `parseFunctionBody` | decl.go:1568 | saveError | `E_GENERAL` | `line %d, column %d: expected left parenthesis, got %s instead` |
| 51 | `parseFunctionBody` | decl.go:1581 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter name, got %s instead` |
| 52 | `parseFunctionBody` | decl.go:1640 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis, got %s instead` |
| 53 | `parseFunctionBody` | decl.go:1651 | saveError | `E_GENERAL` | `line %d, column %d: expected right parenthesis, got %s instead` |
| 54 | `parseFunctionBody` | decl.go:1673 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter name, got %s instead` |
| 55 | `parseFunctionBody` | decl.go:1701 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis, got %s instead` |
| 56 | `parseFunctionBody` | decl.go:1712 | saveError | `E_GENERAL` | `line %d, column %d: expected right parenthesis, got %s instead` |
| 57 | `parseFunctionBody` | decl.go:1774 | saveError | `E_GENERAL` | `line %d, column %d: expected left brace, got %s instead` |
| 58 | `parseExternParamList` | decl.go:1837 | saveError | `E_GENERAL` | `line %d, column %d: expected '(' in extern declaration, got %s instead` |
| 59 | `parseExternParamList` | decl.go:1863 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter name in extern declaration, got %s instead` |
| 60 | `parseExternParamList` | decl.go:1884 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis in extern declaration, got %s instead` |
| 61 | `parseExternParamList` | decl.go:1893 | saveError | `E_GENERAL` | `line %d, column %d: expected right parenthesis in extern declaration, got %s instead` |
| 62 | `parseColonFunctionDefinition` | decl.go:1969 | saveError | `E_GENERAL` | `line %d, column %d: expected ':', got %s instead` |
| 63 | `parseColonMethodDefinition` | decl.go:1996 | saveError | `E_GENERAL` | `line %d, column %d: expected ':', got %s instead` |
| 64 | `validateMatchArmReturns` | desugar.go:145 | saveError | `E_GENERAL` | `line %d, column %d: match arm has no body, cannot determine return value` |
| 65 | `validateMatchArmReturns` | desugar.go:152 | saveError | `E_GENERAL` | `line %d, column %d: match arm in expression context must end with an expression` |
| 66 | `validateMatchArmReturns` | desugar.go:160 | saveError | `E_GENERAL` | `line %d, column %d: match arm never returns a value` |
| 67 | `validateMatchArmReturns` | desugar.go:168 | saveError | `E_GENERAL` | `line %d, column %d: match arm has inconsistent return types: %s vs %s` |
| 68 | `warnf` | error.go:151 | saveError | `E_GENERAL` | `(動態組裝)` |
| 69 | `parseExpression` | expr.go:135 | warnf | `W_DEPRECATED_BRACE` | `,
					` |
| 70 | `parseExpression` | expr.go:254 | saveError | `E_GENERAL` | `line %d, column %d: unexpected 'as' at start of expression` |
| 71 | `parseExpression` | expr.go:399 | saveError | `E_GENERAL` | `line %d, column %d: illegal token %q` |
| 72 | `parseExpression` | expr.go:416 | saveError | `E_GENERAL` | `line %d, column %d: expected identifier after dot, got %s instead` |
| 73 | `parseExpression` | expr.go:447 | saveError | `E_GENERAL` | `line %d, column %d: expected ',' or '>' in generic args, got %s instead` |
| 74 | `parseExpression` | expr.go:455 | saveError | `E_GENERAL` | `line %d, column %d: expected '(' after generic args, got %s instead` |
| 75 | `parseExpression` | expr.go:474 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis, got %s instead` |
| 76 | `parseExpression` | expr.go:560 | saveError | `E_GENERAL` | `line %d, column %d: expected type after 'as', got %s instead` |
| 77 | `parseExpression` | expr.go:567 | saveError | `E_GENERAL` | `line %d, column %d: 'as' cast is only allowed for FFI pointer types (e.g., *byte, **byte), got %s` |
| 78 | `parseIntegerLiteral` | expr.go:678 | saveError | `E_GENERAL` | `line %d, column %d: could not parse %q as integer` |
| 79 | `parseFloatLiteral` | expr.go:742 | saveError | `E_GENERAL` | `line %d, column %d: could not parse %q as float` |
| 80 | `parseInfixExpression` | expr.go:804 | saveError | `E_GENERAL` | `line %d, column %d: expected expression after operator '%s'` |
| 81 | `parseConditionalExpression` | expr.go:831 | saveError | `E_GENERAL` | `line %d, column %d: expected colon in conditional expression, got %s instead` |
| 82 | `parseGroupedExpression` | expr.go:875 | saveError | `E_GENERAL` | `line %d, column %d: expected ')' or ']' to close range, got %s instead` |
| 83 | `parseGroupedExpression` | expr.go:913 | saveError | `E_GENERAL` | `line %d, column %d: expected ')' or ']' to close range, got %s instead` |
| 84 | `parseGroupedExpression` | expr.go:929 | saveError | `E_GENERAL` | `line %d, column %d: expected right parenthesis, got %s instead` |
| 85 | `parseMatchExprFrom` | expr.go:978 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close annotation, got %s` |
| 86 | `parseMatchExprFrom` | expr.go:1442 | saveError | `E_GENERAL` | `line %d, column %d: option match must handle all branches: err, nil, ok (missing: %s)` |
| 87 | `parseIfExpression` | expr.go:1491 | saveError | `E_GENERAL` | `line %d, column %d: expected left brace, got %s instead` |
| 88 | `parseIfExpression` | expr.go:1514 | saveError | `E_GENERAL` | `line %d, column %d: expected left brace, got %s instead` |
| 89 | `parseElifBlock` | expr.go:1602 | saveError | `E_GENERAL` | `line %d, column %d: expected left brace in elif, got %s instead` |
| 90 | `parseElifBlock` | expr.go:1625 | saveError | `E_GENERAL` | `line %d, column %d: expected left brace in else, got %s instead` |
| 91 | `parseArrayLiteral` | expr.go:1681 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right bracket in array literal, got %s instead` |
| 92 | `parseSliceLiteral` | expr.go:1782 | saveError | `E_GENERAL` | `line %d, column %d: expected ']' or ')' to close range, got %s instead` |
| 93 | `parseSliceLiteral` | expr.go:1821 | saveError | `E_GENERAL` | `line %d, column %d: expected ']' or ')' to close range, got %s instead` |
| 94 | `parseSliceLiteral` | expr.go:1862 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right bracket in slice literal, got %s instead` |
| 95 | `parseMapLiteral` | expr.go:1916 | saveError | `E_GENERAL` | `line %d, column %d: expected ':' in map literal, got %s instead` |
| 96 | `parseStructLiteral` | expr.go:2003 | saveError | `E_GENERAL` | `line %d, column %d: expected field name in struct literal, got %s instead` |
| 97 | `parseFunctionLiteral` | expr.go:2087 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter name to be identifier` |
| 98 | `parseFunctionLiteral` | expr.go:2115 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter type, got %s instead` |
| 99 | `parseFunctionLiteral` | expr.go:2142 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis, got %s instead` |
| 100 | `parseFunctionLiteral` | expr.go:2155 | saveError | `E_GENERAL` | `line %d, column %d: expected left brace, got %s instead` |
| 101 | `parseCallExpression` | expr.go:2205 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis, got %s instead` |
| 102 | `walk` | lowering.go:144 | saveError | `E_GENERAL` | `line %d, column %d: `?=` is a statement operator and cannot be used as an expression` |
| 103 | `maybeIndexOutAssign` | lowering.go:1024 | saveError | `E_GENERAL` | `line %d, column %d: cannot infer element type for safe index with #{index-out}; annotate the container or use an explicit type` |
| 104 | `maybeIndexOutAssign` | lowering.go:1041 | saveError | `E_GENERAL` | `line %d, column %d: %s` |
| 105 | `rejectItInNonOkArm` | lowering.go:1594 | saveError | `E_GENERAL` | `line %d, column %d: `it` is not valid in this arm: the catch-all `->` arm may receive %s; claim them with explicit arms (e.g. `nil ->` / `err ->`) or use `ok ->` before reading `it`` |
| 106 | `lowerUnwrapAssign` | lowering.go:2970 | saveError | `E_GENERAL` | `line %d, column %d: `?=` RHS `%s` is not an option — use `=` instead` |
| 107 | `lowerUnwrapAssign` | lowering.go:3035 | saveError | `E_GENERAL` | `line %d, column %d: safe-index `?=` only supports direct array/vec variables, not struct fields (receiver.field[i])` |
| 108 | `lowerUnwrapAssign` | lowering.go:3169 | saveError | `E_GENERAL` | `line %d, column %d: `?=` can only be used inside a function with an option-typed result param` |
| 109 | `parseTildeMatchStatement` | match.go:48 | saveError | `E_GENERAL` | `line %d, column %d: expected '{' after ~match expression, got %s` |
| 110 | `parseTildeMatchStatement` | match.go:78 | saveError | `E_GENERAL` | `line %d, column %d: expected 'case' or 'default', got %s` |
| 111 | `parseTildeMatchStatement` | match.go:123 | saveError | `E_GENERAL` | `line %d, column %d: empty ~match statement` |
| 112 | `parseBareMatchExpr` | match.go:187 | saveError | `E_GENERAL` | `line %d, column %d: expected '}' to close annotation, got %s` |
| 113 | `parseBareMatchExpr` | match.go:505 | saveError | `E_GENERAL` | `line %d, column %d: option match must handle all branches: err, nil, ok (missing: %s)` |
| 114 | `parseMatchExpression` | match.go:732 | saveError | `E_GENERAL` | `line %d, column %d: expected '{' in match expression, got %s` |
| 115 | `parseMatchExpression` | match.go:814 | saveError | `E_GENERAL` | `line %d, column %d: expected ':' after match pattern, got '->' instead` |
| 116 | `parseMatchExpression` | match.go:820 | saveError | `E_GENERAL` | `line %d, column %d: expected ':' after match pattern, got %s` |
| 117 | `parseMatchExpression` | match.go:904 | saveError | `E_GENERAL` | `line %d, column %d: empty match expression` |
| 118 | `parseMatchExpression` | match.go:943 | saveError | `E_GENERAL` | `line %d, column %d: option match must handle all branches: err, nil, ok (missing: %s)` |
| 119 | `peekError` | parser.go:1411 | errorf | `E_UNEXPECTED_TOKEN` | `,
		` |
| 120 | `saveError` | parser.go:1550 | saveError | `E_GENERAL` | `(動態組裝)` |
| 121 | `parseStatement` | stmt.go:322 | saveError | `E_GENERAL` | `line %d, column %d: '%s.%s []T' is not allowed: the field type belongs in the type definition of '%s'. To store into the field write '%s.%s = ...'` |
| 122 | `parseUseStatement` | stmt.go:586 | saveError | `E_GENERAL` | `line %d, column %d: expected identifier in use path, got %s` |
| 123 | `parseExportStatement` | stmt.go:670 | saveError | `E_GENERAL` | `line %d, column %d: expected identifier in export path, got %s` |
| 124 | `parseExportStatement` | stmt.go:713 | saveError | `E_GENERAL` | `line %d, column %d: export alias '%s' is the same as the function name; the alias can be omitted` |
| 125 | `parseMultiAssignStatement` | stmt.go:783 | saveError | `E_GENERAL` | `line %d, column %d: expected variable name after ',', got %s` |
| 126 | `parseMultiAssignStatement` | stmt.go:792 | saveError | `E_GENERAL` | `line %d, column %d: expected '=' after multi-variable list` |
| 127 | `parseLetStatement` | stmt.go:1064 | saveError | `E_GENERAL` | `line %d, column %d: expected assignment operator, got %s instead` |
| 128 | `parseLetStatement` | stmt.go:1175 | saveError | `E_GENERAL` | `line %d, column %d: expected expression, got nil instead` |
| 129 | `parseReturnStatement` | stmt.go:1435 | saveError | `E_GENERAL` | `line %d, column %d: `return` 後不能跟返回值；Nolang 函數結果通過具名結果參數（out-param）傳出，請用裸 `return` 提前返回並在函數體內給結果參數賦值` |
| 130 | `parseSwitchStatement` | stmt.go:1460 | saveError | `E_GENERAL` | `line %d, column %d: expected '{' after switch expression, got %s` |
| 131 | `parseSwitchStatement` | stmt.go:1484 | saveError | `E_GENERAL` | `line %d, column %d: expected 'case' or 'default', got %s` |
| 132 | `parseSwitchStatement` | stmt.go:1542 | saveError | `E_GENERAL` | `line %d, column %d: empty switch statement` |
| 133 | `parseExpressionStatement` | stmt.go:1750 | fatalf | `E_EXPECTED_IDENT` | `,
					` |
| 134 | `parseExpressionStatement` | stmt.go:1756 | fatalf | `E_EXPECTED_ASSIGN` | `,
				` |
| 135 | `parseLoopBody` | stmt.go:2222 | saveError | `E_GENERAL` | `line %d, column %d: expected loop body statement, got %s` |
| 136 | `parseForStatement` | stmt.go:2285 | saveError | `E_GENERAL` | `line %d, column %d: expected 'for', got %s` |
| 137 | `parseForRange` | stmt.go:2491 | saveError | `E_GENERAL` | `line %d, column %d: expected '..' for range or ','/']' for slice, got %s instead` |
| 138 | `parseForRange` | stmt.go:2512 | saveError | `E_GENERAL` | `line %d, column %d: expected '[' or '(' or string in range expression, got %s instead` |
| 139 | `parseRangeBody` | stmt.go:2533 | saveError | `E_GENERAL` | `line %d, column %d: float range boundary not supported, use integers` |
| 140 | `parseRangeBody` | stmt.go:2538 | saveError | `E_GENERAL` | `line %d, column %d: expected '..' in range expression, got %s instead` |
| 141 | `parseRangeBody` | stmt.go:2548 | saveError | `E_GENERAL` | `line %d, column %d: float range boundary not supported, use integers` |
| 142 | `parseRangeBody` | stmt.go:2560 | saveError | `E_GENERAL` | `line %d, column %d: expected ']' or ')' in range expression, got %s instead` |
| 143 | `parseCondLoopPrefix` | stmt.go:2698 | saveError | `E_GENERAL` | `line %d, column %d: expected ')' to close loop condition, got %s` |
| 144 | `parseCondLoopPrefix` | stmt.go:2705 | saveError | `E_GENERAL` | `line %d, column %d: expected '{' to open loop body, got %s` |
| 145 | `parseCountLoopPrefix` | stmt.go:2753 | saveError | `E_GENERAL` | `line %d, column %d: expected integer count before '*' in counted loop, got %s` |
| 146 | `parseCountLoopPrefix` | stmt.go:2759 | saveError | `E_GENERAL` | `line %d, column %d: could not parse %q as integer` |
| 147 | `parseCountLoopPrefix` | stmt.go:2770 | saveError | `E_GENERAL` | `line %d, column %d: expected '*' between count and loop body, got %s` |
| 148 | `parseCountLoopPrefix` | stmt.go:2776 | saveError | `E_GENERAL` | `line %d, column %d: expected '{' to open counted loop body, got %s` |
| 149 | `parseLabeledStatement` | stmt.go:2853 | saveError | `E_GENERAL` | `line %d, column %d: expected loop body after label #%s, got '(' without '(cond) { }'` |
| 150 | `parseLabeledStatement` | stmt.go:2862 | saveError | `E_GENERAL` | `line %d, column %d: expected loop body after label #%s, got %s without '{ }'` |
| 151 | `parseLabeledStatement` | stmt.go:2871 | saveError | `E_GENERAL` | `line %d, column %d: expected loop body after label #%s, got %s without '* N' or '* { }'` |
| 152 | `parseLabeledStatement` | stmt.go:2883 | saveError | `E_GENERAL` | `line %d, column %d: expected loop body after label #%s, got { without '* N' or '(cond)' or '(true)'` |
| 153 | `parseLabeledStatement` | stmt.go:2919 | saveError | `E_GENERAL` | `line %d, column %d: expected loop body after label #%s, got %s` |
| 154 | `parseCountedLoopBlockFirst` | stmt.go:3016 | saveError | `E_GENERAL` | `line %d, column %d: expected '*' after block in counted loop, got %s` |
| 155 | `parseCountedLoopBlockFirst` | stmt.go:3028 | saveError | `E_GENERAL` | `line %d, column %d: expected integer count after '*' in counted loop, got %s` |
| 156 | `parseCountedLoopBlockFirst` | stmt.go:3037 | saveError | `E_GENERAL` | `line %d, column %d: could not parse %q as integer` |
| 157 | `parseCondLoopBlockFirst` | stmt.go:3152 | saveError | `E_GENERAL` | `line %d, column %d: expected '(' after block in loop, got %s` |
| 158 | `parseCondLoopBlockFirst` | stmt.go:3168 | saveError | `E_GENERAL` | `line %d, column %d: expected ')' to close loop condition, got %s` |
| 159 | `parseFunctionTypeAlias` | type.go:505 | saveError | `E_GENERAL` | `line %d, column %d: expected '=' in function type alias, got %s` |
| 160 | `parseFunctionTypeAlias` | type.go:513 | saveError | `E_GENERAL` | `line %d, column %d: expected '(' after '=' in function type alias, got %s` |
| 161 | `parseTypeAlias` | type.go:685 | saveError | `E_GENERAL` | `line %d, column %d: expected type after %q in type alias` |
| 162 | `parseTypeAlias` | type.go:702 | saveError | `E_GENERAL` | `line %d, column %d: expected type after '\|'` |
| 163 | `parseFunctionType` | type.go:850 | saveError | `E_GENERAL` | `line %d, column %d: expected '(' to start function type, got %s instead` |
| 164 | `parseFunctionType` | type.go:876 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis, got %s instead` |
| 165 | `parseFunctionType` | type.go:912 | saveError | `E_GENERAL` | `line %d, column %d: expected comma or right parenthesis, got %s instead` |
| 166 | `parseFunctionTypeEntry` | type.go:938 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter name or type, got %s instead` |
| 167 | `parseParamTypeAfterName` | type.go:988 | saveError | `E_GENERAL` | `line %d, column %d: anonymous function type syntax '()' is disabled; use a named function type alias or enable 'anonymous-fn-type' in package.jsonc` |
| 168 | `parseParamTypeAfterName` | type.go:1075 | saveError | `E_GENERAL` | `line %d, column %d: expected parameter type, got %s instead` |
