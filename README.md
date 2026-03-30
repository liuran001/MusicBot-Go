# MusicBot-Go

一个支持多平台音乐下载/分享的 Telegram Bot。

**当前支持平台**:
- 网易云音乐
- QQ音乐
- 哔哩哔哩
- 酷狗音乐

> **原始项目**: [XiaoMengXinX/Music163bot-Go](https://github.com/XiaoMengXinX/Music163bot-Go)  
> 本项目基于原项目进行了重构，采用插件化架构以支持多音乐平台。

## 功能特性

- 🎵 **多平台支持**: 插件化架构,轻松扩展新音乐平台
- 🔍 **搜索**: 支持关键词搜索和直接 URL 下载
- 📝 **歌词**: 获取时间戳歌词或纯文本歌词
- 🎤 **识曲**: 语音识别功能 (需要 ffmpeg + Node.js)
- 💾 **智能缓存**: 数据库缓存,避免重复下载
- ⚡ **高性能**: 并发下载,限流保护
- 🔌 **插件系统**: 第三方开发者可独立开发平台插件

## 依赖

- Go 1.26.0+
- ffmpeg（/recognize 语音识别需要，可通过 `EnableRecognize = false` 关闭）
- Node.js + npm（识曲服务需要，可通过 `EnableRecognize = false` 关闭）

## Docker 部署

项目现在提供：

- `Dockerfile`
- `docker-compose.yml`

GitHub Actions 会自动构建并推送镜像到 GHCR：

- `ghcr.io/liuran001/musicbot-go:latest`（**full 完整版**，默认分支；包含 `/recognize` 需要的 Node.js + ffmpeg）
- `ghcr.io/liuran001/musicbot-go:lite`（**lite 精简版**，不包含 `/recognize` 依赖）
- `ghcr.io/liuran001/musicbot-go:<tag>`（full，打 tag 时）
- `ghcr.io/liuran001/musicbot-go:<tag>-lite`（lite，打 tag 时）
- `ghcr.io/liuran001/musicbot-go:sha-xxxxxxx` / `sha-xxxxxxx-lite`

如果你不需要 `/recognize`，推荐直接使用 `lite`，并在配置中显式设置：

```ini
EnableRecognize = false
```

### 1. 准备目录

推荐只挂载一个目录，例如：`docker-data/`

```bash
mkdir -p docker-data/plugins/scripts
cp config_example.ini docker-data/config.ini
```

程序工作目录、数据库、缓存、动态脚本都可以放在这个目录里。

建议同时把以下路径改成工作目录内的相对路径：

```ini
Database = data/cache.db
DataDatabase = data/data.db
CacheDir = cache
PluginScriptDir = plugins/scripts
```

### 2. 准备配置

编辑：

```bash
nano docker-data/config.ini
```

至少填写：

- `BOT_TOKEN`
- 各平台 Cookie / 会话配置

如果不需要识曲，可以在配置中关闭：

```ini
EnableRecognize = false
```

### 3. 使用 docker compose 启动

```bash
docker compose up -d --build
```

默认只挂载一个目录：

- `./docker-data -> /app/workdir`

### 4. 纯 docker 启动

```bash
docker build -t musicbot-go .
docker run -d \
  --name musicbot-go \
  --restart unless-stopped \
  -w /app/workdir \
  -v $(pwd)/docker-data:/app/workdir \
  -e TZ=Asia/Shanghai \
  musicbot-go -c /app/workdir/config.ini
```

### 5. 直接使用 GHCR 镜像

如果你不想本地构建，可以直接拉 GitHub Container Registry 镜像：

```bash
docker pull ghcr.io/liuran001/musicbot-go:latest
docker run -d \
  --name musicbot-go \
  --restart unless-stopped \
  -w /app/workdir \
  -v $(pwd)/docker-data:/app/workdir \
  -e TZ=Asia/Shanghai \
  ghcr.io/liuran001/musicbot-go:latest \
  -c /app/workdir/config.ini
```

也可以用单独的 compose 文件直接跑 GHCR 镜像：

```yaml
services:
  musicbot-go:
    image: ghcr.io/liuran001/musicbot-go:latest
    container_name: musicbot-go
    restart: unless-stopped
    working_dir: /app/workdir
    command: ["-c", "/app/workdir/config.ini"]
    volumes:
      - ./docker-data:/app/workdir
    environment:
      TZ: Asia/Shanghai
```

### 6. Docker 说明

- 镜像内已包含 `ffmpeg`
- 镜像内已安装识曲服务所需 Node.js 依赖
- 若你不使用 `/recognize`，依然建议在配置中显式关闭它，以减少无意义的运行开销

## 配置

复制 `config_example.ini` 为 `config.ini`:

### 基础配置
```ini
# Telegram Bot Token (必填)
BOT_TOKEN = YOUR_BOT_TOKEN

```

### 平台配置

#### 网易云音乐
```ini
[plugins.netease]
# MUSIC_U Cookie (用于下载无损音质)
music_u = YOUR_MUSIC_U_COOKIE
```

#### QQ音乐
```ini
[plugins.qqmusic]
# QQ音乐 Cookie (用于高音质/Hi-Res)
cookie = YOUR_QQMUSIC_COOKIE
```

#### 酷狗音乐
```ini
[plugins.kugou]
# 概念版会话会在扫码登录后自动写回以下 concept_* 字段
concept_auto_refresh_enabled = false
concept_auto_refresh_interval_sec = 21600
```

酷狗下载当前统一走概念版会话链路，管理员可使用：

- `/kgqr` 生成二维码登录
- `/kgstatus` 查看概念版会话状态
- `/kgsign` 尝试签到 / 领 VIP
- `/renewck kugou` 手动续期

详见 `config_example.ini` 获取完整配置选项。

### 动态脚本插件（推荐）
- 脚本目录：`plugins/scripts/<name>`，目录名需与 `[plugins.<name>]` 一致
- 修改脚本或配置后，管理员可使用 `/reload` 重载（需配置 `BotAdmin`），或直接重启程序

### 白名单配置（可选）

用于限制可使用 Bot 的聊天（chat）。开启后，非白名单群会被自动退出，私聊会被静默忽略；`BotAdmin` 始终可绕过白名单。

```ini
EnableWhitelist = false
WhitelistChatIDs =
```

`WhitelistChatIDs` 支持逗号/分号/空格分隔多个 chatID。

管理员可使用 `/wl` 动态维护白名单（会回写 `config.ini`）：
- `/wl add <chatID>`
- `/wl del <chatID>`
- `/wl list`

## 运行

```bash
go build -o MusicBot-Go
./MusicBot-Go -c config.ini
```

## 使用方法

### 听歌识曲启动

1) 安装 Node.js，并进入识曲服务目录安装依赖

