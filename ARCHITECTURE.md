# 架构说明

## 概览

MusicBot-Go 采用插件化架构设计，支持多音乐平台扩展。核心采用分层架构，将 Telegram 传输层、音乐平台 API 调用和数据持久化分离。

## 目录结构

```
MusicBot-Go/
├── main.go                      # 应用程序入口
├── bot/                         # 核心代码
│   ├── app/                     # 应用初始化和依赖注入
│   ├── config/                  # 配置管理 (Viper + INI)
│   ├── db/                      # 数据库层 (SQLite/GORM)
│   │   ├── models.go            # 数据模型定义
│   │   └── repository.go        # 数据访问接口实现
│   ├── logger/                  # 日志系统 (slog)
│   ├── dynplugin/               # 动态脚本插件加载 (yaegi)
│   ├── platform/                # 平台抽象层
│   │   ├── interface.go         # Platform 核心接口定义
│   │   ├── manager.go           # 平台管理器 (路由和调度)
│   │   ├── registry/            # 平台注册中心
│   │   ├── plugins/             # 插件注册表 (Contribution)
│   │   ├── types.go             # 通用类型定义
│   │   └── quality.go           # 音质处理
│   ├── recognize/               # 识曲服务抽象
│   ├── telegram/                # Telegram Bot 集成
│   │   ├── bot.go               # Bot 实例创建
│   │   └── handler/             # 命令处理器
│   │       ├── music.go         # 音乐下载/发送核心流程 (/music + 关键词回退)
│   │       ├── search.go        # 搜索处理
│   │       ├── lyric.go         # 歌词获取
│   │       ├── settings.go      # 用户设置
│   │       ├── recognize.go     # 语音识曲
│   │       └── router.go        # 路由注册
│   ├── worker/                  # 并发工作池
│   ├── updater/                 # 动态更新抽象 (未来扩展)
│   ├── interfaces.go            # 全局接口定义
│   └── types.go                 # 全局类型定义
└── plugins/                     # 平台插件
    ├── all/                     # 插件聚合 (空白导入)
    ├── scripts/                 # 动态脚本插件 (PluginScriptDir)
    └── netease/                 # 网易云音乐插件
        ├── client.go            # API 客户端
        ├── platform.go          # Platform 接口实现
        ├── matcher.go           # URL 匹配器
        ├── textmatcher.go       # 文本匹配器 (短链/纯 ID)
        ├── recognizer.go        # 识曲适配器
        ├── register.go          # 插件注册
        └── *_test.go            # 单元测试
```

## 核心流程

### 1. 应用启动流程

```
main.go
  └─> app.New()                  # 创建应用实例
       └─> app.Start()            # 启动应用
            ├─> 加载配置
            ├─> 初始化数据库
             ├─> 加载插件注册表并注册平台
             ├─> 加载动态脚本插件 (PluginScriptDir)
             ├─> 创建 Telegram Bot
             ├─> 注册命令处理器
             ├─> 启动识曲服务 (Node.js)
             └─> 启动 Bot 轮询
```

### 2. 命令处理流程 (以 /music 为例)

```
Telegram Update
  └─> Router
        └─> MusicHandler.Handle()
             ├─> 解析文本/URL/ID/关键词
             ├─> PlatformManager.MatchText()/MatchURL()  # 识别平台
             ├─> (若为关键词) 按默认平台搜索并回退到其他平台
             ├─> Platform.GetTrack()                     # 获取歌曲信息
             ├─> Repository.FindByPlatformTrackID()      # 检查缓存
             ├─> (缓存未命中)
             │    ├─> Platform.GetDownloadInfo()         # 获取下载信息
             │    ├─> DownloadService.Download()         # 下载歌曲
             │    ├─> 处理封面/元数据
             │    └─> Repository.Create()                # 保存缓存
             └─> Bot.SendAudio()                  # 发送给用户
```

### 3. 平台插件系统

