package embind

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/tetratelabs/wazero/api"
)

type classProperty struct {
	name         string
	enumerable   bool
	configurable bool
	readOnly     bool
	isStatic     bool
	setterType   registeredType
	getterType   registeredType
	set          func(ctx context.Context, this any, v any) error
	get          func(ctx context.Context, this any) (any, error)
}

func (cp *classProperty) Name() string {
	return cp.name
}

func (cp *classProperty) GetterType() IType {
	if cp.getterType == nil {
		return nil
	}
	return &exposedType{
		registeredType: cp.getterType,
	}
}

func (cp *classProperty) SetterType() IType {
	if cp.setterType == nil {
		return nil
	}
	return &exposedType{
		registeredType: cp.setterType,
	}
}

func (cp *classProperty) ReadOnly() bool {
	return cp.readOnly
}

func (cp *classProperty) Static() bool {
	return cp.isStatic
}

type classConstructor struct {
	fn            publicSymbolFn
	argumentTypes []registeredType
	resultType    registeredType
}

type classType struct {
	baseType
	legalFunctionName    string
	baseClass            *classType
	rawDestructor        api.Function
	getActualType        api.Function
	upcast               api.Function
	downcast             api.Function
	derivedClasses       []*classType
	goStruct             any
	hasGoStruct          bool
	hasCppClass          bool
	pureVirtualFunctions []string
	methods              map[string]*publicSymbol
	properties           map[string]*classProperty
	constructors         map[int32]*classConstructor
}

func (erc *classType) FromWireType(ctx context.Context, mod api.Module, value uint64) (any, error) {
	panic("FromWireType should not be called on classes")
}

func (erc *classType) ToWireType(ctx context.Context, mod api.Module, destructors *[]*destructorFunc, o any) (uint64, error) {
	panic("ToWireType should not be called on classes")
}

func (erc *classType) ReadValueFromPointer(ctx context.Context, mod api.Module, pointer uint32) (any, error) {
	panic("ReadValueFromPointer should not be called on classes")
}

func (erc *classType) GoType() string {
	return erc.legalFunctionName
}

func (erc *classType) validate() error {
	if !erc.hasGoStruct || !erc.hasCppClass {
		return nil
	}

	// @todo: implement validator here.
	// @todo: we want to check if the Go struct implements everything we need.
	//log.Printf("Running validator on %T", erc.goStruct)

	//for i := range erc.constructors {
	//	log.Println(erc.constructors[i].argTypes)
	//	log.Println(erc.constructors[i].resultType)
	//}

	//log.Println(erc.constructors)
	///log.Println(erc.methods)
	//log.Println(erc.properties)

	return nil
}

func (erc *classType) isDeleted(ctx context.Context, handle IClassBase) bool {
	return handle.getRegisteredPtrTypeRecord().ptr == 0
}

func (erc *classType) deleteLater(ctx context.Context, handle IClassBase) (IClassBase, error) {
	registeredPtrTypeRecord := handle.getRegisteredPtrTypeRecord()
	if registeredPtrTypeRecord.ptr == 0 {
		return nil, fmt.Errorf("class handle already deleted")
	}

	if registeredPtrTypeRecord.deleteScheduled && !registeredPtrTypeRecord.preservePointerOnDelete {
		return nil, fmt.Errorf("object already scheduled for deletion")
	}

	e := MustGetEngineFromContext(ctx, nil).(*engine)
	e.deletionQueue = append(e.deletionQueue, handle)

	if len(e.deletionQueue) == 1 && e.delayFunction != nil {
		err := e.delayFunction(func(ctx context.Context) error {
			return e.FlushPendingDeletes(ctx)
		})
		if err != nil {
			return nil, err
		}
	}

	registeredPtrTypeRecord.deleteScheduled = true

	return handle, nil
}

func (erc *classType) isAliasOf(ctx context.Context, first, second IClassBase) (bool, error) {
	leftClass := first.getRegisteredPtrTypeRecord().ptrType.registeredClass
	left := first.getRegisteredPtrTypeRecord().ptr
	rightClass := second.getRegisteredPtrTypeRecord().ptrType.registeredClass
	right := second.getRegisteredPtrTypeRecord().ptr

	for leftClass.baseClass != nil {
		leftRes, err := leftClass.upcast.Call(ctx, api.EncodeU32(left))
		if err != nil {
			return false, err
		}
		left = api.DecodeU32(leftRes[0])
		leftClass = leftClass.baseClass
	}

	for rightClass.baseClass != nil {
		rightRes, err := rightClass.upcast.Call(ctx, api.EncodeU32(right))
		if err != nil {
			return false, err
		}
		right = api.DecodeU32(rightRes[0])
		rightClass = rightClass.baseClass
	}

	return leftClass == rightClass && left == right, nil
}

func (erc *classType) clone(ctx context.Context, from IClassBase) (IClassBase, error) {
	registeredPtrTypeRecord := from.getRegisteredPtrTypeRecord()
	if registeredPtrTypeRecord.ptr == 0 {
		return nil, fmt.Errorf("class handle already deleted")
	}

	if registeredPtrTypeRecord.preservePointerOnDelete {
		registeredPtrTypeRecord.count.value += 1
		return from, nil
	}

	clone, err := erc.getNewInstance(ctx, registeredPtrTypeRecord.shallowCopyInternalPointer())
	if err != nil {
		return nil, err
	}

	clone.getRegisteredPtrTypeRecord().count.value += 1
	clone.getRegisteredPtrTypeRecord().deleteScheduled = false
	return clone, nil
}

func (erc *classType) delete(ctx context.Context, handle IClassBase) error {
	registeredPtrTypeRecord := handle.getRegisteredPtrTypeRecord()
	if registeredPtrTypeRecord.ptr == 0 {
		return fmt.Errorf("class handle already deleted")
	}

	if registeredPtrTypeRecord.deleteScheduled && !registeredPtrTypeRecord.preservePointerOnDelete {
		return fmt.Errorf("object already scheduled for deletion")
	}

	// @TODO: We don't use the finalizer anymore. When we use the Go GC finalizer
	// 		  we should properly set something on the record to let the finalizer
	// 		  know it shouldn't do anything.
	//
	// err := registeredPtrTypeRecord.detachFinalizer(ctx)
	// if err != nil {
	// 	return err
	// }

	err := registeredPtrTypeRecord.releaseClassHandle(ctx)
	if err != nil {
		return err
	}

	if !registeredPtrTypeRecord.preservePointerOnDelete {
		registeredPtrTypeRecord.smartPtr = 0
		registeredPtrTypeRecord.ptr = 0
	}

	return nil
}

