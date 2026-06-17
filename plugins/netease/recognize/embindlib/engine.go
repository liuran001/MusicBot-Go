package embind

import (
	internal "github.com/jerbob92/wazero-emscripten-embind/internal"

	"github.com/tetratelabs/wazero"
)

type Engine interface {
	internal.IEngine
	NewFunctionExporterForModule(guest wazero.CompiledModule) FunctionExporter
}

type DelayFunction internal.DelayFunction

type EngineKey = internal.EngineKey

func CreateEngine(config internal.IEngineConfig) Engine {
	return &wazeroEngine{
		config:  config,
		IEngine: internal.CreateEngine(config),
	}
}
