package i18n

import "strings"

// mdV2Replacer escapes the full set of MarkdownV2 reserved characters per the
// Telegram Bot API spec. This mirrors the replacer historically inlined in the
// telegram handler package, centralized here so localized text has exactly one
// escaping boundary.
var mdV2Replacer = strings.NewReplacer(
	"_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(",
	"\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>",
	"#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|",
	"\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
)

// EscapeMarkdownV2 escapes s for use as Telegram MarkdownV2 text. Catalog
// entries are authored as plain text; this is applied at the output boundary
// (via Localizer.Tmd) so translators never hand-escape.
func EscapeMarkdownV2(s string) string {
	return mdV2Replacer.Replace(s)
}

// MarkdownV2Replacer returns the shared MarkdownV2 escaping replacer. Handlers
// use it directly to escape dynamic, non-catalog values (platform names, chat
// titles, user input) that must be interpolated into MarkdownV2 messages.
func MarkdownV2Replacer() *strings.Replacer {
	return mdV2Replacer
}
