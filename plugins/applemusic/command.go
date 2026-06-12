package applemusic

import (
	"context"
	"fmt"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/admincmd"
)

// buildLanguageCommand returns the /amlang admin command for viewing and
// setting the Apple Music metadata language.
//
//	/amlang            查看当前 storefront / 语言 / 支持的语言列表
//	/amlang <lang>     设置语言（须在 storefront 支持范围内），并回写 config.ini
func buildLanguageCommand(client *Client) admincmd.Command {
	return admincmd.Command{
		Name:        "amlang",
		Description: "查看/设置 Apple Music 元数据语言",
		Handler: func(ctx context.Context, args string) (string, error) {
			return handleLanguageCommand(ctx, client, args)
		},
	}
}

func handleLanguageCommand(ctx context.Context, client *Client, args string) (string, error) {
	if client == nil {
		return "Apple Music 插件未初始化", nil
	}
	arg := strings.TrimSpace(args)

	// 查看模式
	if arg == "" || arg == "show" || arg == "status" {
		info, err := client.fetchStorefrontInfo(ctx)
		if err != nil {
			// 即使拿不到 storefront（如未配 token），也给出当前内存值
			return fmt.Sprintf("🍎 Apple Music 语言\n- 当前 Storefront: %s\n- 当前语言: %s\n- 获取支持语言失败: %v",
				client.CurrentStorefront(), client.CurrentLanguage(), err), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "🍎 Apple Music 语言\n")
		fmt.Fprintf(&b, "- 账号 Storefront: %s（%s）\n", info.ID, info.Name)
		fmt.Fprintf(&b, "- 当前语言: %s\n", client.CurrentLanguage())
		fmt.Fprintf(&b, "- 账号默认语言: %s\n", info.DefaultLang)
		fmt.Fprintf(&b, "- 支持的语言:\n")
		for _, lang := range info.SupportedLangs {
			marker := "  •"
			if strings.EqualFold(lang, client.CurrentLanguage()) {
				marker = "  ✅"
			}
			fmt.Fprintf(&b, "%s %s\n", marker, lang)
		}
		fmt.Fprintf(&b, "\n用 /amlang <语言> 设置，例如 /amlang %s", info.DefaultLang)
		return b.String(), nil
	}

	// 设置模式
	if err := client.SetLanguage(ctx, arg); err != nil {
		return fmt.Sprintf("❌ 设置失败：%v", err), nil
	}
	return fmt.Sprintf("✅ Apple Music 语言已设为 %s 并写回配置。", client.CurrentLanguage()), nil
}
