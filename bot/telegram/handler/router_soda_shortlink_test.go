package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/mymmrac/telego"
)

type playlistMatcherTestPlatform struct {
	*stubPlatform
	matchedURL string
	matchedID  string
}

func (p *playlistMatcherTestPlatform) Metadata() platform.Meta {
	return platform.Meta{Name: p.Name(), DisplayName: "汽水音乐", Emoji: "🥤", AllowGroupURL: true, Aliases: []string{"soda", "汽水", "汽水音乐"}}
}

func (p *playlistMatcherTestPlatform) MatchPlaylistURL(rawURL string) (string, bool) {
	if strings.Contains(rawURL, p.matchedURL) {
		return p.matchedID, true
	}
	return "", false
}

func TestMatchPlaylistURL_ResolvesSodaAlbumShortLinkShareText(t *testing.T) {
	manager := newStubManager()
	manager.Register(&playlistMatcherTestPlatform{
		stubPlatform: newStubPlatform("soda"),
		matchedURL:   "album_id=6696534425410209793",
		matchedID:    "album:6696534425410209793",
	})

	originalResolver := shortURLResolver
	shortURLResolver = func(ctx context.Context, manager platform.Manager, urlStr string) (string, error) {
		if urlStr == "https://qishui.douyin.com/s/ixBVdUyy/" {
			return "https://music.douyin.com/qishui/share/album?album_id=6696534425410209793&auto_play_bgm=1", nil
		}
		return urlStr, nil
	}
	defer func() { shortURLResolver = originalResolver }()

	text := "专辑《看我72变》 https://qishui.douyin.com/s/ixBVdUyy/ @汽水音乐"
	platformName, collectionID, matched := matchPlaylistURL(context.Background(), manager, text)
	if !matched {
		t.Fatal("expected soda album short link share text to match playlist/album flow")
	}
	if platformName != "soda" || collectionID != "album:6696534425410209793" {
		t.Fatalf("matchPlaylistURL() = (%q,%q,%v)", platformName, collectionID, matched)
	}
}

func TestHasSearchPlatformSuffix_DoesNotTreatShareTextWithURLAsSearch(t *testing.T) {
	manager := newStubManager()
	manager.Register(&playlistMatcherTestPlatform{stubPlatform: newStubPlatform("soda")})

	text := "专辑《看我72变》 https://qishui.douyin.com/s/ixBVdUyy/ @汽水音乐"
	if hasSearchPlatformSuffix(text, manager) {
		t.Fatal("expected share text with URL not to be treated as search suffix input")
	}
}

func TestSearchPredicate_PrefersPlaylistForResolvedSodaAlbumShareText(t *testing.T) {
	manager := newStubManager()
	manager.Register(&playlistMatcherTestPlatform{
		stubPlatform: newStubPlatform("soda"),
		matchedURL:   "album_id=6696534425410209793",
		matchedID:    "album:6696534425410209793",
	})

	originalResolver := shortURLResolver
	shortURLResolver = func(ctx context.Context, manager platform.Manager, urlStr string) (string, error) {
		if urlStr == "https://qishui.douyin.com/s/ixBVdUyy/" {
			return "https://music.douyin.com/qishui/share/album?album_id=6696534425410209793&auto_play_bgm=1", nil
		}
		return urlStr, nil
	}
	defer func() { shortURLResolver = originalResolver }()

	text := "专辑《看我72变》 https://qishui.douyin.com/s/ixBVdUyy/ @汽水音乐"
	baseText, _, _ := parseTrailingOptions(text, manager)
	resolvedText := resolveShortLinkText(context.Background(), manager, baseText)

	if _, _, matched := matchPlaylistURL(context.Background(), manager, resolvedText); !matched {
		t.Fatal("expected resolved text to hit playlist/album matcher")
	}
	if hasSearchPlatformSuffix(text, manager) {
		t.Fatal("expected router search branch not to classify resolved share text as search suffix")
	}
	if _, _, matched := manager.MatchText(resolvedText); matched {
		t.Fatal("expected resolved share text not to fall back to track text match")
	}
	if _, _, matched := manager.MatchURL(resolvedText); matched {
		t.Fatal("expected resolved share text not to fall back to track url match")
	}

	update := telego.Update{Message: &telego.Message{Text: text, Chat: telego.Chat{ID: 1, Type: "private"}}}
	if searchPredicateMatches(update, manager) {
		t.Fatal("expected search predicate to reject soda album share text")
	}
}

func searchPredicateMatches(update telego.Update, manager platform.Manager) bool {
	if update.Message == nil || update.Message.Text == "" || isCommandMessage(update.Message) {
		return false
	}
	if update.Message.Chat.Type != "private" {
		return false
	}
	if update.Message.Voice != nil {
		return false
	}
	text := update.Message.Text
	baseText, _, _ := parseTrailingOptions(text, manager)
	if strings.TrimSpace(baseText) == "" {
		return false
	}
	if manager != nil {
		resolvedText := resolveShortLinkText(context.Background(), manager, baseText)
		if _, _, matched := matchPlaylistURL(context.Background(), manager, resolvedText); matched {
			return false
		}
		if _, _, matched := matchArtistURL(context.Background(), manager, resolvedText); matched {
			return false
		}
		if _, _, matched := manager.MatchText(resolvedText); matched {
			return false
		}
		if _, _, matched := manager.MatchURL(resolvedText); matched {
			return false
		}
	}
	if hasSearchPlatformSuffix(text, manager) {
		return true
	}
	return true
}

var _ platform.PlaylistURLMatcher = (*playlistMatcherTestPlatform)(nil)
var _ platform.MetadataProvider = (*playlistMatcherTestPlatform)(nil)
