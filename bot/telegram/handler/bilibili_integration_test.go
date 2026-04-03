package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/plugins/bilibili"
)

const (
	bilibiliIntegrationConfigPath = "/home/baka/repos/MusicBot-Go/config.ini"
	bilibiliIntegrationTrackID    = "BV17292YiEFp"
)

func TestBilibiliIntegrationDownloadAndNormalize(t *testing.T) {
	if os.Getenv("RUN_BILIBILI_INTEGRATION") != "1" {
		t.Skip("set RUN_BILIBILI_INTEGRATION=1 to run bilibili integration test")
	}

	cfg, err := config.Load(bilibiliIntegrationConfigPath)
	if err != nil {
		t.Fatalf("load config.ini: %v", err)
	}

	cookie := trimIntegrationSecret(cfg.GetPluginString("bilibili", "cookie"))
	refreshToken := trimIntegrationSecret(cfg.GetPluginString("bilibili", "refresh_token"))
	if cookie == "" {
		t.Skip("config.ini [plugins.bilibili] cookie is empty")
	}

	client := bilibili.New(nil, cookie, refreshToken, false, 0, nil)
	plat := bilibili.NewPlatform(client, 1)
	downloadService := download.NewDownloadService(download.DownloadServiceOptions{
		Timeout:         2 * time.Minute,
		CheckMD5:        false,
		MaxRetries:      2,
		EnableMultipart: false,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	info, chosenQuality, err := bilibiliIntegrationDownloadInfo(ctx, plat)
	if err != nil {
		t.Fatalf("get bilibili download info: %v", err)
	}
	if info == nil {
		t.Fatal("download info is nil")
	}

	tmpDir := t.TempDir()
	rawExt := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(info.Format)), ".")
	if rawExt == "" {
		rawExt = "bin"
	}
	rawPath := filepath.Join(tmpDir, "bilibili_integration."+rawExt)

	if _, err := downloadService.Download(ctx, info, rawPath, nil); err != nil {
		t.Fatalf("download bilibili media: %v", err)
	}

	normalizedPath, normalizedExt := normalizeExtractedAudioPath(rawPath, rawExt)
	if normalizedPath == "" {
		t.Fatal("normalized path is empty")
	}
	if _, err := os.Stat(normalizedPath); err != nil {
		t.Fatalf("stat normalized file: %v", err)
	}

	codec, err := detectExtractedAudioCodec(normalizedPath)
	if err != nil {
		t.Fatalf("probe normalized codec: %v", err)
	}
	codec = strings.ToLower(strings.TrimSpace(codec))
	normalizedExt = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(normalizedExt), "."))
	if normalizedExt == "" {
		normalizedExt = strings.TrimPrefix(strings.ToLower(filepath.Ext(normalizedPath)), ".")
	}

	switch codec {
	case "flac":
		if normalizedExt != "flac" {
			t.Fatalf("normalized ext mismatch for codec flac: got %q", normalizedExt)
		}
	case "aac", "alac":
		if normalizedExt != "m4a" {
			t.Fatalf("normalized ext mismatch for codec %s: got %q", codec, normalizedExt)
		}
	}

	t.Logf("bilibili integration result: quality=%s final_path=%s ext=%s codec=%s", chosenQuality.String(), normalizedPath, normalizedExt, codec)
}

func bilibiliIntegrationDownloadInfo(ctx context.Context, plat *bilibili.BilibiliPlatform) (*platform.DownloadInfo, platform.Quality, error) {
	qualities := []platform.Quality{
		platform.QualityHiRes,
		platform.QualityLossless,
		platform.QualityHigh,
	}

	var lastErr error
	for _, quality := range qualities {
		info, err := plat.GetDownloadInfo(ctx, bilibiliIntegrationTrackID, quality)
		if err == nil && info != nil {
			return info, quality, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	if lastErr != nil {
		return nil, platform.QualityStandard, lastErr
	}
	return nil, platform.QualityStandard, fmt.Errorf("no bilibili download info returned")
}

func trimIntegrationSecret(value string) string {
	return strings.Trim(strings.TrimSpace(value), "`\"'")
}
