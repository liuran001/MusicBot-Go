# 插件开发指南

本指南帮助第三方开发者为 MusicBot-Go 开发新的音乐平台插件。
如果你希望编写动态脚本插件，请参考 `plugins/scripts/README.md`。

## 概述

MusicBot-Go 使用基于接口的插件系统，允许轻松扩展对不同音乐平台的支持。

### 插件系统架构

1. **Platform 接口**: 定义音乐平台的核心功能（下载、搜索、歌词等）。
2. **Registry**: 管理已注册的平台插件。
3. **Manager**: 提供高级 API 供 Bot 使用，负责路由请求到正确的平台。
4. **Handlers**: Bot 处理程序通过 Manager 自动与各个平台交互。

### 能力导向设计

插件采用能力导向设计，开发者可以选择性实现功能：
- `SupportsDownload()` - 是否支持下载
- `SupportsSearch()` - 是否支持搜索
- `SupportsLyrics()` - 是否支持歌词
- `SupportsRecognition()` - 是否支持识曲

对于不支持的功能，方法应返回 `platform.ErrUnsupported`。

---

## 快速开始

### 前置要求

- Go 1.25.6+
- 熟悉目标音乐平台的 API
- 了解 Go 接口和错误处理

### 创建插件的 5 个步骤

1. **创建包目录**: `plugins/<platform_name>/`
2. **实现 `Platform` 接口**: 在该目录下创建 `platform.go`。
3. **实现 `URLMatcher`/`TextMatcher` 接口** (可选但推荐): 允许 Bot 识别该平台的 URL 或短链/纯 ID 文本。
4. **编写测试**: 确保插件逻辑正确。
5. **注册插件**: 在插件包内通过工厂注册，并在 `plugins/all` 中进行空白导入。

### 最小可行插件

```go
package spotify

import (
    "context"
    "io"

    "github.com/liuran001/MusicBot-Go/bot/platform"
)

type SpotifyPlatform struct{}

func (p *SpotifyPlatform) Name() string {
    return "spotify"
}

func (p *SpotifyPlatform) SupportsDownload() bool    { return false }
func (p *SpotifyPlatform) SupportsSearch() bool      { return false }
func (p *SpotifyPlatform) SupportsLyrics() bool      { return false }
func (p *SpotifyPlatform) SupportsRecognition() bool { return false }

func (p *SpotifyPlatform) Capabilities() platform.Capabilities {
    return platform.Capabilities{}
}

func (p *SpotifyPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
    return nil, platform.ErrUnsupported
}

// 其他方法按需实现或返回 ErrUnsupported
```

---

## 接口详解

### Platform 接口

完整定义见 `bot/platform/interface.go`。

#### 必需方法

**Name() string**
- 返回平台唯一标识符 (小写，如 "spotify", "qqmusic")。
- 用于 URL 路由、缓存键和日志。

**能力检查方法**
- `SupportsDownload() bool`
- `SupportsSearch() bool`
- `SupportsLyrics() bool`
- `SupportsRecognition() bool`
- `Capabilities() Capabilities`

**GetDownloadInfo(ctx context.Context, trackID string, quality Quality) (*DownloadInfo, error)**
- 获取下载信息（URL、大小、格式、码率等）。
- `trackID`: 平台特定的曲目 ID。
- `quality`: 请求的音质 (standard/high/lossless/hires)。
- **注意**: 即使不支持请求的音质，也应返回最佳可用音质。

**Search(ctx context.Context, query string, limit int) ([]Track, error)**
- 搜索曲目。
- 返回: `Track` 切片，最多 `limit` 个结果。

**GetLyrics(ctx context.Context, trackID string) (*Lyrics, error)**
- 获取歌词。
- 返回: `Lyrics` 结构（支持纯文本和带时间戳的歌词）。

**GetTrack(ctx context.Context, trackID string) (*Track, error)**
- 获取曲目详情（标题、艺术家、专辑封面等）。

**GetArtist/GetAlbum/GetPlaylist**
- 获取艺术家/专辑/歌单详情。
- 如暂不支持，返回 `platform.ErrUnsupported`。

**RecognizeAudio(ctx context.Context, audioData io.Reader) (*Track, error)**
- 听歌识曲。
- 接收原始音频流，返回识别到的曲目。

