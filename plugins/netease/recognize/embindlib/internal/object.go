package embind

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero/api"
)

type objectType struct {
	baseType
	reg            *registeredObject
	elementsLength int
}

func (ot *objectType) FromWireType(ctx context.Context, mod api.Module, ptr uint64) (any, error) {
	var err error
	rv := map[string]any{}

	for i := range ot.reg.fields {
		rv[ot.reg.fields[i].fieldName], err = ot.reg.fields[i].read(ctx, mod, api.DecodeI32(ptr))
		if err != nil {
			return nil, err
		}
	}

	_, err = ot.reg.rawDestructor.Call(ctx, ptr)
	if err != nil {
		return nil, err
	}

	return rv, nil
}

func (ot *objectType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	obj, ok := o.(map[string]any)
	if !ok {
		return 0, fmt.Errorf("incorrect input, not a map[string]any")
	}

	for i := range ot.reg.fields {
		if _, ok = obj[ot.reg.fields[i].fieldName]; !ok {
			return 0, fmt.Errorf("missing field: %s", ot.reg.fields[i].fieldName)
		}
	}

	res, err := ot.reg.rawConstructor.Call(ctx)
	if err != nil {
		return 0, err
	}

	ptr := api.DecodeU32(res[0])

	for i := range ot.reg.fields {
		err = ot.reg.fields[i].write(ctx, mod, int32(ptr), obj[ot.reg.fields[i].fieldName])
		if err != nil {
			return 0, err
		}
	}

	if destructors != nil {
		destructorsRef := *destructors
		destructorsRef = append(destructorsRef, ot.DestructorFunction(ctx, mod, ptr))
		*destructors = destructorsRef
	}

	return api.EncodeU32(ptr), nil
}

func (ot *objectType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	ptr, ok := mod.Memory().ReadUint32Le(pointer)
	if !ok {
		return nil, fmt.Errorf("could not read pointer")
	}
	return ot.FromWireType(ctx, mod, api.EncodeU32(ptr))
}

func (ot *objectType) DestructorFunctionUndefined() bool {
	return false
}

func (ot *objectType) DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) *destructorFunc {
	return &destructorFunc{
		apiFunction: ot.reg.rawDestructor,
		args:        []uint64{api.EncodeU32(pointer)},
	}
}

func (ot *objectType) GoType() string {
	return "map[string]any"
}

func (ot *objectType) FromF64(o float64) uint64 {
	return uint64(o)
}

var RegisterValueObject = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
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

	engine.registeredObjects[rawType] = &registeredObject{
		name:           name,
		rawConstructor: rawConstructorFunc,
		rawDestructor:  rawDestructorFunc,
		fields:         []*registeredObjectField{},
	}
})

var RegisterValueObjectField = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	structType := api.DecodeI32(stack[0])
	fieldNamePtr := api.DecodeI32(stack[1])
	getterReturnType := api.DecodeI32(stack[2])
	getterSignature := api.DecodeI32(stack[3])
	getter := api.DecodeI32(stack[4])
	getterContext := api.DecodeI32(stack[5])
	setterArgumentType := api.DecodeI32(stack[6])
	setterSignature := api.DecodeI32(stack[7])
	setter := api.DecodeI32(stack[8])
	setterContext := api.DecodeI32(stack[9])

	fieldName, err := engine.readCString(uint32(fieldNamePtr))
	if err != nil {
		panic(fmt.Errorf("could not read field name: %w", err))
	}

	engine.registeredObjects[structType].fields = append(engine.registeredObjects[structType].fields, &registeredObjectField{
		fieldName:          fieldName,
		getterReturnType:   getterReturnType,
		getter:             getter,
		getterSignature:    getterSignature,
		getterContext:      getterContext,
		setterArgumentType: setterArgumentType,
		setter:             setter,
		setterSignature:    setterSignature,
		setterContext:      setterContext,
	})
})

var FinalizeValueObject = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	structType := api.DecodeI32(stack[0])
	reg := engine.registeredObjects[structType]
	delete(engine.registeredObjects, structType)
	fieldRecords := reg.fields

	fieldTypes := make([]int32, len(fieldRecords)*2)
	for i := range fieldRecords {
		fieldTypes[i] = fieldRecords[i].getterReturnType
		fieldTypes[i+len(fieldRecords)] = fieldRecords[i].setterArgumentType
	}

	err := engine.whenDependentTypesAreResolved([]int32{structType}, fieldTypes, func(types []registeredType) ([]registeredType, error) {
		for i := range fieldRecords {
			fieldRecord := fieldRecords[i]
			getterReturnType := types[i]
			getterFunc, err := engine.newInvokeFunc(fieldRecord.getterSignature, fieldRecord.getter, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{getterReturnType.NativeType()})
			if err != nil {
				panic(fmt.Errorf("could not create getterFunc: %w", err))
			}

			fieldRecord.read = func(ctx context.Context, mod api.Module, ptr int32) (any, error) {
				res, err := getterFunc.Call(ctx, api.EncodeI32(fieldRecord.getterContext), api.EncodeI32(ptr))
				if err != nil {
					return nil, err
				}
				return getterReturnType.FromWireType(ctx, mod, res[0])
			}

			setterArgumentType := types[i+len(fieldRecords)]
			setterFunc, err := engine.newInvokeFunc(fieldRecord.setterSignature, fieldRecord.setter, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, setterArgumentType.NativeType()}, []api.ValueType{})
			if err != nil {
				panic(fmt.Errorf("could not create setterFunc: %w", err))
			}

			fieldRecord.write = func(ctx context.Context, mod api.Module, ptr int32, o any) error {
				destructors := &[]*destructorFunc{}
				res, err := setterArgumentType.ToWireType(ctx, mod, destructors, o)
				if err != nil {
					return err
				}

				_, err = setterFunc.Call(ctx, api.EncodeI32(fieldRecord.setterContext), api.EncodeI32(ptr), res)
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
			&objectType{
				baseType: baseType{
					rawType:        structType,
					name:           reg.name,
					argPackAdvance: GenericWireTypeSize,
				},
				reg: reg,
			},
		}, nil
	})
	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})
