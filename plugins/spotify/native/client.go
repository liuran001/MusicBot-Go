package native

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/ap"
	"golang.org/x/oauth2"
	spotifyoauth2 "golang.org/x/oauth2/spotify"

	"github.com/liuran001/MusicBot-Go/bot"
)

// ErrNotAuthenticated is returned by Download when no stored credentials exist
// yet (the operator has not run the one-time `spotify-login` flow).
var ErrNotAuthenticated = errors.New("native: spotify not authenticated (run one-time login)")

// storedState is the persisted credential blob. The Data field is the
// non-expiring "reusable credentials" Spotify issues after a successful login;
// it lets the bot reconnect on every start without re-running OAuth.
type storedState struct {
	DeviceID string `json:"device_id"`
	Username string `json:"username"`
	Data     []byte `json:"data"`
}

// oauthScopes are the scopes librespot requests; streaming is the one that
// matters for downloads, the rest mirror the official client so the grant looks
// normal.
var oauthScopes = []string{
	"streaming",
	"user-read-email",
	"user-read-private",
	"playlist-read",
	"playlist-read-private",
	"user-library-read",
}

// Client is the public entry point for native Spotify audio. It persists
// credentials to a file, reconnects automatically, and serves decrypted Ogg
// Vorbis downloads. It is safe for concurrent use.
type Client struct {
	log        librespot.Logger
	httpClient *http.Client
	statePath  string

	mu    sync.Mutex
	state *storedState
	sess  *session
}

// ClientOptions configures a Client.
type ClientOptions struct {
	// StatePath is the file where reusable credentials are persisted.
	StatePath string
	// Logger is the bot logger (may be nil for silent operation).
	Logger bot.Logger
	// HTTPClient is the HTTP client for all Spotify traffic (proxy-aware).
	// A nil client uses a default 30s-timeout client.
	HTTPClient *http.Client
}

// NewClient builds a Client. It does not connect or touch the network until the
// first Download (or Login). Existing credentials are loaded from StatePath if
// present.
func NewClient(opts ClientOptions) *Client {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{
		log:        newLogAdapter(opts.Logger),
		httpClient: httpClient,
		statePath:  opts.StatePath,
	}
	c.state, _ = c.loadState()
	return c
}

// Authenticated reports whether usable stored credentials are present.
func (c *Client) Authenticated() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state != nil && c.state.Username != "" && len(c.state.Data) > 0
}

// loadState reads persisted credentials from disk; returns nil when absent.
func (c *Client) loadState() (*storedState, error) {
	if c.statePath == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(c.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var st storedState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("native: corrupt credential file: %w", err)
	}
	return &st, nil
}

// saveState persists credentials atomically.
func (c *Client) saveState(st *storedState) error {
	if c.statePath == "" {
		return fmt.Errorf("native: no state path configured")
	}
	if dir := filepath.Dir(c.statePath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.statePath), filepath.Base(c.statePath)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	// Credentials are sensitive: restrict to owner.
	_ = os.Chmod(tmpName, 0o600)
	return os.Rename(tmpName, c.statePath)
}

// newDeviceID generates a fresh random 20-byte device id (40 hex chars).
func newDeviceID() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Login runs the one-time interactive OAuth flow: it prints an authorization
// URL, starts a localhost callback server on callbackPort (0 = random), waits
// for the redirect, exchanges the code, connects to Spotify, and persists the
// resulting reusable credentials. After Login succeeds the bot can reconnect
// indefinitely without re-authorizing.
//
// promptURL receives the authorization URL to open in a browser. It must be
// non-nil; the caller decides how to surface it (stdout, Telegram message, …).
func (c *Client) Login(ctx context.Context, callbackPort int, promptURL func(url string)) error {
	if promptURL == nil {
		return fmt.Errorf("native: promptURL callback required")
	}

	port, codeCh, err := startOAuthServer(ctx, c.log, callbackPort)
	if err != nil {
		return err
	}

	conf := &oauth2.Config{
		ClientID:    librespot.ClientIdHex,
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/login", port),
		Scopes:      oauthScopes,
		Endpoint:    spotifyoauth2.Endpoint,
	}
	verifier := oauth2.GenerateVerifier()
	authURL := conf.AuthCodeURL("", oauth2.S256ChallengeOption(verifier))
	promptURL(authURL)

	var code string
	select {
	case code = <-codeCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	if code == "" {
		return fmt.Errorf("native: empty oauth code")
	}

	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("native: oauth exchange failed: %w", err)
	}
	username, _ := token.Extra("username").(string)

	deviceID := newDeviceID()
	sess, err := connect(ctx, c.log, deviceID, c.httpClient, func(ctx context.Context, apc *ap.Accesspoint) error {
		return apc.ConnectSpotifyToken(ctx, username, token.AccessToken)
	})
	if err != nil {
		return err
	}
	defer sess.Close()

	st := &storedState{
		DeviceID: deviceID,
		Username: sess.Username(),
		Data:     sess.StoredCredentials(),
	}
	if len(st.Data) == 0 {
		return fmt.Errorf("native: login produced no reusable credentials")
	}
	if err := c.saveState(st); err != nil {
		return fmt.Errorf("native: failed persisting credentials: %w", err)
	}

	c.mu.Lock()
	c.state = st
	c.mu.Unlock()
	return nil
}

