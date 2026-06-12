# MusicBot-Go

多平台音乐下载 / 分享的 Telegram Bot。发链接或搜索即可下载音乐、歌词与封面，带缓存、限流和插件化扩展。

> 基于 [XiaoMengXinX/Music163bot-Go](https://github.com/XiaoMengXinX/Music163bot-Go) 重构，改为插件化架构以支持多平台。许可证 GPL-3.0。

## 支持平台

| 平台 | 下载 | 搜索 | 歌词 | Hi-Res / 无损 | 识曲 |
|------|:--:|:--:|:--:|:--:|:--:|
| 网易云音乐 | ✓ | ✓ | ✓ | ✓ | ✓ |
| QQ 音乐 | ✓ | ✓ | ✓ | ✓ | — |
| 酷狗音乐 | ✓ | ✓ | ✓ | ✓ | — |
| 汽水音乐 | ✓ | ✓ | ✓ | ✓ | — |
| 哔哩哔哩 | ✓ | ✓ | ✓ | — | — |
| Apple Music | ✓ | ✓ | ✓ | ✓ ¹ | — |

¹ Apple Music 的 AAC 256k 开箱即用；无损 / Hi-Res / Atmos 需额外的解密服务，见 [Apple Music 无损](#apple-music-无损hi-resatmos)。

## 快速开始

### Docker（推荐）

镜像由 CI 自动构建并推送到 GHCR，两个版本：

- `ghcr.io/liuran001/musicbot-go:latest` —— **完整版**，含 ffmpeg + Node.js，支持 `/recognize` 听歌识曲。
- `ghcr.io/liuran001/musicbot-go:lite` —— **精简版**，不含识曲依赖，体积更小。

所有运行数据（配置、数据库、缓存、脚本）放在一个挂载目录里：

```bash
mkdir -p docker-data
cp config_example.ini docker-data/config.ini
# 编辑 docker-data/config.ini，至少填 BOT_TOKEN

docker run -d --name musicbot-go --restart unless-stopped \
  -w /app/workdir -v "$(pwd)/docker-data:/app/workdir" \
  -e TZ=Asia/Shanghai \
  ghcr.io/liuran001/musicbot-go:latest -c /app/workdir/config.ini
```

或用仓库自带的 `docker-compose.yml`（本地构建 full 版）：

```bash
docker compose up -d --build
```

> 用精简版（不需要识曲）时，建议在配置里显式 `EnableRecognize = false`。

### 裸机运行

需要 Go 1.26+；用 `/recognize` 还需 ffmpeg 与 Node.js。

```bash
go build -o MusicBot-Go
./MusicBot-Go -c config.ini
```

## 配置

复制 `config_example.ini` 为 `config.ini`，按注释填写。最少只需一个 Bot Token：

```ini
BOT_TOKEN = YOUR_BOT_TOKEN   # 必填
BotAdmin  = 123456789        # 管理员 Telegram ID（逗号分隔），管理命令需要
```

各平台凭证写在对应的 `[plugins.<name>]` 段，例如：

```ini
[plugins.netease]
music_u = YOUR_MUSIC_U_COOKIE      # 网易云无损需要

[plugins.qqmusic]
cookie = YOUR_QQMUSIC_COOKIE       # 高音质 / Hi-Res 需要

[plugins.applemusic]
media_user_token = YOUR_TOKEN      # 登录 music.apple.com 后从浏览器 Cookie 复制
```

完整选项（并发、缓存、限流、代理、日志、各平台细节等）见 `config_example.ini` 的注释，每一项都有说明。

> 多数平台账号也可以不写进配置，改用管理员命令 `/login <平台> cookie <cookie>` 在运行时导入（会回写 `config.ini`）。

## 命令

**通用命令**

| 命令 | 说明 |
|------|------|
| `/music <URL 或关键词>` | 下载音乐；直接发音乐链接也会自动识别下载 |
| `/search <关键词>` | 搜索并选择下载 |
| `/lyric <URL>` | 获取歌词 |
| `/recognize` | 回复一条语音消息识别歌曲（需完整版 + `EnableRecognize`） |
| `/settings` | 设置默认平台与音质（支持私聊 / 群聊维度） |
| `/status` | 查看统计与各平台账号状态 |
| `/about` · `/help` | 关于 / 帮助 |

也支持 Inline 模式（`@bot 关键词`）和直接粘贴链接。

**管理员命令**（需在 `BotAdmin` 中）

| 命令 | 说明 |
|------|------|
| `/login <平台> cookie <cookie>` | 导入平台 Cookie |
| `/login kugou qr` | 扫码登录酷狗概念版 |
| `/login <平台> check` · `/login check` | 检查单个 / 全部平台账号 |
| `/login <平台> renew` · `/login renew` | 手动续期 |
| `/login <平台> auto on\|off\|status [秒]` | 自动续期开关 |
| `/login applemusic lang [语言]` | 查看 / 设置 Apple Music 元数据语言 |
| `/reload` | 重载配置与动态脚本插件 |
| `/rmcache <平台>\|all` | 清除缓存 |
| `/wl add\|del\|list [chatID]` | 白名单管理（需 `EnableWhitelist = true`） |

## Apple Music 无损（Hi-Res/Atmos）

Apple Music 的解密分两档：

- **AAC 256k —— 开箱即用。** 插件内置原生 Go Widevine 解密，填好 `media_user_token` 即可，无需任何额外服务。
- **无损 ALAC / Hi-Res 24bit / Dolby Atmos —— 需要外部 wrapper。** 这些音质走 FairPlay，Apple 不对 Widevine 放行，必须经
  [WorldObservationLog/wrapper](https://github.com/WorldObservationLog/wrapper) 解密。请求高于 AAC 的音质时若 wrapper 不可用，会自动回退到 AAC 256k。

启用无损（Docker）：

1. **构建 wrapper 镜像。** 上游不发布镜像，仓库提供了手动工作流：进入 GitHub → Actions → **Build Apple Music Wrapper Image** → Run，它会从上游 Release 取预编译二进制打包并推到 `ghcr.io/<你的用户名>/musicbot-wrapper`（仅 x86_64）。
2. **登录 wrapper。** 在 `docker-compose.yml` 的 `wrapper` 服务里填一个**有订阅的 Apple ID**（`USERNAME` / `PASSWORD`）。它模拟安卓客户端，登录是设备级的，**无法复用 bot 的 `media_user_token`**——两套独立凭证，都要有。首次启动会自动登录（含 2FA），会话持久化到挂载卷，之后可清空账密。
3. **指向 wrapper。** 在 `config.ini` 设 `wrapper_host = wrapper`（compose 服务名），`docker compose up -d`。

> 2FA：首次启动后看 wrapper 日志，出现 `Waiting for input...` 时把收到的 6 位验证码写入挂载目录的 `data/com.apple.android.music/files/2fa.txt`（60 秒内）。
>
> **裸机**：自行运行 wrapper（见其仓库），把 `wrapper_host` 指向它的地址（如 `127.0.0.1`，端口 10020/20020/30020）。

## 插件开发

两种方式：

- **动态脚本插件**（无需重新编译）：源码放 `plugins/scripts/<name>/`，在 `config.ini` 加 `[plugins.<name>]` 段，管理员 `/reload` 即可热加载。最小入口见 [`plugins/scripts/README.md`](plugins/scripts/README.md)。
- **静态插件**（编译进二进制，能力最全）：实现 `platform.Platform` 接口并注册，见 [`plugins/README.md`](plugins/README.md)。

架构设计见 [`ARCHITECTURE.md`](ARCHITECTURE.md)。
