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
	esc := func(id string) string { return mdV2Replacer.Replace(tr(ctx, id)) }
	argTrack := esc("help_arg_track")
	argKeyword := esc("help_arg_keyword")
	platformBlock := buildPlatformBlock(ctx, manager)

	text := "*🎵 MusicBot\\-Go*\n\n" + esc("help_intro") + "\n"
	if isPrivateChat {
		text += esc("help_private_hint") + "\n"
	}
	text += "\n*" + esc("help_section_commands") + "*\n" +
		"`/music` " + argTrack + " \\[" + esc("help_platform_label") + "\\] \\[" + esc("help_quality_label") + "\\] \\- " + esc("help_cmd_music") + "\n" +
		"`/search` " + argKeyword + " \\[" + esc("help_platform_label") + "\\] \\[" + esc("help_quality_label") + "\\] \\- " + esc("help_cmd_search") + "\n" +
		"`/lyric` " + argTrack + " \\[" + esc("help_platform_label") + "\\] \\- " + esc("help_cmd_lyric") + "\n"
	if recognizeEnabled {
		text += "`/recognize` \\- " + esc("help_cmd_recognize") + "\n"
	}
	text += "`/settings` \\- " + esc("help_cmd_settings") + "\n" +
		"`/status` \\- " + esc("help_cmd_status") + "\n" +
		"`/queue` \\- " + esc("help_cmd_queue") + "\n" +
		"`/about` \\- " + esc("help_cmd_about") + "\n" +
		"\n*" + esc("help_section_params") + "*\n" +
		esc("help_quality_label") + "：`low` / `high` / `lossless` / `hires`\n" +
		platformBlock +
		"\n*" + esc("help_section_examples") + "*\n" +
		"`/music " + tr(ctx, "help_example_music") + "`\n" +
		"`/music https://music.163.com/song/1859603835`\n" +
		"`/search " + tr(ctx, "help_example_search") + "`"
	adminText := buildAdminHelp(ctx, adminCommands)
	if isAdmin && adminText != "" {
		text += "\n\n*" + esc("help_section_admin") + "*\n" + adminText
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

// buildPlatformBlock renders the "Platform" line of the params section. The
// platform list with per-platform aliases can be long, so it is folded into a
// single expandable MarkdownV2 blockquote (`**>` … `||`): collapsed to one
// summary line by default, tapped to reveal the full alias list. The summary
// line shows the platform count and the first couple of display names so the
// collapsed state is still informative.
//
// Returns the complete line(s) to splice into the help text (label + block),
// already MarkdownV2-escaped, ending in a newline. Falls back to a plain inline
// hint when no platforms are registered.
func buildPlatformBlock(ctx context.Context, manager platform.Manager) string {
	label := mdV2Replacer.Replace(tr(ctx, "help_platform_label"))

	rows, displays := platformAliasRows(ctx, manager)
	if len(rows) == 0 {
		// No registered platforms: keep the section useful with a static hint.
		return label + "：`163` / `qq`\n"
	}

	// Collapsed summary line, e.g. "Platform：NetEase, QQ Music, Apple Music …".
	// Uses the same full-width colon the rest of the help text uses after labels,
	// then an ASCII comma-separated preview so it reads the same in every
	// language. A trailing ellipsis hints there is more behind the fold.
	preview := displays
	const maxPreview = 3
	truncated := false
	if len(preview) > maxPreview {
		preview = preview[:maxPreview]
		truncated = true
	}
	summary := label + "：" + strings.Join(preview, mdV2Replacer.Replace(", "))
	if truncated {
		summary += mdV2Replacer.Replace(" …")
	}

	// Expandable blockquote: first line prefixed `**>`, the rest `>`, last
	// line suffixed `||`. The summary rides on the first quoted line so the
	// collapsed view shows it; expanding reveals every platform's aliases.
	var b strings.Builder
	b.WriteString("**>")
	b.WriteString(summary)
	for _, row := range rows {
		b.WriteString("\n>")
		b.WriteString(row)
	}
	b.WriteString("||\n")
	return b.String()
}

// platformAliasRows returns one MarkdownV2-escaped "DisplayName: `a` / `b`" row
// per registered platform (sorted by display name), plus the list of escaped
// display names in the same order for building a summary.
func platformAliasRows(ctx context.Context, manager platform.Manager) (rows []string, displays []string) {
	if manager == nil {
		return nil, nil
	}
	metaList := manager.ListMeta()
	if len(metaList) == 0 {
		return nil, nil
	}
	sort.Slice(metaList, func(i, j int) bool {
		left := platformDisplayName(ctx, manager, metaList[i].Name)
		right := platformDisplayName(ctx, manager, metaList[j].Name)
		if left == right {
			return strings.TrimSpace(metaList[i].Name) < strings.TrimSpace(metaList[j].Name)
		}
		return left < right
	})
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
		display := platformDisplayName(ctx, manager, platformName)
		escDisplay := mdV2Replacer.Replace(display)
		rows = append(rows, escDisplay+": "+strings.Join(aliasItems, " / "))
		displays = append(displays, escDisplay)
	}
	return rows, displays
}
