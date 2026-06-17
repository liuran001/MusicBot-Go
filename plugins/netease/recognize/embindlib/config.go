package embind

import internal "github.com/jerbob92/wazero-emscripten-embind/internal"

func NewConfig() internal.IEngineConfig {
	return &internal.EngineConfig{}
}
