package embind

import (
	"context"
	"fmt"
	"github.com/tetratelabs/wazero/api"
)

type anyType struct {
	baseType
}

func (at *anyType) FromWireType(ctx context.Context, mod api.Module, ptr uint64) (any, error) {
	return nil, fmt.Errorf("FromWireType on anyType should never be called")
}

func (at *anyType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	return 0, fmt.Errorf("ToWireType on anyType should never be called")
}

func (at *anyType) GoType() string {
	return "any"
}

func createAnyTypeArray(length int32) []registeredType {
	anyTypeArray := make([]registeredType, length)
	for i := range anyTypeArray {
		anyTypeArray[i] = &anyType{}
	}
	return anyTypeArray
}