func (erc *classType) getNewInstance(ctx context.Context, record *registeredPointerTypeRecord) (IClassBase, error) {
	e := MustGetEngineFromContext(ctx, nil).(*engine)
	classBase := &ClassBase{
		classType:               erc,
		ptr:                     record.ptr,
		ptrType:                 record.ptrType,
		registeredPtrTypeRecord: record,
		engine:                  e,
	}

	// If we have a Go struct, wrap the resulting class in it.
	if erc.hasGoStruct {
		typeElem := reflect.TypeOf(erc.goStruct).Elem()
		newElem := reflect.New(typeElem)
		f := newElem.Elem().FieldByName("ClassBase")
		if f.IsValid() && f.CanSet() {
			f.Set(reflect.ValueOf(classBase))
		}

		result := newElem.Interface()

		return result.(IClassBase), nil
	}

	return classBase, nil
}

func (erc *classType) getDerivedClassesRecursive() []*classType {
	derivedClasses := []*classType{}
	for i := range erc.derivedClasses {
		derivedClasses = append(derivedClasses, erc.derivedClasses[i])
		derivedClasses = append(derivedClasses, erc.derivedClasses[i].getDerivedClassesRecursive()...)
	}
	return derivedClasses
}

type IClassType interface {
	Name() string
	Type() IType
	Properties() []IClassTypeProperty
	StaticProperties() []IClassTypeProperty
	Constructors() []IClassTypeConstructor
	Methods() []IClassTypeMethod
	StaticMethods() []IClassTypeMethod
}

type IClassTypeConstructor interface {
	Name() string
	Symbol() string
	ArgumentTypes() []IType
}

type IClassTypeProperty interface {
	Name() string
	GetterType() IType
	SetterType() IType
	ReadOnly() bool
}

type IClassTypeMethod interface {
	Symbol() string
	ReturnType() IType
	ArgumentTypes() []IType
	IsOverload() bool
	OverloadCount() int
}

func (erc *classType) Name() string {
	return erc.legalFunctionName
}

func (erc *classType) Type() IType {
	return &exposedType{registeredType: erc}
}

func (erc *classType) Properties() []IClassTypeProperty {
	properties := make([]IClassTypeProperty, 0)

	for i := range erc.properties {
		if erc.properties[i].isStatic {
			continue
		}
		properties = append(properties, erc.properties[i])
	}

	return properties
}

func (erc *classType) StaticProperties() []IClassTypeProperty {
	properties := make([]IClassTypeProperty, 0)

	for i := range erc.properties {
		if !erc.properties[i].isStatic {
			continue
		}
		properties = append(properties, erc.properties[i])
	}

	return properties
}

func (erc *classType) Methods() []IClassTypeMethod {
	methods := make([]IClassTypeMethod, 0)

	for i := range erc.methods {
		if erc.methods[i].isStatic {
			continue
		}

		if erc.methods[i].overloadTable != nil {
			for overload := range erc.methods[i].overloadTable {
				erc.methods[i].overloadTable[overload].overloadCount = len(erc.methods[i].overloadTable)
				methods = append(methods, erc.methods[i].overloadTable[overload])
			}
		} else {
			methods = append(methods, erc.methods[i])
		}
	}

	return methods
}

func (erc *classType) StaticMethods() []IClassTypeMethod {
	methods := make([]IClassTypeMethod, 0)

	for i := range erc.methods {
		if !erc.methods[i].isStatic {
			continue
		}

		if erc.methods[i].overloadTable != nil {
			for overload := range erc.methods[i].overloadTable {
				erc.methods[i].overloadTable[overload].overloadCount = len(erc.methods[i].overloadTable)
				methods = append(methods, erc.methods[i].overloadTable[overload])
			}
		} else {
			methods = append(methods, erc.methods[i])
		}
	}

	return methods
}

type exposedClassConstructor struct {
	name             string
	classConstructor *classConstructor
}

func (ecc *exposedClassConstructor) Name() string {
	return ecc.name
}

func (ecc *exposedClassConstructor) Symbol() string {
	return ecc.name
}

func (ecc *exposedClassConstructor) ArgumentTypes() []IType {
	exposedTypes := make([]IType, len(ecc.classConstructor.argumentTypes))
	for i := range ecc.classConstructor.argumentTypes {
		exposedTypes[i] = &exposedType{ecc.classConstructor.argumentTypes[i]}
	}
	return exposedTypes
}

func (erc *classType) Constructors() []IClassTypeConstructor {
	constructors := make([]IClassTypeConstructor, 0)

	for i := range erc.constructors {
		constructor := &exposedClassConstructor{
			name:             "",
			classConstructor: erc.constructors[i],
		}

		if len(erc.constructors) > 1 {
			constructor.name = fmt.Sprintf("%d", i)
		}

		constructors = append(constructors, constructor)
	}

	return constructors
}

func (e *engine) GetClasses() []IClassType {
	classes := make([]IClassType, 0)
	for i := range e.registeredClasses {
		classes = append(classes, e.registeredClasses[i])
	}
	return classes
}

type ClassBase struct {
	engine                  *engine
	classType               *classType
	ptr                     uint32
	ptrType                 *registeredPointerType
	registeredPtrTypeRecord *registeredPointerTypeRecord
}

func (ecb *ClassBase) getClassType() *classType {
	return ecb.classType
}

func (ecb *ClassBase) String() string {
	return fmt.Sprintf("%s, ptr: %d", ecb.classType.name, ecb.ptr)
}

func (ecb *ClassBase) getPtr() uint32 {
	return ecb.ptr
}

func (ecb *ClassBase) getPtrType() *registeredPointerType {
	return ecb.ptrType
}

func (ecb *ClassBase) getRegisteredPtrTypeRecord() *registeredPointerTypeRecord {
	return ecb.registeredPtrTypeRecord
}

func (ecb *ClassBase) isValid() bool {
	return ecb != nil
}

func (ecb *ClassBase) CloneInstance(ctx context.Context, this IClassBase) (IClassBase, error) {
	return ecb.classType.clone(ctx, this)
}

func (ecb *ClassBase) DeleteInstance(ctx context.Context, this IClassBase) error {
	return ecb.classType.delete(ctx, this)
}

func (ecb *ClassBase) DeleteInstanceLater(ctx context.Context, this IClassBase) (IClassBase, error) {
	return ecb.classType.deleteLater(ctx, this)
}

