# Apple Music 无损 —— FairPlay wrapper（可选）

AAC 256kbps 由 **bot 内置原生解密** —— 无需 wrapper、零配置，只要填了
`media_user_token` 即可。

无损 **ALAC / Hi-Res 24bit / Dolby Atmos** 走的是 FairPlay，Apple 不允许用
Widevine 解密这些音质。它们需要外部
[WorldObservationLog/wrapper](https://github.com/WorldObservationLog/wrapper)
作为**独立服务**运行（它模拟安卓版 Apple Music App，在安卓 userland 下运行，
因此需要 `--privileged` 和它自己的登录）。

## 镜像来源

上游 wrapper **不发布 Docker 镜像**（只发 Release zip）。本仓库提供一个手动触发的
GitHub Actions 工作流 `.github/workflows/build-wrapper.yml`，它会：

1. 从上游 Release 下载预编译二进制（`Wrapper.x86_64.latest.zip`）；
2. 用上游官方 Dockerfile 打包（自动跳过 Android NDK 编译，很快）；
3. 推送到 `ghcr.io/<你的用户名>/musicbot-wrapper:latest`。

**使用前先跑一次该工作流**：仓库 → Actions → “Build Apple Music Wrapper Image”
→ Run workflow。之后 `docker-compose.yml` 里的 wrapper 服务就能 pull 到镜像。

> 仅支持 x86_64（上游限制）。

## 配置方式（docker compose）

`wrapper` 服务已在 `docker-compose.yml` 中定义好。启用无损：

1. **首次登录**：在 `docker-compose.yml` 的 wrapper 服务里填入一个**有有效订阅**
   的 Apple ID（wrapper 无法复用 bot 的 `media_user_token`——两套独立认证）：

   ```yaml
   environment:
     USERNAME: "appleid@example.com"
     PASSWORD: "your-password"
   ```

   首次启动时 wrapper 会自动登录（含 2FA 流程），会话保存到
   `./docker-data/wrapper/`（挂载的 volume）。**登录成功后把这两项清空**即可，
   之后凭持久化的会话运行。

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

自行运行 wrapper（参见其 README：自行 `docker build` 或从 Release 取二进制），
然后把 `wrapper_host` 设为它的地址（例如 `127.0.0.1`）。wrapper 监听
10020 / 20020 / 30020 端口。

## 注意事项

- `media_user_token`（bot）和 wrapper 的 Apple ID 登录是**两套独立**凭证；
  下载无损时两者都需要。
- 不要提交任何 wrapper 会话数据或凭证。`./docker-data/` 是运行时 volume，
  已在 `.gitignore` 中忽略。
