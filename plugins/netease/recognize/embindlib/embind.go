package embind

import (
	internal "github.com/jerbob92/wazero-emscripten-embind/internal"
)

type Enum interface {
	internal.IEnum
}

type ClassBase interface {
	internal.IClassBase
}

type EmvalConstructor interface {
	internal.IEmvalConstructor
}

type EmvalFunctionMapper interface {
	internal.IEmvalFunctionMapper
}
