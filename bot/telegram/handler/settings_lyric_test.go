package handler

import (
	"strings"
	"testing"

	botpkg "github.com/liuran001/MusicBot-Go/bot"
	"github.com/mymmrac/telego"
)

// findLyricToggleButtons walks a submenu keyboard and returns the translation
// and roma toggle buttons by their callback prefix, if present.
func findLyricToggleButtons(kb *telego.InlineKeyboardMarkup) (trans, roma *telego.InlineKeyboardButton) {
	if kb == nil {
		return nil, nil
	}
	for _, row := range kb.InlineKeyboard {
		for i := range row {
			btn := row[i]
			if strings.HasPrefix(btn.CallbackData, "settings lyrictrans ") {
				trans = &row[i]
			}
			if strings.HasPrefix(btn.CallbackData, "settings lyricroma ") {
				roma = &row[i]
			}
		}
	}
	return trans, roma
}

func TestLyricFormatMenuTogglesOnlyForSideTrackFormats(t *testing.T) {
	h := &SettingsHandler{}

	// A side-track-capable default format (ttml) shows the toggles.
	ttml := &botpkg.UserSettings{UserID: 1, DefaultLyricFormat: "ttml"}
	kb := h.buildLyricFormatMenuKeyboard("private", ttml, nil)
	trans, roma := findLyricToggleButtons(kb)
	if trans == nil || roma == nil {
		t.Fatal("ttml default should expose translation/roma toggles")
	}

	// A pure word format (yrc) does not carry side tracks → no toggles.
	yrc := &botpkg.UserSettings{UserID: 1, DefaultLyricFormat: "yrc"}
	kb = h.buildLyricFormatMenuKeyboard("private", yrc, nil)
	trans, roma = findLyricToggleButtons(kb)
	if trans != nil || roma != nil {
		t.Error("yrc default should not expose side-track toggles")
	}
}

func TestResolveDefaultLyricFlagsFallbackAndExplicit(t *testing.T) {
	h := &SettingsHandler{}

	// Unset pointers: ttml defaults translation on, roma off.
	s := &botpkg.UserSettings{UserID: 1}
	tr, rm := h.resolveDefaultLyricFlags("private", s, nil, "ttml")
	if !tr || rm {
		t.Errorf("ttml unset flags = (%v,%v), want (true,false)", tr, rm)
	}
	// Unset pointers: lrc defaults translation off.
	tr, _ = h.resolveDefaultLyricFlags("private", s, nil, "lrc")
	if tr {
		t.Error("lrc unset translation should default off")
	}
	// Explicit overrides win regardless of format default.
	off := false
	on := true
	s.DefaultLyricIncludeTranslation = &off
	s.DefaultLyricIncludeRoma = &on
	tr, rm = h.resolveDefaultLyricFlags("private", s, nil, "ttml")
	if tr || !rm {
		t.Errorf("explicit flags = (%v,%v), want (false,true)", tr, rm)
	}
}

func TestLyricFormatMenuToggleButtonReflectsState(t *testing.T) {
	h := &SettingsHandler{}
	on := true
	s := &botpkg.UserSettings{UserID: 1, DefaultLyricFormat: "ttml", DefaultLyricIncludeTranslation: &on}
	kb := h.buildLyricFormatMenuKeyboard("private", s, nil)
	trans, _ := findLyricToggleButtons(kb)
	if trans == nil {
		t.Fatal("expected translation toggle")
	}
	// Translation is on → button persists "off" (the next state) and shows 开.
	if !strings.HasSuffix(trans.CallbackData, "off") {
		t.Errorf("translation-on button should toggle to off, got %q", trans.CallbackData)
	}
	if !strings.Contains(trans.Text, "开") {
		t.Errorf("translation-on button text should show 开, got %q", trans.Text)
	}
}
