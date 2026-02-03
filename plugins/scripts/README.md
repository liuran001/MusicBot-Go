# 动态脚本插件

此目录用于存放 **动态脚本插件**（由 yaegi 解释执行），无需重新编译主程序。

如需将插件放在独立仓库，可将 `PluginScriptDir` 指向该仓库的 `scripts` 目录。
脚本目录的上层需存在 `go.mod`（加载器会向上查找最多 10 层）。

## 目录结构
```
plugins/scripts/<name>/
  main.go
```

`<name>` 必须与配置段 `[plugins.<name>]` 一致，并且 **Go package 名称也必须是 `<name>`**。

## 必须实现的函数
```go
// package <name>
func Init(cfg map[string]string) error
func Meta() map[string]interface{}
```

`Meta()` 返回结构示例：
```json
{
  "name": "Meting",
  "version": "0.1.0",
  "url": "https://github.com/liuran001/MusicBot-Meting-Plugin",
  "platforms": [
    {
      "name": "tencent",
      "capabilities": {
        "download": true,
        "search": true,
        "lyrics": true,
        "recognition": false,
        "hi_res": true
      },
      "supports_match_url": true,
      "supports_match_text": true
    }
  ]
}
```

`name/version/url` 会在 `/about` 中展示。

## 可选实现的函数
```go
func MatchURL(platform, url string) (map[string]interface{}, error)
func MatchText(platform, text string) (map[string]interface{}, error)

func Search(platform, query string, limit int) ([]map[string]interface{}, error)
func GetTrack(platform, id string) (map[string]interface{}, error)
func GetDownloadInfo(platform, id, quality string) (map[string]interface{}, error)
func GetLyrics(platform, id string) (map[string]interface{}, error)
func GetPlaylist(platform, id string) (map[string]interface{}, error)
```

返回结构需与 `bot/platform/types.go` 的 JSON 字段一致，例如：
- `Track`: `id`, `platform`, `title`, `artists`, `album`, `duration`, `cover_url`, `url`
- `DownloadInfo`: `url`, `format`, `bitrate`, `quality`, `headers`
- `Lyrics`: `plain`

## 错误返回
可返回带 `Code() string` 方法的 error，Code 取值：
`not_found | unavailable | unsupported | rate_limited | auth_required | invalid`

主程序会将其映射为统一的 platform 错误。

## 重载
修改脚本后可通过 `/reload` 重载（仅 `BotAdmin` 配置的用户可用）。
不配置 `BotAdmin` 时需要重启程序生效。
