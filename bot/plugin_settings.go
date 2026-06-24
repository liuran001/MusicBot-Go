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
