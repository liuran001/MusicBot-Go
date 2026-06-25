package spotify

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/httpproxy"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
)

// RunLogin performs the one-time interactive OAuth login for native Spotify
// audio and persists the resulting reusable credentials. It is meant to be
// invoked from a CLI subcommand (e.g. `MusicBot-Go -spotify-login -c config.ini`)
// on a machine where the operator can open a browser.
//
// callbackPort is the localhost port the OAuth redirect lands on (0 = random).
// On a headless server, forward that port or run this locally and copy the
// resulting credentials file (its path is printed on success) to the server.
//
// promptURL receives the authorization URL to open; pass nil to print to stdout.
func RunLogin(ctx context.Context, cfg *config.Config, logger *logpkg.Logger, callbackPort int, promptURL func(string)) error {
	if cfg == nil {
		return fmt.Errorf("spotify: config required")
	}

	timeoutSec := cfg.GetPluginInt(platformName, "timeout")
	if timeoutSec <= 0 {
		timeoutSec = 15
	}
	timeout := time.Duration(timeoutSec) * time.Second

	nativeHTTP, err := httpproxy.NewHTTPClient(cfg.ResolveAPIProxyConfig(platformName), timeout)
	if err != nil {
		return err
	}

	statePath := strings.TrimSpace(cfg.GetPluginString(platformName, "credentials_path"))
	if statePath == "" {
		cacheDir := strings.TrimSpace(cfg.GetString("CacheDir"))
		if cacheDir == "" {
			cacheDir = "./cache"
		}
		statePath = filepath.Join(cacheDir, "spotify-credentials.json")
	}

	client := native.NewClient(native.ClientOptions{
		StatePath:  statePath,
		Logger:     logger,
		HTTPClient: nativeHTTP,
	})

	if promptURL == nil {
		promptURL = func(u string) {
			fmt.Println("\n请在浏览器中打开以下链接完成 Spotify 授权：")
			fmt.Println()
			fmt.Println("  " + u)
			fmt.Println()
			fmt.Println("授权后页面会跳转到本地回调，凭据将自动保存。")
		}
	}

	if err := client.Login(ctx, callbackPort, promptURL); err != nil {
		return err
	}

	fmt.Printf("\n登录成功，凭据已保存到: %s\n现在可以正常启动 bot，Spotify 将使用原生音频下载。\n", statePath)
	return nil
}
