# MusicBot-Go

一个支持多平台音乐下载/分享的 Telegram Bot。

**当前支持平台**:
- 网易云音乐 (NetEase Cloud Music)
- 更多平台即将到来...

> **原始项目**: [XiaoMengXinX/Music163bot-Go](https://github.com/XiaoMengXinX/Music163bot-Go)  
> 本项目基于原项目进行了重构，采用插件化架构以支持多音乐平台。

## 功能特性

- 🎵 **多平台支持**: 插件化架构,轻松扩展新音乐平台
- 🔍 **搜索**: 支持关键词搜索和直接 URL 下载
- 📝 **歌词**: 获取时间戳歌词或纯文本歌词
- 🎤 **识曲**: 语音识别功能 (需要 ffmpeg)
- 💾 **智能缓存**: 数据库缓存,避免重复下载
- ⚡ **高性能**: 并发下载,限流保护
- 🔌 **插件系统**: 第三方开发者可独立开发平台插件

## 依赖

- Go 1.23+
- ffmpeg（/recognize 语音识别需要）

## 配置

复制 `config_example.ini` 为 `config.ini`:

### 基础配置
```ini
# Telegram Bot Token (必填)
BOT_TOKEN = YOUR_BOT_TOKEN

# Bot 管理员 ID (可选)
BotAdmin = 123456,789012
```

### 平台配置

#### 网易云音乐
```ini
[plugins.netease]
# MUSIC_U Cookie (用于下载无损音质)
music_u = YOUR_MUSIC_U_COOKIE
```

详见 `config_example.ini` 获取完整配置选项。

## 运行

```bash
go build -o MusicBot-Go
./MusicBot-Go -c config.ini
```

## 使用方法

### 基本命令

- `/music <URL>` - 下载音乐 (支持多平台 URL)
- `/search <关键词>` - 搜索音乐
- `/lyric <URL>` - 获取歌词
- `/recognize` - 识别语音中的歌曲 (回复语音消息)
- `/status` - 查看 Bot 状态和支持的平台
- `/about` - 关于本 Bot

### 支持的 URL 格式

**网易云音乐**:
- `https://music.163.com/song?id=12345`
- `https://music.163.com/#/song?id=12345`
- `https://y.music.163.com/m/song?id=12345` (移动端)

**未来支持**: Spotify, YouTube Music, QQ Music 等

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

想要为新的音乐平台开发插件? 查看 `PLUGIN_GUIDE.md` 获取详细指南。

### 架构

详见 `ARCHITECTURE.md` 了解项目架构设计。

## 许可证

GPL-3.0
