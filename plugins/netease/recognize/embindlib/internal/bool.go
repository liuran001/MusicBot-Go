package embind

import (
	"context"
	"fmt"

	"github.com/jerbob92/wazero-emscripten-embind/types"

	"github.com/tetratelabs/wazero/api"
)

type boolType struct {
	baseType
	size     int32
	trueVal  int32
	falseVal int32
}

func (bt *boolType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	// ambiguous emscripten ABI: sometimes return values are
	// true or false, and sometimes integers (0 or 1)
	return value > 0, nil
}

func (bt *boolType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	if o == nil || o == types.Undefined {
		return api.EncodeI32(bt.falseVal), nil
	}

	val, ok := o.(bool)
	if ok {
		if val {
			return api.EncodeI32(bt.trueVal), nil
		}
		return api.EncodeI32(bt.falseVal), nil
	}

	stringVal, ok := o.(string)
	if ok {
		if stringVal != "" {
			return api.EncodeI32(bt.trueVal), nil
		}
		return api.EncodeI32(bt.falseVal), nil
	}

	// Float64 is big enough for any number.
	numberVal := float64(0)
	hasNumberVal := false
	switch v := o.(type) {
	case int:
		numberVal = float64(v)
		hasNumberVal = true
	case uint:
		numberVal = float64(v)
		hasNumberVal = true
	case int8:
		numberVal = float64(v)
		hasNumberVal = true
	case uint8:
		numberVal = float64(v)
		hasNumberVal = true
	case int16:
		numberVal = float64(v)
		hasNumberVal = true
	case uint16:
		numberVal = float64(v)
		hasNumberVal = true
	case int32:
		numberVal = float64(v)
		hasNumberVal = true
	case uint32:
		numberVal = float64(v)
		hasNumberVal = true
	case int64:
		numberVal = float64(v)
		hasNumberVal = true
	case uint64:
		numberVal = float64(v)
		hasNumberVal = true
	case float32:
		numberVal = float64(v)
		hasNumberVal = true
	case float64:
		numberVal = v
		hasNumberVal = true
	}

	if hasNumberVal {
		if numberVal > 0 {
			return api.EncodeI32(bt.trueVal), nil
		}
		return api.EncodeI32(bt.falseVal), nil
	}

	// @todo: implement nil pointer check

	// Any other type could be considered true?
	return api.EncodeI32(bt.trueVal), nil
}

func (bt *boolType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	if bt.size == 1 {
		val, _ := mod.Memory().ReadByte(pointer)
		return bt.FromWireType(ctx, mod, uint64(val))
	} else if bt.size == 2 {
		val, _ := mod.Memory().ReadUint16Le(pointer)
		return bt.FromWireType(ctx, mod, uint64(val))
	} else if bt.size == 4 {
		val, _ := mod.Memory().ReadUint32Le(pointer)
		return bt.FromWireType(ctx, mod, uint64(val))
	} else {
		return nil, fmt.Errorf("unknown boolean type size %d: %s", bt.size, bt.name)
	}
}

func (bt *boolType) GoType() string {
	return "bool"
}

func (bt *boolType) DestructorFunctionUndefined() bool {
	return false
}

var RegisterBool = func(hasSize bool) api.GoModuleFunc {
	return api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
		engine := MustGetEngineFromContext(ctx, mod).(*engine)

		rawType := api.DecodeI32(stack[0])

		name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
		if err != nil {
			panic(fmt.Errorf("could not read name: %w", err))
		}

		var size, trueVal, falseVal int32

		// Since Emscripten 3.1.45, the size of the boolean is put to 1, while
		// before the size was part of the registration.
		if hasSize {
			size = api.DecodeI32(stack[2])
			trueVal = api.DecodeI32(stack[3])
			falseVal = api.DecodeI32(stack[4])
		} else {
			size = int32(1)
			trueVal = api.DecodeI32(stack[2])
			falseVal = api.DecodeI32(stack[3])
		}

		err = engine.registerType(rawType, &boolType{
			baseType: baseType{
				rawType:        rawType,
				name:           name,
				argPackAdvance: GenericWireTypeSize,
			},
			size:     size,
			trueVal:  trueVal,
			falseVal: falseVal,
		}, nil)
		if err != nil {
			panic(fmt.Errorf("could not register: %w", err))
		}
	})
}
