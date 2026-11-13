# Nolang AST 節點清單、文法與錯誤訊息

> 由 `scripts/ast_inventory.py` **直接從 Go 原始碼機械抽取**，無人工轉錄。
> 來源：`src/parser/ast.go`（節點定義）、`src/parser/*.go`（建構點、錯誤點）、
> `src/parser/dump/dump.go`（L2 oracle 的封閉型別 switch）。

## 0. 總覽

| 項目 | 數量 |
|---|---|
| `ast.go` 內 struct 總數 | 81 |
| 實作 `Node`（Pos+EndPos） | 73 |
| 　└ `Statement` | 19 |
| 　└ `Expression` | 32 |
| 　└ `Type` | 9 |
| 　└ 純 `Node`（無 marker） | 13 |
| 非 `Node` 輔助 struct | 8 |
| `dumpNode()` case 數 | 74 |
| ❌ **oracle 未覆蓋的 Node** | **0** |
| ⚠️ 只印「殼」的節點（無遞迴） | 23 |
| `nodeString()` case 數 | 7 |
| 錯誤訊息站點（`saveError` + 結構化 API） | 10 |

## 1. 未被 oracle 覆蓋的節點（預期為空 ⇒ 空表就是綠燈）

> 這一節由 `dumpNode` 的 case 清單與 `ast.go` 的 Node 清單相減而得。
> **空 = `no ast` 對每個節點型別都有 case**（2026-10-10 補齊，見設計文檔 §5.3.1）。
> 非空 ⇒ 那些節點的**整個子樹**在 L2 差分中不可見。

| # | 節點 | ast.go 行 | 種類 | 欄位 | 建構函式 |
|---|---|---|---|---|---|

## 2. 只印「殼」的節點（dump 有 case，但無遞迴欄位 ⇒ 子樹不可見）

| 節點 | 行 | 種類 | dump 引用到的欄位 | **未印出的欄位** |
|---|---|---|---|---|
| `EmptyStatement` | 850 | Statement | Literal | — |
| `UseStatement` | 604 | Statement | Alias, AsKeyword, Function, Path | — |
| `ExportStatement` | 618 | Statement | Alias, AsKeyword, Function, Path | — |
| `AnnotationStatement` | 1328 | Statement | Entries | — |
| `BreakStatement` | 1882 | Statement | Label, LabelKind | — |
| `ContinueStatement` | 1895 | Statement | Label, LabelKind | — |
| `InterfaceDefinition` | 2160 | Statement | Implements, Methods, Name | — |
| `Identifier` | 788 | Expression | IsImplicitGeneric, Value | — |
| `IntegerLiteral` | 1511 | Expression | Raw, Value | — |
| `FloatLiteral` | 1524 | Expression | Raw, Value | — |
| `StringLiteral` | 1534 | Expression | Raw, Value | — |
| `CharLiteral` | 1544 | Expression | Raw, Value | — |
| `RegexLiteral` | 1564 | Expression | Flags, Pattern | — |
| `BooleanLiteral` | 1574 | Expression | Value | — |
| `NilLiteral` | 1583 | Expression | — | — |
| `EnumValue` | 1997 | Node | Explicit, Name, Value | — |
| `NamedType` | 36 | Type | IsInferred, Value | — |
| `AnnotationBoolValue` | 1107 | Node | Value | — |
| `AnnotationIntValue` | 1123 | Node | Value | — |
| `AnnotationStringValue` | 1134 | Node | Value | — |
| `AnnotationIdentValue` | 1145 | Node | Value | — |
| `AnnotationStructValue` | 1183 | Node | Fields, Type | — |
| `AnnotationEntry` | 1249 | Node | — | `Key`, `Value`, `Trailing`, `Propagated` |

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
| `BlockMatchStatement` | `Body`, `Match` |
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
| `Parameter` | `Type` |
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
| `SurfaceMatch` | `Arms`, `Matched` |

## 4. 非 Node 輔助 struct（被節點欄位引用，本身不進 AST 走訪）

