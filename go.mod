module github.com/liuran001/MusicBot-Go

go 1.26.0

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/Eyevinn/mp4ff v0.48.0
	github.com/devgianlu/go-librespot v0.7.4
	github.com/glebarez/sqlite v1.11.0
	github.com/go-flac/go-flac v1.0.0
	github.com/guohuiyuan/music-lib v1.0.6-0.20260308165809-ea321c84b16a
	github.com/hashicorp/go-retryablehttp v0.7.8
	github.com/iyear/gowidevine v0.1.3
	github.com/mymmrac/telego v1.10.0
	github.com/nfnt/resize v0.0.0-20180221191011-83c6a9932646
	github.com/nicksnyder/go-i18n/v2 v2.6.1
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/sony/gobreaker v1.0.0
	github.com/spf13/viper v1.21.0
	github.com/stretchr/testify v1.11.1
	github.com/traefik/yaegi v0.16.1
	go.senan.xyz/taglib v0.11.1
	golang.org/x/oauth2 v0.34.0
	golang.org/x/sync v0.20.0
	golang.org/x/time v0.14.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/ini.v1 v1.67.1
	gorm.io/gorm v1.31.1
)

require (
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/devgianlu/shannon v0.0.0-20230613115856-82ec90b7fa7e // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/net v0.54.0 // indirect
)

require (
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.15.2 // indirect
	github.com/bytedance/sonic/loader v0.5.1 // indirect
	github.com/chmike/cmac-go v1.1.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/glebarez/go-sqlite v1.22.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grbit/go-json v0.11.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/jerbob92/wazero-emscripten-embind v0.0.0
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tetratelabs/wazero v1.10.1
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.71.0 // indirect
	github.com/valyala/fastjson v1.6.10 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/arch v0.23.0 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.67.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.44.3 // indirect
)

replace github.com/jerbob92/wazero-emscripten-embind => ./plugins/netease/recognize/embindlib
