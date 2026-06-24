package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/app"
	_ "github.com/liuran001/MusicBot-Go/plugins/all"
)

// configTemplate is the example config compiled into the binary. When the
// target config file is missing, it is written out so a fresh deployment only
// needs the single binary to bootstrap.
//
//go:embed config_example.ini
var configTemplate []byte

var (
	versionName = ""
	commitSHA   = ""
	buildTime   = ""
)

// ensureConfig writes the embedded template to path when no config file exists
// yet. It returns true when a new file was created so the caller can prompt the
// user to fill in required values before the first real start.
func ensureConfig(path string) (created bool, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}

	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
	}

	if err := os.WriteFile(path, configTemplate, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func main() {
	configPath := flag.String("c", "config.ini", "配置文件")
	flag.Parse()

	if created, err := ensureConfig(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write config file: %v\n", err)
		os.Exit(1)
	} else if created {
		fmt.Fprintf(os.Stderr, "未找到配置文件，已生成默认配置: %s\n请填写 BOT_TOKEN 等必填项后重新启动。\n", *configPath)
		os.Exit(0)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	buildInfo := app.BuildInfo{
		RuntimeVer: runtime.Version(),
		BinVersion: versionName,
		CommitSHA:  commitSHA,
		BuildTime:  buildTime,
		BuildArch:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	application, err := app.New(ctx, *configPath, buildInfo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create application: %v\n", err)
		os.Exit(1)
	}

	startErr := make(chan error, 1)
	go func() {
		startErr <- application.Start(ctx)
	}()

	select {
	case err := <-startErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start application: %v\n", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := application.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "Shutdown error: %v\n", err)
		os.Exit(1)
	}
}