```bash
cd plugins/netease/recognize/service
npm install
```

2) 确保机器已安装 `ffmpeg`

3) 可选：在配置里设置识曲服务端口（默认 3737）

```ini
RecognizePort = 3737
```

4) 重启 Bot，识曲服务会自动启动（启动失败会提示缺少 node_modules）

### 基本命令

- `/music <URL>` - 下载音乐 (支持多平台 URL)
- `/search <关键词>` - 搜索音乐
- `/lyric <URL>` - 获取歌词
- `/recognize` - 识别语音中的歌曲 (回复语音消息)
- `/settings` - 设置默认平台和音质
- `/status` - 查看 Bot 状态和支持的平台
- `/about` - 关于本 Bot
- `/rmcache <platform> <trackID>|<URL>|all` - 清理缓存（管理员）
- `/reload` - 重新加载动态脚本插件（管理员）
- `/wl <add|del|list> [chatID]` - 白名单管理（管理员）

### 支持的 URL 格式

**网易云音乐**:
- `https://music.163.com/song?id=12345`
- `https://music.163.com/#/song?id=12345`
- `https://y.music.163.com/m/song?id=12345` (移动端)
- `https://music.163.com/#/album?id=3411281`
- `https://music.163.com/#/playlist?id=19723756`

**QQ音乐**:
- `https://y.qq.com/n/ryqq_v2/songDetail/003IGhQO0JdnuC`
- `https://y.qq.com/n/ryqq_v2/playlist/114514`
- `https://y.qq.com/n/ryqq_v2/albumDetail/003MNOTS3FmvaO`
- `https://c6.y.qq.com/base/fcgi-bin/u?__=xxxxxx` (短链, 自动解析)

**酷狗音乐**:
- `https://www.kugou.com/song/#hash=...&album_id=...`
- `https://www.kugou.com/share/...`
- `https://www.kugou.com/album/123456.html`
- `https://www.kugou.com/songlist/gcid_xxxxx/`
- `https://www.kugou.com/share/zlist.html?global_collection_id=...`

> 专辑/歌单链接会进入分页列表模式；简介默认以 Telegram 可折叠引用样式展示。

### 分页配置

搜索与歌单共用单页展示条数：
```ini
ListPageSize = 8
```

### 使用示例

1. **URL 下载**: 直接发送音乐链接到 Bot
2. **搜索下载**: `/search 周杰伦 晴天`
3. **获取歌词**: `/lyric https://music.163.com/song?id=12345`

## 开发

### 构建
```bash
go build -o MusicBot-Go
```

### 开发插件

想要为新的音乐平台开发插件? 可选两种方式：

1) 静态插件（需要重新编译）
- 参阅 `plugins/README.md`

1) 动态脚本插件（无需重新编译）
- 将插件源码放在 `plugins/scripts/<name>`
- 在 `config.ini` 添加 `[plugins.<name>]` 配置
- 可通过 `PluginScriptDir` 修改脚本目录（默认 `./plugins/scripts`）
- 修改脚本后管理员可用 `/reload` 重载（需配置 `BotAdmin`）

动态脚本插件最小入口：
```go
// package <name>
func Init(cfg map[string]string) error
func Meta() map[string]interface{}
```
可选实现：`Search/GetTrack/GetDownloadInfo/GetLyrics/GetPlaylist/MatchURL/MatchText`

### 架构

详见 `ARCHITECTURE.md` 了解项目架构设计。

## 许可证

GPL-3.0
