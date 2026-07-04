package youtubemusic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

type youtubeMusicRoundTripFunc func(*http.Request) (*http.Response, error)

func (f youtubeMusicRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestYouTubeMusicCookieKeys(t *testing.T) {
	got := youtubeMusicCookieKeys("SID=one; SAPISID=two; SID=duplicate; PREF=hl=en")
	want := []string{"PREF", "SAPISID", "SID"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("cookie keys = %v, want %v", got, want)
	}
}

func TestYouTubeMusicCheckCookieAnonymousDownloadProbe(t *testing.T) {
	client := NewClient("", 0, nil)
	client.httpClient = &http.Client{Transport: youtubeMusicRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Host == "www.youtube.com" && req.URL.Path == "/watch":
			return youtubeMusicTestResponse(req, http.StatusOK, `{"visitorData":"VISITOR"}`), nil
		case req.Method == http.MethodPost && req.URL.Host == "www.youtube.com" && req.URL.Path == "/youtubei/v1/player":
			if got := req.Header.Get("X-Goog-Visitor-Id"); got != "VISITOR" {
				t.Fatalf("visitor header = %q, want VISITOR", got)
			}
			body := `{
				"playabilityStatus": {"status": "OK"},
				"streamingData": {
					"expiresInSeconds": "3600",
					"adaptiveFormats": [
						{
							"url": "https://rr1---sn.googlevideo.com/videoplayback?id=test",
							"mimeType": "audio/webm; codecs=\"opus\"",
							"averageBitrate": 256000,
							"contentLength": "12345"
						}
					]
				},
				"videoDetails": {
					"videoId": "dQw4w9WgXcQ",
					"title": "Test",
					"lengthSeconds": "10",
					"author": "Artist"
				}
			}`
			return youtubeMusicTestResponse(req, http.StatusOK, body), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.String())
		}
	})}
	plat := NewPlatform(client)

	got, err := plat.CheckCookie(context.Background())
	if err != nil {
		t.Fatalf("CheckCookie() error = %v", err)
	}
	if !got.OK {
		t.Fatalf("CheckCookie() OK = false, message=%q", got.Message)
	}
	if !strings.Contains(got.Message, "匿名 InnerTube 可用") || !strings.Contains(got.Message, "opus 256k") {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestYouTubeMusicAccountStatusGuestMode(t *testing.T) {
	client := NewClient("", 0, nil)
	client.httpClient = &http.Client{Transport: youtubeMusicRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost && req.URL.Host == "music.youtube.com" && req.URL.Path == "/youtubei/v1/search" {
			return youtubeMusicTestResponse(req, http.StatusOK, `{}`), nil
		}
		return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.String())
	})}
	plat := NewPlatform(client)

	got, err := plat.AccountStatus(context.Background())
	if err != nil {
		t.Fatalf("AccountStatus() error = %v", err)
	}
	if got.LoggedIn {
		t.Fatalf("LoggedIn = true, want false for anonymous mode")
	}
	if !strings.Contains(got.Summary, "访客模式") || !strings.Contains(got.Summary, "InnerTube 可访问") {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func youtubeMusicTestResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestYouTubeMusicCheckCookieUnavailable(t *testing.T) {
	plat := NewPlatform(nil)
	got, err := plat.CheckCookie(context.Background())
	if err != nil {
		t.Fatalf("CheckCookie() error = %v", err)
	}
	if got.OK || !strings.Contains(got.Message, "未初始化") {
		t.Fatalf("result = %+v", got)
	}
}

func TestYouTubeMusicSupportedLoginMethodsIncludesCheck(t *testing.T) {
	got := NewPlatform(nil).SupportedLoginMethods()
	if !containsYouTubeMusicMethod(got, "check") || !containsYouTubeMusicMethod(got, "status") {
		t.Fatalf("methods = %v", got)
	}
}

func containsYouTubeMusicMethod(methods []string, want string) bool {
	for _, method := range methods {
		if method == want {
			return true
		}
	}
	return false
}

var _ platform.CookieChecker = (*YouTubeMusicPlatform)(nil)
