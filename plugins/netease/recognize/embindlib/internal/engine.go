package embind

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental/table"
)

type engine struct {
	config               IEngineConfig
	mod                  api.Module
	publicSymbols        map[string]*publicSymbol
	registeredTypes      map[int32]registeredType
	typeDependencies     map[int32][]int32
	awaitingDependencies map[int32][]*awaitingDependency
	registeredConstants  map[string]*registeredConstant
	registeredEnums      map[string]*enumType
	registeredPointers   map[int32]*registeredPointer
	registeredClasses    map[string]*classType
	registeredClassTypes map[reflect.Type]*classType
	registeredTuples     map[int32]*registeredTuple
	registeredObjects    map[int32]*registeredObject
	registeredInstances  map[uint32]IClassBase
	deletionQueue        []IClassBase
	delayFunction        DelayFunction
	emvalEngine          *emvalEngine
}

func (e *engine) Attach(ctx context.Context) context.Context {
	return context.WithValue(ctx, EngineKey{}, e)
}

func (e *engine) RegisterConstant(name string, val any) error {
	_, ok := e.registeredConstants[name]
	if !ok {
		e.registeredConstants[name] = &registeredConstant{
			name: name,
		}
	}

	if e.registeredConstants[name].hasGoValue {
		return fmt.Errorf("constant %s is already registered", name)
	}

	e.registeredConstants[name].hasGoValue = true
	e.registeredConstants[name].goValue = val

	return e.registeredConstants[name].validate()
}

func (e *engine) RegisterEnum(name string, enum IEnum) error {
	_, ok := e.registeredEnums[name]
	if !ok {
		e.registeredEnums[name] = &enumType{
			valuesByName:     map[string]*enumValue{},
			valuesByCppValue: map[any]*enumValue{},
			valuesByGoValue:  map[any]*enumValue{},
		}
	}

	registeredEnum := e.registeredEnums[name]

	if registeredEnum.registeredInGo {
		return fmt.Errorf("constant %s is already registered", name)
	}

	registeredEnum.registeredInGo = true
	registeredEnum.goValue = enum.Type()

	values := enum.Values()
	for i := range values {
		_, ok = registeredEnum.valuesByName[i]
		if !ok {
			registeredEnum.valuesByName[i] = &enumValue{
				name: i,
			}
		}

		if registeredEnum.valuesByName[i].hasGoValue {
			return fmt.Errorf("enum value %s for enum %s was already registered", i, name)
		}

		registeredEnum.valuesByName[i].hasGoValue = true
		registeredEnum.valuesByName[i].goValue = values[i]
		registeredEnum.valuesByGoValue[values[i]] = registeredEnum.valuesByName[i]
	}

	return nil
}

func (e *engine) RegisterEmvalSymbol(name string, symbol any) error {
	existingSymbol, ok := e.emvalEngine.globals[name]
	if ok {
		return fmt.Errorf("could not register symbol %s, already registered as type %T", name, existingSymbol)
	}
	e.emvalEngine.globals[name] = symbol
	return nil
}

func (e *engine) RegisterClass(name string, class any) error {
	if _, ok := class.(IClassBase); !ok {
		return fmt.Errorf("could not register class %s with type %T, it does not embed embind.ClassBase", name, class)
	}

	reflectClassType := reflect.TypeOf(class)
	if reflectClassType.Kind() != reflect.Ptr {
		return fmt.Errorf("could not register class %s with type %T, given value should be a pointer type", name, class)
	}

	existingClass, ok := e.registeredClasses[name]
	if ok {
		if existingClass.hasGoStruct {
			return fmt.Errorf("could not register class %s, already registered as type %T", name, existingClass.goStruct)
		}
	} else {
		e.registeredClasses[name] = &classType{
			pureVirtualFunctions: []string{},
			methods:              map[string]*publicSymbol{},
			properties:           map[string]*classProperty{},
		}
	}

	e.registeredClasses[name].goStruct = class
	e.registeredClasses[name].hasGoStruct = true

	err := e.registeredClasses[name].validate()
	if err != nil {
		e.registeredClasses[name].goStruct = nil
		e.registeredClasses[name].hasGoStruct = false
	}

	e.registeredClassTypes[reflectClassType] = e.registeredClasses[name]

	return err
}

func (e *engine) EmvalToHandle(value any) int32 {
	return e.emvalEngine.toHandle(value)
}

func (e *engine) EmvalToValue(handle int32) (any, error) {
	return e.emvalEngine.toValue(handle)
}

