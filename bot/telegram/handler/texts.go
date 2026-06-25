package handler

import (
	"context"
	"sort"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/i18n"
	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// mdV2Replacer escapes MarkdownV2 reserved characters. It is the same replacer
// centralized in the i18n package; kept here as a thin alias so existing handler
// call sites (which escape dynamic, non-catalog values like platform names and
// chat titles) need no churn.
var mdV2Replacer = i18n.MarkdownV2Replacer()

// callbackText is a protocol token shown in callback acknowledgements; it is
// intentionally NOT localized (Telegram treats empty/▾ differently and existing
// clients expect the literal). Kept as a const for call-site clarity.
const callbackText = "Success"

// buildHelpText assembles the /help and /start text for the request language in
// ctx. Structural markdown (bold headers, inline code, list markers) lives in
// code; only the human-readable labels come from the catalog. Dynamic values
// (platform names/aliases) are MarkdownV2-escaped by their builders, so the
// whole result is sent with ParseMode=MarkdownV2.
func buildHelpText(ctx context.Context, manager platform.Manager, isAdmin bool, adminCommands []admincmd.Command, recognizeEnabled bool, isPrivateChat bool) string {
	aliasText := buildAliasHint(manager)
	platformText := buildPlatformList(manager)
	if aliasText == "" {
		aliasText = "`163` / `qq`"
	}
	if platformText == "" {
		platformText = mdV2Replacer.Replace(tr(ctx, "help_default_platforms"))
	}
	esc := func(id string) string { return mdV2Replacer.Replace(tr(ctx, id)) }
	argTrack := esc("help_arg_track")
	argKeyword := esc("help_arg_keyword")

	text := "*🎵 MusicBot\\-Go*\n\n" + esc("help_intro") + "\n"
	if isPrivateChat {
		text += esc("help_private_hint") + "\n"
	}
	text += "\n*🚀 " + esc("help_section_commands") + "*\n" +
		"`/music` " + argTrack + " \\[" + esc("help_platform_label") + "\\] \\[" + esc("help_quality_label") + "\\] \\- " + esc("help_cmd_music") + "\n" +
		"`/search` " + argKeyword + " \\[" + esc("help_platform_label") + "\\] \\[" + esc("help_quality_label") + "\\] \\- " + esc("help_cmd_search") + "\n" +
		"`/lyric` " + argTrack + " \\[" + esc("help_platform_label") + "\\] \\- " + esc("help_cmd_lyric") + "\n"
	if recognizeEnabled {
		text += "`/recognize` \\- " + esc("help_cmd_recognize") + "\n"
	}
	text += "`/settings` \\- " + esc("help_cmd_settings") + "\n" +
		"`/status` \\- " + esc("help_cmd_status") + "\n" +
		"`/about` \\- " + esc("help_cmd_about") + "\n" +
		"\n*🎚 " + esc("help_section_params") + "*\n" +
		esc("help_quality_label") + "：`low` / `high` / `lossless` / `hires`\n" +
		esc("help_platform_label") + "：\n" + aliasText + "\n" +
		"\n" + esc("help_supported_platforms") + "：" + platformText + "\n" +
		"\n*💡 " + esc("help_section_examples") + "*\n" +
		"`/music " + tr(ctx, "help_example_music") + "`\n" +
		"`/music https://music.163.com/song/1859603835`\n" +
		"`/search " + tr(ctx, "help_example_search") + "`"
	adminText := buildAdminHelp(ctx, adminCommands)
	if isAdmin && adminText != "" {
		text += "\n\n*🛠 " + esc("help_section_admin") + "*\n" + adminText
	}
	return text
}

func buildAdminHelp(ctx context.Context, adminCommands []admincmd.Command) string {
	if len(adminCommands) == 0 {
		return ""
	}
	items := make([]admincmd.Command, 0, len(adminCommands))
	for _, cmd := range adminCommands {
		if strings.TrimSpace(cmd.Name) == "" {
			continue
		}
		items = append(items, cmd)
	}
	if len(items) == 0 {
		return ""
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	lines := make([]string, 0, len(items))
	for _, cmd := range items {
		name := mdV2Replacer.Replace(cmd.Name)
		// Prefer a localized description keyed by command name; fall back to the
		// command's own Description (which is already localized for commands built
		// with a request context, e.g. reload/rmcache).
		description := strings.TrimSpace(cmd.Description)
		key := "help_admincmd_" + strings.TrimSpace(cmd.Name)
		if localized := tr(ctx, key); localized != key {
			description = localized
		}
		desc := mdV2Replacer.Replace(description)
		if desc == "" {
			lines = append(lines, "`/"+name+"`")
			continue
		}
		lines = append(lines, "`/"+name+"` \\- "+desc)
	}
	return strings.Join(lines, "\n")
}

func buildAliasHint(manager platform.Manager) string {
	if manager == nil {
		return ""
	}
	metaList := manager.ListMeta()
	if len(metaList) == 0 {
		return ""
	}
	sort.Slice(metaList, func(i, j int) bool {
		left := strings.TrimSpace(metaList[i].DisplayName)
		if left == "" {
			left = strings.TrimSpace(metaList[i].Name)
		}
		right := strings.TrimSpace(metaList[j].DisplayName)
		if right == "" {
			right = strings.TrimSpace(metaList[j].Name)
		}
		if left == right {
			return strings.TrimSpace(metaList[i].Name) < strings.TrimSpace(metaList[j].Name)
		}
		return left < right
	})
	lines := make([]string, 0, len(metaList))
	for _, meta := range metaList {
		platformName := strings.TrimSpace(meta.Name)
		if platformName == "" {
			continue
		}
		aliases := meta.Aliases
		if len(aliases) == 0 {
			aliases = []string{platformName}
		}
		aliasSet := make(map[string]struct{})
		aliasItems := make([]string, 0, len(aliases))
		for _, alias := range aliases {
			key := platform.NormalizeAliasToken(alias)
			if key == "" {
				continue
			}
			if _, ok := aliasSet[key]; ok {
				continue
			}
			aliasSet[key] = struct{}{}
			aliasItems = append(aliasItems, key)
		}
		if len(aliasItems) == 0 {
			continue
		}
		sort.Strings(aliasItems)
		for i := range aliasItems {
			aliasItems[i] = "`" + mdV2Replacer.Replace(aliasItems[i]) + "`"
		}
		display := strings.TrimSpace(meta.DisplayName)
		if display == "" {
			display = platformName
		}
		lines = append(lines, "• "+mdV2Replacer.Replace(display)+": "+strings.Join(aliasItems, " / "))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func buildPlatformList(manager platform.Manager) string {
	if manager == nil {
		return ""
	}
	metaList := manager.ListMeta()
	if len(metaList) == 0 {
		return ""
	}
	items := make([]string, 0, len(metaList))
	for _, meta := range metaList {
		display := strings.TrimSpace(meta.DisplayName)
		if display == "" {
			display = meta.Name
		}
		if display == "" {
			continue
		}
		items = append(items, mdV2Replacer.Replace(display))
	}
	return strings.Join(items, ", ")
}
