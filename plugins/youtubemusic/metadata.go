package youtubemusic

import "github.com/liuran001/MusicBot-Go/bot/platform"

// platformName is the canonical identifier for this platform.
const platformName = "youtubemusic"

// metadata describes how this platform is presented in menus and matched by
// command aliases. It is returned via the optional MetadataProvider interface.
func metadata() platform.Meta {
	return platform.Meta{
		Name:        platformName,
		DisplayName: "YouTube Music",
		Emoji:       "🎬",
		Aliases:     []string{"youtubemusic", "ytmusic", "ytm", "yt", "youtube", "ytmsc"},
		// Only YouTube Music links are safe to auto-parse in groups; ordinary
		// youtube.com shares should remain normal chat messages unless explicit.
		AllowGroupURL: true,
		GroupURLHosts: []string{"music.youtube.com"},
	}
}