| struct | 行 | 欄位 |
|---|---|---|
| `Comment` | 410 | `Pos`:lexer.Position, `End`:lexer.Position, `Kind`:CommentKind, `Text`:string, `Marker`:string |
| `CommentGroup` | 420 | `List`:[]*Comment, `Start`:lexer.Position, `End`:lexer.Position |
| `AnnotatedNode` | 444 | `Annotations`:[]*AnnotationEntry |
| `CommentedNode` | 476 | `Doc`:*CommentGroup, `Comment`:*CommentGroup |
| `FuncSignature` | 949 | `Parameters`:[]*Parameter, `Results`:[]*Parameter, `GenericParams`:[]*Identifier, `IsVariadic`:bool |
| `AnnotationStructField` | 1190 | `Name`:string, `Value`:AnnotationValue |
| `TaggedEnumField` | 2083 | `Token`:lexer.Token, `Name`:string, `Type`:Type |
| `InterfaceMethod` | 2150 | `Token`:lexer.Token, `Name`:string, `Parameters`:[]*Parameter, `IsVariadic`:bool, `Results`:[]*Parameter, `Receiver`:string, `IsGenericReceiver`:bool |

## 5. 節點 ← 建構函式（文法歸屬）

| 節點 | 種類 | 建構它的函式 |
|---|---|---|
| `Comment` | - | commentGroupFromTokens, parseBareMatchExpr, parseBlockStatement |
| `CommentGroup` | - | commentGroupFromTokens, dropCommentsUpTo, parseBareMatchExpr, parseBlockStatement |
| `InterfaceMethod` | - | parseInterfaceDefinition |
| `TaggedEnumField` | - | parseTaggedEnumDefinition |
| `ArrayLiteral` | Expression | parseArrayLiteral, parseLetStatement, zeroValueLiteralFor |
| `AssignExpression` | Expression | blockMatchTail, lowerUnwrapAssign, parseExpression, rewriteItPlaceWritesStmts |
| `AwaitExpression` | Expression | parseExpression |
| `BlockMatchStatement` | Expression | parseBlockMatchTail |
| `BooleanLiteral` | Expression | parseBangLoop, parseBoolLiteralLoop, parseCondLoopBlockFirst, parseCondLoopPrefix, parseExpression, parseForStatement, parseMatchExprFrom, scalarZeroLiteral |
| `CallExpression` | Expression | OperatorOverloadReturnType, guardInArithmetic, guardInCallArg, guardOptionOperands, maybeDesugarInfix, parseCallExpression, parseExpression |
| `CastExpression` | Expression | parseExpression |
| `CharLiteral` | Expression | parseExpression, parseLetStatement |
| `CoExpression` | Expression | parseExpression |
| `ConditionalExpression` | Expression | parseConditionalExpression |
| `DotExpression` | Expression | OperatorOverloadReturnType, maybeDesugarInfix, parseExpression, rewriteSubjectArm |
| `FloatLiteral` | Expression | parseFloatLiteral, scalarZeroLiteral |
| `FunctionLiteral` | Expression | parseFunctionLiteral |
| `GroupedExpression` | Expression | parseGroupedExpression |
| `Identifier` | Expression | ResolveProgram, addCaptureNode, addImplicitGeneric, bindAndGuard, blockMatchGuard, blockMatchTail, buildCaptureChain, buildItBinding, buildItBindingForArm, buildType, emitSubjectMatchPlain, emitSubjectMatchWrappersHIR, expandBlockMatch, lowerCaptureToHIR, lowerUnwrapAssign, lowerUnwrapAssignToIndex, makePropagateGuard, namedItBinding, parseAnnotationFFIDeclaration, parseAssignTarget, parseBareMatchExpr, parseCompoundAssignStatement, parseElifBlock, parseExpression, parseFunctionDefinition, parseIdentifier, parseLetStatement, parseMatchExprFrom, parseTildeMatchStatement, parseTypeExpression, parseUnwrapAssignStatement, resolveArmCond, resolveBareMatchPlan, resolveSubjectMatch, resolveSubjectMatchPlan, resolveTypeMatchArms, rewriteItPlaceWritesStmts, rewriteSubjectArm, warnUnreachableArms |
| `IfExpression` | Expression | buildTypeMatchDesugar, emitMatchIfPlain, emitSubjectMatchPlain, guardStmt, lowerUnwrapAssign, parseElifBlock, parseExpressionStatement, parseIfExpression, parseLabeledStatement, parseLetStatement, wrapStandaloneChainDepth |
| `IndexExpression` | Expression | emitSubjectMatchPlain, emitSubjectMatchWrappersHIR, parseAssignTarget, parseSliceExpression, rewriteItPlaceWritesStmts |
| `InfixExpression` | Expression | blockMatchGuard, desugarRangeCondition, emitSubjectMatchPlain, guardInArithmetic, guardInCallArg, guardOptionOperands, parseCompoundAssignStatement, parseExpression, parseForStatement, parseInfixExpression, resolveArmCond, resolveSubjectMatchPlan |
| `IntegerLiteral` | Expression | blockMatchGuard, blockMatchTail, buildType, desugarRangeCondition, emitSubjectMatchPlain, emitSubjectMatchWrappersHIR, expandBlockMatch, parseCountLoopPrefix, parseCountedLoopBlockFirst, parseExpressionStatement, parseIntegerLiteral, parseLetStatement, parseTypeExpression, resolveBareMatchPlan, resolveSubjectMatchPlan, scalarZeroLiteral, zeroValueLiteralFor |
| `IterationExpr` | Expression | parseForStatement |
| `MapLiteral` | Expression | parseMapLiteral, zeroValueLiteralFor |
| `NilLiteral` | Expression | buildCaptureChain, expandBlockMatch, parseBareMatchExpr, parseExpression, parseNilLiteral |
| `Parameter` | Expression | parseArrayTypeMethodDefinition, parseColonMethodDefinition, parseExternParamList, parseFunctionBody, parseFunctionLiteral, parseFunctionTypeEntry, parseInterfaceDefinition, parseMethodDefinition |
| `PrefixExpression` | Expression | parsePrefixExpression |
| `RangeExpression` | Expression | parseForRange, parseGroupedExpression, parseRangeBody, parseSliceExpression, parseSliceLiteral |
| `RegexLiteral` | Expression | parseExpression |
| `RunExpression` | Expression | parseExpression |
| `SliceExpression` | Expression | parseSliceExpression |
| `SliceLiteral` | Expression | parseSliceLiteral |
| `StringLiteral` | Expression | parseStringLiteral, scalarZeroLiteral |
| `StructLiteral` | Expression | parseStructLiteral, zeroValueLiteralFor |
| `UnwrapAssignStatement` | Expression | lowerUnwrapAssignToIndex, parseCompoundAssignStatement, parseExpression, parseUnwrapAssignStatement |
| `AnnotationArrayValue` | Node | parseAnnotationArray |
| `AnnotationBoolValue` | Node | parseAnnotationSimpleValue |
| `AnnotationEntry` | Node | parseAnnotationBody |
| `AnnotationIdentValue` | Node | carryOverflowAnnotation, parseAnnotationSimpleValue |
| `AnnotationIntValue` | Node | parseAnnotationSimpleValue |
| `AnnotationRangeValue` | Node | parseAnnotationRange, parseAnnotationValue |
| `AnnotationStringValue` | Node | parseAnnotationSimpleValue |
| `AnnotationStructValue` | Node | parseAnnotationSimpleValue |
| `EnumValue` | Node | parseEnumDefinition |
| `Program` | Node | CloneProgram, ParseProgram, detToProgram |
| `StructField` | Node | parseStructDefinition, parseStructLiteral |
| `TaggedEnumVariant` | Node | parseTaggedEnumDefinition |
| `AnnotationStatement` | Statement | parseAnnotationStatement |
| `BlockStatement` | Statement | buildCaptureChain, buildTypeMatchDesugar, emitMatchIfPlain, emitSubjectMatchPlain, expandBlockMatch, guardStmt, lowerUnwrapAssign, lowerUnwrapAssignToIndex, makePropagateGuard, parseBareMatchExpr, parseBlockStatement, parseElifBlock, parseLabeledStatement, parseLoopBody, parseMatchExprFrom, parseStandaloneBody, parseSwitchStatement, parseTildeMatchStatement, prependStmt, resolveSubjectMatch, rewriteItPlaceWrites, wrapLoopBody, wrapStandaloneChainDepth |
| `BreakStatement` | Statement | parseBreakStatement, parseStarStarBreak |
| `ContinueStatement` | Statement | parseContinueStatement, parseStarContinueOrExpr |
| `EmptyStatement` | Statement | expandUnwrap, guardStmt, parseEmptyStatement |
| `EnumDefinition` | Statement | parseEnumDefinition |
| `ExportStatement` | Statement | parseExportStatement |
| `ExpressionStatement` | Statement | blockMatchTail, buildCaptureChain, buildTypeMatchDesugar, emitMatchIfPlain, emitSubjectMatchPlain, expandBlockMatch, guardStmt, lowerUnwrapAssign, lowerUnwrapAssignToIndex, makePropagateGuard, parseBlockStatement, parseElifBlock, parseExpressionStatement, parseLabeledStatement, parseStandaloneBody, parseStatement, parseSwitchStatement, parseTildeMatchStatement, resolveSubjectMatch, rewriteItPlaceWritesStmts, wrapStandaloneChainDepth |
| `ExternStatement` | Statement | parseAnnotationFFIDeclaration |
| `ForStatement` | Statement | parseBangLoop, parseBoolLiteralLoop, parseCondLoopBlockFirst, parseCondLoopPrefix, parseCountLoopPrefix, parseCountedLoopBlockFirst, parseExpressionStatement, parseForStatement, parseLabeledStatement |
| `FunctionDefinition` | Statement | parseArrayTypeMethodDefinition, parseColonFunctionDefinition, parseColonMethodDefinition, parseFunctionDefinition |
| `InterfaceDefinition` | Statement | parseInterfaceDefinition |
| `LetStatement` | Statement | bindAndGuard, buildCaptureChain, buildItBinding, buildItBindingForArm, emitSubjectMatchPlain, emitSubjectMatchWrappersHIR, expandBlockMatch, lowerUnwrapAssign, lowerUnwrapAssignToIndex, makePropagateGuard, parseLetStatement, resolveTypeMatchArms, rewriteItPlaceWritesStmts, rewriteSubjectArm |
| `MultiAssignStatement` | Statement | parseExpressionStatement, parseMultiAssignStatement |
| `ReturnStatement` | Statement | blockMatchTail, lowerUnwrapAssign, parseReturnStatement |
| `StructDefinition` | Statement | parseStructDefinition |
| `TaggedEnumDefinition` | Statement | parseTaggedEnumDefinition |
| `TypeAlias` | Statement | parseFunctionTypeAlias, parseTypeAlias |
| `UseStatement` | Statement | parseUseStatement |
| `ArrayType` | Type | buildType, parseLetStatement, parseTypeExpression |
| `FunctionType` | Type | parseFunctionType |
| `MapType` | Type | buildType, parseLetStatement, parseMapTypeString, parseTypeExpression |
| `NamedType` | Type | bindAndGuard, buildCaptureChain, buildItBindingForArm, buildType, expandBlockMatch, exprToType, lowerUnwrapAssign, parseFunctionTypeEntry, parseLetStatement, parseMapTypeString, parseTypeExpression, resolveSubjectMatch, resolveTypeMatchArms, typeFromInferredString |
| `NullableType` | Type | bindAndGuard, buildCaptureChain, buildType, expandBlockMatch, lowerUnwrapAssign, parseLetStatement, parseNullableType, parseTypeExpression, typeFromInferredString |
| `PointerType` | Type | buildType, parseExternType, parsePointerType, parseTypeExpression |
| `SliceType` | Type | buildType, parseLetStatement, parseTypeExpression |
| `UnionType` | Type | parseTypeAlias |
| `ViewType` | Type | buildType |

