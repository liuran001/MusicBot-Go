// Package native implements real Spotify audio downloading without any CGo
// dependency, so it builds under the project's CGO_ENABLED=0 constraint.
//
// It does NOT use go-librespot's high-level session/player packages: those
// transitively import the CGo audio decoders (flac/vorbis/output) used for
// live playback. A download bot never decodes to PCM — it only needs to
// authenticate, resolve a track to an encrypted Ogg on the CDN, fetch it,
// AES-128-CTR decrypt it, strip Spotify's proprietary leading Ogg page, and
// hand the resulting standard Ogg Vorbis stream to ffmpeg.
//
// The bootstrap below is a trimmed copy of
// go-librespot/session.NewSessionFromOptions that wires only the pure-Go
// low-level packages (ap, login5, spclient, audio, apresolve, mercury).
package native

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/ap"
	"github.com/devgianlu/go-librespot/apresolve"
	"github.com/devgianlu/go-librespot/audio"
	"github.com/devgianlu/go-librespot/login5"
	"github.com/devgianlu/go-librespot/mercury"
	pbdata "github.com/devgianlu/go-librespot/proto/spotify/clienttoken/data/v0"
	pbhttp "github.com/devgianlu/go-librespot/proto/spotify/clienttoken/http/v0"
	credentialspb "github.com/devgianlu/go-librespot/proto/spotify/login5/v3/credentials"
	"github.com/devgianlu/go-librespot/spclient"
	"google.golang.org/protobuf/proto"
)

// session holds the connected low-level Spotify clients needed to download a
// track. It is built by connect and torn down by Close.
type session struct {
	log         librespot.Logger
	client      *http.Client
	deviceID    string
	clientToken string

	resolver *apresolve.ApResolver
	login5   *login5.Login5
	ap       *ap.Accesspoint
	sp       *spclient.Spclient
	hg       *mercury.Client
	audioKey *audio.KeyProvider
}

