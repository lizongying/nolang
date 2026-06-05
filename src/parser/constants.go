package parser

import (
	"fmt"
	"strings"
)

type ValueType uint8

const (
	ValueTypeUnknown ValueType = iota

	// 容器类型
	ValueTypeObject
	ValueTypeMap
	ValueTypeArray
	ValueTypeVec

	// 基础类型
	ValueTypeByte
	ValueTypeBool
	ValueTypeChar
	ValueTypeString

	ValueTypeInt8
	ValueTypeInt16
	ValueTypeInt32
	ValueTypeInt64
	ValueTypeUint8
	ValueTypeUint16
	ValueTypeUint32
	ValueTypeUint64

	ValueTypeFloat32
	ValueTypeFloat64

	vtUnknownStr = "unknown"

	vtObjectStr = "obj"
	vtMapStr    = "map"
	vtArrayStr  = "arr"
	vtVecStr    = "vec"

	vtByteStr   = "byte"
	vtBoolStr   = "bool"
	vtCharStr   = "char"
	vtStringStr = "str"

	vtI8Str  = "i8"
	vtI16Str = "i16"
	vtI32Str = "i32"
	vtI64Str = "i64"
	vtU8Str  = "u8"
	vtU16Str = "u16"
	vtU32Str = "u32"
	vtU64Str = "u64"

	vtF32Str = "f32"
	vtF64Str = "f64"
)

func (v ValueType) String() string {
	switch v {
	case ValueTypeUnknown:
		return vtUnknownStr
	case ValueTypeObject:
		return vtObjectStr
	case ValueTypeMap:
		return vtMapStr
	case ValueTypeArray:
		return vtArrayStr
	case ValueTypeVec:
		return vtVecStr
	case ValueTypeByte:
		return vtByteStr
	case ValueTypeBool:
		return vtBoolStr
	case ValueTypeChar:
		return vtCharStr
	case ValueTypeString:
		return vtStringStr
	case ValueTypeInt8:
		return vtI8Str
	case ValueTypeInt16:
		return vtI16Str
	case ValueTypeInt32:
		return vtI32Str
	case ValueTypeInt64:
		return vtI64Str
	case ValueTypeUint8:
		return vtU8Str
	case ValueTypeUint16:
		return vtU16Str
	case ValueTypeUint32:
		return vtU32Str
	case ValueTypeUint64:
		return vtU64Str
	case ValueTypeFloat32:
		return vtF32Str
	case ValueTypeFloat64:
		return vtF64Str
	default:
		return fmt.Sprintf("ValueType(%d)", v)
	}
}

var strToValueType = map[string]ValueType{
	vtUnknownStr: ValueTypeUnknown,
	vtObjectStr:  ValueTypeObject,
	vtMapStr:     ValueTypeMap,
	vtArrayStr:   ValueTypeArray,
	vtVecStr:     ValueTypeVec,
	vtByteStr:    ValueTypeByte,
	vtBoolStr:    ValueTypeBool,
	vtCharStr:    ValueTypeChar,
	vtStringStr:  ValueTypeString,
	vtI8Str:      ValueTypeInt8,
	vtI16Str:     ValueTypeInt16,
	vtI32Str:     ValueTypeInt32,
	vtI64Str:     ValueTypeInt64,
	vtU8Str:      ValueTypeUint8,
	vtU16Str:     ValueTypeUint16,
	vtU32Str:     ValueTypeUint32,
	vtU64Str:     ValueTypeUint64,
	vtF32Str:     ValueTypeFloat32,
	vtF64Str:     ValueTypeFloat64,
}

func ParseValueType(s string) (ValueType, error) {
	s = strings.ToLower(s)

	vt, ok := strToValueType[s]
	if !ok {
		return ValueTypeUnknown, fmt.Errorf("Invalid ValueType string: %s", s)
	}
	return vt, nil
}