func (ecb *ClassBase) IsInstanceDeleted(ctx context.Context, this IClassBase) bool {
	return ecb.classType.isDeleted(ctx, this)
}

func (ecb *ClassBase) IsAliasOfInstance(ctx context.Context, this IClassBase, second IClassBase) (bool, error) {
	return ecb.classType.isAliasOf(ctx, this, second)
}

func (ecb *ClassBase) CallInstanceMethod(ctx context.Context, this any, name string, arguments ...any) (any, error) {
	method, ok := ecb.classType.methods[name]
	if !ok {
		return nil, fmt.Errorf("%s.%s() is not found", ecb.classType.name, name)
	}

	// Ensure that the engine is attached. Allows calling methods on the class
	// without keeping track of the engine.
	ctx = ecb.engine.Attach(ctx)

	if method.isStatic && this != nil {
		return nil, fmt.Errorf("%s.%s() is static", ecb.classType.name, name)
	}

	return method.fn(ctx, this, arguments...)
}

func (ecb *ClassBase) SetInstanceProperty(ctx context.Context, this any, name string, value any) error {
	property, ok := ecb.classType.properties[name]
	if !ok {
		return fmt.Errorf("%s.%s is a not found", ecb.classType.name, name)
	}

	// Ensure that the engine is attached. Allows setting properties on the
	// class without keeping track of the engine.
	ctx = ecb.engine.Attach(ctx)

	if property.Static() && this != nil {
		return fmt.Errorf("%s.%s is static", ecb.classType.name, name)
	}

	if property.ReadOnly() {
		return fmt.Errorf("%s.%s is a read-only property", ecb.classType.name, name)
	}

	return property.set(ctx, this, value)
}

func (ecb *ClassBase) GetInstanceProperty(ctx context.Context, this any, name string) (any, error) {
	property, ok := ecb.classType.properties[name]
	if !ok {
		return nil, fmt.Errorf("%s.%s is a not found", ecb.classType.name, name)
	}

	// Ensure that the engine is attached. Allows setting properties on the
	// class without keeping track of the engine.
	ctx = ecb.engine.Attach(ctx)

	if property.Static() && this != nil {
		return nil, fmt.Errorf("%s.%s is static", ecb.classType.name, name)
	}

	return property.get(ctx, this)
}

func (ecb *ClassBase) DeleteInheritedInstance(ctx context.Context) error {
	// detachFinalizer(ecb);
	err := ecb.engine.unregisterInheritedInstance(ctx, ecb.classType, ecb.ptr, ecb)
	if err != nil {
		return fmt.Errorf("could not unregister instance: %w", err)
	}
	return nil
}

type IClassBase interface {
	getClassType() *classType
	getPtr() uint32
	getPtrType() *registeredPointerType
	getRegisteredPtrTypeRecord() *registeredPointerTypeRecord
	isValid() bool
	CloneInstance(ctx context.Context, this IClassBase) (IClassBase, error)
	DeleteInstance(ctx context.Context, this IClassBase) error
	DeleteInstanceLater(ctx context.Context, this IClassBase) (IClassBase, error)
	IsInstanceDeleted(ctx context.Context, this IClassBase) bool
	IsAliasOfInstance(ctx context.Context, this IClassBase, second IClassBase) (bool, error)
	CallInstanceMethod(ctx context.Context, this any, name string, arguments ...any) (any, error)
	SetInstanceProperty(ctx context.Context, this any, name string, value any) error
	GetInstanceProperty(ctx context.Context, this any, name string) (any, error)
}

var RegisterClass = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawType := api.DecodeI32(stack[0])
	rawPointerType := api.DecodeI32(stack[1])
	rawConstPointerType := api.DecodeI32(stack[2])
	baseClassRawType := api.DecodeI32(stack[3])
	getActualTypeSignature := api.DecodeI32(stack[4])
	getActualType := api.DecodeI32(stack[5])
	upcastSignature := api.DecodeI32(stack[6])
	upcast := api.DecodeI32(stack[7])
	downcastSignature := api.DecodeI32(stack[8])
	downcast := api.DecodeI32(stack[9])
	namePtr := api.DecodeI32(stack[10])
	destructorSignature := api.DecodeI32(stack[11])
	rawDestructor := api.DecodeI32(stack[12])

	name, err := engine.readCString(uint32(namePtr))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	getActualTypeFunc, err := engine.newInvokeFunc(getActualTypeSignature, getActualType, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})
	if err != nil {
		panic(fmt.Errorf("could not read getActualType: %w", err))
	}

	var upcastFunc api.Function
	if upcast > 0 {
		upcastFunc, err = engine.newInvokeFunc(upcastSignature, upcast, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})
		if err != nil {
			panic(fmt.Errorf("could not read upcast: %w", err))
		}
	}

	var downcastFunc api.Function
	if downcast > 0 {
		downcastFunc, err = engine.newInvokeFunc(downcastSignature, downcast, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})
		if err != nil {
			panic(fmt.Errorf("could not read downcast: %w", err))
		}
	}

	rawDestructorFunc, err := engine.newInvokeFunc(destructorSignature, rawDestructor, []api.ValueType{api.ValueTypeI32}, []api.ValueType{})
	if err != nil {
		panic(fmt.Errorf("could not read rawDestructor: %w", err))
	}

	legalFunctionName := engine.makeLegalFunctionName(name)

	// Set a default callback that errors out when not all types are resolved.
	err = engine.exposePublicSymbol(legalFunctionName, func(ctx context.Context, this any, arguments ...any) (any, error) {
		return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", name), []int32{baseClassRawType})
	}, nil)
	if err != nil {
		panic(fmt.Errorf("could not expose public symbol: %w", err))
	}

	dependentTypes := make([]int32, 0)
	if baseClassRawType > 0 {
		dependentTypes = append(dependentTypes, baseClassRawType)
	}

	err = engine.whenDependentTypesAreResolved([]int32{rawType, rawPointerType, rawConstPointerType}, dependentTypes, func(resolvedTypes []registeredType) ([]registeredType, error) {
		existingClass, ok := engine.registeredClasses[name]
		if ok {
			if existingClass.baseType.rawType != 0 {
				return nil, fmt.Errorf("could not register class %s, already registered as raw type %d", name, existingClass.baseType.rawType)
			}
		} else {
			engine.registeredClasses[name] = &classType{
				baseType: baseType{
					rawType: rawType,
					name:    name,
				},
				pureVirtualFunctions: []string{},
				methods:              map[string]*publicSymbol{},
				properties:           map[string]*classProperty{},
			}
		}

		engine.registeredClasses[name].hasCppClass = true
		engine.registeredClasses[name].legalFunctionName = legalFunctionName
		engine.registeredClasses[name].rawDestructor = rawDestructorFunc
		engine.registeredClasses[name].getActualType = getActualTypeFunc
		engine.registeredClasses[name].upcast = upcastFunc
		engine.registeredClasses[name].downcast = downcastFunc

		if baseClassRawType > 0 {
			engine.registeredClasses[name].baseClass = resolvedTypes[0].(*registeredPointerType).registeredClass
			if engine.registeredClasses[name].baseClass.derivedClasses == nil {
				engine.registeredClasses[name].baseClass.derivedClasses = []*classType{engine.registeredClasses[name]}
			} else {
				engine.registeredClasses[name].baseClass.derivedClasses = append(engine.registeredClasses[name].baseClass.derivedClasses, engine.registeredClasses[name])
			}
		}

		referenceConverter := &registeredPointerType{
			baseType: baseType{
				argPackAdvance: GenericWireTypeSize,
				name:           name,
			},
			registeredClass: engine.registeredClasses[name],
			isReference:     true,
			isConst:         false,
			isSmartPointer:  false,
		}

		pointerConverter := &registeredPointerType{
			baseType: baseType{
				argPackAdvance: GenericWireTypeSize,
				name:           name + "*",
			},
			registeredClass: engine.registeredClasses[name],
			isReference:     false,
			isConst:         false,
			isSmartPointer:  false,
		}

		constPointerConverter := &registeredPointerType{
			baseType: baseType{
				argPackAdvance: GenericWireTypeSize,
				name:           name + " const*",
			},
			registeredClass: engine.registeredClasses[name],
			isReference:     false,
			isConst:         true,
			isSmartPointer:  false,
		}

		engine.registeredPointers[rawType] = &registeredPointer{
			pointerType:      pointerConverter,
			constPointerType: constPointerConverter,
		}

		err := engine.registeredClasses[name].validate()
		if err != nil {
			return nil, err
		}

		err = engine.replacePublicSymbol(legalFunctionName, func(ctx context.Context, _ any, arguments ...any) (any, error) {
			if engine.registeredClasses[name].constructors == nil {
				return nil, fmt.Errorf("%s has no accessible constructor", name)
			}

			constructor, ok := engine.registeredClasses[name].constructors[int32(len(arguments))]
			if !ok {
				availableLengths := make([]string, 0)
				for i := range engine.registeredClasses[name].constructors {
					availableLengths = append(availableLengths, strconv.Itoa(int(i)))
				}
				sort.Strings(availableLengths)
				return nil, fmt.Errorf("tried to invoke ctor of %s with invalid number of parameters (%d) - expected (%s) parameters instead", name, len(arguments), strings.Join(availableLengths, " or "))
			}

			return constructor.fn(ctx, nil, arguments...)
		}, nil, nil, referenceConverter)

		if err != nil {
			panic(fmt.Errorf("could not replace public symbol: %w", err))
		}

		return []registeredType{referenceConverter, pointerConverter, constPointerConverter}, nil
	})

	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})