#### 可选接口
- `URLMatcher`: 解析平台 URL。
- `TextMatcher`: 解析短链/纯 ID 文本（例如分享短链）。

---

## 实现示例: Spotify 插件

以下是一个简化的 Spotify 插件实现示例。

### 文件结构
```
plugins/spotify/
├── platform.go    # 主实现
├── matcher.go     # URL 匹配
├── types.go       # 类型转换辅助
├── register.go    # 插件注册
└── platform_test.go
```

### platform.go

```go
package spotify

import (
    "context"
    "io"
    "fmt"
    
    "github.com/liuran001/MusicBot-Go/bot/platform"
    "github.com/zmb3/spotify/v2"
)

type SpotifyPlatform struct {
    client *spotify.Client
}

func New(client *spotify.Client) *SpotifyPlatform {
    return &SpotifyPlatform{client: client}
}

func (p *SpotifyPlatform) Name() string {
    return "spotify"
}

func (p *SpotifyPlatform) SupportsDownload() bool {
    return false // Spotify API 不支持直接下载音频流
}

func (p *SpotifyPlatform) SupportsSearch() bool {
    return true
}

func (p *SpotifyPlatform) SupportsLyrics() bool {
    return false
}

func (p *SpotifyPlatform) SupportsRecognition() bool {
    return false
}

func (p *SpotifyPlatform) Capabilities() platform.Capabilities {
    return platform.Capabilities{Search: true}
}

func (p *SpotifyPlatform) GetDownloadInfo(ctx context.Context, trackID string, quality platform.Quality) (*platform.DownloadInfo, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) Search(ctx context.Context, query string, limit int) ([]platform.Track, error) {
    results, err := p.client.Search(ctx, query, spotify.SearchTypeTrack, spotify.Limit(limit))
    if err != nil {
        return nil, fmt.Errorf("spotify search: %w", err)
    }
    
    var tracks []platform.Track
    for _, item := range results.Tracks.Tracks {
        tracks = append(tracks, p.convertTrack(&item))
    }
    return tracks, nil
}

func (p *SpotifyPlatform) GetTrack(ctx context.Context, trackID string) (*platform.Track, error) {
    track, err := p.client.GetTrack(ctx, spotify.ID(trackID))
    if err != nil {
        return nil, platform.NewNotFoundError("spotify", "track", trackID)
    }
    res := p.convertTrack(track)
    return &res, nil
}

func (p *SpotifyPlatform) GetLyrics(ctx context.Context, trackID string) (*platform.Lyrics, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) RecognizeAudio(ctx context.Context, audioData io.Reader) (*platform.Track, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) GetArtist(ctx context.Context, artistID string) (*platform.Artist, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) GetAlbum(ctx context.Context, albumID string) (*platform.Album, error) {
    return nil, platform.ErrUnsupported
}

func (p *SpotifyPlatform) GetPlaylist(ctx context.Context, playlistID string) (*platform.Playlist, error) {
    return nil, platform.ErrUnsupported
}

// 其他方法实现...
```

### 类型转换 (types.go)

```go
func (p *SpotifyPlatform) convertTrack(st *spotify.FullTrack) platform.Track {
    artists := make([]platform.Artist, len(st.Artists))
    for i, a := range st.Artists {
        artists[i] = platform.Artist{
            ID:       string(a.ID),
            Name:     a.Name,
            Platform: "spotify",
        }
    }
    
    return platform.Track{
        ID:       string(st.ID),
        Platform: "spotify",
        Title:    st.Name,
        Artists:  artists,
        Duration: st.TimeDuration(),
        CoverURL: st.Album.Images[0].URL,
    }
}
```

---

## URL / 文本匹配

实现 `URLMatcher` 接口允许 Bot 自动识别并处理特定平台的链接；实现 `TextMatcher` 可处理短链/纯 ID 等文本输入。

### matcher.go

```go
package spotify

import (
    "net/url"
    "strings"
)

type URLMatcher struct{}

func (m *URLMatcher) MatchURL(rawURL string) (string, bool) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return "", false
    }
    
    // 匹配 open.spotify.com/track/xxx
    if !strings.Contains(u.Host, "spotify.com") {
        return "", false
    }
    
    parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
    if len(parts) >= 2 && parts[0] == "track" {
        return parts[1], true
    }
    
    return "", false
}
```

