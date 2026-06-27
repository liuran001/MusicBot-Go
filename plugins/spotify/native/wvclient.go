package native

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	widevine "github.com/iyear/gowidevine"
)

// WidevineClient is the production entry point for real Spotify audio via the
// web-player + Widevine path (decrypted AAC/MP4). It is the 2026-viable route:
// the old librespot OGG/Shannon path is increasingly refused (playplay DRM), so
// this client uses an sp_dc cookie to mint a web-player token, resolves the MP4
// (Widevine CENC) file, runs the Widevine license flow with the operator's L3
// device, and decrypts in pure Go (no CGo, no ffmpeg subprocess).
//
// It is safe for concurrent use; the web token is cached and refreshed under a
// mutex.
type WidevineClient struct {
	httpClient *http.Client
	spDC       string
	device     *widevine.Device

	mu          sync.Mutex
	token       string
	clientToken string
	tokenExpiry time.Time
}

// WidevineOptions configures a WidevineClient.
type WidevineOptions struct {
	// Cookie is the sp_dc cookie value (a logged-in web session). Required.
	Cookie string
	// HTTPClient is the proxy-aware client for all Spotify traffic. A nil client
	// uses a default 30s-timeout client.
	HTTPClient *http.Client
	// Device is the Widevine L3 device used to decrypt audio. Required for
	// downloads — there is no embedded default (the operator supplies a .wvd).
	Device *widevine.Device
}

// NewWidevineClient builds a WidevineClient. It does not touch the network until
// the first Download. A missing device surfaces as an error on Download.
func NewWidevineClient(opts WidevineOptions) *WidevineClient {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &WidevineClient{
		httpClient: hc,
		spDC:       opts.Cookie,
		device:     opts.Device,
	}
}

// Configured reports whether an sp_dc cookie is present.
func (c *WidevineClient) Configured() bool {
	return c != nil && c.spDC != ""
}

// ensureToken returns a valid web-player auth (Bearer + client-token),
// refreshing when within 60s of expiry. Callers must hold c.mu.
func (c *WidevineClient) ensureToken(ctx context.Context) (WebAuth, error) {
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-60*time.Second)) {
		return WebAuth{Bearer: c.token, ClientToken: c.clientToken}, nil
	}
	wt, err := WebTokenFromCookie(ctx, c.httpClient, c.spDC)
	if err != nil {
		return WebAuth{}, err
	}
	c.token = wt.AccessToken
	c.clientToken = wt.ClientToken
	if wt.ExpiresAtMs > 0 {
		c.tokenExpiry = time.UnixMilli(wt.ExpiresAtMs)
	} else {
		c.tokenExpiry = time.Now().Add(30 * time.Minute)
	}
	return WebAuth{Bearer: c.token, ClientToken: c.clientToken}, nil
}

// ensureDevice returns the Widevine device. It must have been supplied at
// construction (from the operator's wvd_path); there is no embedded default.
func (c *WidevineClient) ensureDevice() (*widevine.Device, error) {
	if c.device == nil {
		return nil, fmt.Errorf("native: no Widevine device — set [plugins.spotify] wvd_path to a .wvd file")
	}
	return c.device, nil
}

// Download resolves a Spotify track id to decrypted, playable MP4 (AAC) bytes.
// preferredBitrate selects the tier in kbps (0 = highest available, ~256). On a
// token-expiry error the cached token is dropped so the next call refreshes.
func (c *WidevineClient) Download(ctx context.Context, trackID string, preferredBitrate int) (*WidevineResult, error) {
	if !c.Configured() {
		return nil, ErrNotAuthenticated
	}
	c.mu.Lock()
	auth, err := c.ensureToken(ctx)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	device, err := c.ensureDevice()
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	res, err := DownloadWidevineMP4(ctx, c.httpClient, auth, device, trackID, preferredBitrate)
	if err != nil {
		// Drop the token so a stale-token failure self-heals next call.
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
		return res, fmt.Errorf("widevine download: %w", err)
	}
	return res, nil
}
