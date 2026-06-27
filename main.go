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
	"github.com/liuran001/MusicBot-Go/bot/config"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	_ "github.com/liuran001/MusicBot-Go/plugins/all"
	"github.com/liuran001/MusicBot-Go/plugins/spotify"
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
	spotifyLogin := flag.Bool("spotify-login", false, "运行一次性 Spotify 授权登录（获取原生音频下载所需的长期凭据），完成后退出")
	spotifyLoginPort := flag.Int("spotify-login-port", 0, "Spotify 授权回调监听端口（0 为随机，远程服务器可固定后做端口转发）")
	spotifyVerify := flag.Bool("spotify-verify", false, "探测 AAC+Widevine 链路是否可用（诊断用）。先不带 code 运行获取授权链接，授权后用 -spotify-code 粘贴回来")
	spotifyCode := flag.String("spotify-code", "", "授权后从回调地址里复制的 code（配合 -spotify-verify 使用）")
	spotifyCookie := flag.String("spotify-cookie", "", "用 sp_dc cookie 探测 Widevine 链路（诊断用，从浏览器登录 open.spotify.com 后复制 sp_dc）")
	spotifyWvdDir := flag.String("spotify-wvd-dir", "", "批量测试一个目录里的所有 .wvd 设备，找出未被 Spotify 吊销的（配合 -spotify-cookie 用）")
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

	if *spotifyWvdDir != "" {
		if err := runSpotifyBatchWvd(ctx, *configPath, *spotifyCookie, *spotifyWvdDir); err != nil {
			fmt.Fprintf(os.Stderr, "Spotify 批量 wvd 测试失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *spotifyCookie != "" {
		if err := runSpotifyVerifyCookie(ctx, *configPath, *spotifyCookie); err != nil {
			fmt.Fprintf(os.Stderr, "Spotify Widevine (cookie) 探测失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *spotifyVerify {
		if err := runSpotifyVerify(ctx, *configPath, *spotifyCode); err != nil {
			fmt.Fprintf(os.Stderr, "Spotify Widevine 探测失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *spotifyLogin {
		if err := runSpotifyLogin(ctx, *configPath, *spotifyLoginPort); err != nil {
			fmt.Fprintf(os.Stderr, "Spotify 登录失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

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

// runSpotifyLogin performs the one-time interactive Spotify OAuth login and
// persists the long-lived credentials needed for native (real) Spotify audio
// downloads. It loads config + logger directly (without starting the full bot)
// so the operator can run it once on a machine with a browser.
func runSpotifyLogin(ctx context.Context, configPath string, callbackPort int) error {
	conf, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed loading config: %w", err)
	}
	logger, err := logpkg.New(conf.GetString("LogLevel"), conf.GetString("LogFormat"), false)
	if err != nil {
		logger, _ = logpkg.New("info", "text", false)
	}
	return spotify.RunLogin(ctx, conf, logger, callbackPort, nil)
}

// runSpotifyVerify logs in via the paste-the-code OAuth flow (if needed) and
// probes the AAC+Widevine web-stream chain to determine whether this account's
// token can sign Widevine licenses.
func runSpotifyVerify(ctx context.Context, configPath string, code string) error {
	conf, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed loading config: %w", err)
	}
	logger, err := logpkg.New(conf.GetString("LogLevel"), conf.GetString("LogFormat"), false)
	if err != nil {
		logger, _ = logpkg.New("info", "text", false)
	}
	return spotify.RunVerifyManual(ctx, conf, logger, code, "")
}

// runSpotifyVerifyCookie probes the AAC+Widevine chain using an sp_dc cookie
// (the web-player token path, which can see streaming file ids).
func runSpotifyVerifyCookie(ctx context.Context, configPath, spDC string) error {
	conf, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed loading config: %w", err)
	}
	logger, err := logpkg.New(conf.GetString("LogLevel"), conf.GetString("LogFormat"), false)
	if err != nil {
		logger, _ = logpkg.New("info", "text", false)
	}
	return spotify.RunVerifyCookie(ctx, conf, logger, spDC, "")
}

// runSpotifyBatchWvd tries every .wvd in a directory against Spotify's license
// endpoint to find one that isn't revoked.
func runSpotifyBatchWvd(ctx context.Context, configPath, spDC, wvdDir string) error {
	if spDC == "" {
		return fmt.Errorf("需要 -spotify-cookie <sp_dc> 一起用")
	}
	conf, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed loading config: %w", err)
	}
	logger, err := logpkg.New(conf.GetString("LogLevel"), conf.GetString("LogFormat"), false)
	if err != nil {
		logger, _ = logpkg.New("info", "text", false)
	}
	return spotify.RunBatchWvd(ctx, conf, logger, spDC, wvdDir, "")
}
