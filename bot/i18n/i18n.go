// Package i18n provides localization for user-facing bot text.
//
// Design goals:
//   - Handlers never touch a global text singleton. Instead the router resolves
//     the request language once and injects a *Localizer into the context; every
//     handler reads it via From(ctx) and calls T / Tmd.
//   - Locale catalogs are TOML files embedded into the binary, so deployment
//     stays a single static binary with no external assets.
//   - A single fallback language (English) guarantees every key resolves even
//     when a translation is missing.
package i18n

import (
	"embed"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.toml
var localeFS embed.FS

// DefaultLanguage is the fallback used when the requested language is missing or
// has no dedicated catalog. It MUST have a complete catalog.
const DefaultLanguage = "en"

// SupportedLanguages lists the catalogs shipped with the binary, in display
// order. The first entry must be DefaultLanguage.
var SupportedLanguages = []string{"en", "zh", "ja", "ru"}

// Manager owns the message bundle and caches one Localizer per language.
type Manager struct {
	bundle    *goi18n.Bundle
	mu        sync.RWMutex
	localizer map[string]*Localizer
}

var (
	globalMu      sync.RWMutex
	globalManager *Manager
)

// Init loads embedded catalogs and installs the process-wide Manager. It is safe
// to call multiple times; the last successful load wins. Returns an error if the
// fallback catalog fails to load.
func Init() (*Manager, error) {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		if _, err := bundle.LoadMessageFileFS(localeFS, "locales/"+entry.Name()); err != nil {
			return nil, err
		}
	}

	m := &Manager{bundle: bundle, localizer: make(map[string]*Localizer)}
	globalMu.Lock()
	globalManager = m
	globalMu.Unlock()
	return m, nil
}

// global returns the installed Manager, or nil if Init was never called.
func global() *Manager {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalManager
}

// Localizer renders messages for one resolved language.
type Localizer struct {
	lang  string
	inner *goi18n.Localizer
}

// localizerFor returns a cached Localizer for the given 2-letter language code,
// building one on first use. lang must already be normalized.
func (m *Manager) localizerFor(lang string) *Localizer {
	if m == nil {
		return fallbackLocalizer(lang)
	}
	m.mu.RLock()
	loc, ok := m.localizer[lang]
	m.mu.RUnlock()
	if ok {
		return loc
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if loc, ok := m.localizer[lang]; ok {
		return loc
	}
	// Request order: requested language first, English last so any missing key
	// transparently falls back to the complete catalog.
	loc = &Localizer{
		lang:  lang,
		inner: goi18n.NewLocalizer(m.bundle, lang, DefaultLanguage),
	}
	m.localizer[lang] = loc
	return loc
}

// fallbackLocalizer is used when no Manager has been installed (e.g. in tests
// that skip Init). It echoes the key so output is debuggable rather than empty.
func fallbackLocalizer(lang string) *Localizer {
	return &Localizer{lang: lang, inner: nil}
}

// Lang returns the resolved 2-letter language code this Localizer renders.
func (l *Localizer) Lang() string {
	if l == nil {
		return DefaultLanguage
	}
	return l.lang
}

// T renders the message for id, interpolating the optional template data. The
// first variadic argument, if present, must be a map[string]any of template
// values. A missing key returns the id itself so problems are visible in the UI
// rather than silently blank.
func (l *Localizer) T(id string, args ...map[string]any) string {
	if l == nil || l.inner == nil {
		return id
	}
	cfg := &goi18n.LocalizeConfig{MessageID: id}
	if len(args) > 0 && args[0] != nil {
		cfg.TemplateData = args[0]
	}
	out, err := l.inner.Localize(cfg)
	if err != nil || out == "" {
		return id
	}
	return out
}

// Tn renders a pluralized message. count selects the CLDR plural form and is
// also exposed to the template as {{.Count}} unless overridden in data.
func (l *Localizer) Tn(id string, count int, args ...map[string]any) string {
	if l == nil || l.inner == nil {
		return id
	}
	data := map[string]any{"Count": count}
	if len(args) > 0 && args[0] != nil {
		for k, v := range args[0] {
			data[k] = v
		}
	}
	out, err := l.inner.Localize(&goi18n.LocalizeConfig{
		MessageID:    id,
		PluralCount:  count,
		TemplateData: data,
	})
	if err != nil || out == "" {
		return id
	}
	return out
}

// Tmd renders the message for id and escapes the result for Telegram
// MarkdownV2. Catalog entries stay plain (unescaped) text; escaping happens here
// at the output boundary so translators never hand-escape. Template values are
// interpolated BEFORE escaping, so values are escaped too — pass already-safe
// markup via T + manual assembly when you need raw markdown.
func (l *Localizer) Tmd(id string, args ...map[string]any) string {
	return EscapeMarkdownV2(l.T(id, args...))
}