var RegisterClassConstructor = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawClassType := api.DecodeI32(stack[0])
	argCount := api.DecodeI32(stack[1])
	rawArgTypesAddr := api.DecodeI32(stack[2])
	invokerSignature := api.DecodeI32(stack[3])
	invoker := api.DecodeI32(stack[4])
	rawConstructor := api.DecodeI32(stack[5])

	rawArgTypes, err := engine.heap32VectorToArray(argCount, rawArgTypesAddr)
	if err != nil {
		panic(fmt.Errorf("could not read arg types: %w", err))
	}

	err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawClassType}, func(resolvedTypes []registeredType) ([]registeredType, error) {
		classType := resolvedTypes[0].(*registeredPointerType)
		humanName := "constructor " + classType.name

		if classType.registeredClass.constructors == nil {
			classType.registeredClass.constructors = map[int32]*classConstructor{}
		}

		if _, ok := classType.registeredClass.constructors[argCount-1]; ok {
			return nil, fmt.Errorf("cannot register multiple constructors with identical number of parameters (%d) for class '%s'! Overload resolution is currently only performed using the parameter count, not actual type info", argCount-1, classType.name)
		}

		classType.registeredClass.constructors[argCount-1] = &classConstructor{
			fn: func(ctx context.Context, this any, arguments ...any) (any, error) {
				return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", classType.name), rawArgTypes)
			},
			argumentTypes: createAnyTypeArray(argCount - 1),
			resultType:    &anyType{},
		}

		err := engine.whenDependentTypesAreResolved([]int32{}, rawArgTypes, func(argTypes []registeredType) ([]registeredType, error) {
			// Insert empty slot for context type (argTypes[1]).
			newArgTypes := []registeredType{argTypes[0], nil}
			if len(argTypes) > 1 {
				newArgTypes = append(newArgTypes, argTypes[1:]...)
			}

			expectedParamTypes := make([]api.ValueType, len(newArgTypes[2:])+1)
			expectedParamTypes[0] = api.ValueTypeI32 // fn
			for i := range newArgTypes[2:] {
				expectedParamTypes[i+1] = newArgTypes[i+2].NativeType()
			}

			invokerFunc, err := engine.newInvokeFunc(invokerSignature, invoker, expectedParamTypes, []api.ValueType{argTypes[0].NativeType()})
			if err != nil {
				return nil, fmt.Errorf("could not create invoke func: %w", err)
			}

			classType.registeredClass.constructors[argCount-1].resultType = argTypes[0]
			classType.registeredClass.constructors[argCount-1].argumentTypes = argTypes[1:]
			classType.registeredClass.constructors[argCount-1].fn = engine.craftInvokerFunction(humanName, newArgTypes, nil, invokerFunc, rawConstructor, false)
			return []registeredType{}, err
		})

		return []registeredType{}, err
	})

	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})