// connect authenticates with Spotify using the given credentials and returns a
// ready session. credentials must be one of *credentialspb.StoredCredential
// (runtime path) handled by the caller via ConnectStored, but here we accept a
// pre-built connect closure so both the stored and token flows share setup.
func connect(ctx context.Context, log librespot.Logger, deviceID string, httpClient *http.Client, doConnect func(ctx context.Context, ap *ap.Accesspoint) error) (*session, error) {
	if log == nil {
		log = &librespot.NullLogger{}
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if deviceID == "" {
		return nil, fmt.Errorf("native: empty device id")
	}
	if raw, err := hex.DecodeString(deviceID); err != nil || len(raw) != 20 {
		return nil, fmt.Errorf("native: invalid device id (need 40 hex chars / 20 bytes)")
	}

	s := &session{log: log, client: httpClient, deviceID: deviceID}

	// Obtain a client token (required by spclient for all API calls).
	clientToken, err := retrieveClientToken(httpClient, deviceID)
	if err != nil {
		return nil, fmt.Errorf("native: failed obtaining client token: %w", err)
	}
	s.clientToken = clientToken

	// Resolve current access point / spclient / dealer endpoints.
	s.resolver = apresolve.NewApResolver(log, httpClient)
	s.login5 = login5.NewLogin5(log, httpClient, deviceID, clientToken)

	apAddr, err := s.resolver.GetAccesspoint(ctx)
	if err != nil {
		return nil, fmt.Errorf("native: failed getting accesspoint: %w", err)
	}
	s.ap = ap.NewAccesspoint(log, apAddr, deviceID)

	// Authenticate the access point (token or stored credentials).
	if err := doConnect(ctx, s.ap); err != nil {
		return nil, fmt.Errorf("native: accesspoint authentication failed: %w", err)
	}

	// Authenticate with login5 using the stored credentials the AP just
	// negotiated. This yields the bearer token spclient needs.
	if err := s.login5.Login(ctx, &credentialspb.StoredCredential{
		Username: s.ap.Username(),
		Data:     s.ap.StoredCredentials(),
	}); err != nil {
		s.ap.Close()
		return nil, fmt.Errorf("native: login5 authentication failed: %w", err)
	}

	// spclient: track metadata, storage resolve, web API.
	spAddr, err := s.resolver.GetSpclient(ctx)
	if err != nil {
		s.ap.Close()
		return nil, fmt.Errorf("native: failed getting spclient endpoint: %w", err)
	}
	s.sp, err = spclient.NewSpclient(ctx, log, httpClient, spAddr, s.login5.AccessToken(), deviceID, clientToken)
	if err != nil {
		s.ap.Close()
		return nil, fmt.Errorf("native: failed initializing spclient: %w", err)
	}

	// mercury + audio key provider (legacy AES-key-over-AP path).
	s.hg = mercury.NewClient(log, s.ap)
	s.audioKey = audio.NewAudioKeyProvider(log, s.ap)

	return s, nil
}

// Username returns the authenticated account username.
func (s *session) Username() string { return s.ap.Username() }

// AccessToken returns the current login5 bearer token used for spclient calls.
// Exposed so the Widevine (web-stream) path can authenticate against the same
// spclient endpoints (storage-resolve, widevine-license) with this session's
// token.
func (s *session) AccessToken(ctx context.Context) (string, error) {
	return s.sp.GetAccessToken(ctx, false)
}

// StoredCredentials returns the reusable (non-expiring) credential blob, used
// to persist the login so future starts skip the OAuth flow.
func (s *session) StoredCredentials() []byte { return s.ap.StoredCredentials() }

// Close tears down the access point connection.
func (s *session) Close() {
	if s == nil {
		return
	}
	if s.ap != nil {
		s.ap.Close()
	}
}

// retrieveClientToken requests a Spotify client token for the given device id.
// This is a verbatim port of the unexported session.retrieveClientToken, which
// we cannot import (the session package pulls in CGo via player).
func retrieveClientToken(c *http.Client, deviceID string) (string, error) {
	body, err := proto.Marshal(&pbhttp.ClientTokenRequest{
		RequestType: pbhttp.ClientTokenRequestType_REQUEST_CLIENT_DATA_REQUEST,
		Request: &pbhttp.ClientTokenRequest_ClientData{
			ClientData: &pbhttp.ClientDataRequest{
				ClientId:      librespot.ClientIdHex,
				ClientVersion: librespot.SpotifyLikeClientVersion(),
				Data: &pbhttp.ClientDataRequest_ConnectivitySdkData{
					ConnectivitySdkData: &pbdata.ConnectivitySdkData{
						DeviceId:             deviceID,
						PlatformSpecificData: librespot.GetPlatformSpecificData(),
					},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed marshalling ClientTokenRequest: %w", err)
	}

	reqURL, err := url.Parse("https://clienttoken.spotify.com/v1/clienttoken")
	if err != nil {
		return "", fmt.Errorf("invalid clienttoken url: %w", err)
	}

	resp, err := c.Do(&http.Request{
		Method: "POST",
		URL:    reqURL,
		Header: http.Header{
			"Accept":     []string{"application/x-protobuf"},
			"User-Agent": []string{librespot.UserAgent()},
		},
		Body: io.NopCloser(bytes.NewReader(body)),
	})
	if err != nil {
		return "", fmt.Errorf("failed requesting clienttoken: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("invalid status code from clienttoken: %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed reading clienttoken response: %w", err)
	}

	var protoResp pbhttp.ClientTokenResponse
	if err := proto.Unmarshal(respBody, &protoResp); err != nil {
		return "", fmt.Errorf("failed unmarshalling clienttoken response: %w", err)
	}

	switch protoResp.ResponseType {
	case pbhttp.ClientTokenResponseType_RESPONSE_GRANTED_TOKEN_RESPONSE:
		return protoResp.GetGrantedToken().Token, nil
	case pbhttp.ClientTokenResponseType_RESPONSE_CHALLENGES_RESPONSE:
		return "", fmt.Errorf("clienttoken challenge not supported")
	default:
		return "", fmt.Errorf("unknown clienttoken response type: %v", protoResp.ResponseType)
	}
}
