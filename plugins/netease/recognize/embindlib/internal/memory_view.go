package embind

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/tetratelabs/wazero/api"
)

type memoryViewType struct {
	baseType
	dataTypeIndex int32
	nativeType    any
	nativeSize    uint32
}

func (mvt *memoryViewType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	memoryViewPtr := api.DecodeU32(value)

	var ok bool

	size, ok := mod.Memory().ReadUint32Le(memoryViewPtr)
	if !ok {
		return nil, fmt.Errorf("could not read size of memory view")
	}

	pointer, ok := mod.Memory().ReadUint32Le(memoryViewPtr + 4)
	if !ok {
		return nil, fmt.Errorf("could not read pointer of memory view")
	}

	var typedMemoryView any

	if mvt.dataTypeIndex == 0 {
		typedMemoryView, ok = memoryAs[int8](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 1 {
		typedMemoryView, ok = memoryAs[uint8](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 2 {
		typedMemoryView, ok = memoryAs[int16](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 3 {
		typedMemoryView, ok = memoryAs[uint16](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 4 {
		typedMemoryView, ok = memoryAs[int32](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 5 {
		typedMemoryView, ok = memoryAs[uint32](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 6 {
		typedMemoryView, ok = memoryAs[float32](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 7 {
		typedMemoryView, ok = memoryAs[float64](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 8 {
		typedMemoryView, ok = memoryAs[int64](mod.Memory(), pointer, mvt.nativeSize, size)
	} else if mvt.dataTypeIndex == 9 {
		typedMemoryView, ok = memoryAs[uint64](mod.Memory(), pointer, mvt.nativeSize, size)
	} else {
		return nil, fmt.Errorf("unknown memory view type %s", mvt.name)
	}

	if !ok {
		return nil, fmt.Errorf("could not create memory view")
	}

	return typedMemoryView, nil
}

func memoryAs[T any](memory api.Memory, offset uint32, elementSize uint32, length uint32) ([]T, bool) {
	memoryView, ok := memory.Read(offset, elementSize*length)
	if !ok {
		return nil, ok
	}

	return unsafe.Slice((*T)(unsafe.Pointer(&memoryView[0])), length), true
}

func (mvt *memoryViewType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	return 0, fmt.Errorf("ToWireType is not supported for memory views")
}

func (mvt *memoryViewType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	return mvt.FromWireType(ctx, mod, api.EncodeU32(pointer))
}

func (mvt *memoryViewType) GoType() string {
	if mvt.dataTypeIndex == 0 {
		return "[]int8"
	} else if mvt.dataTypeIndex == 1 {
		return "[]uint8"
	} else if mvt.dataTypeIndex == 2 {
		return "[]int16"
	} else if mvt.dataTypeIndex == 3 {
		return "[]uint16"
	} else if mvt.dataTypeIndex == 4 {
		return "[]int32"
	} else if mvt.dataTypeIndex == 5 {
		return "[]uint32"
	} else if mvt.dataTypeIndex == 6 {
		return "[]float32"
	} else if mvt.dataTypeIndex == 7 {
		return "[]float64"
	} else if mvt.dataTypeIndex == 8 {
		return "[]int64"
	} else if mvt.dataTypeIndex == 9 {
		return "[]uint64"
	}

	return "[]uint64"
}

var RegisterMemoryView = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)

	rawType := api.DecodeI32(stack[0])
	dataTypeIndex := api.DecodeI32(stack[1])
	name, err := engine.readCString(uint32(api.DecodeI32(stack[2])))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	typeMapping := []any{
		int8(0),
		uint8(0),
		int16(0),
		uint16(0),
		int32(0),
		uint32(0),
		float32(0),
		float64(0),
		int64(0),
		uint64(0),
	}

	if dataTypeIndex < 0 || int(dataTypeIndex) >= len(typeMapping) {
		panic(fmt.Errorf("invalid memory view data type index: %d", dataTypeIndex))
	}

	sizeMapping := []uint32{
		1, // int8
		1, // uint8
		2, // int16
		2, // uint16
		4, // int32
		4, // uint32
		4, // float32
		8, // float64
		8, // int64
		8, // uint64
	}

	err = engine.registerType(rawType, &memoryViewType{
		baseType: baseType{
			rawType:        rawType,
			name:           name,
			argPackAdvance: GenericWireTypeSize,
		},
		dataTypeIndex: dataTypeIndex,
		nativeSize:    sizeMapping[dataTypeIndex],
		nativeType:    typeMapping[dataTypeIndex],
	}, &registerTypeOptions{
		ignoreDuplicateRegistrations: true,
	})
	if err != nil {
		panic(fmt.Errorf("could not register: %w", err))
	}
})