var RegisterClassFunction = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawClassType := api.DecodeI32(stack[0])
	methodNamePtr := api.DecodeI32(stack[1])
	argCount := api.DecodeI32(stack[2])
	rawArgTypesAddr := api.DecodeI32(stack[3])
	invokerSignature := api.DecodeI32(stack[4])
	rawInvoker := api.DecodeI32(stack[5])
	contextPtr := api.DecodeI32(stack[6])
	isPureVirtual := api.DecodeI32(stack[7])
	isAsync := api.DecodeI32(stack[8])

	rawArgTypes, err := engine.heap32VectorToArray(argCount, rawArgTypesAddr)
	if err != nil {
		panic(fmt.Errorf("could not read arg types: %w", err))
	}

	methodName, err := engine.readCString(uint32(methodNamePtr))
	if err != nil {
		panic(fmt.Errorf("could not read method name: %w", err))
	}

	methodName, err = getFunctionName(methodName)
	if err != nil {
		panic(fmt.Errorf("could not read method name: %w", err))
	}

	err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawClassType}, func(classTypes []registeredType) ([]registeredType, error) {
		classType := classTypes[0].(*registeredPointerType)
		humanName := classType.Name() + "." + methodName

		if strings.HasPrefix(methodName, "@@") {
			return nil, fmt.Errorf("could not get function name %s: well-known symbols are not supported in Go", methodName)
		}

		if isPureVirtual > 0 {
			classType.registeredClass.pureVirtualFunctions = append(classType.registeredClass.pureVirtualFunctions, methodName)
		}

		unboundTypesHandler := &publicSymbol{
			name: methodName,
			fn: func(ctx context.Context, this any, arguments ...any) (any, error) {
				return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", humanName), rawArgTypes)
			},
			argumentTypes: createAnyTypeArray(argCount - 2),
			resultType:    &anyType{},
		}

		newMethodArgCount := argCount - 2
		existingMethod, ok := classType.registeredClass.methods[methodName]
		if !ok || (existingMethod.overloadTable == nil && existingMethod.className != classType.name && *existingMethod.argCount == newMethodArgCount) {
			// This is the first overload to be registered, OR we are replacing a
			// function in the base class with a function in the derived class.
			unboundTypesHandler.argCount = &newMethodArgCount
			unboundTypesHandler.className = classType.name
			unboundTypesHandler.isOverload = true
			classType.registeredClass.methods[methodName] = unboundTypesHandler
		} else {
			// There was an existing function with the same name registered. Set up
			// a function overload routing table.
			engine.ensureOverloadTable(classType.registeredClass.methods, methodName, humanName)
			classType.registeredClass.methods[methodName].overloadTable[argCount-2] = unboundTypesHandler
		}

		err = engine.whenDependentTypesAreResolved([]int32{}, rawArgTypes, func(argTypes []registeredType) ([]registeredType, error) {
			expectedResultTypes := make([]api.ValueType, len(argTypes))
			expectedResultTypes[0] = api.ValueTypeI32 // contextPtr
			for i := range argTypes[1:] {
				expectedResultTypes[i+1] = argTypes[i+1].NativeType()
			}

			rawInvokerFunc, err := engine.newInvokeFunc(invokerSignature, rawInvoker, expectedResultTypes, []api.ValueType{argTypes[0].NativeType()})
			if err != nil {
				panic(fmt.Errorf("could not create _embind_register_class_function raw invoke func: %w", err))
			}

			fn := engine.craftInvokerFunction(humanName, argTypes, classType, rawInvokerFunc, contextPtr, isAsync > 0)

			memberFunction := &publicSymbol{
				name:          methodName,
				resultType:    argTypes[0],
				argumentTypes: argTypes[2:],
				fn:            fn,
			}

			// Replace the initial unbound-handler-stub function with the appropriate member function, now that all types
			// are resolved. If multiple overloads are registered for this function, the function goes into an overload table.
			if classType.registeredClass.methods[methodName].overloadTable == nil {
				// Set argCount in case an overload is registered later
				memberFunction.argCount = &newMethodArgCount
				classType.registeredClass.methods[methodName] = memberFunction
			} else {
				memberFunction.isOverload = true
				classType.registeredClass.methods[methodName].overloadTable[argCount-2] = memberFunction
			}

			derivesClasses := classType.registeredClass.getDerivedClassesRecursive()
			if derivesClasses != nil {
				for i := range derivesClasses {
					derivedMemberFunction := &publicSymbol{
						name:          methodName,
						resultType:    argTypes[0],
						argumentTypes: argTypes[2:],
						fn:            fn,
					}

					derivedClass := derivesClasses[i]
					_, ok := derivedClass.methods[methodName]
					if !ok {
						derivedMemberFunction.argCount = &newMethodArgCount
						// This is the first function to be registered with this name.
						derivedClass.methods[methodName] = derivedMemberFunction
					} else {
						// There was an existing function with the same name registered. Set up
						// a function overload routing table.
						engine.ensureOverloadTable(derivedClass.methods, methodName, humanName)
						derivedMemberFunction.isOverload = true

						// Do not override already registered methods.
						_, ok := derivedClass.methods[methodName].overloadTable[argCount-2]
						if !ok {
							derivedClass.methods[methodName].overloadTable[argCount-2] = derivedMemberFunction
						}
					}
				}
			}

			return []registeredType{}, nil
		})

		return []registeredType{}, err
	})

	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})