func (e *engine) newInvokeFunc(signaturePtr, rawInvoker int32, expectedParamTypes, expectedResultTypes []api.ValueType) (api.Function, error) {
	// Not used in Wazero.
	signature, err := e.readCString(uint32(signaturePtr))
	if err != nil {
		panic(fmt.Errorf("could not read signature: %w", err))
	}

	// Filter out void result.
	if len(expectedResultTypes) == 1 && expectedResultTypes[0] == 0 {
		expectedResultTypes = []api.ValueType{}
	}

	var lookupErr error
	f := func() api.Function {
		defer func() {
			if recoverErr := recover(); recoverErr != nil {
				realError, ok := recoverErr.(error)
				if ok {
					lookupErr = fmt.Errorf("could not create invoke func for signature %s on invoker %d: %w", signature, rawInvoker, realError)
				}
				lookupErr = fmt.Errorf("could not create invoke func for signature %s on invoker %d: %v", signature, rawInvoker, recoverErr)
			}
		}()

		// Note: Emscripten doesn't use multiple tables
		return table.LookupFunction(e.mod, 0, uint32(rawInvoker), expectedParamTypes, expectedResultTypes)
	}()

	if lookupErr != nil {
		return nil, lookupErr
	}

	return f, nil
}

func (e *engine) heap32VectorToArray(count, firstElement int32) ([]int32, error) {
	array := make([]int32, count)
	for i := int32(0); i < count; i++ {
		val, ok := e.mod.Memory().ReadUint32Le(uint32(firstElement + (i * 4)))
		if !ok {
			return nil, errors.New("could not read uint32")
		}
		array[i] = int32(val)
	}
	return array, nil
}