// ensureSession returns a connected session, reconnecting with stored
// credentials if needed. Callers must hold c.mu.
func (c *Client) ensureSession(ctx context.Context) (*session, error) {
	if c.sess != nil {
		return c.sess, nil
	}
	if c.state == nil || c.state.Username == "" || len(c.state.Data) == 0 {
		return nil, ErrNotAuthenticated
	}
	deviceID := c.state.DeviceID
	if deviceID == "" {
		deviceID = newDeviceID()
	}
	username := c.state.Username
	data := c.state.Data
	sess, err := connect(ctx, c.log, deviceID, c.httpClient, func(ctx context.Context, apc *ap.Accesspoint) error {
		return apc.ConnectStored(ctx, username, data)
	})
	if err != nil {
		return nil, err
	}
	c.sess = sess
	return sess, nil
}

// Download resolves a Spotify track ID to a decrypted Ogg Vorbis stream.
// preferredBitrate selects the tier (0 = highest available). The returned
// stream's Close must be called by the caller.
//
// On a connection error the cached session is dropped so the next call
// reconnects. Errors are returned as-is so the caller can distinguish
// "unavailable / DRM" (fall back to YTM) from "not authenticated".
func (c *Client) Download(ctx context.Context, trackID string, preferredBitrate int) (*resolvedStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	sess, err := c.ensureSession(ctx)
	if err != nil {
		return nil, err
	}

	uri := fmt.Sprintf("spotify:track:%s", trackID)
	stream, err := sess.download(ctx, uri, preferredBitrate)
	if err != nil {
		// Drop the session on transport-level failures so we reconnect next time.
		if isConnectionError(err) {
			sess.Close()
			c.sess = nil
		}
		return nil, err
	}
	return stream, nil
}

// Close tears down any live session.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess != nil {
		c.sess.Close()
		c.sess = nil
	}
}

// isConnectionError reports whether err indicates the AP connection is dead and
// a reconnect is warranted.
func isConnectionError(err error) bool {
	return errors.Is(err, ap.ErrAccesspointClosed)
}

// startOAuthServer starts a localhost HTTP server that captures the OAuth
// redirect's ?code= parameter. It is a CGo-free reimplementation of
// session.NewOAuth2Server (which we cannot import). Returns the bound port and
// a channel that receives the code (or "" on error).
func startOAuthServer(ctx context.Context, log librespot.Logger, callbackPort int) (int, chan string, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", callbackPort))
	if err != nil {
		return 0, nil, fmt.Errorf("native: failed to listen for oauth callback: %w", err)
	}

	errCh := make(chan error, 1)
	resCh := make(chan string, 1)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			resCh <- r.URL.Query().Get("code")
			_, _ = rw.Write([]byte("Login complete. You can close this tab and return to the bot."))
		}),
	}
	go func() { errCh <- srv.Serve(lis) }()
	go func() {
		select {
		case <-ctx.Done():
			_ = srv.Close()
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				if log != nil {
					log.WithError(err).Errorf("oauth callback server error")
				}
				resCh <- ""
			}
		}
	}()

	return lis.Addr().(*net.TCPAddr).Port, resCh, nil
}
