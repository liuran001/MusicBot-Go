package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/app"
	_ "github.com/liuran001/MusicBot-Go/plugins/all"
)

var (
	versionName = ""
	commitSHA   = ""
	buildTime   = ""
)

func main() {
	configPath := flag.String("c", "config.ini", "配置文件")
	flag.Parse()

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
