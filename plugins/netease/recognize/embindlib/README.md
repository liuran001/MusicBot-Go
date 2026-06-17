# wazero-emscripten-embind

[![Go Reference](https://pkg.go.dev/badge/github.com/jerbob92/wazero-emscripten-embind.svg)](https://pkg.go.dev/github.com/jerbob92/wazero-emscripten-embind)
[![Build Status][build-status]][build-url]
[![codecov](https://codecov.io/gh/jerbob92/wazero-emscripten-embind/graph/badge.svg?token=4SC2SOJNZK)](https://codecov.io/gh/jerbob92/wazero-emscripten-embind)

[build-status]:https://github.com/jerbob92/wazero-emscripten-embind/workflows/Go/badge.svg
[build-url]:https://github.com/jerbob92/wazero-emscripten-embind/actions

:rocket: *Emscripten [Embind](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html) support for Go using [Wazero](https://github.com/tetratelabs/wazero)* :rocket:

## Features

* Support for almost all [Embind](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html) features
* Code generator for all Embind bindings:
    * [Functions](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html#a-quick-example)
    * [Classes](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html#classes)
    * [Enums](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html#enums)
    * [Constants](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html#constants)
* Typed data and function signatures in generated code where possible
* Ability to call Go code from
  Embind [using Emval](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html#using-val-to-transliterate-javascript-to-c)
* Communicate between guest and host without worrying about data encoding/decoding
* Direct access to memory
  through [memory views](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html#memory-views)
* Tested with custom test C++ and standard tests from Emscripten

But not everything is supported:

* ASYNCIFY: does not make a lot of sense in Go, might come later to allow Async C++ implementations to work
* EM_ASM/EM_JS (and related methods): naturally, you can't run JS in Go/Wazero. Since Go is not an interpreted language,
  I don't see much sense to add support for Go code in it, as that would require a Go interpreter.
* Binding class method names to well-known symbols (which is something JS specific).

## What does Embind do?

[Embind](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html) allows developers to write C++
code and directly interact with that code from Javascript in the browser. It also allows to call Javascript methods
directly from C++.

This is done by registering enums, functions, classes, arrays, objects, vectors and maps from C++. When the compiled
WebAssembly initializes, it will register all those types in the host using host function calls.
Due to these registrations, the host knows how to encode/decode values to communicate with the guest.

## Wazero implementation

This implementation is trying to be a 1-on-1 implementation of the JS version in Emscripten so that the same codebase
can be used for both Web and WASI WebAssembly builds.

The difference between this implementation and the Emscripten implementation is that this implementation tries to be as
strict as possible during runtime regarding the types that are encoded/decoded, while in the Emscripten implementation
a lot is trusted to the browser WebAssembly VM to cast between types, something we can't do in Go.

## Compiling with Emscripten to WebAssembly with Embind

Be sure to read the [documentation](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/embind.html) to
get to know how Embind works. The most basic version to compile something with Embind is:

```shell
emcc -sERROR_ON_UNDEFINED_SYMBOLS=0 -sEXPORTED_FUNCTIONS="_free,_malloc" -g embind_example.cpp -o embind.wasm -lembind --no-entry
```

It is very important to include `-lembind` to the command and export the functions `_free` and `__malloc`, if these
functions are not available, this package won't work. The Embind Engine will notify you of missing exports.

## Attaching the Embind Engine to the context

The Embind Engine allows itself to be attached to a context value so that it can be used in the Wazero runtime.
This is necessary to make the guest side register itself with the Engine to notify it of all the available Embind
parts.

Attaching it to the context is as simple as:
```go
ctx := context.Background()
// ... Setup Wazero ...

// Create a new engine and attach it to the context.
engine := embind.CreateEngine(embind.NewConfig())
ctx = engine.Attach(ctx)

// InstantiateModule the module on the runtime
r.InstantiateModule(ctx, compiledModule, moduleConfig)

// Attach the generated code to the engine (if any)
generated.Attach(engine)
```

Here is an example to set up a basic Wazero example with Embind integration:
<details>
  <summary>main.go</summary>

```go
package main

import (
	"context"
	"log"

	"github.com/jerbob92/wazero-emscripten-embind"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/emscripten"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed wasm/embind.wasm
var wasm []byte

func main() {
	ctx := context.Background()
	runtimeConfig := wazero.NewRuntimeConfig()
	r := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)
	defer r.Close(ctx)

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		log.Fatal(err)
	}

	compiledModule, err := r.CompileModule(ctx, wasm)
	if err != nil {
		log.Fatal(err)
	}

	builder := r.NewHostModuleBuilder("env")

	emscriptenExporter, err := emscripten.NewFunctionExporterForModule(compiledModule)
	if err != nil {
		log.Fatal(err)
	}

	emscriptenExporter.ExportFunctions(builder)

	engine := embind.CreateEngine(embind.NewConfig())

	embindExporter := engine.NewFunctionExporterForModule(compiledModule)
	err = embindExporter.ExportFunctions(builder)
	if err != nil {
		log.Fatal(err)
	}

	_, err = builder.Instantiate(ctx)
	if err != nil {
		log.Fatal(err)
	}

	moduleConfig := wazero.NewModuleConfig().
		WithStartFunctions("_initialize").
		WithName("")

	ctx = engine.Attach(ctx)
	_, err = r.InstantiateModule(ctx, compiledModule, moduleConfig)
	if err != nil {
		log.Fatal(err)
	}

	// If you have a generated package, you have to attach it to the engine to
	// register the generated values/types with the Engine.
	err = generated.Attach(engine)
	if err != nil {
		log.Fatal(err)
	}
}
```

</details>

You can find more examples in the examples directory.

## Code generator

This project includes a code generator that will automatically generate typed code based on a given WASM file that has
Embind instructions in it. You can generate the code like this:

First create a file in a new package, let's call it `generated/generated.go` for now. Add the following to the file:

```go
//go:generate go run github.com/jerbob92/wazero-emscripten-embind/generator -wasm=../wasm/embind.wasm
package generated
```

Where `../wasm/embind.wasm` is the path to the WASM file, relative to the file `generated.go`.

You can now run the command from the `generated` folder to make it generate the typed Go code:

```shell
go generate
```

Or from the project root:

```shell
go generate ./...
```

You are allowed to put other things in this package, as long as they don't conflict with the filenames of Embind:

* `classes.go`
* `constants.go`
* `engine.go`
* `enums.go`
* `functions.go`

In the examples directory you will find some full examples that show what the generated code looks like.

## Using Embind/C++ from Go

The easiest way to call Embind from Go would be to use the generator, but it's also possible to do things directly using
the Engine, the generated code is basically a wrapper around these functions in the Engine.

Here are a few examples:

```go
// Calling an exposed symbol (function) called returnRawData with a string argument.
imageData, err := engine.CallPublicSymbol(ctx, "returnRawData", "image")

// Creating a new instance of the class MyClass using the values 5 and 10 in the constructor.
newClassInstance, err := engine.CallPublicSymbol(ctx, "MyClass", 50, 10)

// Call methods on the class.
err := newClassInstance.IncrementX(ctx)

// Call setters and getters on the class.
x, err := newClassInstance.GetX(ctx)
err := newClassInstance.SetX(ctx, 42)

// Call static methods on the class
resultString, err := engine.CallStaticClassMethod(ctx, "MyClass", "getStringFromInstance", newClassInstance)
```

## Using Go from Embind/C++

This package also allows you to use Go code directly from Embind
using [Emval](https://emscripten.org/docs/api_reference/val.h.html), in the same way that would work in JS, the only
difference is that in JS, the full global namespace is available, while in Go you specifically have to expose things to
Embind to be able to access them.

You can do the following things with this:

* Call methods on structs
* Set/Get properties on structs
* Create new instances of structs
* Share arbitrary data like strings and integers

For example, given the following Go code:

```go
package main

import (
	"log"

	"github.com/jerbob92/wazero-emscripten-embind"
	"github.com/tetratelabs/wazero"
)

type testStruct struct {
	Property1 string `embind_arg:"0" embind_property:"propone"`
	Property2 string `embind_property:"proptwo"`
	Property3 string
}

func (ts *testStruct) Trigger() {
	log.Printf("Triggering %s %s %s on testStruct", ts.Property1, ts.Property2, ts.Property3)
}

func main() {
	// Initialize Wazero runtime and Embind engine ...
	engine.RegisterEmvalSymbol("testStruct", &testStruct{})
}
```

You can then do the following on the C++ side:

```cpp
val testStruct = val::global("testStruct");
val newStruct = testStruct.new_("valueInProperty1");
newStruct.set("proptwo", val("valueInProperty2"));
newStruct.set("Property3", val("valueInProperty3"));
newStruct.call<void>("Trigger");
```

A few things to note:

* You can return structs and then also set/get properties and call methods on that
* If your function is void in C++, you can return nothing or an error in Go
* If your function has a return in C++, you can return something, or something and an error, where the error has to be
  the second return
* If your function returns an error, the whole call where the Emval call originated from will fail
* You can use the `embind_arg` tag to tell the Engine which argument index should end up in which property in
  case `.new_()` is used on the C++ side
* Or you can implement the `embind.EmvalConstructor` interface on the struct to make your own constructor
* You can use the `embind_property` tag to tell the Engine which property should be accessed when a set or get is done 
  in C++
* You can implement the `embind.EmvalFunctionMapper` interface on the struct to map function calls on your struct based
  on the arguments (and/or length) and name

## Support Policy

We offer an API stability promise with semantic versioning. In other words, we promise to not break any exported
function signature without incrementing the major version. New features and behaviors happen with a minor version
increment, e.g. 1.0.11 to 1.1.0. We also fix bugs or change internal details with a patch version, e.g. 1.0.0 to 1.0.1.
Upgrades of the supported Emscripten version will cause a minor version update.

### Go

This project will support the last 3 version of Go, this means that if the last version of Go is 1.21, our `go.mod`
will be set to Go 1.19, and our CI tests will be run on Go 1.19, 1.20 and 1.21. It won't mean that the library won't
work with older versions of Go, but it will tell you what to expect of the supported  Go versions. If we change the
supported Go versions, we will make that a minor version upgrade. This policy allows you to not be forced to the latest
Go version for a pretty long time, but it still allows us to use new language features in a pretty reasonable
time-frame.

### Emscripten

This package has been built against Emscripten version `3.1.44`. Since Emscripten compiles both the WASM and JS, they
don't have to think about compatibility between versions, which makes it difficult for us to maintain compatibility with
multiple Emscripten version if they change anything Embind related.

This package will try to keep compatibility between Emscripten versions where that is possible, that is also why you 
need to provide the compiled module to the engine, using the compiled module we can validate the available exports and
imports, if any of the import signatures will change we can use that to dynamically build host functions based on the
function signature.

If it is not possible to maintain compatibility automatically, this package will add compatibility flags to the
configuration that is passed to the initialization of the engine to keep the package working with different versions of
Emscripten.

## License

The MIT License (MIT)