package dynplugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

const (
	defaultScriptDir = "./plugins/scripts"
)

func loadScriptPlugin(ctx context.Context, name string, cfg *config.Config, logger *logpkg.Logger) (*scriptPlugin, *pluginMeta, error) {
	if name == "" {
		return nil, nil, fmt.Errorf("plugin name required")
	}
	scriptDir := strings.TrimSpace(cfg.GetString("PluginScriptDir"))
	if scriptDir == "" {
		scriptDir = defaultScriptDir
	}
	plugPath := filepath.Join(scriptDir, name)
	files, err := listScriptFiles(plugPath)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("script plugin %s not found", name)
	}

	interpreter, err := newInterpreter(plugPath)
	if err != nil {
		return nil, nil, err
	}

	for _, file := range files {
		if _, err := interpreter.EvalPath(file); err != nil {
			return nil, nil, fmt.Errorf("script %s: %w", file, err)
		}
	}

	plug := newScriptPlugin(name, interpreter, logger)
	if err := plug.Init(ctx, pluginConfig(cfg, name)); err != nil {
		return nil, nil, err
	}
	meta, err := plug.Meta(ctx)
	if err != nil {
		return nil, nil, err
	}
	return plug, meta, nil
}

func listScriptFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files, nil
}

func newInterpreter(startPath string) (*interp.Interpreter, error) {
	modRoot, err := findModuleRoot(startPath)
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(), "GOMOD="+filepath.Join(modRoot, "go.mod"))
	if _, err := os.Stat(filepath.Join(modRoot, "go.work")); err == nil {
		env = append(env, "GOWORK="+filepath.Join(modRoot, "go.work"))
	}
	options := interp.Options{
		GoPath: os.Getenv("GOPATH"),
		Env:    env,
		// SECURITY: Unrestricted + the full stdlib give scripts unrestricted
		// access to the host (os/exec, net, filesystem, etc.). A dynamic
		// script plugin can therefore run arbitrary system operations with
		// this process's privileges. PluginScriptDir MUST be a fully trusted,
		// permission-controlled location — never point it at user-writable or
		// untrusted content. See plugins/scripts/README.md.
		Unrestricted: true,
	}
	interpreter := interp.New(options)
	if err := interpreter.Use(stdlib.Symbols); err != nil {
		return nil, err
	}
	return interpreter, nil
}

func findModuleRoot(startPath string) (string, error) {
	path := startPath
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
			return path, nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return "", fmt.Errorf("go.mod not found for %s", startPath)
}

func pluginConfig(cfg *config.Config, name string) map[string]string {
	result := make(map[string]string)
	if cfg == nil {
		return result
	}
	pluginCfg, ok := cfg.GetPluginConfig(name)
	if !ok {
		return result
	}
	for key, value := range pluginCfg {
		if value == nil {
			continue
		}
		result[key] = fmt.Sprintf("%v", value)
	}
	return result
}