```
Handler
  └─> PlatformManager
        ├─> Plugins Registry (init 注册工厂)
        ├─> Contribution (Platform / ID3 / Recognizer)
        ├─> 动态脚本插件 Meta (name/version/url)
       ├─> Registry.GetPlatform("netease")
       ├─> Registry.MatchText(text)/MatchURL(url)
       └─> Platform Interface
            ├─> GetTrack()
            ├─> GetDownloadInfo()
            ├─> Search()
            ├─> GetLyrics()
            └─> RecognizeAudio()
```

## 关键模块说明

### Platform 抽象层 (`bot/platform/`)

**核心接口**: `Platform`
```go
type Platform interface {
    Name() string

    SupportsDownload() bool
    SupportsSearch() bool
    SupportsLyrics() bool
    SupportsRecognition() bool
    Capabilities() Capabilities

    GetDownloadInfo(ctx context.Context, trackID string, quality Quality) (*DownloadInfo, error)
    Search(ctx context.Context, query string, limit int) ([]Track, error)
    GetLyrics(ctx context.Context, trackID string) (*Lyrics, error)
    RecognizeAudio(ctx context.Context, audioData io.Reader) (*Track, error)
    GetTrack(ctx context.Context, trackID string) (*Track, error)
    GetArtist(ctx context.Context, artistID string) (*Artist, error)
    GetAlbum(ctx context.Context, albumID string) (*Album, error)
    GetPlaylist(ctx context.Context, playlistID string) (*Playlist, error)
}
```

**能力导向设计**:
- 插件可选择性实现功能
- 不支持的功能返回 `ErrUnsupported`
- Handler 自动适配平台能力

**Manager 职责**:
- URL 路由到对应平台
- 平台切换和回退
- 统一错误处理

### 数据层 (`bot/db/`)

**核心接口**: `SongRepository`（由 `bot/db/Repository` 实现）
```go
type SongRepository interface {
    FindByMusicID(ctx context.Context, musicID int) (*SongInfo, error)
    FindByPlatformTrackID(ctx context.Context, platform, trackID, quality string) (*SongInfo, error)
    FindByFileID(ctx context.Context, fileID string) (*SongInfo, error)
    Create(ctx context.Context, song *SongInfo) error
    Update(ctx context.Context, song *SongInfo) error
    Delete(ctx context.Context, musicID int) error
    DeleteAll(ctx context.Context) error
    DeleteByPlatformTrackID(ctx context.Context, platform, trackID, quality string) error
    DeleteAllQualitiesByPlatformTrackID(ctx context.Context, platform, trackID string) error
    Count(ctx context.Context) (int64, error)
    CountByUserID(ctx context.Context, userID int64) (int64, error)
    CountByChatID(ctx context.Context, chatID int64) (int64, error)
    CountByPlatform(ctx context.Context) (map[string]int64, error)
    GetSendCount(ctx context.Context) (int64, error)
    IncrementSendCount(ctx context.Context) error
    Last(ctx context.Context) (*SongInfo, error)
    GetUserSettings(ctx context.Context, userID int64) (*UserSettings, error)
    UpdateUserSettings(ctx context.Context, settings *UserSettings) error
    GetGroupSettings(ctx context.Context, chatID int64) (*GroupSettings, error)
    UpdateGroupSettings(ctx context.Context, settings *GroupSettings) error
}
```

**数据模型**:
- `SongInfoModel`: 歌曲缓存
- `UserSettingsModel`: 用户偏好设置 (默认平台/音质)
- `GroupSettingsModel`: 群聊偏好设置 (默认平台/音质)
- `BotStatModel`: 统计信息（发送次数等）

### Telegram 处理器 (`bot/telegram/handler/`)

**主要处理器**:
- `MusicHandler`: 音乐下载核心逻辑
- `SearchHandler`: 搜索功能 (使用用户默认平台)
- `LyricHandler`: 歌词获取
- `SettingsHandler`: 用户设置 (平台/音质偏好)
- `RecognizeHandler`: 语音识曲
- `StatusHandler`: 状态查询

