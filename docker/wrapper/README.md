# Apple Music 无损 —— FairPlay wrapper（可选）

AAC 256kbps 由 **bot 内置原生解密** —— 无需 wrapper、零配置，只要填了
`media_user_token` 即可。

无损 **ALAC / Hi-Res 24bit / Dolby Atmos** 走的是 FairPlay，Apple 不允许用
Widevine 解密这些音质。它们需要外部
[WorldObservationLog/wrapper](https://github.com/WorldObservationLog/wrapper)
作为**独立服务**运行（它模拟安卓版 Apple Music App，在安卓 userland 下运行，
因此需要 `--privileged` 和它自己的登录）。

## 配置方式（docker compose）

`wrapper` 服务已经在 `docker-compose.yml` 中定义好了。启用无损：

1. **一次性登录**，使用一个**有有效订阅**的 Apple ID
   （wrapper 无法复用 bot 的 `media_user_token` —— 两套独立认证）：

   ```bash
   docker compose run --rm wrapper -L "appleid@example.com:password" -F -H 0.0.0.0
   ```

   完成 2FA 验证后按 Ctrl-C。会话会保存到 `./docker-data/wrapper/`
   （一个挂载的 volume），所以只需登录这一次。

2. **让 bot 指向 wrapper。** 在 `config.ini` 中：

   ```ini
   [plugins.applemusic]
   media_user_token = YOUR_TOKEN     # 仍然需要（元数据、AAC、歌词）
   wrapper_host = wrapper            # compose 服务名
   ```

3. **启动全部服务：**

   ```bash
   docker compose up -d
   ```

此后请求 `lossless` / `hires` 就会走 wrapper。如果 wrapper 未运行，或某首歌没有
无损版本，bot 会自动回退到 AAC 256k。

## 裸核部署

自行运行 wrapper（参见其 README），然后把 `wrapper_host` 设为它的地址
（例如 `127.0.0.1`）。wrapper 监听 10020 / 20020 / 30020 端口。

## 注意事项

- `media_user_token`（bot）和 wrapper 的 Apple ID 登录是**两套独立**凭证；
  下载无损时两者都需要。
- 不要提交任何 wrapper 会话数据或凭证。`./docker-data/` 是运行时 volume，
  应排除在 git 之外（`.gitignore` 已忽略）。