## 6. 錯誤訊息目錄（供 `noc` 逐字重現）

| # | 函式 | 位置 | 通道 | 錯誤碼 | 訊息格式 |
|---|---|---|---|---|---|
| 1 | `warnfFix` | error.go:330 | saveError | `E_GENERAL` | `(動態組裝)` |
| 2 | `parseExpression` | expr.go:135 | warnf | `b9cf2s8p` | `, ` |
| 3 | `parseExpression` | expr.go:218 | errorf | `elps0exp` | `, ` |
| 4 | `peekError` | parser.go:1666 | errorf | `m1yd7r5x` | `, ` |
| 5 | `saveError` | parser.go:1905 | saveError | `E_GENERAL` | `(動態組裝)` |
| 6 | `parseEmptyStatement` | stmt.go:1794 | errorf | `elps0ln0` | `, ` |
| 7 | `parseEmptyStatement` | stmt.go:1803 | errorf | `elps1ln1` | `, ` |
| 8 | `parseExpressionStatement` | stmt.go:2117 | fatalf | `q3v8kn2m` | `, ` |
| 9 | `parseExpressionStatement` | stmt.go:2123 | fatalf | `w6zt4h9c` | `, ` |
| 10 | `rangeTripleDotError` | stmt.go:3087 | errorf | `elps1rng` | `, ` |
