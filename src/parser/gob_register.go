package parser

import "encoding/gob"

// registerNodeTypesForGob registers every concrete AST node type so that
// *parser.Program can be serialized with encoding/gob for the incremental-build
// AST cache (Tier 0: baked std AST in the compiler binary; Tier 1: on-disk
// user-module AST cache).
//
// gob requires all concrete types that ever appear behind an interface
// (Statement / Expression / Type / Node) to be registered; otherwise Encode or
// Decode panics with "type not registered for interface: parser.X". The node
// types are all stored by pointer, so we register the pointer types.
//
// When a new node type is added to ast.go it MUST be added here too, or the
// AST-cache tests (TestProgramGobRoundTrip) will fail fast.
// gobNodeTypes 是所有可能出現在介面背後的具體節點型別，供 gob 註冊與
// MetaMessage 的 kind 註冊表共用（見 mm_kinds.go）：兩者必須完全一致，
// 否則 mm 反序列化會對某些 kind 找不到型別。
var gobNodeTypes = []any{
	// ---- Statements ----
	&UseStatement{},
	&ExportStatement{},
	&MultiAssignStatement{},
	&UnwrapAssignStatement{},
	&LetStatement{},
	&ReturnStatement{},
	&ExpressionStatement{},
	&BlockStatement{},
	&FunctionDefinition{},
	&ExternStatement{},
	&AnnotationStatement{},
	&ForStatement{},
	&BreakStatement{},
	&ContinueStatement{},
	&EnumDefinition{},
	&TypeAlias{},
	&TaggedEnumDefinition{},
	&InterfaceDefinition{},
	&StructDefinition{},

	// ---- Expressions ----
	&Identifier{},
	&Parameter{},
	&FunctionLiteral{},
	&CallExpression{},
	&DotExpression{},
	&GroupedExpression{},
	&IntegerLiteral{},
	&FloatLiteral{},
	&StringLiteral{},
	&CharLiteral{},
	&RegexLiteral{},
	&BooleanLiteral{},
	&NilLiteral{},
	&PrefixExpression{},
	&InfixExpression{},
	&RunExpression{},
	&CoExpression{},
	&AwaitExpression{},
	&IfExpression{},
	&RangeExpression{},
	&SliceExpression{},
	&IndexExpression{},
	&AssignExpression{},
	&ConditionalExpression{},
	&CastExpression{},
	&IterationExpr{},
	&ArrayLiteral{},
	&MapLiteral{},
	&SliceLiteral{},
	&StructLiteral{},

	// ---- Types ----
	&NamedType{},
	&ArrayType{},
	&SliceType{},
	&MapType{},
	&FunctionType{},
	&NullableType{},
	&PointerType{},
	&ViewType{},
	&UnionType{},

	// ---- Annotation values (behind the AnnotationValue interface in
	// AnnotationEntry.Value) ----
	&AnnotationBoolValue{},
	&AnnotationIntValue{},
	&AnnotationStringValue{},
	&AnnotationIdentValue{},
	&AnnotationArrayValue{},
	&AnnotationStructValue{},
	&AnnotationRangeValue{},
}

func init() {
	for _, t := range gobNodeTypes {
		gob.Register(t)
	}
}
