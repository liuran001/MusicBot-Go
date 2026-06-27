package native

import (
	"context"
	"fmt"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/ap"
	"golang.org/x/oauth2"
	spotifyoauth2 "golang.org/x/oauth2/spotify"
)

// Manual (paste-the-code) OAuth login. This avoids relying on a localhost
// callback server being reachable from the browser — which is fragile on
// headless servers and on WSL2, where a Windows browser cannot reach the
// loopback HTTP server bound inside the Linux VM. Instead the operator opens
// the URL, authorizes, copies the `code` query parameter from the (possibly
// "connection refused") redirect, and pastes it back.
//
// The PKCE verifier from ManualAuthURL must be persisted by the caller and
// passed to ManualExchange, so the two steps can run as separate processes.

func (c *Client) oauthConfig(redirectURI string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    librespot.ClientIdHex,
		RedirectURL: redirectURI,
		Scopes:      oauthScopes,
		Endpoint:    spotifyoauth2.Endpoint,
	}
}

// ManualAuthURL returns an authorization URL and the PKCE verifier that must be
// persisted and handed back to ManualExchange. redirectURI must be identical in
// both calls (Spotify validates it on exchange); it does not need to be
// reachable — a loopback URL like http://127.0.0.1:8898/login is fine.
func (c *Client) ManualAuthURL(redirectURI string) (authURL, verifier string) {
	verifier = oauth2.GenerateVerifier()
	authURL = c.oauthConfig(redirectURI).AuthCodeURL("", oauth2.S256ChallengeOption(verifier))
	return authURL, verifier
}

// ManualExchangeToken does only the OAuth code→token exchange and returns the
// raw access token, WITHOUT connecting to the Spotify access point. This is for
// probing the web-stream (spclient HTTPS) path, which does not use the AP
// protocol and therefore is not subject to the AP's TravelRestriction check.
func (c *Client) ManualExchangeToken(ctx context.Context, code, verifier, redirectURI string) (string, error) {
	conf := c.oauthConfig(redirectURI)
	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", fmt.Errorf("native: oauth exchange failed: %w", err)
	}
	return token.AccessToken, nil
}

// ManualExchange completes login using the pasted code and the verifier from
// ManualAuthURL, connects to Spotify, and persists the reusable credentials.
func (c *Client) ManualExchange(ctx context.Context, code, verifier, redirectURI string) error {
	conf := c.oauthConfig(redirectURI)
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
