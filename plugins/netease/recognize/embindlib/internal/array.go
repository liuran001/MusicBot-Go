package embind

import (
	"context"
	"fmt"
	"github.com/tetratelabs/wazero/api"
)

type arrayType struct {
	baseType
	reg            *registeredTuple
	elementsLength int
}

func (at *arrayType) FromWireType(ctx context.Context, mod api.Module, ptr uint64) (any, error) {
	var err error
	rv := make([]any, at.elementsLength)
	for i := 0; i < at.elementsLength; i++ {
		rv[i], err = at.reg.elements[i].read(ctx, mod, api.DecodeI32(ptr))
		if err != nil {
			return nil, err
		}
	}

	_, err = at.reg.rawDestructor.Call(ctx, ptr)
	if err != nil {
		return nil, err
	}

	return rv, nil
}

func (at *arrayType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	arr, ok := o.([]any)
	if !ok {
		return 0, fmt.Errorf("incorrect input, not an array, make sure that the input is of type []any")
	}

	if at.elementsLength != len(arr) {
		return 0, fmt.Errorf("incorrect number of tuple elements for %s: expected=%d, actual=%d", at.reg.name, at.elementsLength, len(arr))
	}

	res, err := at.reg.rawConstructor.Call(ctx)
	if err != nil {
		return 0, err
	}

	ptr := api.DecodeU32(res[0])

	for i := 0; i < at.elementsLength; i++ {
		err = at.reg.elements[i].write(ctx, mod, int32(ptr), arr[i])
		if err != nil {
			return 0, err
		}
	}

	if destructors != nil {
		destructorsRef := *destructors
		destructorsRef = append(destructorsRef, at.DestructorFunction(ctx, mod, ptr))
		*destructors = destructorsRef
	}

	return api.EncodeU32(ptr), nil
}

func (at *arrayType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	ptr, ok := mod.Memory().ReadUint32Le(pointer)
	if !ok {
		return nil, fmt.Errorf("could not read pointer")
	}
	return at.FromWireType(ctx, mod, api.EncodeU32(ptr))
}

func (at *arrayType) DestructorFunctionUndefined() bool {
	return false
}

func (at *arrayType) DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) *destructorFunc {
	return &destructorFunc{
		apiFunction: at.reg.rawDestructor,
		args:        []uint64{api.EncodeU32(pointer)},
	}
}

func (at *arrayType) GoType() string {
	return "[]any"
}

func (at *arrayType) FromF64(o float64) uint64 {
	return uint64(o)
}

var RegisterValueArray = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawType := api.DecodeI32(stack[0])
	namePtr := api.DecodeI32(stack[1])
	constructorSignature := api.DecodeI32(stack[2])
	rawConstructor := api.DecodeI32(stack[3])
	destructorSignature := api.DecodeI32(stack[4])
	rawDestructor := api.DecodeI32(stack[5])

	name, err := engine.readCString(uint32(namePtr))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	rawConstructorFunc, err := engine.newInvokeFunc(constructorSignature, rawConstructor, []api.ValueType{}, []api.ValueType{api.ValueTypeI32})
	if err != nil {
		panic(fmt.Errorf("could not create rawConstructorFunc: %w", err))
	}

	rawDestructorFunc, err := engine.newInvokeFunc(destructorSignature, rawDestructor, []api.ValueType{api.ValueTypeI32}, []api.ValueType{})
	if err != nil {
		panic(fmt.Errorf("could not create rawDestructorFunc: %w", err))
	}

	engine.registeredTuples[rawType] = &registeredTuple{
		name:           name,
		rawConstructor: rawConstructorFunc,
		rawDestructor:  rawDestructorFunc,
		elements:       []*registeredTupleElement{},
	}
})

var RegisterValueArrayElement = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawTupleType := api.DecodeI32(stack[0])
	getterReturnType := api.DecodeI32(stack[1])
	getterSignature := api.DecodeI32(stack[2])
	getter := api.DecodeI32(stack[3])
	getterContext := api.DecodeI32(stack[4])
	setterArgumentType := api.DecodeI32(stack[5])
	setterSignature := api.DecodeI32(stack[6])
	setter := api.DecodeI32(stack[7])
	setterContext := api.DecodeI32(stack[8])

	engine.registeredTuples[rawTupleType].elements = append(engine.registeredTuples[rawTupleType].elements, &registeredTupleElement{
		getter:             getter,
		getterSignature:    getterSignature,
		getterReturnType:   getterReturnType,
		getterContext:      getterContext,
		setter:             setter,
		setterSignature:    setterSignature,
		setterArgumentType: setterArgumentType,
		setterContext:      setterContext,
	})
})

var FinalizeValueArray = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawTupleType := api.DecodeI32(stack[0])
	reg := engine.registeredTuples[rawTupleType]
	delete(engine.registeredTuples, rawTupleType)
	elements := reg.elements
	elementsLength := len(elements)

	elementTypes := make([]int32, len(elements)*2)
	for i := range elements {
		elementTypes[i] = elements[i].getterReturnType
		elementTypes[i+len(elements)] = elements[i].setterArgumentType
	}

	err := engine.whenDependentTypesAreResolved([]int32{rawTupleType}, elementTypes, func(types []registeredType) ([]registeredType, error) {
		for i := range elements {
			element := elements[i]
			getterReturnType := types[i]

			getterFunc, err := engine.newInvokeFunc(element.getterSignature, element.getter, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{getterReturnType.NativeType()})
			if err != nil {
				return nil, fmt.Errorf("could not create getterFunc: %w", err)
			}

			setterArgumentType := types[i+len(elements)]
			setterFunc, err := engine.newInvokeFunc(element.setterSignature, element.setter, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, setterArgumentType.NativeType()}, []api.ValueType{})
			if err != nil {
				return nil, fmt.Errorf("could not create setterFunc: %w", err)
			}

			element.read = func(ctx context.Context, mod api.Module, ptr int32) (any, error) {
				res, err := getterFunc.Call(ctx, api.EncodeI32(element.getterContext), api.EncodeI32(ptr))
				if err != nil {
					return nil, err
				}
				return getterReturnType.FromWireType(ctx, mod, res[0])
			}
			element.write = func(ctx context.Context, mod api.Module, ptr int32, o any) error {
				destructors := &[]*destructorFunc{}
				res, err := setterArgumentType.ToWireType(ctx, mod, destructors, o)
				if err != nil {
					return err
				}

				_, err = setterFunc.Call(ctx, api.EncodeI32(element.setterContext), api.EncodeI32(ptr), res)
				if err != nil {
					return err
				}

				err = engine.runDestructors(ctx, *destructors)
				if err != nil {
					return err
				}

				return nil
			}
		}

		return []registeredType{
			&arrayType{
				baseType: baseType{
					rawType:        rawTupleType,
					name:           reg.name,
					argPackAdvance: GenericWireTypeSize,
				},
				reg:            reg,
				elementsLength: elementsLength,
			},
		}, nil
	})
	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})