func (e *engine) registerType(rawType int32, registeredInstance registeredType, options *registerTypeOptions) error {
	name := registeredInstance.Name()
	if rawType == 0 {
		return fmt.Errorf("type \"%s\" must have a positive integer typeid pointer", name)
	}

	_, ok := e.registeredTypes[rawType]
	if ok {
		if options != nil && options.ignoreDuplicateRegistrations {
			return nil
		} else {
			return fmt.Errorf("cannot register type '%s' twice", name)
		}
	}

	e.registeredTypes[rawType] = registeredInstance
	delete(e.typeDependencies, rawType)

	callbacks, ok := e.awaitingDependencies[rawType]
	if ok {
		delete(e.awaitingDependencies, rawType)
		for i := range callbacks {
			err := callbacks[i].cb()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *engine) ensureOverloadTable(registry map[string]*publicSymbol, methodName, humanName string) {
	if registry[methodName].overloadTable == nil {
		prevFunc := registry[methodName].fn
		prevArgCount := registry[methodName].argCount

		registry[methodName].isOverload = true

		// Inject an overload resolver function that routes to the appropriate overload based on the number of arguments.
		registry[methodName].fn = func(ctx context.Context, this any, arguments ...any) (any, error) {
			_, ok := registry[methodName].overloadTable[int32(len(arguments))]
			if !ok {
				possibleOverloads := make([]string, len(registry[methodName].overloadTable))
				for i := range registry[methodName].overloadTable {
					possibleOverloads[i] = strconv.Itoa(int(i))
				}
				sort.Strings(possibleOverloads)
				return nil, fmt.Errorf("function '%s' called with an invalid number of arguments (%d) - expects one of (%s)", humanName, len(arguments), strings.Join(possibleOverloads, ", "))
			}

			return registry[methodName].overloadTable[int32(len(arguments))].fn(ctx, this, arguments...)
		}

		// Move the previous function into the overload table.
		registry[methodName].overloadTable = map[int32]*publicSymbol{}
		registry[methodName].overloadTable[*prevArgCount] = &publicSymbol{
			name:          methodName,
			resultType:    registry[methodName].resultType,
			argumentTypes: registry[methodName].argumentTypes,
			argCount:      prevArgCount,
			fn:            prevFunc,
			isStatic:      registry[methodName].isStatic,
			isOverload:    true,
		}
	}
}

func (e *engine) exposePublicSymbol(name string, value publicSymbolFn, numArguments *int32) error {
	_, ok := e.publicSymbols[name]
	if ok {
		if numArguments == nil {
			return fmt.Errorf("cannot register public name '%s' twice", name)
		}

		_, ok = e.publicSymbols[name].overloadTable[*numArguments]
		if ok {
			return fmt.Errorf("cannot register public name '%s' twice", name)
		}

		e.ensureOverloadTable(e.publicSymbols, name, name)

		// What does this actually do? Looks like a bug in Emscripten JS.
		//if (Module.hasOwnProperty(numArguments)) {
		//	throwBindingError(`Cannot register multiple overloads of a function with the same number of arguments (${numArguments})!`);
		//}

		// Add the new function into the overload table.
		e.publicSymbols[name].overloadTable[*numArguments] = &publicSymbol{
			name:          name,
			argCount:      numArguments,
			fn:            value,
			isOverload:    true,
			argumentTypes: createAnyTypeArray(*numArguments),
			resultType:    &anyType{},
		}
	} else {
		e.publicSymbols[name] = &publicSymbol{
			name:       name,
			fn:         value,
			resultType: &anyType{},
		}

		if numArguments != nil {
			e.publicSymbols[name].argCount = numArguments
			e.publicSymbols[name].argumentTypes = createAnyTypeArray(*numArguments)
		}
	}

	return nil
}

func (e *engine) replacePublicSymbol(name string, value func(ctx context.Context, this any, arguments ...any) (any, error), numArguments *int32, argumentTypes []registeredType, resultType registeredType) error {
	_, ok := e.publicSymbols[name]
	if !ok {
		return fmt.Errorf("tried to replace a nonexistant public symbol %s", name)
	}

	// If there's an overload table for this symbol, replace the symbol in the overload table instead.
	if e.publicSymbols[name].overloadTable != nil && numArguments != nil {
		e.publicSymbols[name].overloadTable[*numArguments] = &publicSymbol{
			name:          name,
			argCount:      numArguments,
			fn:            value,
			argumentTypes: argumentTypes,
			resultType:    resultType,
			isOverload:    true,
		}
	} else {
		e.publicSymbols[name] = &publicSymbol{
			name:          name,
			argCount:      numArguments,
			fn:            value,
			argumentTypes: argumentTypes,
			resultType:    resultType,
		}
	}

	return nil
}

func (e *engine) whenDependentTypesAreResolved(myTypes, dependentTypes []int32, getTypeConverters func([]registeredType) ([]registeredType, error)) error {
	for i := range myTypes {
		e.typeDependencies[myTypes[i]] = dependentTypes
	}

	onComplete := func(typeConverters []registeredType) error {
		var myTypeConverters, err = getTypeConverters(typeConverters)
		if err != nil {
			return err
		}

		if len(myTypeConverters) != len(myTypes) {
			return fmt.Errorf("mismatched type converter count")
		}

		for i := range myTypes {
			err = e.registerType(myTypes[i], myTypeConverters[i], nil)
			if err != nil {
				return err
			}
		}

		return nil
	}

	typeConverters := make([]registeredType, len(dependentTypes))
	unregisteredTypes := make([]int32, 0)
	registered := 0

	for i := range dependentTypes {
		// Make a local var to use it inside the callback.
		myI := i
		dt := dependentTypes[i]
		_, ok := e.registeredTypes[dt]
		if ok {
			typeConverters[i] = e.registeredTypes[dt]
		} else {
			unregisteredTypes = append(unregisteredTypes, dt)
			_, ok = e.awaitingDependencies[dt]
			if !ok {
				e.awaitingDependencies[dt] = []*awaitingDependency{}
			}

			e.awaitingDependencies[dt] = append(e.awaitingDependencies[dt], &awaitingDependency{
				cb: func() error {
					typeConverters[myI] = e.registeredTypes[dt]
					registered++
					if registered == len(unregisteredTypes) {
						err := onComplete(typeConverters)
						if err != nil {
							return err
						}
					}

					return nil
				},
			})
		}
	}

	if 0 == len(unregisteredTypes) {
		err := onComplete(typeConverters)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *engine) craftInvokerFunction(humanName string, argTypes []registeredType, classType *registeredPointerType, cppInvokerFunc api.Function, cppTargetFunc int32, isAsync bool) publicSymbolFn {
	// humanName: a human-readable string name for the function to be generated.
	// argTypes: An array that contains the embind type objects for all types in the function signature.
	//    argTypes[0] is the type object for the function return value.
	//    argTypes[1] is the type object for function this object/class type, or null if not crafting an invoker for a class method.
	//    argTypes[2...] are the actual function parameters.
	// classType: The embind type object for the class to be bound, or null if this is not a method of a class.
	// cppInvokerFunc: JS Function object to the C++-side function that interops into C++ code.
	// cppTargetFunc: Function pointer (an integer to FUNCTION_TABLE) to the target C++ function the cppInvokerFunc will end up calling.
	// isAsync: Optional. If true, returns an async function. Async bindings are only supported with JSPI.
	argCount := len(argTypes)
	if argCount < 2 {
		panic(fmt.Errorf("argTypes array size mismatch! Must at least get return value and 'this' types"))
	}

	if isAsync {
		panic(fmt.Errorf("async bindings are only supported with JSPI"))
	}

	isClassMethodFunc := argTypes[1] != nil && classType != nil
	// Free functions with signature "void function()" do not need an invoker that marshalls between wire types.
	// TODO: This omits argument count check - enable only at -O3 or similar.
	//    if (ENABLE_UNSAFE_OPTS && argCount == 2 && argTypes[0].name == "void" && !isClassMethodFunc) {
	//       return FUNCTION_TABLE[fn];
	//    }

	// Determine if we need to use a dynamic stack to store the destructors for the function parameters.
	// TODO: Remove this completely once all function invokers are being dynamically generated.
	needsDestructorStack := false
	for i := 1; i < len(argTypes); i++ { // Skip return value at index 0 - it's not deleted here.
		if argTypes[i] != nil && argTypes[i].DestructorFunctionUndefined() { // The type does not define a destructor function - must use dynamic stack
			needsDestructorStack = true
			break
		}
	}

	returns := argTypes[0].Name() != "void"

	return func(ctx context.Context, this any, arguments ...any) (any, error) {
		if len(arguments) != argCount-2 {
			return nil, fmt.Errorf("function %s called with %d argument(s), expected %d arg(s)", humanName, len(arguments), argCount-2)
		}

		invoker := cppInvokerFunc
		fn := cppTargetFunc
		retType := argTypes[0]
		classParam := argTypes[1]

		var destructors *[]*destructorFunc

		if needsDestructorStack {
			destructors = &[]*destructorFunc{}
		}

		var thisWired uint64
		var err error

		if isClassMethodFunc {
			thisWired, err = classParam.ToWireType(ctx, e.mod, destructors, this)
			if err != nil {
				return nil, fmt.Errorf("could not get wire type of class param: %w", err)
			}
		}

		argsWired := make([]uint64, argCount-2)
		for i := 0; i < argCount-2; i++ {
			argsWired[i], err = argTypes[i+2].ToWireType(ctx, e.mod, destructors, arguments[i])
			if err != nil {
				return nil, fmt.Errorf("could not get wire type of argument %d (%s): %w", i, argTypes[i+2].Name(), err)
			}
		}

		callArgs := []uint64{api.EncodeI32(fn)}
		if isClassMethodFunc {
			callArgs = append(callArgs, thisWired)
		}
		callArgs = append(callArgs, argsWired...)

		res, err := invoker.Call(ctx, callArgs...)
		if err != nil {
			return nil, err
		}

		var returnVal any
		if returns {
			returnVal, err = retType.FromWireType(ctx, e.mod, res[0])
			if err != nil {
				return nil, fmt.Errorf("could not get wire type of return value (%s) on %T: %w", retType.Name(), retType, err)
			}
		}

		if needsDestructorStack {
			err = e.runDestructors(ctx, *destructors)
			if err != nil {
				return nil, err
			}
		} else {
			// Skip return value at index 0 - it's not deleted here. Also skip class type if not a method.
			startArg := 2
			if isClassMethodFunc {
				startArg = 1
			}

			for i := startArg; i < len(argTypes); i++ {
				ptrIndex := i
				if !isClassMethodFunc {
					ptrIndex -= 1
				}

				argDestructorFunc := argTypes[i].DestructorFunction(ctx, e.mod, api.DecodeU32(callArgs[ptrIndex]))
				if argDestructorFunc != nil {
					err = argDestructorFunc.run(ctx, e.mod)
					if err != nil {
						return nil, err
					}
				}
			}
		}

		return returnVal, nil
	}
}

type destructorFunc struct {
	function    string
	apiFunction api.Function
	args        []uint64
}

func (df *destructorFunc) run(ctx context.Context, mod api.Module) error {
	if df.apiFunction != nil {
		_, err := df.apiFunction.Call(ctx, df.args...)
		if err != nil {
			return err
		}
	} else {
		_, err := mod.ExportedFunction(df.function).Call(ctx, df.args...)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *engine) runDestructors(ctx context.Context, destructors []*destructorFunc) error {
	for i := range destructors {
		err := destructors[i].run(ctx, e.mod)
		if err != nil {
			return err
		}
	}

	return nil
}

// readCString reads a C string by reading byte per byte until it sees a NULL
// byte which is used as a string terminator in C.
// @todo: limit this so we won't try to read too much when some mistake is made?
func (e *engine) readCString(addr uint32) (string, error) {
	var sb strings.Builder
	for {
		b, success := e.mod.Memory().ReadByte(addr)
		if !success {
			return "", errors.New("could not read C string data")
		}

		// Stop when we encounter nil terminator of Cstring
		if b == 0 {
			break
		}

		// Write byte to string builder.
		sb.WriteByte(b)

		// Move to next char.
		addr++
	}

	return sb.String(), nil
}

// checkRegisteredTypeDependencies recursively loops through types to return
// which types not have registered on the engine yet. The seen map is used to
// keep track which types has been seen so the same type isn't reported or
// checked twice.
func (e *engine) checkRegisteredTypeDependencies(typeToVisit int32, seen *map[int32]bool) []int32 {
	unboundTypes := make([]int32, 0)
	seenMap := *seen
	if seenMap[typeToVisit] {
		return nil
	}

	_, ok := e.registeredTypes[typeToVisit]
	if ok {
		return nil
	}

	_, ok = e.typeDependencies[typeToVisit]
	if ok {
		for i := range e.typeDependencies[typeToVisit] {
			newUnboundTypes := e.checkRegisteredTypeDependencies(e.typeDependencies[typeToVisit][i], &seenMap)
			if newUnboundTypes != nil {
				unboundTypes = append(unboundTypes, newUnboundTypes...)
			}
		}
		return unboundTypes
	}

	unboundTypes = append(unboundTypes, typeToVisit)
	seenMap[typeToVisit] = true
	*seen = seenMap
	return unboundTypes
}

// getTypeName calls the Emscripten exported function __getTypeName to get a
// pointer to the C string that contains the type name.
func (e *engine) getTypeName(ctx context.Context, typeId int32) (string, error) {
	typeNameRes, err := e.mod.ExportedFunction("G").Call(ctx, api.EncodeI32(typeId))
	if err != nil {
		return "", err
	}

	ptr := api.DecodeI32(typeNameRes[0])
	rv, err := e.readCString(uint32(ptr))
	if err != nil {
		return "", err
	}

	_, err = e.mod.ExportedFunction("F").Call(ctx, api.EncodeI32(ptr))
	if err != nil {
		return "", err
	}

	return rv, nil
}

// createUnboundTypeError generated the error for when not all required type
// dependencies are resolved. It will traverse the dependency tree and list all
// the missing types by name.
func (e *engine) createUnboundTypeError(ctx context.Context, message string, types []int32) error {
	unregisteredTypes := []int32{}

	// The seen map is used to keep tracks of the seen dependencies, so that if
	// two types have the same dependency, it won't be listed or traversed twice.
	seen := map[int32]bool{}

	// Loop through all required types.
	for i := range types {
		// If we have any unregistered types, add them to the list
		newUnregisteredTypes := e.checkRegisteredTypeDependencies(types[i], &seen)
		if newUnregisteredTypes != nil {
			unregisteredTypes = append(unregisteredTypes, newUnregisteredTypes...)
		}
	}

	// Resolve the name for every unregistered type.
	typeNames := make([]string, len(unregisteredTypes))
	var err error
	for i := range unregisteredTypes {
		typeNames[i], err = e.getTypeName(ctx, unregisteredTypes[i])
		if err != nil {
			return err
		}
	}

	return fmt.Errorf("%s: %s", message, strings.Join(typeNames, ", "))
}

func (e *engine) getStringOrSymbol(address uint32) (string, error) {
	symbol, ok := e.emvalEngine.symbols[address]
	if ok {
		return symbol, nil
	}
	return e.readCString(address)
}

func (e *engine) requireRegisteredType(ctx context.Context, rawType int32, humanName string) (registeredType, error) {
	registeredType, ok := e.registeredTypes[rawType]
	if !ok {
		typeName, err := e.getTypeName(ctx, rawType)
		if err != nil {
			panic(err)
		}
		return nil, fmt.Errorf("%s has unknown type %s", humanName, typeName)
	}

	return registeredType, nil
}

func (e *engine) lookupTypes(ctx context.Context, argCount int32, argTypes int32) ([]registeredType, error) {
	types := make([]registeredType, argCount)

	for i := 0; i < int(argCount); i++ {
		rawType, ok := e.mod.Memory().ReadUint32Le(uint32((argTypes) + (int32(i) * 4)))
		if !ok {
			return nil, fmt.Errorf("could not read memory for the argument type")
		}

		requiredType, err := e.requireRegisteredType(ctx, int32(rawType), fmt.Sprintf("parameter %d", i))
		if err != nil {
			return nil, err
		}

		types[i] = requiredType
	}

	return types, nil
}

var illegalCharsRegex = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func (e *engine) makeLegalFunctionName(name string) string {
	// Replace illegal chars with underscore. In JS this is a dollar sign, but
	// that is not valid in Go.
	name = illegalCharsRegex.ReplaceAllString(name, `_`)

	// Prepend with underscore if it starts with a number.
	if name[0] >= '0' && name[0] <= '9' {
		name = "_" + name
	}

	return name
}

func (e *engine) upcastPointer(ctx context.Context, ptr uint32, ptrClass *classType, desiredClass *classType) (uint32, error) {
	for ptrClass != desiredClass {
		if ptrClass.upcast == nil {
			return 0, fmt.Errorf("expected null or instance of %s, got an instance of %s", desiredClass.name, ptrClass.name)
		}
		res, err := ptrClass.upcast.Call(ctx, api.EncodeU32(ptr))
		if err != nil {
			return 0, err
		}

		ptr = api.DecodeU32(res[0])
		ptrClass = ptrClass.baseClass
	}

	return ptr, nil
}

func (e *engine) validateThis(ctx context.Context, this any, classType *registeredPointerType, humanName string) (uint32, error) {
	if this == nil {
		return 0, fmt.Errorf("%s called with invalid \"this\"", humanName)
	}

	based, ok := this.(IClassBase)
	if !ok {
		return 0, fmt.Errorf("given value of type %T is not based on IClassBase", this)
	}

	if based.getPtr() == 0 {
		return 0, fmt.Errorf("cannot call emscripten binding method %s on deleted object", humanName)
	}

	// @todo: check if based.ptrType.registeredClass is or extends classType.registeredClass

	// todo: kill this (comment from Emscripten)
	return e.upcastPointer(ctx, based.getPtr(), based.getPtrType().registeredClass, classType.registeredClass)
}

func (e *engine) CountEmvalHandles() int {
	return len(e.emvalEngine.allocator.allocated) - len(e.emvalEngine.allocator.freelist) - e.emvalEngine.allocator.reserved
}

func (e *engine) registerInheritedInstance(ctx context.Context, registeredClass *classType, ptr uint32, instance IClassBase) error {
	ptr, err := instance.getPtrType().getBasestPointer(ctx, registeredClass, ptr)
	if err != nil {
		return err
	}

	_, ok := e.registeredInstances[ptr]
	if ok {
		return fmt.Errorf("tried to register registered instance: %d", ptr)
	}

	e.registeredInstances[ptr] = instance

	return nil
}

func (e *engine) unregisterInheritedInstance(ctx context.Context, registeredClass *classType, ptr uint32, instance IClassBase) error {
	ptr, err := instance.getPtrType().getBasestPointer(ctx, registeredClass, ptr)
	if err != nil {
		return err
	}

	_, ok := e.registeredInstances[ptr]
	if !ok {
		return fmt.Errorf("tried to unregister unregistered instance: %d", ptr)
	}

	delete(e.registeredInstances, ptr)

	return nil
}

func (e *engine) GetInheritedInstanceCount() int {
	return len(e.registeredInstances)
}

func (e *engine) GetLiveInheritedInstances() []IClassBase {
	instances := make([]IClassBase, len(e.registeredInstances))
	i := 0
	for id := range e.registeredInstances {
		instances[i] = e.registeredInstances[id]
		i++
	}
	return instances
}

func (e *engine) FlushPendingDeletes(ctx context.Context) error {
	for len(e.deletionQueue) > 0 {
		obj := e.deletionQueue[len(e.deletionQueue)-1]
		e.deletionQueue = e.deletionQueue[:len(e.deletionQueue)-1]

		obj.getRegisteredPtrTypeRecord().deleteScheduled = false
		err := obj.DeleteInstance(ctx, obj)
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *engine) SetDelayFunction(fn DelayFunction) error {
	e.delayFunction = fn

	if len(e.deletionQueue) > 0 && fn != nil {
		err := fn(func(ctx context.Context) error {
			return e.FlushPendingDeletes(ctx)
		})
		if err != nil {
			return err
		}
	}

	return nil
}
