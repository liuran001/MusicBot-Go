package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
	"github.com/liuran001/MusicBot-Go/bot/i18n"
)

// profileFieldByConfigKey resolves a user-supplied field name (name / desc /
// description / short / short_description) to a botProfileField. The bool
// reports whether the name was recognized.
func profileFieldByConfigKey(name string) (botProfileField, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "name":
		return botProfileFields[0], true
	case "desc", "description":
		return botProfileFields[1], true
	case "short", "short_description", "shortdescription":
		return botProfileFields[2], true
	default:
		return botProfileField{}, false
	}
}

// profileUsage is shown when /profile is called with no or bad arguments.
const profileUsage = `用法:
/profile                      显示全部语言的名称/简介
/profile show <lang>          只显示某语言 (en/zh/ja...)
/profile set name <lang> <值>      设置机器人名称
/profile set desc <lang> <值>      设置完整简介
/profile set short <lang> <值>     设置短简介
/profile reset <lang>         清除该语言全部覆盖, 回退内置文案

字符上限: name 64 / desc 512 / short 120。
设置/清除后会立即推送到 Telegram。`

// BuildProfileCommand returns the /profile admin command for viewing and
// editing the per-language bot name and descriptions. Changes are persisted to
// the [bot_profile.<lang>] config sections and pushed to Telegram immediately.
func (a *App) BuildProfileCommand() admincmd.Command {
	return admincmd.Command{
		Name:        "profile",
		Description: "管理机器人名称/简介 (多语言)",
		Handler: func(ctx context.Context, args string) (string, error) {
			fields := strings.Fields(strings.TrimSpace(args))
			if len(fields) == 0 {
				return a.renderProfile(""), nil
			}

			switch strings.ToLower(fields[0]) {
			case "show":
				lang := ""
				if len(fields) >= 2 {
					lang = i18n.NormalizeLang(fields[1])
				}
				return a.renderProfile(lang), nil

			case "set":
				// /profile set <field> <lang> <value...>
				if len(fields) < 4 {
					return "用法: /profile set <name|desc|short> <lang> <值>", nil
				}
				field, ok := profileFieldByConfigKey(fields[1])
				if !ok {
					return "字段无效, 可用: name / desc / short", nil
				}
				lang := i18n.NormalizeLang(fields[2])
				if lang == "" || !i18n.IsSupported(lang) {
					return fmt.Sprintf("语言无效或未支持: %q\n支持的语言: %s", fields[2], strings.Join(i18n.SupportedLanguages, ", ")), nil
				}
				// The value is everything after the first three tokens
				// (set <field> <lang>), preserving its internal whitespace.
				value := strings.TrimSpace(stripTokens(args, 3))
				if value == "" {
					return "值不能为空 (如需清除请用 /profile reset)", nil
				}
				if err := a.Config.SetBotProfileField(lang, field.configKey, value); err != nil {
					return "", err
				}
				a.repushProfile(ctx, lang)
				return fmt.Sprintf("已更新 [%s] %s 并推送到 Telegram:\n%s", lang, field.configKey, value), nil

			case "reset":
				if len(fields) < 2 {
					return "用法: /profile reset <lang>", nil
				}
				lang := i18n.NormalizeLang(fields[1])
				if lang == "" || !i18n.IsSupported(lang) {
					return fmt.Sprintf("语言无效或未支持: %q", fields[1]), nil
				}
				if err := a.Config.ResetBotProfile(lang); err != nil {
					return "", err
				}
				a.repushProfile(ctx, lang)
				return fmt.Sprintf("已清除 [%s] 的全部覆盖, 回退内置文案并推送到 Telegram。", lang), nil

			default:
				return profileUsage, nil
			}
		},
	}
}

// repushProfile re-publishes one language's profile to Telegram. For the
// default language it also rewrites the empty-language_code default table so
// clients without a dedicated table see the change too.
func (a *App) repushProfile(ctx context.Context, lang string) {
	a.pushBotProfile(ctx, lang, lang)
	if lang == i18n.DefaultLanguage {
		a.pushBotProfile(ctx, "", i18n.DefaultLanguage)
	}
}

// renderProfile builds a human-readable listing of the effective profile
// values. When lang is empty, every supported language is shown; an "*" marks
// values coming from a config override rather than the embedded default.
func (a *App) renderProfile(lang string) string {
	langs := i18n.SupportedLanguages
	if lang != "" {
		if !i18n.IsSupported(lang) {
			return fmt.Sprintf("语言无效或未支持: %q\n支持的语言: %s", lang, strings.Join(i18n.SupportedLanguages, ", "))
		}
		langs = []string{lang}
	}

	var b strings.Builder
	b.WriteString("机器人名称/简介 (* = 配置覆盖, 否则为内置默认)\n")
	for _, l := range langs {
		loc := i18n.For(l)
		b.WriteString("\n[")
		b.WriteString(l)
		b.WriteString("]\n")
		for _, f := range botProfileFields {
			override := strings.TrimSpace(a.configProfileField(l, f.configKey))
			val, _ := a.effectiveBotProfile(l, f, loc)
			mark := " "
			if override != "" {
				mark = "*"
			}
			b.WriteString(fmt.Sprintf("%s %-18s %s\n", mark, f.configKey+":", val))
		}
	}
	return b.String()
}

// configProfileField is a nil-safe accessor for a single override field.
func (a *App) configProfileField(lang, key string) string {
	if a.Config == nil {
		return ""
	}
	return a.Config.GetBotProfileField(lang, key)
}

// stripTokens drops the first n whitespace-separated tokens from s and returns
// the remainder with its original internal spacing preserved. It is used to
// recover a free-form value (which may contain spaces) that follows a fixed
// number of leading arguments.
func stripTokens(s string, n int) string {
	s = strings.TrimLeft(s, " \t")
	for i := 0; i < n; i++ {
		j := strings.IndexAny(s, " \t")
		if j < 0 {
			return ""
		}
		s = strings.TrimLeft(s[j:], " \t")
	}
	return s
}
