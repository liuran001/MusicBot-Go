package platform

import "strings"

// Meta describes optional platform metadata used for UI and alias resolution.
type Meta struct {
	Name          string
	DisplayName   string
	Emoji         string
	Aliases       []string
	AllowGroupURL bool
}

// MetadataProvider can be implemented by platforms to expose metadata.
type MetadataProvider interface {
	Metadata() Meta
}

// normalizeAlias prepares an alias token for lookup.
func normalizeAlias(alias string) string {
	trimmed := strings.TrimSpace(alias)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "@")
	return strings.ToLower(strings.TrimSpace(trimmed))
}

// NormalizeAliasToken exposes alias normalization for callers.
func NormalizeAliasToken(alias string) string {
	return normalizeAlias(alias)
}
