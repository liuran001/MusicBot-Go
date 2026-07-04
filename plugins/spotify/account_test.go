package spotify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type fakeSpotifyProbeSource struct {
	cookie       bool
	device       bool
	product      string
	productErr   error
	probe        spotifyAudioProbeResult
	probeErr     error
	probeTrackID string
	probeQuality platform.Quality
}

func (f *fakeSpotifyProbeSource) Available() bool {
	return f.cookie
}

func (f *fakeSpotifyProbeSource) BuildDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
	return &platform.DownloadInfo{URL: "spotify-native:track:" + trackID, Format: "m4a", Quality: quality}, nil
}

func (f *fakeSpotifyProbeSource) CookieConfigured() bool {
	return f.cookie
}

func (f *fakeSpotifyProbeSource) DeviceConfigured() bool {
	return f.device
}

func (f *fakeSpotifyProbeSource) AccountProduct(ctx context.Context) (string, error) {
	if f.productErr != nil {
		return "", f.productErr
	}
	return f.product, nil
}

func (f *fakeSpotifyProbeSource) ProbeDownload(ctx context.Context, trackID string, quality platform.Quality) (spotifyAudioProbeResult, error) {
	f.probeTrackID = trackID
	f.probeQuality = quality
	if f.probeErr != nil {
		return spotifyAudioProbeResult{}, f.probeErr
	}
	return f.probe, nil
}

func TestSpotifyCheckCookieLicenseProbe(t *testing.T) {
	src := &fakeSpotifyProbeSource{
		cookie:  true,
		device:  true,
		product: "free",
		probe:   spotifyAudioProbeResult{Bitrate: 128, Format: "10", CDNHost: "audio-fa.scdn.co", NumKeys: 1},
	}
	plat := &SpotifyPlatform{client: &Client{}, native: src}

	got, err := plat.CheckCookie(context.Background())
	if err != nil {
		t.Fatalf("CheckCookie() error = %v", err)
	}
	if !got.OK {
		t.Fatalf("CheckCookie() OK = false, message=%q", got.Message)
	}
	if !strings.Contains(got.Message, "AAC 128k license OK") {
		t.Fatalf("message = %q, want license bitrate", got.Message)
	}
	if src.probeTrackID != spotifyCookieCheckTrackID {
		t.Fatalf("probe track = %q, want %q", src.probeTrackID, spotifyCookieCheckTrackID)
	}
	if src.probeQuality != platform.QualityHiRes {
		t.Fatalf("probe quality = %s, want hires", src.probeQuality)
	}
}

func TestSpotifyCheckCookieRequiresCookieAndDevice(t *testing.T) {
	plat := &SpotifyPlatform{client: &Client{}, native: &fakeSpotifyProbeSource{}}
	got, err := plat.CheckCookie(context.Background())
	if err != nil {
		t.Fatalf("CheckCookie() error = %v", err)
	}
	if got.OK || !strings.Contains(got.Message, "sp_dc") {
		t.Fatalf("missing cookie result = %+v", got)
	}

	plat.native = &fakeSpotifyProbeSource{cookie: true}
	got, err = plat.CheckCookie(context.Background())
	if err != nil {
		t.Fatalf("CheckCookie() error = %v", err)
	}
	if got.OK || !strings.Contains(got.Message, "Widevine") {
		t.Fatalf("missing device result = %+v", got)
	}
}

func TestSpotifyAccountStatusReportsProduct(t *testing.T) {
	plat := &SpotifyPlatform{
		client: &Client{},
		native: &fakeSpotifyProbeSource{cookie: true, device: true, product: "premium"},
	}
	got, err := plat.AccountStatus(context.Background())
	if err != nil {
		t.Fatalf("AccountStatus() error = %v", err)
	}
	if !got.LoggedIn {
		t.Fatalf("LoggedIn = false")
	}
	if !strings.Contains(got.Summary, "账号: premium") || !strings.Contains(got.Summary, "AAC 256k") {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestSpotifyAccountStatusKeepsAuthFailureNonFatal(t *testing.T) {
	plat := &SpotifyPlatform{
		client: &Client{},
		native: &fakeSpotifyProbeSource{cookie: true, device: true, productErr: errors.New("expired")},
	}
	got, err := plat.AccountStatus(context.Background())
	if err != nil {
		t.Fatalf("AccountStatus() error = %v", err)
	}
	if got.LoggedIn {
		t.Fatalf("LoggedIn = true, want false")
	}
	if !strings.Contains(got.Summary, "校验失败") {
		t.Fatalf("summary = %q", got.Summary)
	}
}
