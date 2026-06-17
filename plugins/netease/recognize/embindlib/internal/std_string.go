package embind

import (
	"context"
	"fmt"
	"github.com/tetratelabs/wazero/api"
)

type stdStringType struct {
	baseType

	// process only std::string bindings with UTF8 support, in contrast to e.g. std::basic_string<unsigned char>
	// Do we really care in Go?

	stdStringIsUTF8 bool
}

func (sst *stdStringType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	strPointer := api.DecodeI32(value)

	length, ok := mod.Memory().ReadUint32Le(uint32(strPointer))
	if !ok {
		return nil, fmt.Errorf("could not read length of string")
	}

	payload := strPointer + 4

	data, ok := mod.Memory().Read(uint32(payload), length)
	if !ok {
		return nil, fmt.Errorf("could not read data of string")
	}

	str := string(data)

	_, err := mod.ExportedFunction("F").Call(ctx, value)
	if err != nil {
		return nil, err
	}

	return str, nil
}

func (sst *stdStringType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	stringVal, ok := o.(string)
	if !ok {
		return 0, fmt.Errorf("value must be of type string")
	}

	// assumes 4-byte alignment
	length := len(stringVal)
	mallocRes, err := mod.ExportedFunction("E").Call(ctx, api.EncodeI32(4+int32(length)+1))
	if err != nil {
		return 0, err
	}

	base := api.DecodeU32(mallocRes[0])
	ptr := base + 4

	ok = mod.Memory().WriteUint32Le(base, uint32(length))
	if !ok {
		return 0, fmt.Errorf("could not write length to memory")
	}

	ok = mod.Memory().Write(ptr, []byte(stringVal))
	if !ok {
		return 0, fmt.Errorf("could not write string to memory")
	}

	ok = mod.Memory().WriteByte(ptr+uint32(length)+1, 0)
	if !ok {
		return 0, fmt.Errorf("could not write NULL terminator to memory")
	}

	if destructors != nil {
		destructorsRef := *destructors
		destructorsRef = append(destructorsRef, sst.DestructorFunction(ctx, mod, base))
		*destructors = destructorsRef
	}

	return api.EncodeU32(base), nil
}

func (sst *stdStringType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	ptr, ok := mod.Memory().ReadUint32Le(pointer)
	if !ok {
		return nil, fmt.Errorf("could not read pointer")
	}
	return sst.FromWireType(ctx, mod, api.EncodeU32(ptr))
}

func (sst *stdStringType) DestructorFunctionUndefined() bool {
	return false
}

func (sst *stdStringType) DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) *destructorFunc {
	return &destructorFunc{
		apiFunction: mod.ExportedFunction("F"),
		args:        []uint64{api.EncodeU32(pointer)},
	}
}

func (sst *stdStringType) GoType() string {
	return "string"
}

func (sst *stdStringType) FromF64(o float64) uint64 {
	return api.EncodeU32(uint32(o))
}

var RegisterStdString = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)

	rawType := api.DecodeI32(stack[0])
	name, err := engine.readCString(uint32(api.DecodeI32(stack[1])))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	err = engine.registerType(rawType, &stdStringType{
		baseType: baseType{
			rawType:        rawType,
			name:           name,
			argPackAdvance: GenericWireTypeSize,
		},
		stdStringIsUTF8: name == "std::string",
	}, nil)
	if err != nil {
		panic(fmt.Errorf("could not register: %w", err))
	}
})