**设计特点**:
- 每个处理器独立，职责单一
- 通过依赖注入获取 Repository 和 PlatformManager
- 统一错误处理和用户反馈

### 插件实现 (`plugins/netease/`)

**网易云音乐插件结构**:
- `client.go`: HTTP 客户端封装
  - 重试机制 (hashicorp/go-retryablehttp)
  - 熔断保护 (sony/gobreaker)
  - Cookie 管理
- `platform.go`: Platform 接口实现
  - 歌曲信息获取
  - 下载流管理
  - 搜索和歌词
- `matcher.go`: URL 识别
  - 支持多种 URL 格式
  - ID 提取

## 用户设置系统

### 功能
- 用户可设置默认音乐平台 (未来多平台时生效)
- 用户可设置默认音质 (standard/high/lossless/hires)
- 群聊可设置默认平台/音质（群聊优先级高于用户设置）

### 集成点
 - **SearchHandler**: 使用用户默认平台搜索
 - **MusicHandler**: 使用用户默认音质下载
 - **平台回退**: 搜索失败时自动切换到 `SearchFallbackPlatform`
 - **/music 关键词**: 未匹配链接/ID 时执行同样的回退搜索

### 数据库
```sql
CREATE TABLE user_settings (
    id INTEGER PRIMARY KEY,
    user_id INTEGER UNIQUE NOT NULL,
    default_platform TEXT DEFAULT 'netease',
    default_quality TEXT DEFAULT 'hires',
    created_at DATETIME,
    updated_at DATETIME
);
```

```sql
CREATE TABLE group_settings (
    id INTEGER PRIMARY KEY,
    chat_id INTEGER UNIQUE NOT NULL,
    default_platform TEXT DEFAULT 'netease',
    default_quality TEXT DEFAULT 'hires',
    created_at DATETIME,
    updated_at DATETIME
);
```

## 设计原则

### 1. **插件化优先**
- 新平台通过插件方式添加，无需修改核心代码
- 平台能力自声明，Handler 自动适配

### 2. **分层清晰**
- Transport 层 (Telegram) 不直接依赖平台实现
- Platform 层不感知 Telegram 细节
- 数据层独立于业务逻辑

### 3. **容错设计**
- API 调用重试 + 熔断
- 平台回退机制
- 缓存优先，减少 API 压力

### 4. **可测试性**
- 接口驱动设计
- 依赖注入
- 每层独立测试

## 扩展指南

### 添加新平台插件

1. **创建插件目录**: `plugins/<platform>/`
2. **实现 Platform 接口**: 参考 `plugins/netease/platform.go`
3. **实现 URLMatcher** (可选): 用于 URL 识别
4. **注册插件**: 在插件包内注册工厂，并在 `plugins/all` 添加空白导入

详见 `PLUGIN_GUIDE.md`。

### 添加新命令

1. 在 `bot/telegram/handler/` 创建新处理器
2. 实现处理逻辑
3. 在 `router.go` 注册命令
4. (可选) 在 `app.go` 的 `SetMyCommands()` 添加命令描述

## 技术栈

- **语言**: Go 1.25.6+
- **Telegram SDK**: github.com/mymmrac/telego
- **数据库**: SQLite (github.com/glebarez/sqlite + GORM)
- **配置**: Viper + INI
- **日志**: slog
- **HTTP 客户端**: hashicorp/go-retryablehttp
- **熔断器**: sony/gobreaker
- **网易云 API**: github.com/XiaoMengXinX/Music163Api-Go

## 注意事项

- **模块路径**: `github.com/liuran001/MusicBot-Go`
- **主分支**: `main`
- **原始项目**: [XiaoMengXinX/Music163bot-Go](https://github.com/XiaoMengXinX/Music163bot-Go)
