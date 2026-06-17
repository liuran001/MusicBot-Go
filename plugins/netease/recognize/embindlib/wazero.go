package embind

import (
	"fmt"
	internal "github.com/jerbob92/wazero-emscripten-embind/internal"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

type wazeroEngine struct {
	internal.IEngine
	config internal.IEngineConfig
}

func (we *wazeroEngine) NewFunctionExporterForModule(guest wazero.CompiledModule) FunctionExporter {
	return &functionExporter{
		config: we.config,
		guest:  guest,
	}
}

// FunctionExporter configures the functions in the "env" module used by
// Emscripten embind.
type FunctionExporter interface {
	// ExportFunctions builds functions to export with a wazero.HostModuleBuilder
	// named "env".
	ExportFunctions(wazero.HostModuleBuilder) error
}

type functionExporter struct {
	config internal.IEngineConfig
	guest  wazero.CompiledModule
}

type unexportedFunctionError struct {
	name string
}

func (e unexportedFunctionError) Error() string {
	return fmt.Sprintf("you need to export the \"%s\" function to make embind work, you can do this using the \"EXPORTED_FUNCTIONS\" option in Emscripten during compilation, you will need to prepend exports with an underscore, so you have to add \"_%s\" to the list", e.name, e.name)
}

func (e functionExporter) GetImportedFunction(name string) api.FunctionDefinition {
	importedFunctions := e.guest.ImportedFunctions()
	for i := range importedFunctions {
		module, importName, _ := importedFunctions[i].Import()
		if module == "env" && importName == name {
			return importedFunctions[i]
		}
	}

	return nil
}

// ExportFunctions implements FunctionExporter.ExportFunctions
func (e functionExporter) ExportFunctions(b wazero.HostModuleBuilder) error {
	// First validate whether required functions are available.
	requiredFunctions := []string{"free", "malloc", "__getTypeName"}
	exportedFunctions := e.guest.ExportedFunctions()
	for i := range requiredFunctions {
		requiredFunction := requiredFunctions[i]
		if _, ok := exportedFunctions[requiredFunction]; !ok {
			return unexportedFunctionError{
				name: requiredFunction,
			}
		}
	}

	b.NewFunctionBuilder().
		WithName("_embind_register_function").
		WithParameterNames("name", "argCount", "rawArgTypesAddr", "signature", "rawInvoker", "fn", "isAsync").
		WithGoModuleFunction(internal.RegisterFunction, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_function")

	b.NewFunctionBuilder().
		WithName("_embind_register_void").
		WithParameterNames("rawType", "name").
		WithGoModuleFunction(internal.RegisterVoid, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_void")

	importedEmbindRegisterBool := e.GetImportedFunction("_embind_register_bool")
	if importedEmbindRegisterBool != nil {
		// Since Emscripten 3.1.45, the size of the boolean is put to 1, while
		// before the size was part of the registration.
		boolHasSizeArgument := len(importedEmbindRegisterBool.ParamTypes()) == 5
		if boolHasSizeArgument {
			b.NewFunctionBuilder().
				WithName("_embind_register_bool").
				WithParameterNames("rawType", "name", "size", "trueValue", "falseValue").
				WithGoModuleFunction(internal.RegisterBool(true), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
				Export("_embind_register_bool")
		} else {
			b.NewFunctionBuilder().
				WithName("_embind_register_bool").
				WithParameterNames("rawType", "name", "trueValue", "falseValue").
				WithGoModuleFunction(internal.RegisterBool(false), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
				Export("_embind_register_bool")
		}
	}

	b.NewFunctionBuilder().
		WithName("_embind_register_integer").
		WithParameterNames("rawType", "name", "size", "minRange", "maxRange").
		WithGoModuleFunction(internal.RegisterInteger, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_integer")

	b.NewFunctionBuilder().
		WithName("_embind_register_bigint").
		WithParameterNames("rawType", "name", "size", "minRange", "maxRange").
		WithGoModuleFunction(internal.RegisterBigInt, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI64, api.ValueTypeI64}, []api.ValueType{}).
		Export("_embind_register_bigint")

	b.NewFunctionBuilder().
		WithName("_embind_register_float").
		WithParameterNames("rawType", "name", "size").
		WithGoModuleFunction(internal.RegisterFloat, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_float")

	b.NewFunctionBuilder().
		WithName("_embind_register_std_string").
		WithParameterNames("rawType", "name").
		WithGoModuleFunction(internal.RegisterStdString, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_std_string")

	b.NewFunctionBuilder().
		WithName("_embind_register_std_wstring").
		WithParameterNames("rawType", "charSize", "name").
		WithGoModuleFunction(internal.RegisterStdWString, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_std_wstring")

	b.NewFunctionBuilder().
		WithName("_embind_register_memory_view").
		WithParameterNames("rawType", "dataTypeIndex", "name").
		WithGoModuleFunction(internal.RegisterMemoryView, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_memory_view")

	importedEmbindRegisterEmval := e.GetImportedFunction("_embind_register_emval")
	if importedEmbindRegisterEmval != nil {
		// Since Emscripten 3.1.53, the name parameter has been removed.
		emvalHasNameArgument := len(importedEmbindRegisterEmval.ParamTypes()) == 2
		if emvalHasNameArgument {
			b.NewFunctionBuilder().
				WithName("_embind_register_emval").
				WithParameterNames("rawType", "name").
				WithGoModuleFunction(internal.RegisterEmval(true), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
				Export("_embind_register_emval")
		} else {
			b.NewFunctionBuilder().
				WithName("_embind_register_emval").
				WithParameterNames("rawType").
				WithGoModuleFunction(internal.RegisterEmval(false), []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
				Export("_embind_register_emval")
		}
	}

	b.NewFunctionBuilder().
		WithName("_embind_register_constant").
		WithParameterNames("rawType", "type", "value").
		WithGoModuleFunction(internal.RegisterConstant, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeF64}, []api.ValueType{}).
		Export("_embind_register_constant")

	b.NewFunctionBuilder().
		WithName("_embind_register_enum").
		WithParameterNames("rawType", "name", "size", "isSigned").
		WithGoModuleFunction(internal.RegisterEnum, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_enum")

	b.NewFunctionBuilder().
		WithName("_embind_register_enum_value").
		WithParameterNames("rawEnumType", "name", "enumValue").
		WithGoModuleFunction(internal.RegisterEnumValue, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_enum_value")

	b.NewFunctionBuilder().
		WithName("_emval_take_value").
		WithParameterNames("type", "arg").
		WithResultNames("emval_handle").
		WithGoModuleFunction(internal.EmvalTakeValue, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_take_value")

	b.NewFunctionBuilder().
		WithName("_emval_incref").
		WithParameterNames("handle").
		WithGoModuleFunction(internal.EmvalIncref, []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
		Export("_emval_incref")

	b.NewFunctionBuilder().
		WithName("_emval_decref").
		WithParameterNames("handle").
		WithGoModuleFunction(internal.EmvalDecref, []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
		Export("_emval_decref")

	b.NewFunctionBuilder().
		WithName("_emval_register_symbol").
		WithParameterNames("address").
		WithGoModuleFunction(internal.EmvalRegisterSymbol, []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
		Export("_emval_register_symbol")

	b.NewFunctionBuilder().
		WithName("_emval_get_global").
		WithParameterNames("name").
		WithResultNames("handle").
		WithGoModuleFunction(internal.EmvalGetGlobal, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_get_global")

	b.NewFunctionBuilder().
		WithName("_emval_as").
		WithParameterNames("handle", "returnType", "destructorsRef").
		WithResultNames("val").
		WithGoModuleFunction(internal.EmvalAs, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeF64}).
		Export("_emval_as")

	b.NewFunctionBuilder().
		WithName("_emval_new").
		WithParameterNames("handle", "argCount", "argTypes", "args").
		WithResultNames("val").
		WithGoModuleFunction(internal.EmvalNew, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_new")

	b.NewFunctionBuilder().
		WithName("_emval_set_property").
		WithParameterNames("handle", "key", "value").
		WithGoModuleFunction(internal.EmvalSetProperty, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_emval_set_property")

	b.NewFunctionBuilder().
		WithName("_emval_get_property").
		WithParameterNames("handle", "key").
		WithResultNames("value").
		WithGoModuleFunction(internal.EmvalGetProperty, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_get_property")

	b.NewFunctionBuilder().
		WithName("_emval_new_cstring").
		WithParameterNames("v").
		WithResultNames("handle").
		WithGoModuleFunction(internal.EmvalNewCString, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_new_cstring")

	b.NewFunctionBuilder().
		WithName("_emval_run_destructors").
		WithParameterNames("handle").
		WithGoModuleFunction(internal.EmvalRunDestructors, []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
		Export("_emval_run_destructors")

	importedEmvalGetMethodCaller := e.GetImportedFunction("_emval_get_method_caller")
	if importedEmvalGetMethodCaller != nil {
		// Since Emscripten 3.1.48, _emval_get_method_caller has 3 params.
		// The new param is called "kind".
		emvalGetMethodCallerHasKindParam := len(importedEmvalGetMethodCaller.ParamTypes()) == 3
		if emvalGetMethodCallerHasKindParam {
			b.NewFunctionBuilder().
				WithName("_emval_get_method_caller").
				WithParameterNames("argCount", "argTypes", "kind").
				WithResultNames("id").
				WithGoModuleFunction(internal.EmvalGetMethodCaller(true), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
				Export("_emval_get_method_caller")
		} else {
			b.NewFunctionBuilder().
				WithName("_emval_get_method_caller").
				WithParameterNames("argCount", "argTypes").
				WithResultNames("id").
				WithGoModuleFunction(internal.EmvalGetMethodCaller(false), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
				Export("_emval_get_method_caller")
		}
	}

	importedEmvalCall := e.GetImportedFunction("_emval_call")
	if importedEmvalCall != nil {
		// Since Emscripten 3.1.48, _emval_call has a F64 return.
		importedEmvalCallHasF64Return := importedEmvalCall.ResultTypes()[0] == api.ValueTypeF64
		if importedEmvalCallHasF64Return {
			b.NewFunctionBuilder().
				WithName("_emval_call").
				WithParameterNames("handle", "argCount", "argTypes", "argv").
				WithResultNames("handle").
				WithGoModuleFunction(internal.EmvalCall(true), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeF64}).
				Export("_emval_call")

		} else {
			b.NewFunctionBuilder().
				WithName("_emval_call").
				WithParameterNames("handle", "argCount", "argTypes", "argv").
				WithResultNames("handle").
				WithGoModuleFunction(internal.EmvalCall(false), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
				Export("_emval_call")

		}
	}

	b.NewFunctionBuilder().
		WithName("_emval_call_method").
		WithParameterNames("caller", "id", "methodName", "destructorsRef", "args").
		WithResultNames("value").
		WithGoModuleFunction(internal.EmvalCallMethod, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeF64}).
		Export("_emval_call_method")

	b.NewFunctionBuilder().
		WithName("_emval_call_void_method").
		WithParameterNames("caller", "id", "methodName", "args").
		WithGoModuleFunction(internal.EmvalCallVoidMethod, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_emval_call_void_method")

	b.NewFunctionBuilder().
		WithName("_embind_register_class").
		WithParameterNames("rawType", "rawPointerType", "rawConstPointerType", "baseClassRawType", "getActualTypeSignature", "getActualType", "upcastSignature", "upcast", "downcastSignature", "downcast", "name", "destructorSignature", "rawDestructor").
		WithGoModuleFunction(internal.RegisterClass, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_class")

	b.NewFunctionBuilder().
		WithName("_embind_register_class_constructor").
		WithParameterNames("rawClassType", "argCount", "rawArgTypesAddr", "invokerSignature", "invoker", "rawConstructor").
		WithGoModuleFunction(internal.RegisterClassConstructor, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_class_constructor")

	b.NewFunctionBuilder().
		WithName("_embind_register_class_function").
		WithParameterNames(
			"rawClassType",
			"methodName",
			"argCount",
			"rawArgTypesAddr", // [ReturnType, ThisType, Args...]
			"invokerSignature",
			"rawInvoker",
			"context",
			"isPureVirtual",
			"isAsync",
		).
		WithGoModuleFunction(internal.RegisterClassFunction, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_class_function")

	b.NewFunctionBuilder().
		WithName("_embind_register_class_class_function").
		WithParameterNames(
			"rawClassType",
			"methodName",
			"argCount",
			"rawArgTypesAddr", // [ReturnType, ThisType, Args...]
			"invokerSignature",
			"rawInvoker",
			"fn",
			"isAsync",
		).
		WithGoModuleFunction(internal.RegisterClassClassFunction, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_class_class_function")

	b.NewFunctionBuilder().
		WithName("_embind_register_class_property").
		WithParameterNames(
			"classType",
			"fieldName",
			"getterReturnType",
			"getterSignature",
			"getter",
			"getterContext",
			"setterArgumentType",
			"setterSignature",
			"setter",
			"setterContext",
		).
		WithGoModuleFunction(internal.RegisterClassProperty, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_class_property")

	b.NewFunctionBuilder().
		WithName("_embind_register_value_array").
		WithParameterNames(
			"rawType",
			"name",
			"constructorSignature",
			"rawConstructor",
			"destructorSignature",
			"rawDestructor",
		).
		WithGoModuleFunction(internal.RegisterValueArray, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_value_array")

	b.NewFunctionBuilder().
		WithName("_embind_register_value_array_element").
		WithParameterNames(
			"rawTupleType",
			"getterReturnType",
			"getterSignature",
			"getter",
			"getterContext",
			"setterArgumentType",
			"setterSignature",
			"setter",
			"setterContext",
		).
		WithGoModuleFunction(internal.RegisterValueArrayElement, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_value_array_element")

	b.NewFunctionBuilder().
		WithName("_embind_finalize_value_array").
		WithParameterNames("rawTupleType").
		WithGoModuleFunction(internal.FinalizeValueArray, []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_finalize_value_array")

	b.NewFunctionBuilder().
		WithName("_embind_register_value_object").
		WithParameterNames(
			"rawType",
			"name",
			"constructorSignature",
			"rawConstructor",
			"destructorSignature",
			"rawDestructor",
		).
		WithGoModuleFunction(internal.RegisterValueObject, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_value_object")

	b.NewFunctionBuilder().
		WithName("_embind_register_value_object_field").
		WithParameterNames(
			"structType",
			"fieldName",
			"getterReturnType",
			"getterSignature",
			"getter",
			"getterContext",
			"setterArgumentType",
			"setterSignature",
			"setter",
			"setterContext",
		).
		WithGoModuleFunction(internal.RegisterValueObjectField, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_value_object_field")

	b.NewFunctionBuilder().
		WithName("_embind_finalize_value_object").
		WithParameterNames("structType").
		WithGoModuleFunction(internal.FinalizeValueObject, []api.ValueType{api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_finalize_value_object")

	b.NewFunctionBuilder().
		WithName("_embind_register_smart_ptr").
		WithParameterNames(
			"rawType",
			"rawPointeeType",
			"name",
			"sharingPolicy",
			"getPointeeSignature",
			"rawGetPointee",
			"constructorSignature",
			"rawConstructor",
			"shareSignature",
			"rawShare",
			"destructorSignature",
			"rawDestructor",
		).
		WithGoModuleFunction(internal.RegisterSmartPtr, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_smart_ptr")

	b.NewFunctionBuilder().
		WithName("_embind_create_inheriting_constructor").
		WithParameterNames("constructorName", "wrapperType", "properties").
		WithResultNames("handle").
		WithGoModuleFunction(internal.CreateInheritingConstructor, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_embind_create_inheriting_constructor")

	b.NewFunctionBuilder().
		WithName("_embind_register_class_class_property").
		WithParameterNames(
			"rawClassType",
			"fieldName",
			"rawFieldType",
			"rawFieldPtr",
			"getterSignature",
			"getter",
			"setterSignature",
			"setter",
		).
		WithGoModuleFunction(internal.RegisterClassClassProperty, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("_embind_register_class_class_property")

	b.NewFunctionBuilder().
		WithName("_emval_as_int64").
		WithParameterNames("handle", "returnType").
		WithResultNames("value").
		WithGoModuleFunction(internal.EmvalAsInt64, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI64}).
		Export("_emval_as_int64")

	b.NewFunctionBuilder().
		WithName("_emval_as_uint64").
		WithParameterNames("handle", "returnType").
		WithResultNames("value").
		WithGoModuleFunction(internal.EmvalAsUint64, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI64}).
		Export("_emval_as_uint64")

	b.NewFunctionBuilder().
		WithName("_emval_await").
		WithParameterNames("handle").
		WithResultNames("value").
		WithGoModuleFunction(internal.EmvalAwait, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_await")

	b.NewFunctionBuilder().
		WithName("_emval_delete").
		WithParameterNames("object", "property").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalDelete, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_delete")

	b.NewFunctionBuilder().
		WithName("_emval_equals").
		WithParameterNames("handle", "handle2").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalEquals, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_equals")

	b.NewFunctionBuilder().
		WithName("_emval_get_module_property").
		WithParameterNames("name").
		WithResultNames("handle").
		WithGoModuleFunction(internal.EmvalGetModuleProperty, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_get_module_property")

	b.NewFunctionBuilder().
		WithName("_emval_greater_than").
		WithParameterNames("handle", "handle2").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalGreaterThan, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}).
		Export("_emval_greater_than")

	b.NewFunctionBuilder().
		WithName("_emval_in").
		WithParameterNames("handle", "handle2").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalIn, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_in")

	b.NewFunctionBuilder().
		WithName("_emval_instanceof").
		WithParameterNames("handle", "handle2").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalInstanceof, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_instanceof")

	b.NewFunctionBuilder().
		WithName("_emval_is_number").
		WithParameterNames("handle").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalIsNumber, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_is_number")

	b.NewFunctionBuilder().
		WithName("_emval_is_string").
		WithParameterNames("handle").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalIsString, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_is_string")

	b.NewFunctionBuilder().
		WithName("_emval_less_than").
		WithParameterNames("handle", "handle2").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalLessThan, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_less_than")

	b.NewFunctionBuilder().
		WithName("_emval_new_array").
		WithResultNames("array").
		WithGoModuleFunction(internal.EmvalNewArray, []api.ValueType{}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_new_array")

	b.NewFunctionBuilder().
		WithName("_emval_new_array_from_memory_view").
		WithParameterNames("view").
		WithResultNames("array").
		WithGoModuleFunction(internal.EmvalNewArrayFromMemoryView, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_new_array_from_memory_view")

	b.NewFunctionBuilder().
		WithName("_emval_new_object").
		WithResultNames("object").
		WithGoModuleFunction(internal.EmvalNewObject, []api.ValueType{}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_new_object")

	b.NewFunctionBuilder().
		WithName("_emval_new_u16string").
		WithParameterNames("in").
		WithResultNames("out").
		WithGoModuleFunction(internal.EmvalNewU16string, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_new_u16string")

	b.NewFunctionBuilder().
		WithName("_emval_new_u8string").
		WithParameterNames("in").
		WithResultNames("out").
		WithGoModuleFunction(internal.EmvalNewU8string, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_new_u8string")

	b.NewFunctionBuilder().
		WithName("_emval_not").
		WithParameterNames("handle").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalNot, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_not")

	b.NewFunctionBuilder().
		WithName("_emval_strictly_equals").
		WithParameterNames("handle1", "handle2").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalStrictlyEquals, []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_strictly_equals")

	b.NewFunctionBuilder().
		WithName("_emval_throw").
		WithParameterNames("handle").
		WithResultNames("object").
		WithGoModuleFunction(internal.EmvalThrow, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_throw")

	b.NewFunctionBuilder().
		WithName("_emval_typeof").
		WithParameterNames("handle").
		WithResultNames("type").
		WithGoModuleFunction(internal.EmvalTypeof, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_typeof")

	b.NewFunctionBuilder().
		WithName("_emval_iter_begin").
		WithParameterNames("iterable").
		WithResultNames("iterator").
		WithGoModuleFunction(internal.EmvalIterBegin, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_iter_begin")

	b.NewFunctionBuilder().
		WithName("_emval_iter_next").
		WithParameterNames("iterator").
		WithResultNames("result").
		WithGoModuleFunction(internal.EmvalIterNext, []api.ValueType{api.ValueTypeI32}, []api.ValueType{api.ValueTypeI32}).
		Export("_emval_iter_next")

	return nil
}
