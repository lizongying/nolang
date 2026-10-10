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
func init() {
	// ---- Statements ----
	gob.Register(&UseStatement{})
	gob.Register(&ExportStatement{})
	gob.Register(&MultiAssignStatement{})
	gob.Register(&UnwrapAssignStatement{})
	gob.Register(&LetStatement{})
	gob.Register(&ReturnStatement{})
	gob.Register(&ExpressionStatement{})
	gob.Register(&BlockStatement{})
	gob.Register(&FunctionDefinition{})
	gob.Register(&ExternStatement{})
	gob.Register(&AnnotationStatement{})
	gob.Register(&ForStatement{})
	gob.Register(&BreakStatement{})
	gob.Register(&ContinueStatement{})
	gob.Register(&EnumDefinition{})
	gob.Register(&TypeAlias{})
	gob.Register(&TaggedEnumDefinition{})
	gob.Register(&InterfaceDefinition{})
	gob.Register(&StructDefinition{})

	// ---- Expressions ----
	gob.Register(&Identifier{})
	gob.Register(&Parameter{})
	gob.Register(&FunctionLiteral{})
	gob.Register(&CallExpression{})
	gob.Register(&DotExpression{})
	gob.Register(&GroupedExpression{})
	gob.Register(&IntegerLiteral{})
	gob.Register(&FloatLiteral{})
	gob.Register(&StringLiteral{})
	gob.Register(&CharLiteral{})
	gob.Register(&RegexLiteral{})
	gob.Register(&BooleanLiteral{})
	gob.Register(&NilLiteral{})
	gob.Register(&PrefixExpression{})
	gob.Register(&InfixExpression{})
	gob.Register(&RunExpression{})
	gob.Register(&CoExpression{})
	gob.Register(&AwaitExpression{})
	gob.Register(&IfExpression{})
	gob.Register(&RangeExpression{})
	gob.Register(&SliceExpression{})
	gob.Register(&IndexExpression{})
	gob.Register(&AssignExpression{})
	gob.Register(&ConditionalExpression{})
	gob.Register(&CastExpression{})
	gob.Register(&IterationExpr{})
	gob.Register(&ArrayLiteral{})
	gob.Register(&MapLiteral{})
	gob.Register(&SliceLiteral{})
	gob.Register(&StructLiteral{})

	// ---- Types ----
	gob.Register(&NamedType{})
	gob.Register(&ArrayType{})
	gob.Register(&SliceType{})
	gob.Register(&MapType{})
	gob.Register(&FunctionType{})
	gob.Register(&NullableType{})
	gob.Register(&PointerType{})
	gob.Register(&ViewType{})
	gob.Register(&UnionType{})

	// ---- Annotation values (behind the AnnotationValue interface in
	// AnnotationEntry.Value) ----
	gob.Register(&AnnotationBoolValue{})
	gob.Register(&AnnotationIntValue{})
	gob.Register(&AnnotationStringValue{})
	gob.Register(&AnnotationIdentValue{})
	gob.Register(&AnnotationArrayValue{})
	gob.Register(&AnnotationStructValue{})
	gob.Register(&AnnotationRangeValue{})
}
