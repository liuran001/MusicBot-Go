package bot

import "strings"

const (
	PluginScopeUser  = "user"
	PluginScopeGroup = "group"
)

type PluginSettingOption struct {
	Value string
	Label string
	// LabelKey is an optional i18n catalog key. When set, the settings renderer
	// resolves it against the request language and uses the result instead of
	// Label. Label remains the fallback when the key is empty or unresolved.
	LabelKey string
}

type PluginSettingDefinition struct {
	Plugin                string
	Key                   string
	Title                 string
	Description           string
	DefaultUser           string
	DefaultGroup          string
	Options               []PluginSettingOption
	RequireAutoLinkDetect bool
	// GroupOnly hides this setting from the per-user (private chat) settings
	// menu; it is only shown and editable in group settings.
	GroupOnly bool
	Order     int
	// TitleKey / DescriptionKey are optional i18n catalog keys. When set, the
	// settings renderer resolves them against the request language; Title /
	// Description remain the fallback when a key is empty or unresolved.
	TitleKey       string
	DescriptionKey string
}

func (d PluginSettingDefinition) Validate(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	if len(d.Options) == 0 {
		return true
	}
	for _, opt := range d.Options {
		if strings.TrimSpace(opt.Value) == v {
			return true
		}
	}
	return false
}

func (d PluginSettingDefinition) LabelOf(value string) string {
	v := strings.TrimSpace(value)
	for _, opt := range d.Options {
		if strings.TrimSpace(opt.Value) == v {
			return opt.Label
		}
	}
	return v
}

// LabelOfLocalized resolves the display label for value, preferring a localized
// catalog lookup when the matching option declares a LabelKey. resolve maps a
// catalog key to localized text (returning "" when unresolved); it is supplied
// by the caller so this core package needs no i18n dependency. Falls back to the
// option's literal Label, then the raw value.
func (d PluginSettingDefinition) LabelOfLocalized(resolve func(key string) string, value string) string {
	v := strings.TrimSpace(value)
	for _, opt := range d.Options {
		if strings.TrimSpace(opt.Value) != v {
			continue
		}
		if resolve != nil && strings.TrimSpace(opt.LabelKey) != "" {
			if localized := strings.TrimSpace(resolve(opt.LabelKey)); localized != "" {
				return localized
			}
		}
		return opt.Label
	}
	return v
}

// TitleLocalized resolves the definition's title, preferring a localized catalog
// lookup when TitleKey is set. See LabelOfLocalized for the resolve contract.
func (d PluginSettingDefinition) TitleLocalized(resolve func(key string) string) string {
	if resolve != nil && strings.TrimSpace(d.TitleKey) != "" {
		if localized := strings.TrimSpace(resolve(d.TitleKey)); localized != "" {
			return localized
		}
	}
	return d.Title
}

func (d PluginSettingDefinition) DefaultForScope(scope string) string {
	if scope == PluginScopeGroup {
		if strings.TrimSpace(d.DefaultGroup) != "" {
			return strings.TrimSpace(d.DefaultGroup)
		}
		return strings.TrimSpace(d.DefaultUser)
	}
	if strings.TrimSpace(d.DefaultUser) != "" {
		return strings.TrimSpace(d.DefaultUser)
	}
	return strings.TrimSpace(d.DefaultGroup)
}
