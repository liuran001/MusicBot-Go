package embind

import (
	"context"
	"fmt"

	internal "github.com/jerbob92/wazero-emscripten-embind/internal"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// AFPFunctionExporter builds the host module "a" for the minified netease
// afp.wasm module. The module imports embind/emscripten glue under module name
// "a" with single-letter names and PRE-isAsync arities. This exporter maps each
// minified import to the right internal embind register/emval function (padding
// arity where the wasm predates the isAsync parameter), and provides the
// emscripten/WASI runtime functions the module also imports under "a".
type AFPFunctionExporter struct {
	config internal.IEngineConfig
}

func (we *wazeroEngine) NewAFPFunctionExporter() *AFPFunctionExporter {
	return &AFPFunctionExporter{config: we.config}
}

// NewAFPFunctionExporter builds an exporter for the minified afp.wasm host
// module "a". Works with any Engine created by CreateEngine.
func NewAFPFunctionExporter(e Engine) *AFPFunctionExporter {
	return e.(*wazeroEngine).NewAFPFunctionExporter()
}

const i32 = api.ValueTypeI32

func n32(c int) []api.ValueType {
	r := make([]api.ValueType, c)
	for i := range r {
		r[i] = i32
	}
	return r
}

// pad wraps an internal GoModuleFunc that expects `needed` stack slots but is
// imported by the wasm with only `real` slots (older Emscripten without the
// trailing isAsync arg). Extra slots default to 0 (isAsync=false). Results are
// copied back into the original stack head.
func pad(fn api.GoModuleFunc, real, needed int) api.GoModuleFunc {
	return func(ctx context.Context, mod api.Module, stack []uint64) {
		if needed > real {
			ns := make([]uint64, needed)
			copy(ns, stack)
			fn(ctx, mod, ns)
			copy(stack, ns[:real])
			return
		}
		fn(ctx, mod, stack)
	}
}

func (e *AFPFunctionExporter) add(b wazero.HostModuleBuilder, name string, params, results int, fn api.GoModuleFunc) {
	b.NewFunctionBuilder().
		WithGoModuleFunction(fn, n32(params), n32(results)).
		Export(name)
}

// ExportFunctions builds the "a" host module. Call r.NewHostModuleBuilder("a").
func (e *AFPFunctionExporter) ExportFunctions(b wazero.HostModuleBuilder) error {
	// ---- embind registration glue (mapped from sandbox.bundle.cjs Oe object) ----
	e.add(b, "a", 3, 0, internal.RegisterMemoryView)               // typed array memory view
	e.add(b, "b", 5, 0, internal.RegisterInteger)                  // register_integer
	e.add(b, "c", 8, 0, pad(internal.RegisterClassFunction, 8, 9)) // register_class_function (no isAsync)
	e.add(b, "e", 3, 0, internal.RegisterStdWString)               // register_std_wstring
	e.add(b, "j", 3, 0, internal.RegisterFloat)                    // register_float
	e.add(b, "k", 2, 0, internal.RegisterStdString)                // register_std_string
	e.add(b, "l", 6, 0, pad(internal.RegisterFunction, 6, 7))      // register_function (no isAsync)
	e.add(b, "m", 1, 0, internal.EmvalDecref)                      // emval decref (he)
	e.add(b, "n", 1, 0, internal.EmvalIncref)                      // emval incref
	e.add(b, "o", 2, 1, internal.EmvalTakeValue)                   // _emval_take_value
	e.add(b, "w", 6, 0, internal.RegisterClassConstructor)         // register_class_constructor
	e.add(b, "x", 2, 0, internal.RegisterEmval(true))              // register_emval (with name)
	e.add(b, "y", 5, 0, internal.RegisterBool(true))               // register_bool (with size)
	e.add(b, "z", 2, 0, internal.RegisterVoid)                     // register_void
	e.add(b, "A", 13, 0, internal.RegisterClass)                   // register_class

	// ---- emscripten / WASI runtime functions (also under module "a") ----
	e.add(b, "d", 4, 0, abortStub("__assert_fail"))
	e.add(b, "f", 3, 0, abortStub("__cxa_throw"))
	e.add(b, "g", 1, 1, cxaAllocateException)
	e.add(b, "h", 0, 0, abortStub("abort"))
	e.add(b, "i", 4, 1, fdWrite)
	e.add(b, "p", 7, 0, noop) // empty in glue
	e.add(b, "q", 1, 0, noop) // no-op in glue
	e.add(b, "r", 3, 1, memcpyBig)
	e.add(b, "s", 1, 1, resizeHeap)
	e.add(b, "t", 5, 1, retZero) // strftime
	e.add(b, "u", 2, 1, retZero) // environ_get
	e.add(b, "v", 2, 1, retZero) // environ_sizes_get
	return nil
}

func noop(ctx context.Context, mod api.Module, stack []uint64) {}

func retZero(ctx context.Context, mod api.Module, stack []uint64) {
	stack[0] = 0
}

func abortStub(name string) api.GoModuleFunc {
	return func(ctx context.Context, mod api.Module, stack []uint64) {
		panic(fmt.Errorf("afp.wasm runtime abort via %s (args=%v)", name, stack))
	}
}

// cxaAllocateException: malloc(size+16)+16
func cxaAllocateException(ctx context.Context, mod api.Module, stack []uint64) {
	size := api.DecodeI32(stack[0])
	res, err := mod.ExportedFunction("E").Call(ctx, api.EncodeI32(size+16))
	if err != nil {
		panic(err)
	}
	stack[0] = api.EncodeI32(api.DecodeI32(res[0]) + 16)
}

// memcpyBig: HEAPU8.copyWithin(dest, src, src+num); return dest
func memcpyBig(ctx context.Context, mod api.Module, stack []uint64) {
	dest := api.DecodeU32(stack[0])
	src := api.DecodeU32(stack[1])
	num := api.DecodeU32(stack[2])
	data, ok := mod.Memory().Read(src, num)
	if !ok {
		panic(fmt.Errorf("memcpy: bad read src=%d num=%d", src, num))
	}
	if !mod.Memory().Write(dest, data) {
		panic(fmt.Errorf("memcpy: bad write dest=%d num=%d", dest, num))
	}
	stack[0] = api.EncodeU32(dest)
}

// resizeHeap: grow linear memory to at least requested bytes. Return 1 on success.
func resizeHeap(ctx context.Context, mod api.Module, stack []uint64) {
	requested := api.DecodeU32(stack[0])
	mem := mod.Memory()
	const pageSize = 65536
	cur := mem.Size()
	if requested <= cur {
		stack[0] = 1
		return
	}
	needPages := (requested - cur + pageSize - 1) / pageSize
	if _, ok := mem.Grow(needPages); !ok {
		stack[0] = 0
		return
	}
	stack[0] = 1
}

// fdWrite: emulate writev to a captured buffer, write total byte count to *pnum.
func fdWrite(ctx context.Context, mod api.Module, stack []uint64) {
	iov := api.DecodeU32(stack[1])
	iovcnt := api.DecodeI32(stack[2])
	pnum := api.DecodeU32(stack[3])
	var total uint32
	for i := int32(0); i < iovcnt; i++ {
		ptr, _ := mod.Memory().ReadUint32Le(iov)
		length, _ := mod.Memory().ReadUint32Le(iov + 4)
		iov += 8
		_ = ptr
		total += length
	}
	mod.Memory().WriteUint32Le(pnum, total)
	stack[0] = 0
}
