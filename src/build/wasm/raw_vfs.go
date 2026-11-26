package wasm

import (
	"bytes"
	"reflect"

	"github.com/lizongying/nolang/parser"
)

// rawVFSParameter describes the host-side representation of one raw VFS
// argument. Pointer/length pairs use the Direct-WASM descriptor layout for
// both str and []byte; byte elements occupy one i64 slot in this backend.
type rawVFSParameter uint8

const (
	rawParamI64 rawVFSParameter = iota
	rawParamPtrLen
	rawParamBool
)

type rawVFSSpec struct {
	name     string
	hostName string
	params   []rawVFSParameter
	result   ValType
	option   bool
	boolean  bool
}

var rawVFSSpecs = []rawVFSSpec{
	{name: "raw-open", hostName: "raw_open", params: []rawVFSParameter{rawParamPtrLen, rawParamI64, rawParamI64}, result: I64, option: true},
	{name: "raw-close", hostName: "raw_close", params: []rawVFSParameter{rawParamI64}, result: I64, option: true, boolean: true},
	{name: "raw-seek", hostName: "raw_seek", params: []rawVFSParameter{rawParamI64, rawParamI64, rawParamI64}, result: I64, option: true},
	{name: "raw-pread", hostName: "raw_pread", params: []rawVFSParameter{rawParamI64, rawParamPtrLen, rawParamI64, rawParamI64}, result: I64, option: true},
	{name: "raw-pwrite", hostName: "raw_pwrite", params: []rawVFSParameter{rawParamI64, rawParamPtrLen, rawParamI64, rawParamI64}, result: I64, option: true},
	{name: "raw-ftruncate", hostName: "raw_ftruncate", params: []rawVFSParameter{rawParamI64, rawParamI64}, result: I64, option: true, boolean: true},
	{name: "raw-fstat-size", hostName: "raw_fstat_size", params: []rawVFSParameter{rawParamI64}, result: I64, option: true},
	{name: "raw-fsync", hostName: "raw_fsync", params: []rawVFSParameter{rawParamI64}, result: I64, option: true, boolean: true},
	{name: "raw-fdatasync", hostName: "raw_fdatasync", params: []rawVFSParameter{rawParamI64}, result: I64, option: true, boolean: true},
	{name: "raw-sync-dir", hostName: "raw_sync_dir", params: []rawVFSParameter{rawParamPtrLen}, result: I64, option: true, boolean: true},
	{name: "raw-lock-range", hostName: "raw_lock_range", params: []rawVFSParameter{rawParamI64, rawParamI64, rawParamI64, rawParamI64, rawParamBool}, result: I64, option: true, boolean: true},
	{name: "raw-unlock-range", hostName: "raw_unlock_range", params: []rawVFSParameter{rawParamI64, rawParamI64, rawParamI64}, result: I64, option: true, boolean: true},
	{name: "raw-capabilities", hostName: "raw_capabilities", result: I64},
}

func rawVFSSpecByName(name string) (rawVFSSpec, bool) {
	for _, spec := range rawVFSSpecs {
		if spec.name == name {
			return spec, true
		}
	}
	return rawVFSSpec{}, false
}

func rawCallSpec(expr parser.Expression) (rawVFSSpec, bool) {
	ce, ok := expr.(*parser.CallExpression)
	if !ok || ce == nil {
		return rawVFSSpec{}, false
	}
	de, ok := ce.Function.(*parser.DotExpression)
	if !ok || de == nil {
		return rawVFSSpec{}, false
	}
	id, ok := de.Receiver.(*parser.Identifier)
	if !ok || id.Value != "raw" {
		return rawVFSSpec{}, false
	}
	return rawVFSSpecByName(de.Property)
}

func (g *Generator) rawCallSpec(expr parser.Expression) (rawVFSSpec, bool) {
	spec, ok := rawCallSpec(expr)
	if !ok {
		return rawVFSSpec{}, false
	}
	ce := expr.(*parser.CallExpression)
	de := ce.Function.(*parser.DotExpression)
	id := de.Receiver.(*parser.Identifier)
	if _, isLocal := g.lookupLocal(id.Value); isLocal {
		return rawVFSSpec{}, false
	}
	return spec, true
}

// setupRawVFSImports adds only the host functions referenced by the AST. The
// scan starts at Program.Statements, deliberately avoiding Program.Sem and its
// side tables. Existing non-raw modules therefore retain exactly two imports.
func (g *Generator) setupRawVFSImports(program *parser.Program) {
	used := make(map[string]bool)
	seen := make(map[uintptr]bool)
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if !v.IsValid() {
			return
		}
		switch v.Kind() {
		case reflect.Ptr:
			if v.IsNil() {
				return
			}
			ptr := v.Pointer()
			if seen[ptr] {
				return
			}
			seen[ptr] = true
			if v.CanInterface() {
				if expr, ok := v.Interface().(parser.Expression); ok {
					if spec, ok := rawCallSpec(expr); ok {
						used[spec.name] = true
					}
				}
			}
			walk(v.Elem())
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Struct:
			t := v.Type()
			for i := 0; i < t.NumField(); i++ {
				if t.Field(i).PkgPath == "" {
					walk(v.Field(i))
				}
			}
		}
	}
	walk(reflect.ValueOf(program.Statements))

	for _, spec := range rawVFSSpecs {
		if !used[spec.name] {
			continue
		}
		params := make([]ValType, 0, len(spec.params)+1)
		for _, param := range spec.params {
			switch param {
			case rawParamPtrLen:
				params = append(params, I32, I32)
			case rawParamBool:
				params = append(params, I32)
			default:
				params = append(params, I64)
			}
		}
		typeIdx := g.internType(params, []ValType{spec.result})
		g.rawImports[spec.name] = g.addFuncImport("nolang_raw", spec.hostName, typeIdx)
	}
}