### TextMatcher (可选)

```go
func (p *SpotifyPlatform) MatchText(text string) (string, bool) {
    // 解析短链或纯 ID，返回 trackID
    return "", false
}
```

### 集成到 Platform

```go
// 在 platform.go 中
func (p *SpotifyPlatform) MatchURL(url string) (string, bool) {
    return (&URLMatcher{}).MatchURL(url)
}
```

---

## 错误处理

请使用 `bot/platform/errors.go` 中定义的统一错误处理机制。

- **资源未找到**: `platform.NewNotFoundError(platform, resource, id)`
- **速率限制**: `platform.NewRateLimitedError(platform)`
- **内容不可用**: `platform.NewUnavailableError(platform, resource, id)`
- **功能不支持**: `platform.NewUnsupportedError(platform, feature)`

示例：
```go
if err == api.ErrNotFound {
    return nil, platform.NewNotFoundError("myplatform", "track", trackID)
}
```

---

## 测试

建议为插件编写单元测试，特别是 URL 匹配和类型转换逻辑。

```go
func TestURLMatcher(t *testing.T) {
    matcher := &URLMatcher{}
    tests := []struct {
        url    string
        wantID string
        ok     bool
    }{
        {"https://open.spotify.com/track/4cOdK2wG6ZIB9s99v9p9p9", "4cOdK2wG6ZIB9s99v9p9p9", true},
        {"https://music.163.com/song?id=123", "", false},
    }
    
    for _, tt := range tests {
        id, ok := matcher.MatchURL(tt.url)
        if ok != tt.ok || id != tt.wantID {
            t.Errorf("MatchURL(%s) = (%s, %v), want (%s, %v)", tt.url, id, ok, tt.wantID, tt.ok)
        }
    }
}
```

---

## 集成

### 1. 注册插件

在插件包内注册工厂，并在 `plugins/all` 中添加空白导入。示例：

```go
// plugins/spotify/register.go
package spotify

import (
    "github.com/liuran001/MusicBot-Go/bot/config"
    logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
    platformplugins "github.com/liuran001/MusicBot-Go/bot/platform/plugins"
)

func init() {
    if err := platformplugins.Register("spotify", buildContribution); err != nil {
        panic(err)
    }
}

func buildContribution(cfg *config.Config, logger *logpkg.Logger) (*platformplugins.Contribution, error) {
    client := NewClient(cfg.GetString("SPOTIFY_ID"), cfg.GetString("SPOTIFY_SECRET"))
    platform := NewPlatform(client)
    return &platformplugins.Contribution{Platform: platform}, nil
}
```

> `Contribution` 还可选提供 `ID3` 标签提供器与 `Recognizer` 识曲服务。

```go
// plugins/all/all.go
package all

import (
    _ "github.com/liuran001/MusicBot-Go/plugins/spotify"
)
```

### 2. 添加配置

在 `config.ini` 中添加插件所需的配置项，并在 `bot/config/config.go` 中确保它们能被正确读取。

---

## 最佳实践

1. **并发安全**: `Platform` 实例会被多个 goroutine 并发调用，请确保实现是线程安全的。
2. **Context 尊重**: 始终将 `context.Context` 传递给底层网络请求，并尊重其取消信号。
3. **音质映射**: 将平台特有的音质定义映射到 `platform.Quality` 枚举。
4. **日志记录**: 使用项目统一的日志组件记录关键操作和非预期错误。
5. **优雅降级**: 如果某个功能（如歌词）获取失败，不应影响主流程，应返回清晰的错误。

---

## FAQ

**Q: 我的平台不支持下载，只能搜索，可以吗？**
A: 完全可以。只需在 `SupportsDownload()` 返回 `false`，并在 `GetDownloadInfo()` 中返回 `platform.ErrUnsupported`。

**Q: 如何处理 API Token 过期？**
A: 建议在插件内部实现 Token 自动刷新机制，对外部调用者透明。

**Q: 插件需要依赖外部二进制文件（如 ffmpeg）怎么办？**
A: 请在文档中注明，并在插件初始化时检查依赖是否存在。
