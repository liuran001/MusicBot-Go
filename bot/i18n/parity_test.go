package i18n

import (
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// loadLangKeys reads every embedded locales/*.toml file for the given language
// suffix and returns the union of top-level keys -> value. go-i18n merges all
// shards of one language into a single bundle, so the aggregate key set is what
// actually determines whether a lookup resolves or falls back.
func loadLangKeys(t *testing.T, lang string) map[string]string {
	t.Helper()
	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		t.Fatalf("read locales dir: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".toml") {
			continue
		}
		// Match "<lang>.toml" (base) or "<domain>.<lang>.toml" (shard).
		if name != lang+".toml" && !strings.HasSuffix(name, "."+lang+".toml") {
			continue
		}
		data, err := localeFS.ReadFile("locales/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var raw map[string]any
		if err := toml.Unmarshal(data, &raw); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for k, v := range raw {
			if s, ok := v.(string); ok {
				out[k] = s
			} else {
				// go-i18n plural tables are sub-tables; record presence only.
				out[k] = ""
			}
		}
	}
	return out
}

// TestCatalogParity guards against the most common i18n regression: adding a
// key to one language's catalog and forgetting the others. Every key present in
// ANY shipped language must exist in ALL of them; a missing key would silently
// fall back to English (or echo the id) at runtime.
func TestCatalogParity(t *testing.T) {
	perLang := map[string]map[string]string{}
	union := map[string]struct{}{}
	for _, lang := range SupportedLanguages {
		keys := loadLangKeys(t, lang)
		perLang[lang] = keys
		for k := range keys {
			union[k] = struct{}{}
		}
	}

	allKeys := make([]string, 0, len(union))
	for k := range union {
		allKeys = append(allKeys, k)
	}
	sort.Strings(allKeys)

	for _, lang := range SupportedLanguages {
		var missing []string
		for _, k := range allKeys {
			if _, ok := perLang[lang][k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			t.Errorf("language %q is missing %d key(s) present in other catalogs:\n  %s",
				lang, len(missing), strings.Join(missing, "\n  "))
		}
	}
}

// TestNoUntranslatedValues flags non-English catalog values that are byte-for-byte
// identical to English AND contain Latin letters — the typical signature of a
// copy-paste that never got translated. Pure-symbol/identifier values (e.g.
// "LRC", "%", emoji, format tokens) are legitimately shared and are skipped.
func TestNoUntranslatedValues(t *testing.T) {
	en := loadLangKeys(t, DefaultLanguage)
	for _, lang := range SupportedLanguages {
		if lang == DefaultLanguage {
			continue
		}
		other := loadLangKeys(t, lang)
		var suspect []string
		for k, enVal := range en {
			if intentionallyEnglish[k] {
				continue
			}
			ov, ok := other[k]
			if !ok || ov == "" || enVal == "" {
				continue
			}
			if ov == enVal && hasMeaningfulLatin(enVal) {
				suspect = append(suspect, k+" = "+enVal)
			}
		}
		if len(suspect) > 0 {
			sort.Strings(suspect)
			t.Errorf("language %q has %d value(s) identical to English (likely untranslated):\n  %s",
				lang, len(suspect), strings.Join(suspect, "\n  "))
		}
	}
}

// intentionallyEnglish lists keys whose value is deliberately identical across
// languages and must NOT be flagged as untranslated:
//   - brand/product name (about_title)
//   - the audio-quality technical term "Hi-Res" (kept verbatim everywhere)
//   - the language selector's own-name labels (each language name is shown in
//     its native script, so set_lang_name_en is "English" in every catalog)
//   - proper nouns of Chinese music services with no localized Japanese form
var intentionallyEnglish = map[string]bool{
	"about_title":            true,
	"cb_quality_hires":       true,
	"set_quality_hires":      true,
	"set_lang_name_en":       true,
	"set_lang_name_zh":       true,
	"set_lang_name_ja":       true,
	"set_lang_name_ru":       true,
	"help_default_platforms": true,
}

// hasMeaningfulLatin reports whether s contains a run of 2+ ASCII letters, which
// distinguishes real words ("Settings") from language-neutral tokens ("LRC" is
// allowed via the allowlist below; single letters, %, digits, emoji are not
// flagged).
func hasMeaningfulLatin(s string) bool {
	// Allowlist of language-neutral values that are intentionally shared.
	switch strings.TrimSpace(s) {
	case "LRC", "YRC", "QRC", "LYS", "KRC", "ELRC", "SPL", "ASS", "TTML",
		"LQE", "AM-JSON", "SRT", "TXT":
		return false
	}
	run := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			run++
			if run >= 2 {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}