var RegisterClassClassFunction = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawClassType := api.DecodeI32(stack[0])
	methodNamePtr := api.DecodeI32(stack[1])
	argCount := api.DecodeI32(stack[2])
	rawArgTypesAddr := api.DecodeI32(stack[3])
	invokerSignature := api.DecodeI32(stack[4])
	rawInvoker := api.DecodeI32(stack[5])
	fn := api.DecodeI32(stack[6])
	isAsync := api.DecodeI32(stack[7])

	rawArgTypes, err := engine.heap32VectorToArray(argCount, rawArgTypesAddr)
	if err != nil {
		panic(fmt.Errorf("could not read arg types: %w", err))
	}

	methodName, err := engine.readCString(uint32(methodNamePtr))
	if err != nil {
		panic(fmt.Errorf("could not read method name: %w", err))
	}

	methodName, err = getFunctionName(methodName)
	if err != nil {
		panic(fmt.Errorf("could not read method name: %w", err))
	}

	err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawClassType}, func(classTypes []registeredType) ([]registeredType, error) {
		classType := classTypes[0].(*registeredPointerType)
		humanName := classType.Name() + "." + methodName

		if strings.HasPrefix(methodName, "@@") {
			return nil, fmt.Errorf("could not get class function name %s: well-known symbols are not supported in Go", methodName)
		}

		unboundTypesHandler := &publicSymbol{
			name:     methodName,
			isStatic: true,
			fn: func(ctx context.Context, this any, arguments ...any) (any, error) {
				return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot call %s due to unbound types", humanName), rawArgTypes)
			},
			argumentTypes: createAnyTypeArray(argCount - 1),
			resultType:    &anyType{},
		}

		newArgCount := argCount - 1
		_, ok := classType.registeredClass.methods[methodName]
		if !ok {
			// This is the first function to be registered with this name.
			unboundTypesHandler.argCount = &newArgCount
			classType.registeredClass.methods[methodName] = unboundTypesHandler
		} else {
			// There was an existing function with the same name registered. Set up
			// a function overload routing table.
			engine.ensureOverloadTable(classType.registeredClass.methods, methodName, humanName)
			classType.registeredClass.methods[methodName].overloadTable[argCount-1] = unboundTypesHandler
		}

		err = engine.whenDependentTypesAreResolved([]int32{}, rawArgTypes, func(argTypes []registeredType) ([]registeredType, error) {
			invokerArgsArray := []registeredType{argTypes[0], nil}
			invokerArgsArray = append(invokerArgsArray, argTypes[1:]...)

			expectedParamTypes := make([]api.ValueType, len(invokerArgsArray[2:])+1)
			expectedParamTypes[0] = api.ValueTypeI32 // fn
			for i := range invokerArgsArray[2:] {
				expectedParamTypes[i+1] = invokerArgsArray[i+2].NativeType()
			}

			rawInvokerFunc, err := engine.newInvokeFunc(invokerSignature, rawInvoker, expectedParamTypes, []api.ValueType{argTypes[0].NativeType()})
			if err != nil {
				panic(fmt.Errorf("could not create raw invoke func: %w", err))
			}

			fn := engine.craftInvokerFunction(humanName, invokerArgsArray, nil, rawInvokerFunc, fn, isAsync > 0)
			memberFunction := &publicSymbol{
				name:          methodName,
				argumentTypes: argTypes[1:],
				resultType:    argTypes[0],
				isStatic:      true,
				fn:            fn,
			}

			// Replace the initial unbound-handler-stub function with the appropriate member function, now that all types
			// are resolved. If multiple overloads are registered for this function, the function goes into an overload table.
			if classType.registeredClass.methods[methodName].overloadTable == nil {
				// Set argCount in case an overload is registered later
				memberFunction.argCount = &newArgCount
				classType.registeredClass.methods[methodName] = memberFunction
			} else {
				memberFunction.isOverload = true
				classType.registeredClass.methods[methodName].overloadTable[argCount-1] = memberFunction
			}

			derivesClasses := classType.registeredClass.getDerivedClassesRecursive()
			if derivesClasses != nil {
				for i := range derivesClasses {
					derivedMemberFunction := &publicSymbol{
						name:          methodName,
						argumentTypes: argTypes[1:],
						resultType:    argTypes[0],
						isStatic:      true,
						fn:            fn,
					}

					derivedClass := derivesClasses[i]
					_, ok := derivedClass.methods[methodName]
					if !ok {
						// This is the first function to be registered with this name.
						derivedMemberFunction.argCount = &newArgCount
						derivedClass.methods[methodName] = derivedMemberFunction
					} else {
						// There was an existing function with the same name registered. Set up
						// a function overload routing table.
						engine.ensureOverloadTable(derivedClass.methods, methodName, humanName)
						derivedMemberFunction.isOverload = true

						// Do not override already registered methods.
						_, ok := derivedClass.methods[methodName].overloadTable[argCount-1]
						if !ok {
							derivedClass.methods[methodName].overloadTable[argCount-1] = derivedMemberFunction
						}
					}
				}
			}

			return []registeredType{}, nil
		})

		return []registeredType{}, err
	})
	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})

var RegisterClassClassProperty = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawClassType := api.DecodeI32(stack[0])
	fieldNamePtr := api.DecodeI32(stack[1])
	rawFieldType := api.DecodeI32(stack[2])
	rawFieldPtr := api.DecodeI32(stack[3])
	getterSignaturePtr := api.DecodeI32(stack[4])
	getter := api.DecodeI32(stack[5])
	setterSignaturePtr := api.DecodeI32(stack[6])
	setter := api.DecodeI32(stack[7])

	fieldName, err := engine.readCString(uint32(fieldNamePtr))
	if err != nil {
		panic(fmt.Errorf("could not read method name: %w", err))
	}

	getterFunc, err := engine.newInvokeFunc(getterSignaturePtr, getter, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})
	if err != nil {
		panic(fmt.Errorf("could not read getter: %w", err))
	}

	err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawClassType}, func(classTypes []registeredType) ([]registeredType, error) {
		classType := classTypes[0].(*registeredPointerType)

		humanName := classType.Name() + "." + fieldName

		desc := &classProperty{
			name:     fieldName,
			isStatic: true,
			get: func(ctx context.Context, this any) (any, error) {
				return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot access %s due to unbound types", humanName), []int32{rawFieldType})
			},
			getterType:   &anyType{},
			enumerable:   true,
			configurable: true,
		}

		if setter > 0 {
			desc.setterType = &anyType{}
			desc.set = func(ctx context.Context, this any, v any) error {
				return engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot access %s due to unbound types", humanName), []int32{rawFieldType})
			}
		} else {
			desc.readOnly = true
			desc.set = func(ctx context.Context, this any, v any) error {
				return fmt.Errorf("%s is a read-only property", humanName)
			}
		}

		classType.registeredClass.properties[fieldName] = desc
		err = engine.whenDependentTypesAreResolved([]int32{}, []int32{rawFieldType}, func(fieldTypes []registeredType) ([]registeredType, error) {
			fieldType := fieldTypes[0]

			desc := &classProperty{
				name:       fieldName,
				getterType: fieldType,
				isStatic:   true,
				get: func(ctx context.Context, this any) (any, error) {
					res, err := getterFunc.Call(ctx, api.EncodeI32(rawFieldPtr))
					if err != nil {
						return nil, err
					}
					return fieldType.FromWireType(ctx, engine.mod, res[0])
				},
				enumerable: true,
				readOnly:   true,
			}

			if setter > 0 {
				setterFunc, err := engine.newInvokeFunc(setterSignaturePtr, setter, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{})
				if err != nil {
					return nil, fmt.Errorf("could not create _embind_register_class_class_property setterFunc: %w", err)
				}

				desc.readOnly = false
				desc.setterType = fieldType
				desc.set = func(ctx context.Context, this any, v any) error {
					destructors := &[]*destructorFunc{}
					setterRes, err := fieldType.ToWireType(ctx, engine.mod, destructors, v)
					if err != nil {
						return err
					}

					_, err = setterFunc.Call(ctx, api.EncodeI32(rawFieldPtr), setterRes)
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

			classType.registeredClass.properties[fieldName] = desc

			derivesClasses := classType.registeredClass.getDerivedClassesRecursive()
			if derivesClasses != nil {
				for i := range derivesClasses {
					derivedClass := derivesClasses[i]

					// Do not override already registered methods.
					_, ok := derivedClass.properties[fieldName]
					if !ok {
						derivedClass.properties[fieldName] = desc
					}
				}
			}

			return []registeredType{}, err
		})
		return []registeredType{}, err
	})

	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})

