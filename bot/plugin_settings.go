package bot

import "strings"

const (
	PluginScopeUser  = "user"
	PluginScopeGroup = "group"
)

type PluginSettingOption struct {
	Value string
	Label string
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
	Order                 int
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
