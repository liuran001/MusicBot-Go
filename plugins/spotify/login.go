package spotify

import (
	"context"
	"fmt"
	"os"
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

// manualRedirectURI is the fixed loopback redirect used for the paste-the-code
// OAuth flow. It does NOT need to be reachable — Spotify only checks that the
// authorize and exchange calls use the same value.
const manualRedirectURI = "http://127.0.0.1:8898/login"

// RunVerifyManual drives the AAC+Widevine probe with a paste-the-code OAuth
// flow that does not depend on a reachable localhost callback (works on WSL2 /
// headless). When code is empty it prints an authorization URL and persists the
// PKCE verifier next to the credentials file, then returns. When code is
// non-empty it reads that verifier, completes login, and runs the Widevine
// chain probe. trackID may be empty (a default catalog track is used).
func RunVerifyManual(ctx context.Context, cfg *config.Config, logger *logpkg.Logger, code, trackID string) error {
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
	verifierPath := statePath + ".verifier"

	client := native.NewClient(native.ClientOptions{
		StatePath:  statePath,
		Logger:     logger,
		HTTPClient: nativeHTTP,
	})

	if trackID == "" {
		// "Mr. Brightside" — The Killers; a widely-available catalog track.
		trackID = "003vvx7Niy0yvhvHt4a68B"
	}

	tokenPath := statePath + ".oauthtoken"

	// With a code: exchange to a RAW OAuth token, cache it, then probe the
	// web-stream path directly (no AP connection → bypasses TravelRestriction).
	if code != "" {
		verifierBytes, err := os.ReadFile(verifierPath)
		if err != nil {
			return fmt.Errorf("找不到 verifier（请先不带 -spotify-code 运行一次获取授权链接）: %w", err)
		}
		fmt.Println("用粘贴的 code 换取 OAuth token（不连接 AP，直接测网页流路径）…")
		token, err := client.ManualExchangeToken(ctx, strings.TrimSpace(code), strings.TrimSpace(string(verifierBytes)), manualRedirectURI)
		if err != nil {
			return err
		}
		_ = os.Remove(verifierPath)
		_ = os.WriteFile(tokenPath, []byte(token), 0o600)
		return probeAndReport(ctx, client, token, trackID)
	}

	// No code, but a cached token exists: reuse it so we can re-probe (e.g. while
	// iterating on market/format handling) without re-authorizing.
	if tokenBytes, rerr := os.ReadFile(tokenPath); rerr == nil && len(strings.TrimSpace(string(tokenBytes))) > 0 {
		fmt.Println("复用已缓存的 OAuth token 重新探测（token 失效就删掉该文件重新授权）…")
		return probeAndReport(ctx, client, strings.TrimSpace(string(tokenBytes)), trackID)
	}

	// No code and no cached token: emit the authorization URL + persist verifier.
	authURL, verifier := client.ManualAuthURL(manualRedirectURI)
	if err := os.MkdirAll(filepath.Dir(verifierPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(verifierPath, []byte(verifier), 0o600); err != nil {
		return fmt.Errorf("failed persisting verifier: %w", err)
	}
	fmt.Println("\n请在浏览器中打开以下链接完成 Spotify 授权：")
	fmt.Println("\n  " + authURL)
	fmt.Println("\n授权后浏览器会跳转到 " + manualRedirectURI + "?code=...（页面可能显示“拒绝连接”，正常）。")
	fmt.Println("把地址栏里 code= 后面那一长串复制下来，再用 -spotify-code <粘贴> 重新运行即可。")
	return nil
}

// probeAndReport runs the Widevine chain probe with a raw token and prints the
// step-by-step trace.
func probeAndReport(ctx context.Context, client *native.Client, token, trackID string) error {
	res, err := client.VerifyWidevineRawToken(ctx, token, trackID)
	fmt.Println("\n=== Widevine 链路探测结果 ===")
	if res != nil {
		for _, s := range res.Steps {
			fmt.Println("  •", s)
		}
	}
	if err != nil {
		fmt.Printf("\n探测在某步失败: %v\n", err)
		return err
	}
	fmt.Println()
	return nil
}

// RunVerifyCookie probes the AAC+Widevine chain using a web-player token minted
// from an sp_dc cookie. Unlike the OAuth token (which returns metadata without
// the streaming `file` array), the web token can see file ids, so it can drive
// the full chain: web token -> metadata(file) -> storage-resolve -> seektable
// (PSSH) -> widevine-license.
func RunVerifyCookie(ctx context.Context, cfg *config.Config, logger *logpkg.Logger, spDC, trackID string) error {
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
	if trackID == "" {
		trackID = "003vvx7Niy0yvhvHt4a68B"
	}

	fmt.Println("用 sp_dc cookie 换取 web-player token…")
	wt, err := native.WebTokenFromCookie(ctx, nativeHTTP, spDC)
	if wt != nil {
		for _, s := range wt.Steps {
			fmt.Println("  •", s)
		}
	}
	if err != nil {
		return err
	}

	fmt.Println("\n=== 完整 Widevine AAC 链路 ===")
	wvdPath := strings.TrimSpace(cfg.GetPluginString(platformName, "wvd_path"))
	device, derr := native.LoadWVDeviceFile(wvdPath)
	if derr != nil {
		return fmt.Errorf("加载 Widevine L3 device 失败（请在 [plugins.spotify] 配置 wvd_path）: %w", derr)
	}
	wv, err := native.DownloadWidevineMP4(ctx, nativeHTTP, native.WebAuth{Bearer: wt.AccessToken, ClientToken: wt.ClientToken}, device, trackID, 0)
	if wv != nil {
		for _, s := range wv.Steps {
			fmt.Println("  •", s)
		}
	}
	if err != nil {
		fmt.Printf("\n探测在某步失败: %v\n", err)
		return err
	}

	// Write the decrypted MP4 to disk as proof the whole chain works.
	outPath := filepath.Join(filepath.Dir(statePath), "spotify-verify-"+trackID+".m4a")
	if werr := os.WriteFile(outPath, wv.MP4, 0o644); werr != nil {
		return fmt.Errorf("写出解密文件失败: %w", werr)
	}
	fmt.Printf("\n✅ 成功！解密的 AAC 已写入: %s (%d 字节, %d kbps)\n", outPath, len(wv.MP4), wv.Bitrate/1000)
	return nil
}
