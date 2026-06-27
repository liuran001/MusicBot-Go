package i18n

import "strings"

// supported is a fast lookup of shipped catalogs, built from SupportedLanguages.
var supported = func() map[string]struct{} {
	m := make(map[string]struct{}, len(SupportedLanguages))
	for _, l := range SupportedLanguages {
		m[l] = struct{}{}
	}
	return m
}()

// IsSupported reports whether lang (a normalized 2-letter code) ships a catalog.
func IsSupported(lang string) bool {
	_, ok := supported[NormalizeLang(lang)]
	return ok
}

// NormalizeLang reduces an IETF/BCP-47 tag (as found in Telegram's
// User.LanguageCode, e.g. "zh-hans", "pt-BR", "en") to a lowercase 2-letter
// primary subtag. Empty or unrecognized input yields "".
func NormalizeLang(tag string) string {
	tag = strings.TrimSpace(strings.ToLower(tag))
	if tag == "" {
		return ""
	}
	if i := strings.IndexAny(tag, "-_"); i >= 0 {
		tag = tag[:i]
	}
	return tag
}

// Resolve picks the effective language from, in priority order:
//  1. an explicit user/group override (override), e.g. a persisted /settings pick
//  2. the Telegram client UI language (clientTag, IETF tag from User.LanguageCode)
//  3. DefaultLanguage
//
// Each candidate must have a shipped catalog to be chosen; otherwise the next
// candidate is tried. The result is always a supported 2-letter code.
func Resolve(override, clientTag string) string {
	if l := NormalizeLang(override); l != "" && IsSupported(l) {
		return l
	}
	if l := NormalizeLang(clientTag); l != "" && IsSupported(l) {
		return l
	}
	return DefaultLanguage
}

// For returns the Localizer for the resolved language using the installed
// global Manager. Safe to call before Init (returns a key-echoing fallback).
func For(lang string) *Localizer {
	if m := global(); m != nil {
		return m.localizerFor(NormalizeLang(lang))
	}
	return fallbackLocalizer(NormalizeLang(lang))
}
