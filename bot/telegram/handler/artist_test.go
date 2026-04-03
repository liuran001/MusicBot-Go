package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

type artistTestPlatform struct {
	*stubPlatform
	artist     *platform.Artist
	trackCount int
	err        error
	matchedID  string
}

func (p *artistTestPlatform) Metadata() platform.Meta {
	return platform.Meta{Name: p.Name(), DisplayName: "汽水音乐", Emoji: "🥤", AllowGroupURL: true}
}

func (p *artistTestPlatform) MatchArtistURL(rawURL string) (string, bool) {
	if strings.Contains(rawURL, "artist_id="+p.matchedID) {
		return p.matchedID, true
	}
	return "", false
}

func (p *artistTestPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
	return p.artist, p.err
}

func (p *artistTestPlatform) GetArtistDetails(ctx context.Context, artistID string) (*platform.Artist, int, error) {
	return p.artist, p.trackCount, p.err
}

func TestMatchArtistURL(t *testing.T) {
	manager := newStubManager()
	manager.Register(&artistTestPlatform{stubPlatform: newStubPlatform("soda"), matchedID: "423456789"})

	platformName, artistID, ok := matchArtistURL(context.Background(), manager, "https://music.douyin.com/qishui/share/artist?artist_id=423456789")
	if !ok {
		t.Fatalf("expected artist url to match")
	}
	if platformName != "soda" || artistID != "423456789" {
		t.Fatalf("matchArtistURL() = (%q,%q)", platformName, artistID)
	}
}

func TestFormatArtistMessage(t *testing.T) {
	manager := newStubManager()
	manager.Register(&artistTestPlatform{stubPlatform: newStubPlatform("soda")})
	text := formatArtistMessage(manager, "soda", &platform.Artist{
		Name:      "周杰伦",
		URL:       "https://music.douyin.com/qishui/share/artist?artist_id=1",
		AvatarURL: "https://img.example/avatar.jpg",
	}, 18)
	for _, want := range []string{"歌手信息", "平台：汽水音乐", "歌手：周杰伦", "链接：https://music.douyin.com/qishui/share/artist?artist_id=1", "代表作品数：18", "头像：https://img.example/avatar.jpg"} {
		if !strings.Contains(text, want) {
			t.Fatalf("formatArtistMessage() missing %q in %q", want, text)
		}
	}
}

func TestArtistHandlerTryHandle(t *testing.T) {
	manager := newStubManager()
	manager.Register(&artistTestPlatform{
		stubPlatform: newStubPlatform("soda"),
		matchedID:    "423456789",
		artist: &platform.Artist{
			ID:        "423456789",
			Platform:  "soda",
			Name:      "Artist A",
			URL:       "https://music.douyin.com/qishui/share/artist?artist_id=423456789",
			AvatarURL: "https://img.example/avatar.jpg",
		},
		trackCount: 7,
	})

	var sentText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal request: %v body=%s", err, string(body))
		}
		if text, ok := payload["text"].(string); ok {
			sentText = text
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 1,
				"date":       1,
				"chat":       map[string]any{"id": 1001, "type": "private"},
				"text":       sentText,
			},
		})
	}))
	defer server.Close()

	bot, err := telego.NewBot("123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi", telego.WithAPIServer(server.URL))
	if err != nil {
		t.Fatalf("NewBot() error = %v", err)
	}

	h := &ArtistHandler{PlatformManager: manager}
	update := &telego.Update{Message: &telego.Message{
		MessageID: 10,
		Text:      "https://music.douyin.com/qishui/share/artist?artist_id=423456789",
		Chat: telego.Chat{
			ID:   1001,
			Type: "private",
		},
	}}

	if !h.TryHandle(context.Background(), bot, update) {
		t.Fatalf("TryHandle() = false, want true")
	}
	for _, want := range []string{"平台：汽水音乐", "歌手：Artist A", "链接：https://music.douyin.com/qishui/share/artist?artist_id=423456789"} {
		if !strings.Contains(sentText, want) {
			t.Fatalf("sent text missing %q: %q", want, sentText)
		}
	}
}

var _ platform.ArtistURLMatcher = (*artistTestPlatform)(nil)
var _ platform.MetadataProvider = (*artistTestPlatform)(nil)
var _ io.Reader = strings.NewReader("")