func (g *Generator) emitRawVFSCall(sb *bytes.Buffer, ce *parser.CallExpression, spec rawVFSSpec) ValType {
	if len(ce.Arguments) != len(spec.params) {
		sb.WriteByte(OpUnreachable)
		return Void
	}
	importIdx, ok := g.rawImports[spec.name]
	if !ok {
		sb.WriteByte(OpUnreachable)
		return Void
	}
	for i, arg := range ce.Arguments {
		switch spec.params[i] {
		case rawParamPtrLen:
			g.emitExpr(sb, arg)
			g.emitStrDescriptorLoad(sb)
		case rawParamBool:
			if vt := g.emitExpr(sb, arg); vt != I32 {
				g.emitConvert(sb, vt, I32)
			}
		default:
			if vt := g.emitExpr(sb, arg); vt != I64 {
				g.emitConvert(sb, vt, I64)
			}
		}
	}
	sb.WriteByte(OpCall)
	writeU32LEB(sb, importIdx)
	if !spec.option {
		return spec.result
	}

	// Raw options use a 16-byte record: i32 tag at +0, i64 payload at +8.
	// Host value results are nonnegative on success. Boolean hosts return
	// positive on success and zero/negative on failure.
	resultIdx := g.addLocal(g.uniqueLocal("$raw.result"), I64)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(resultIdx))
	writeI32ConstU(sb, 16)
	g.emitMallocCall(sb)
	optionIdx := g.addLocal(g.uniqueLocal("$raw.option"), I32)
	sb.WriteByte(OpLocalSet)
	writeU32LEB(sb, uint32(optionIdx))

	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(optionIdx))
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(resultIdx))
	sb.WriteByte(OpI64Const)
	writeSLEB(sb, 0)
	if spec.boolean {
		sb.WriteByte(OpI64GtS)
	} else {
		sb.WriteByte(OpI64GeS)
	}
	sb.WriteByte(OpI32Eqz) // success -> tag 0, failure -> tag 1
	g.emitI32Store(sb)

	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(optionIdx))
	g.emitI32ConstOffset(sb, 8)
	sb.WriteByte(OpI32Add)
	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(resultIdx))
	if spec.boolean {
		sb.WriteByte(OpI64Const)
		writeSLEB(sb, 0)
		sb.WriteByte(OpI64GtS)
		sb.WriteByte(OpI64ExtendI32U)
	}
	g.emitI64Store(sb)

	sb.WriteByte(OpLocalGet)
	writeU32LEB(sb, uint32(optionIdx))
	return I32
}

func (g *Generator) emitRawOptionTagCompare(sb *bytes.Buffer, expr parser.Expression, tag string, notEqual bool) {
	g.emitExpr(sb, expr)
	g.emitI32Load(sb)
	writeI32Const(sb, int32(rawOptionTagValue(tag)))
	if notEqual {
		sb.WriteByte(OpI32Ne)
	} else {
		sb.WriteByte(OpI32Eq)
	}
}

func rawOptionTagValue(tag string) int {
	switch tag {
	case "ok":
		return 0
	case "nil":
		return 1
	case "err":
		return 2
	default:
		return -1
	}
}

func (g *Generator) emitRawOptionPayload(sb *bytes.Buffer, expr parser.Expression, targetType ValType) {
	g.emitExpr(sb, expr)
	g.emitI32ConstOffset(sb, 8)
	sb.WriteByte(OpI32Add)
	g.emitI64Load(sb)
	if targetType == I32 {
		sb.WriteByte(OpI32WrapI64)
	}
}

var rawConstantValues = map[string]int64{
	"RAW-READ":                 1,
	"RAW-WRITE":                2,
	"RAW-CREATE":               4,
	"RAW-EXCLUSIVE":            8,
	"RAW-TRUNCATE":             16,
	"RAW-SEEK-SET":             0,
	"RAW-SEEK-CUR":             1,
	"RAW-SEEK-END":             2,
	"RAW-LOCK-SHARED":          1,
	"RAW-LOCK-EXCLUSIVE":       2,
	"RAW-CAP-POSITIONAL-IO":    1,
	"RAW-CAP-TRUNCATE":         2,
	"RAW-CAP-FILE-SYNC":        4,
	"RAW-CAP-DATA-SYNC":        8,
	"RAW-CAP-DIR-SYNC":         16,
	"RAW-CAP-RANGE-LOCK":       32,
	"RAW-CAP-SHARED-LOCK":      64,
	"RAW-CAP-NONBLOCKING-LOCK": 128,
}

func rawConstantValue(name string) (int64, bool) {
	value, ok := rawConstantValues[name]
	return value, ok
}

func (g *Generator) rawConstantExpr(expr parser.Expression) (int64, bool) {
	de, ok := expr.(*parser.DotExpression)
	if !ok || de == nil {
		return 0, false
	}
	id, ok := de.Receiver.(*parser.Identifier)
	if !ok || id.Value != "raw" {
		return 0, false
	}
	if _, isLocal := g.lookupLocal(id.Value); isLocal {
		return 0, false
	}
	return rawConstantValue(de.Property)
}
