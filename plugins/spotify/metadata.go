package spotify

import "github.com/liuran001/MusicBot-Go/bot/platform"

// platformName is the canonical identifier for this platform.
const platformName = "spotify"

// metadata describes how this platform is presented in menus and matched by
// command aliases. Returned via the optional MetadataProvider interface.
//
// Spotify serves as a METADATA source: it provides rich track/album/artist
// info and search, but audio is resolved by delegating to an audio-capable
// platform (YouTube Music) via ISRC/title matching. AllowGroupURL stays true
// so shared Spotify links resolve in groups like any other platform link.
func metadata() platform.Meta {
	return platform.Meta{
		Name:          platformName,
		DisplayName:   "Spotify",
		Emoji:         "🟢",
		Aliases:       []string{"spotify", "spot", "sp"},
		AllowGroupURL: true,
	}
}
