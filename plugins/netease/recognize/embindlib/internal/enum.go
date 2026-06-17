package embind

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

type IEnum interface {
	Type() any
	Values() map[string]any
}

type enumValue struct {
	name        string
	hasCppValue bool
	cppValue    any
	rawCppValue uint64
	hasGoValue  bool
	goValue     any
}

func (ev *enumValue) Name() string {
	return ev.name
}

func (ev *enumValue) Value() any {
	return ev.cppValue
}

type IEnumType interface {
	Name() string
	Type() IType
	Values() []IEnumTypeValue
}

type IEnumTypeValue interface {
	Name() string
	Value() any
}

type enumType struct {
	baseType
	intHelper        intType // Enums are basically ints, we use the intType underwater to make things easier.
	valuesByName     map[string]*enumValue
	valuesByCppValue map[any]*enumValue
	valuesByGoValue  map[any]*enumValue
	registeredInGo   bool
	goValue          any
}

func (et *enumType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	val, err := et.intHelper.FromWireType(ctx, mod, value)
	if err != nil {
		return nil, err
	}

	return et.mapToGoEnum(val)
}

func (et *enumType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	val, ok := et.valuesByGoValue[o]
	if !ok {
		val, ok = et.valuesByCppValue[o]
		if !ok {
			return 0, fmt.Errorf("could not map enum value %v, enum value not registered as Go or C++ enum", o)
		}
	}

	return et.intHelper.ToWireType(ctx, mod, destructors, val.cppValue)
}

func (et *enumType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	val, err := et.intHelper.ReadValueFromPointer(ctx, mod, pointer)
	if err != nil {
		return nil, err
	}

	return et.mapToGoEnum(val)
}

func (et *enumType) mapToGoEnum(value any) (any, error) {
	if !et.registeredInGo {
		return value, nil
	}

	val, ok := et.valuesByCppValue[value]
	if !ok {
		return nil, fmt.Errorf("could not map enum value %v, enum value not registered as C++ enum", value)
	}

	if !val.hasGoValue {
		return value, nil
	}

	return val.goValue, nil
}

func (et *enumType) DestructorFunctionUndefined() bool {
	return false
}

func (et *enumType) GoType() string {
	return et.name
}

func (et *enumType) Type() IType {
	return &exposedType{registeredType: &et.intHelper}
}

func (et *enumType) Name() string {
	return et.name
}

func (et *enumType) Values() []IEnumTypeValue {
	values := make([]IEnumTypeValue, 0)
	for i := range et.valuesByName {
		values = append(values, et.valuesByName[i])
	}
	return values
}

func (e *engine) GetEnums() []IEnumType {
	enums := make([]IEnumType, 0)
	for i := range e.registeredEnums {
		enums = append(enums, e.registeredEnums[i])
	}
	return enums
}

var RegisterEnum = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)

	rawType := api.DecodeI32(stack[0])
	name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	_, ok := engine.registeredEnums[name]
	if !ok {
		engine.registeredEnums[name] = &enumType{
			valuesByName:     map[string]*enumValue{},
			valuesByCppValue: map[any]*enumValue{},
			valuesByGoValue:  map[any]*enumValue{},
		}
	}

	engine.registeredEnums[name].baseType = baseType{
		rawType:        rawType,
		name:           name,
		argPackAdvance: GenericWireTypeSize,
	}

	engine.registeredEnums[name].intHelper = intType{
		size:   api.DecodeI32(stack[2]),
		signed: api.DecodeI32(stack[3]) > 0,
	}

	err = engine.registerType(rawType, engine.registeredEnums[name], nil)
	if err != nil {
		panic(fmt.Errorf("could not register: %w", err))
	}
})

var RegisterEnumValue = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)

	rawType := api.DecodeI32(stack[0])
	name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	registeredType, ok := engine.registeredTypes[rawType]
	if !ok {
		typeName, err := engine.getTypeName(ctx, rawType)
		if err != nil {
			panic(err)
		}
		panic(fmt.Errorf("%s has unknown type %s", name, typeName))
	}

	enumType := registeredType.(*enumType)
	enumWireValue, err := enumType.intHelper.FromWireType(ctx, mod, stack[2])
	if err != nil {
		panic(fmt.Errorf("could not read value for enum %s", name))
	}

	_, ok = enumType.valuesByName[name]
	if !ok {
		enumType.valuesByName[name] = &enumValue{
			name: name,
		}
	}

	if enumType.valuesByName[name].hasCppValue {
		panic(fmt.Errorf("enum value %s for enum %s was already registered", name, enumType.name))
	}

	enumType.valuesByName[name].hasCppValue = true
	enumType.valuesByName[name].cppValue = enumWireValue
	enumType.valuesByCppValue[enumWireValue] = enumType.valuesByName[name]
})
