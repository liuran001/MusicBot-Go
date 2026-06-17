package embind

import (
	"context"
	"fmt"
	"github.com/tetratelabs/wazero/api"
	"reflect"
)

type IConstant interface {
	Name() string
	Value() any
	Type() IType
}

type registeredConstant struct {
	registeredType registeredType
	name           string
	hasCppValue    bool
	cppValue       any
	rawCppValue    uint64
	hasGoValue     bool
	goValue        any
}

func (rc *registeredConstant) validate() error {
	if rc.hasGoValue && rc.hasCppValue {
		// Only compare when values can be compared.
		goValueType := reflect.TypeOf(rc.goValue)
		cppValueType := reflect.TypeOf(rc.cppValue)
		if goValueType.Comparable() && cppValueType.Comparable() && rc.goValue != rc.cppValue {
			return fmt.Errorf("constant %s has a different value in Go than in C++ (go: %v (%T), cpp: %v (%T))", rc.name, rc.goValue, rc.goValue, rc.cppValue, rc.cppValue)
		}
	}

	return nil
}

func (rc *registeredConstant) Name() string {
	return rc.name
}

func (rc *registeredConstant) Value() any {
	return rc.cppValue
}

func (rc *registeredConstant) Type() IType {
	return &exposedType{registeredType: rc.registeredType}
}

func (e *engine) GetConstants() []IConstant {
	constants := make([]IConstant, 0)
	for i := range e.registeredConstants {
		constants = append(constants, e.registeredConstants[i])
	}
	return constants
}

var RegisterConstant = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)

	name, err := engine.readCString(uint32(api.DecodeI32(stack[0])))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	rawType := api.DecodeI32(stack[1])
	constantValue := api.DecodeF64(stack[2])

	err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawType}, func(argTypes []registeredType) ([]registeredType, error) {
		registeredType := argTypes[0]
		// We need to do this since the JS VM automatically converts between
		// the float64 and other types, but we can't do this, we need to
		// manually convert everything. Note that the value inside the original
		// uint64 is an actual typecast F64, so it's not that the stack
		// contains FromWireType'able data for the given type.
		cppValue := registeredType.FromF64(constantValue)
		val, err := registeredType.FromWireType(ctx, engine.mod, cppValue)
		if err != nil {
			return nil, fmt.Errorf("could not initialize constant %s: %w", name, err)
		}

		_, ok := engine.registeredConstants[name]
		if !ok {
			engine.registeredConstants[name] = &registeredConstant{
				name: name,
			}
		}

		engine.registeredConstants[name].registeredType = registeredType
		engine.registeredConstants[name].hasCppValue = true
		engine.registeredConstants[name].cppValue = val
		engine.registeredConstants[name].rawCppValue = cppValue

		return nil, engine.registeredConstants[name].validate()
	})

	if err != nil {
		panic(err)
	}
})
