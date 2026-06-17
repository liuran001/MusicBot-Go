package embind

import (
	"context"
	"github.com/tetratelabs/wazero/api"
)

type publicSymbolFn func(ctx context.Context, this any, arguments ...any) (any, error)

type baseType struct {
	rawType        int32
	name           string
	argPackAdvance int32
}

func (bt *baseType) RawType() int32 {
	return bt.rawType
}

func (bt *baseType) Name() string {
	return bt.name
}

func (bt *baseType) ArgPackAdvance() int32 {
	return bt.argPackAdvance
}

func (bt *baseType) DestructorFunctionUndefined() bool {
	return true
}

func (bt *baseType) DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) *destructorFunc {
	return nil
}

func (bt *baseType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	return nil, nil
}

func (bt *baseType) HasDeleteObject() bool {
	return false
}

func (bt *baseType) DeleteObject(ctx context.Context, mod api.Module, handle any) error {
	return nil
}

func (bt *baseType) NativeType() api.ValueType {
	return api.ValueTypeI32
}

func (bt *baseType) FromF64(o float64) uint64 {
	return api.EncodeF64(o)
}

func (bt *baseType) ToF64(o uint64) float64 {
	return float64(o)
}

type registeredType interface {
	RawType() int32
	Name() string
	ArgPackAdvance() int32
	DestructorFunctionUndefined() bool
	DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) *destructorFunc
	FromWireType(ctx context.Context, mod api.Module, wt uint64) (any, error)
	ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error)
	ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error)
	HasDeleteObject() bool
	DeleteObject(ctx context.Context, mod api.Module, handle any) error
	NativeType() api.ValueType
	GoType() string
	FromF64(o float64) uint64
	ToF64(o uint64) float64
}

type IType interface {
	Name() string
	Type() string
	IsClass() bool
	IsEnum() bool
}

type exposedType struct {
	registeredType registeredType
}

func (et *exposedType) Type() string {
	return et.registeredType.GoType()
}

func (et *exposedType) Name() string {
	return et.registeredType.Name()
}

func (et *exposedType) IsClass() bool {
	_, ok := et.registeredType.(*registeredPointerType)
	return ok
}

func (et *exposedType) IsEnum() bool {
	_, ok := et.registeredType.(*enumType)
	return ok
}

type registerTypeOptions struct {
	ignoreDuplicateRegistrations bool
}

type awaitingDependency struct {
	cb func() error
}

type registeredPointer struct {
	pointerType      *registeredPointerType
	constPointerType *registeredPointerType
}

type registeredTuple struct {
	name           string
	rawConstructor api.Function
	rawDestructor  api.Function
	elements       []*registeredTupleElement
}

type registeredTupleElement struct {
	getterReturnType   int32
	getter             int32
	getterSignature    int32
	getterContext      int32
	setterArgumentType int32
	setter             int32
	setterSignature    int32
	setterContext      int32
	read               func(ctx context.Context, mod api.Module, ptr int32) (any, error)
	write              func(ctx context.Context, mod api.Module, ptr int32, o any) error
}

type registeredObject struct {
	name           string
	rawConstructor api.Function
	rawDestructor  api.Function
	fields         []*registeredObjectField
}

type registeredObjectField struct {
	fieldName          string
	getterReturnType   int32
	getter             int32
	getterSignature    int32
	getterContext      int32
	setterArgumentType int32
	setter             int32
	setterSignature    int32
	setterContext      int32
	read               func(ctx context.Context, mod api.Module, ptr int32) (any, error)
	write              func(ctx context.Context, mod api.Module, ptr int32, o any) error
}
