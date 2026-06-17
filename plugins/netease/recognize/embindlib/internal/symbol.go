package embind

import (
	"context"
	"fmt"
	"github.com/tetratelabs/wazero/api"
)

type ISymbol interface {
	Symbol() string
	ReturnType() IType
	ArgumentTypes() []IType
	IsOverload() bool
	OverloadCount() int
}

type publicSymbol struct {
	name          string
	argCount      *int32
	overloadTable map[int32]*publicSymbol
	fn            publicSymbolFn
	className     string
	argumentTypes []registeredType
	resultType    registeredType
	isStatic      bool
	isOverload    bool
	overloadCount int
}

func (ps *publicSymbol) Symbol() string {
	return ps.name
}

func (ps *publicSymbol) ReturnType() IType {
	if ps.resultType == nil {
		return nil
	}
	return &exposedType{ps.resultType}
}

func (ps *publicSymbol) IsOverload() bool {
	return ps.isOverload
}

func (ps *publicSymbol) OverloadCount() int {
	return ps.overloadCount
}

func (ps *publicSymbol) ArgumentTypes() []IType {
	exposedTypes := make([]IType, len(ps.argumentTypes))
	for i := range ps.argumentTypes {
		exposedTypes[i] = &exposedType{ps.argumentTypes[i]}
	}
	return exposedTypes
}

type PublicSymbol interface {
	Call(ctx context.Context, this any, arguments ...any) (any, error)
}

func (e *engine) CallPublicSymbol(ctx context.Context, name string, arguments ...any) (any, error) {
	_, ok := e.publicSymbols[name]
	if !ok {
		return nil, fmt.Errorf("could not find public symbol %s", name)
	}

	ctx = e.Attach(ctx)
	res, err := e.publicSymbols[name].fn(ctx, nil, arguments...)
	if err != nil {
		return nil, fmt.Errorf("error while calling embind function %s: %w", name, err)
	}

	return res, nil
}

func (e *engine) GetSymbols() []ISymbol {
	symbols := make([]ISymbol, 0)

	for i := range e.publicSymbols {
		if e.publicSymbols[i].overloadTable != nil {
			for argCount := range e.publicSymbols[i].overloadTable {
				e.publicSymbols[i].overloadTable[argCount].overloadCount = len(e.publicSymbols[i].overloadTable)
				symbols = append(symbols, e.publicSymbols[i].overloadTable[argCount])
			}
		} else {
			symbols = append(symbols, e.publicSymbols[i])
		}
	}

	return symbols
}

var RegisterFunction = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	namePtr := api.DecodeI32(stack[0])
	argCount := api.DecodeI32(stack[1])
	rawArgTypesAddr := api.DecodeI32(stack[2])
	signaturePtr := api.DecodeI32(stack[3])
	rawInvoker := api.DecodeI32(stack[4])
	fn := api.DecodeI32(stack[5])
	isAsync := api.DecodeI32(stack[6]) != 0

	argTypes, err := engine.heap32VectorToArray(argCount, rawArgTypesAddr)
	if err != nil {
		panic(fmt.Errorf("could not read arg types: %w", err))
	}

	name, err := engine.readCString(uint32(namePtr))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	name, err = getFunctionName(name)
	if err != nil {
		panic(fmt.Errorf("could not read function name: %w", err))
	}

	publicSymbolArgs := argCount - 1

	// Set a default callback that errors out when not all types are resolved.
	err = engine.exposePublicSymbol(name, func(ctx context.Context, this any, arguments ...any) (any, error) {
		return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call _embind_register_function %s due to unbound types", name), argTypes)
	}, &publicSymbolArgs)
	if err != nil {
		panic(fmt.Errorf("could not expose public symbol: %w", err))
	}

	// When all types are resolved, replace the callback with the actual implementation.
	err = engine.whenDependentTypesAreResolved([]int32{}, argTypes, func(argTypes []registeredType) ([]registeredType, error) {
		invokerArgsArray := []registeredType{argTypes[0] /* return value */, nil /* no class 'this'*/}
		invokerArgsArray = append(invokerArgsArray, argTypes[1:]... /* actual params */)

		expectedParamTypes := make([]api.ValueType, len(invokerArgsArray[2:])+1)
		expectedParamTypes[0] = api.ValueTypeI32 // fn
		for i := range invokerArgsArray[2:] {
			expectedParamTypes[i+1] = invokerArgsArray[i+2].NativeType()
		}

		// Create an api.Function to be able to invoke the function on the
		// Emscripten side.
		invokerFunc, err := engine.newInvokeFunc(signaturePtr, rawInvoker, expectedParamTypes, []api.ValueType{argTypes[0].NativeType()})
		if err != nil {
			return nil, fmt.Errorf("could not create _embind_register_function invoke func: %w", err)
		}

		err = engine.replacePublicSymbol(name, engine.craftInvokerFunction(name, invokerArgsArray, nil /* no class 'this'*/, invokerFunc, fn, isAsync), &publicSymbolArgs, argTypes[1:], argTypes[0])
		if err != nil {
			return nil, err
		}

		return []registeredType{}, nil
	})
	if err != nil {
		panic(fmt.Errorf("could not setup type dependenant lookup callbacks: %w", err))
	}
})