var RegisterClassProperty = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	classType := api.DecodeI32(stack[0])
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
		panic(fmt.Errorf("could not read method name: %w", err))
	}

	err = engine.whenDependentTypesAreResolved([]int32{}, []int32{classType}, func(classTypes []registeredType) ([]registeredType, error) {
		classType := classTypes[0].(*registeredPointerType)
		humanName := classType.Name() + "." + fieldName

		desc := &classProperty{
			name: fieldName,
			get: func(ctx context.Context, this any) (any, error) {
				return nil, engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot access %s due to unbound types", humanName), []int32{getterReturnType, setterArgumentType})
			},
			getterType:   &anyType{},
			enumerable:   true,
			configurable: true,
		}

		if setter > 0 {
			desc.setterType = &anyType{}
			desc.set = func(ctx context.Context, this any, v any) error {
				return engine.createUnboundTypeError(ctx, fmt.Sprintf("Cannot access %s due to unbound types", humanName), []int32{getterReturnType, setterArgumentType})
			}
		} else {
			desc.readOnly = true
			desc.set = func(ctx context.Context, this any, v any) error {
				return fmt.Errorf("%s is a read-only property", humanName)
			}
		}

		classType.registeredClass.properties[fieldName] = desc

		requiredTypes := []int32{getterReturnType}
		if setter > 0 {
			requiredTypes = append(requiredTypes, setterArgumentType)
		}

		err = engine.whenDependentTypesAreResolved([]int32{}, requiredTypes, func(types []registeredType) ([]registeredType, error) {
			getterReturnType := types[0]

			getterFunc, err := engine.newInvokeFunc(getterSignature, getter, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{getterReturnType.NativeType()})
			if err != nil {
				return nil, fmt.Errorf("could not create _embind_register_class_property getterFunc: %w", err)
			}

			desc := &classProperty{
				name:       fieldName,
				getterType: getterReturnType,
				get: func(ctx context.Context, this any) (any, error) {
					ptr, err := engine.validateThis(ctx, this, classType, humanName+" getter")
					if err != nil {
						return nil, err
					}

					res, err := getterFunc.Call(ctx, api.EncodeI32(getterContext), api.EncodeU32(ptr))
					if err != nil {
						return nil, err
					}
					return getterReturnType.FromWireType(ctx, engine.mod, res[0])
				},
				enumerable: true,
				readOnly:   true,
			}

			if setter > 0 {
				setterArgumentType := types[1]
				setterFunc, err := engine.newInvokeFunc(setterSignature, setter, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, setterArgumentType.NativeType()}, []api.ValueType{})
				if err != nil {
					return nil, fmt.Errorf("could not create _embind_register_class_property setterFunc: %w", err)
				}

				desc.readOnly = false
				desc.setterType = setterArgumentType
				desc.set = func(ctx context.Context, this any, v any) error {
					ptr, err := engine.validateThis(ctx, this, classType, humanName+" setter")
					if err != nil {
						return err
					}

					destructors := &[]*destructorFunc{}
					setterRes, err := setterArgumentType.ToWireType(ctx, engine.mod, destructors, v)
					if err != nil {
						return err
					}

					_, err = setterFunc.Call(ctx, api.EncodeI32(setterContext), api.EncodeU32(ptr), setterRes)
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

			classType.registeredClass.properties[fieldName] = desc

			derivesClasses := classType.registeredClass.getDerivedClassesRecursive()
			if derivesClasses != nil {
				for i := range derivesClasses {
					derivedClass := derivesClasses[i]

					// Do not override already registered methods.
					_, ok := derivedClass.properties[fieldName]
					if !ok {
						derivedClass.properties[fieldName] = desc
					}
				}
			}

			return []registeredType{}, err
		})

		return []registeredType{}, err
	})
	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})

var RegisterSmartPtr = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	rawType := api.DecodeI32(stack[0])
	rawPointeeType := api.DecodeI32(stack[1])
	namePtr := api.DecodeI32(stack[2])
	sharingPolicy := api.DecodeI32(stack[3])
	getPointeeSignature := api.DecodeI32(stack[4])
	rawGetPointee := api.DecodeI32(stack[5])
	constructorSignature := api.DecodeI32(stack[6])
	rawConstructor := api.DecodeI32(stack[7])
	shareSignature := api.DecodeI32(stack[8])
	rawShare := api.DecodeI32(stack[9])
	destructorSignature := api.DecodeI32(stack[10])
	rawDestructor := api.DecodeI32(stack[11])

	name, err := engine.readCString(uint32(namePtr))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	rawGetPointeeFunc, err := engine.newInvokeFunc(getPointeeSignature, rawGetPointee, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})
	if err != nil {
		panic(fmt.Errorf("could not read rawGetPointee: %w", err))
	}

	rawConstructorFunc, err := engine.newInvokeFunc(constructorSignature, rawConstructor, []api.ValueType{}, []api.ValueType{api.ValueTypeI32})
	if err != nil {
		panic(fmt.Errorf("could not read constructorSignature: %w", err))
	}

	rawShareFunc, err := engine.newInvokeFunc(shareSignature, rawShare, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32})
	specialShare := false
	if err != nil {
		// Some classes have a different share signature for some reason.
		rawShareFunc, err = engine.newInvokeFunc(shareSignature, rawShare, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{})
		if err != nil {
			panic(fmt.Errorf("could not read rawShare: %w", err))
		}
		specialShare = true
	}

	rawDestructorFunc, err := engine.newInvokeFunc(destructorSignature, rawDestructor, []api.ValueType{api.ValueTypeI32}, []api.ValueType{})
	if err != nil {
		panic(fmt.Errorf("could not read rawDestructor: %w", err))
	}

	err = engine.whenDependentTypesAreResolved([]int32{rawType}, []int32{rawPointeeType}, func(types []registeredType) ([]registeredType, error) {
		pointeeType := types[0]

		smartPointerType := &registeredPointerType{
			baseType: baseType{
				argPackAdvance: GenericWireTypeSize,
				name:           name,
			},
			registeredClass: pointeeType.(*registeredPointerType).registeredClass,
			isReference:     false,
			isConst:         false,
			isSmartPointer:  true,
			pointeeType:     pointeeType.(*registeredPointerType),
			sharingPolicy:   sharingPolicy,
			rawGetPointee:   rawGetPointeeFunc,
			rawConstructor:  rawConstructorFunc,
			rawShare:        rawShareFunc,
			rawDestructor:   rawDestructorFunc,
			specialShare:    specialShare,
		}

		return []registeredType{smartPointerType}, nil
	})

	if err != nil {
		panic(fmt.Errorf("could not call whenDependentTypesAreResolved: %w", err))
	}
})

