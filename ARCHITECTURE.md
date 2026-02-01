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
│   ├── platform/                # 平台抽象层
│   │   ├── interface.go         # Platform 核心接口定义
│   │   ├── manager.go           # 平台管理器 (路由和调度)
│   │   ├── registry/            # 平台注册中心
│   │   ├── types.go             # 通用类型定义
│   │   └── quality.go           # 音质处理
│   ├── telegram/                # Telegram Bot 集成
│   │   ├── bot.go               # Bot 实例创建
│   │   └── handler/             # 命令处理器
│   │       ├── music.go         # 音乐下载/发送核心流程
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
    └── netease/                 # 网易云音乐插件
        ├── client.go            # API 客户端 (重试 + 熔断)
        ├── platform.go          # Platform 接口实现
        ├── matcher.go           # URL 匹配器
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
            ├─> 注册平台插件
            ├─> 创建 Telegram Bot
            ├─> 注册命令处理器
            └─> 启动 Bot 轮询
```

### 2. 命令处理流程 (以 /music 为例)

```
Telegram Update
  └─> Router
       └─> MusicHandler.Handle()
            ├─> 解析 URL/ID
            ├─> PlatformManager.MatchURL()       # 识别平台
            ├─> Platform.GetSongInfo()           # 获取歌曲信息
            ├─> Repository.FindByMusicID()       # 检查缓存
            ├─> (缓存未命中)
            │    ├─> Platform.DownloadSong()    # 下载歌曲
            │    ├─> 处理封面/元数据
            │    └─> Repository.Save()          # 保存缓存
            └─> Bot.SendAudio()                  # 发送给用户
```

### 3. 平台插件系统

```
Handler
  └─> PlatformManager
       ├─> Registry.GetPlatform("netease")
       ├─> Registry.MatchURL(url)
       └─> Platform Interface
            ├─> GetSongInfo()
            ├─> DownloadSong()
            ├─> SearchSongs()
            ├─> GetLyrics()
            └─> RecognizeSong()
```

## 关键模块说明

### Platform 抽象层 (`bot/platform/`)

**核心接口**: `Platform`
```go
type Platform interface {
    Name() string
    DisplayName() string
    Emoji() string
    
    // 能力声明
    SupportsDownload() bool
    SupportsSearch() bool
    SupportsLyrics() bool
    SupportsRecognition() bool
    
    // 核心功能
    GetSongInfo(ctx, id string, quality Quality) (*SongInfo, error)
    DownloadSong(ctx, id string, quality Quality) (io.ReadCloser, *SongInfo, error)
    SearchSongs(ctx, keyword string, limit int) ([]SearchResult, error)
    GetLyrics(ctx, id string) (*Lyrics, error)
    RecognizeSong(ctx, audioData []byte) (*RecognitionResult, error)
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

**核心接口**: `SongRepository`
```go
type SongRepository interface {
    FindByMusicID(ctx, musicID int) (*SongInfo, error)
    FindByPlatformAndID(ctx, platform, id string) (*SongInfo, error)
    Save(ctx, info *SongInfo) error
    Delete(ctx, musicID int) error
    GetUserSettings(ctx, userID int64) (*UserSettings, error)
    UpdateUserSettings(ctx, settings *UserSettings) error
}
```

**数据模型**:
- `SongModel`: 歌曲缓存
- `UserSettingsModel`: 用户偏好设置 (默认平台/音质)

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

### 集成点
- **SearchHandler**: 使用用户默认平台搜索
- **MusicHandler**: 使用用户默认音质下载
- **平台回退**: 搜索失败时自动切换到网易云 (仅关键词搜索)

### 数据库
```sql
CREATE TABLE user_settings (
    id INTEGER PRIMARY KEY,
    user_id INTEGER UNIQUE NOT NULL,
    default_platform TEXT DEFAULT 'netease',
    default_quality TEXT DEFAULT 'high',
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
4. **注册插件**: 在 `bot/app/app.go` 中注册

详见 `PLUGIN_GUIDE.md`。

### 添加新命令

1. 在 `bot/telegram/handler/` 创建新处理器
2. 实现处理逻辑
3. 在 `router.go` 注册命令
4. (可选) 在 `app.go` 的 `SetMyCommands()` 添加命令描述

## 技术栈

- **语言**: Go 1.23+
- **Telegram SDK**: github.com/go-telegram/bot
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
