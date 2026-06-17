package embind

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/tetratelabs/wazero/api"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
	"golang.org/x/text/transform"
)

type stdWStringType struct {
	baseType
	charSize int32
}

// @todo: decide whether we should return string, []byte or something else for UTF16/UTF32.

func (swst *stdWStringType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	defer mod.ExportedFunction("F").Call(ctx, value)

	strPointer := api.DecodeI32(value)
	length, ok := mod.Memory().ReadUint32Le(uint32(strPointer))
	if !ok {
		return nil, fmt.Errorf("could not read length of string")
	}

	payload := strPointer + 4
	data, ok := mod.Memory().Read(uint32(payload), length*uint32(swst.charSize))
	if !ok {
		return nil, fmt.Errorf("could not read data of string")
	}

	var unicodeReader *transform.Reader
	if swst.charSize == 4 {
		pdf32le := utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM)
		unicodeReader = transform.NewReader(bytes.NewReader(data), pdf32le.NewDecoder())
	} else {
		pdf16le := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
		utf16bom := unicode.BOMOverride(pdf16le.NewDecoder())
		unicodeReader = transform.NewReader(bytes.NewReader(data), utf16bom)
	}

	decoded, err := io.ReadAll(unicodeReader)
	if err != nil {
		return "", err
	}

	// Remove NULL terminator.
	decoded = bytes.TrimSuffix(decoded, []byte("\x00"))

	return string(decoded), nil
}

func (swst *stdWStringType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	stringVal, ok := o.(string)
	if !ok {
		return 0, fmt.Errorf("input must be a string, was %T", o)
	}

	var encoder transform.Transformer
	if swst.charSize == 4 {
		pdf32le := utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM)
		encoder = pdf32le.NewEncoder()
	} else {
		pdf16le := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
		encoder = unicode.BOMOverride(pdf16le.NewEncoder())
	}

	output := &bytes.Buffer{}
	unicodeWriter := transform.NewWriter(output, encoder)
	_, err := unicodeWriter.Write([]byte(stringVal))
	if err != nil {
		return 0, err
	}
	err = unicodeWriter.Close()
	if err != nil {
		return 0, err
	}

	mallocRes, err := mod.ExportedFunction("E").Call(ctx, api.EncodeI32(4+int32(output.Len())+swst.charSize))
	if err != nil {
		return 0, err
	}
	base := api.DecodeU32(mallocRes[0])

	ok = mod.Memory().WriteUint32Le(base, uint32(len(stringVal)))
	if !ok {
		return 0, fmt.Errorf("could not write length to memory")
	}

	ok = mod.Memory().Write(base+4, output.Bytes())
	if !ok {
		return 0, fmt.Errorf("could not write string to memory")
	}

	ok = mod.Memory().Write(base+4+uint32(output.Len()), make([]byte, swst.charSize))
	if !ok {
		return 0, fmt.Errorf("could not write NULL terminator to memory")
	}

	if destructors != nil {
		destructorsRef := *destructors
		destructorsRef = append(destructorsRef, swst.DestructorFunction(ctx, mod, base))
		*destructors = destructorsRef
	}

	return api.EncodeU32(base), nil
}

func (swst *stdWStringType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	ptr, ok := mod.Memory().ReadUint32Le(pointer)
	if !ok {
		return nil, fmt.Errorf("could not read pointer")
	}
	return swst.FromWireType(ctx, mod, api.EncodeU32(ptr))
}

func (swst *stdWStringType) DestructorFunctionUndefined() bool {
	return false
}

func (swst *stdWStringType) DestructorFunction(ctx context.Context, mod api.Module, pointer uint32) *destructorFunc {
	return &destructorFunc{
		apiFunction: mod.ExportedFunction("F"),
		args:        []uint64{api.EncodeU32(pointer)},
	}
}

func (swst *stdWStringType) GoType() string {
	return "string"
}

func (swst *stdWStringType) FromF64(o float64) uint64 {
	return api.EncodeU32(uint32(o))
}

var RegisterStdWString = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)

	rawType := api.DecodeI32(stack[0])
	name, err := engine.readCString(uint32(api.DecodeI32(stack[2])))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	err = engine.registerType(rawType, &stdWStringType{
		baseType: baseType{
			rawType:        rawType,
			name:           name,
			argPackAdvance: GenericWireTypeSize,
		},
		charSize: api.DecodeI32(stack[1]),
	}, nil)
	if err != nil {
		panic(fmt.Errorf("could not register: %w", err))
	}
})
