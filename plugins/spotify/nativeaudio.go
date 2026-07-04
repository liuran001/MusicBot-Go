package spotify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/plugins/spotify/native"
)

// directAudioSource is the surface SpotifyPlatform needs from the native
// Spotify audio path. It is kept local so the platform depends on behavior
// rather than a concrete client and can be stubbed in tests.
type directAudioSource interface {
	// Available reports whether native audio is configured.
	Available() bool
	// BuildDownloadInfo resolves a Spotify track to a DownloadInfo whose
	// Downloader writes decrypted AAC/MP4 to disk.
	BuildDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error)
}

// nativeSource adapts a *native.WidevineClient to directAudioSource. Audio is
// real Spotify audio: decrypted AAC/MP4 via the web-player + Widevine path
// (the 2026-viable route; the old librespot OGG/Shannon path is DRM-refused).
type nativeSource struct {
	client *native.WidevineClient
}

type spotifyAudioProbeResult struct {
	Bitrate int
	Format  string
	CDNHost string
	NumKeys int
}

// newNativeSource wraps a native.WidevineClient. A nil client yields a source
// that always reports unavailable.
func newNativeSource(client *native.WidevineClient) *nativeSource {
	return &nativeSource{client: client}
}

func (n *nativeSource) Available() bool {
	return n != nil && n.client != nil && n.client.Configured()
}

func (n *nativeSource) CookieConfigured() bool {
	return n != nil && n.client != nil && n.client.Configured()
}

func (n *nativeSource) DeviceConfigured() bool {
	return n != nil && n.client != nil && n.client.HasDevice()
}

func (n *nativeSource) AccountProduct(ctx context.Context) (string, error) {
	if n == nil || n.client == nil {
		return "", native.ErrNotAuthenticated
	}
	return n.client.AccountProduct(ctx)
}

func (n *nativeSource) ProbeDownload(ctx context.Context, trackID string, quality platform.Quality) (spotifyAudioProbeResult, error) {
	if n == nil || n.client == nil {
		return spotifyAudioProbeResult{}, native.ErrNotAuthenticated
	}
	bitrate := qualityToBitrate(quality)
	if bitrate > 128 {
		if product, err := n.client.AccountProduct(ctx); err == nil && !strings.EqualFold(product, "premium") {
			bitrate = 128
		}
	}
	res, err := n.client.Probe(ctx, trackID, bitrate)
	if err != nil {
		return spotifyAudioProbeResult{}, err
	}
	return spotifyAudioProbeResult{
		Bitrate: res.Bitrate / 1000,
		Format:  res.Format,
		CDNHost: res.CDNHost,
		NumKeys: res.NumKeys,
	}, nil
}

// qualityToBitrate maps the unified quality tiers onto the AAC bitrate tiers
// Spotify serves via the Widevine path. The ceiling is AAC 256k (MP4_256) —
// lossless/Hi-Res are not attainable here (FLAC/OGG are playplay-gated), so
// every tier at or above High maps to the 256k ceiling.
func qualityToBitrate(q platform.Quality) int {
	switch q {
	case platform.QualityStandard:
		return 128
	case platform.QualityHigh, platform.QualityLossless, platform.QualityHiRes:
		return 256
	default:
		return 0 // highest available
	}
}

func spotifyQualityForBitrate(bitrate int) platform.Quality {
	if bitrate <= 128 {
		return platform.QualityStandard
	}
	return platform.QualityHigh
}

// BuildDownloadInfo resolves account-appropriate AAC quality and returns a
// lazy downloader for the Widevine fetch/decrypt pipeline.
func (n *nativeSource) BuildDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	if !n.Available() {
		return nil, native.ErrNotAuthenticated
	}

	bitrate := qualityToBitrate(quality)
	if bitrate > 128 {
		if product, err := n.client.AccountProduct(ctx); err == nil && !strings.EqualFold(product, "premium") {
			bitrate = 128
		}
	}
	resolvedQuality := spotifyQualityForBitrate(bitrate)

	downloadFn := func(ctx context.Context, info *platform.DownloadInfo, destPath string, progress func(written, total int64)) (int64, error) {
		// The whole Widevine chain (web token -> manifest -> storage-resolve ->
		// license -> CENC decrypt) runs here, lazily, so a DRM/region failure
		// surfaces as a download error rather than at info-build time.
		wv, err := n.client.Download(ctx, trackID, bitrate)
		if err != nil {
			return 0, err
		}
		info.Bitrate = wv.Bitrate / 1000
		info.Quality = spotifyQualityForBitrate(info.Bitrate)

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(destPath, wv.MP4, 0o644); err != nil {
			return 0, err
		}
		n := int64(len(wv.MP4))
		if progress != nil {
			progress(n, n)
		}
		return n, nil
	}

	// URL is a non-fetchable sentinel: the download service rejects an empty
	// URL before consulting Downloader, but never fetches it when Downloader is
	// set. We encode the track so logs are meaningful.
	return &platform.DownloadInfo{
		URL:        fmt.Sprintf("spotify-native:track:%s", trackID),
		Format:     "m4a",
		Bitrate:    bitrate,
		Quality:    resolvedQuality,
		Downloader: downloadFn,
	}, nil
}
