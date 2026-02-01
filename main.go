package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/liuran001/MusicBot-Go/bot/app"
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
		RuntimeVer: fmt.Sprintf(runtime.Version()),
		BinVersion: versionName,
		CommitSHA:  commitSHA,
		BuildTime:  buildTime,
		BuildArch:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	application, err := app.New(ctx, *configPath, buildInfo)
	if err != nil {
		panic(err)
	}

	if err := application.Start(ctx); err != nil {
		panic(err)
	}

	<-ctx.Done()
	_ = application.Shutdown(context.Background())
}
