package embind

import (
	"context"
	"fmt"

	"github.com/jerbob92/wazero-emscripten-embind/types"

	"github.com/tetratelabs/wazero/api"
)

type voidType struct {
	baseType
}

func (vt *voidType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	return types.Undefined, nil
}

func (vt *voidType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	// TODO: assert if anything else is given? (comment from Emscripten)
	return 0, nil
}

func (vt *voidType) NativeType() api.ValueType {
	return 0
}

func (vt *voidType) GoType() string {
	return ""
}

var RegisterVoid = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)

	rawType := api.DecodeI32(stack[0])
	name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	err = engine.registerType(rawType, &voidType{
		baseType: baseType{
			rawType:        rawType,
			name:           name,
			argPackAdvance: 0,
		},
	}, nil)
	if err != nil {
		panic(fmt.Errorf("could not register: %w", err))
	}
})