var CreateInheritingConstructor = api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
	engine := MustGetEngineFromContext(ctx, mod).(*engine)
	constructorNamePtr := api.DecodeI32(stack[0])
	wrapperTypePtr := api.DecodeI32(stack[1])
	propertiesId := api.DecodeI32(stack[2])

	constructorName, err := engine.readCString(uint32(constructorNamePtr))
	if err != nil {
		panic(fmt.Errorf("could not read name: %w", err))
	}

	wrapperType, err := engine.requireRegisteredType(ctx, wrapperTypePtr, "wrapper")
	if err != nil {
		panic(fmt.Errorf("could not require registered type: %w", err))
	}

	properties, err := engine.emvalEngine.toValue(propertiesId)
	if err != nil {
		panic(fmt.Errorf("could not get properties val: %w", err))
	}

	if _, ok := properties.(IClassBase); !ok {
		panic(fmt.Errorf("could not register class %s with type %T, it does not embed embind.ClassBase", constructorName, properties))
	}

	reflectClassType := reflect.TypeOf(properties)
	if reflectClassType.Kind() != reflect.Ptr {
		panic(fmt.Errorf("could not register class %s with type %T, given value should be a pointer type", constructorName, properties))
	}

	registeredPointerType := wrapperType.(*registeredPointerType)
	registeredClass := registeredPointerType.registeredClass

	legalFunctionName := engine.makeLegalFunctionName(constructorName)
	err = engine.exposePublicSymbol(legalFunctionName, func(ctx context.Context, this any, arguments ...any) (any, error) {
		innerClass := registeredClass.baseClass
		implement, ok := innerClass.methods["implement"]
		if !ok {
			return nil, fmt.Errorf("base class %s does not have implement method", innerClass.name)
		}

		// Create a new class base to hold this instance.
		classBase := &ClassBase{}

		args := append([]any{classBase}, arguments...)
		inner, err := implement.fn(ctx, nil, args...)

		if err != nil {
			return nil, err
		}

		// @todo: do something with finalizer?
		// detachFinalizer(inner);

		innerTyped := inner.(IClassBase)
		ptrTypeRecord := innerTyped.getRegisteredPtrTypeRecord()

		_, err = innerTyped.CallInstanceMethod(ctx, inner, "notifyOnDestruction")
		if err != nil {
			return nil, err
		}

		ptrTypeRecord.preservePointerOnDelete = true

		classBase.classType = registeredClass
		classBase.ptr = ptrTypeRecord.ptr
		classBase.ptrType = ptrTypeRecord.ptrType
		classBase.registeredPtrTypeRecord = ptrTypeRecord
		classBase.engine = engine

		typeElem := reflectClassType.Elem()
		newElem := reflect.New(typeElem)
		f := newElem.Elem().FieldByName("ClassBase")
		if f.IsValid() && f.CanSet() {
			f.Set(reflect.ValueOf(classBase))
		}

		result := newElem.Interface()

		resultClassBase := result.(IClassBase)

		// @todo: do something with finalizer?
		// attachFinalizer(result);

		err = engine.registerInheritedInstance(ctx, registeredClass, ptrTypeRecord.ptr, resultClassBase)
		if err != nil {
			return nil, err
		}

		return result, nil
	}, nil)
	if err != nil {
		panic(fmt.Errorf("could not expose public symbol: %w", err))
	}

	newFn := func(ctx context.Context, arguments ...any) (any, error) {
		return engine.publicSymbols[legalFunctionName].fn(ctx, nil, arguments...)
	}

	stack[0] = api.EncodeI32(engine.emvalEngine.toHandle(newFn))
})

func (e *engine) CallStaticClassMethod(ctx context.Context, className, name string, arguments ...any) (any, error) {
	_, ok := e.publicSymbols[className]
	if !ok {
		return nil, fmt.Errorf("could not find class %s", className)
	}

	_, ok = e.registeredClasses[className].methods[name]
	if !ok {
		return nil, fmt.Errorf("could not find method %s on class %s", name, className)
	}

	if !e.registeredClasses[className].methods[name].isStatic {
		return nil, fmt.Errorf("method %s on class %s is not static", name, className)
	}

	ctx = e.Attach(ctx)
	res, err := e.registeredClasses[className].methods[name].fn(ctx, nil, arguments...)
	if err != nil {
		return nil, fmt.Errorf("error while calling embind function %s on class %s: %w", name, className, err)
	}

	return res, nil
}

func (e *engine) GetStaticClassProperty(ctx context.Context, className, name string) (any, error) {
	_, ok := e.publicSymbols[className]
	if !ok {
		return nil, fmt.Errorf("could not find class %s", className)
	}

	_, ok = e.registeredClasses[className].properties[name]
	if !ok {
		return nil, fmt.Errorf("could not find property %s on class %s", name, className)
	}

	if !e.registeredClasses[className].properties[name].isStatic {
		return nil, fmt.Errorf("property %s on class %s is not static", name, className)
	}

	ctx = e.Attach(ctx)
	res, err := e.registeredClasses[className].properties[name].get(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error while calling embind property getter %s on class %s: %w", name, className, err)
	}

	return res, nil
}

func (e *engine) SetStaticClassProperty(ctx context.Context, className, name string, value any) error {
	_, ok := e.publicSymbols[className]
	if !ok {
		return fmt.Errorf("could not find class %s", className)
	}

	_, ok = e.registeredClasses[className].properties[name]
	if !ok {
		return fmt.Errorf("could not find property %s on class %s", name, className)
	}

	if !e.registeredClasses[className].properties[name].isStatic {
		return fmt.Errorf("property %s on class %s is not static", name, className)
	}

	if e.registeredClasses[className].properties[name].readOnly {
		return fmt.Errorf("property %s on class %s is read-only", name, className)
	}

	ctx = e.Attach(ctx)
	err := e.registeredClasses[className].properties[name].set(ctx, nil, value)
	if err != nil {
		return fmt.Errorf("error while calling embind property setter %s on class %s: %w", name, className, err)
	}

	return nil
}
