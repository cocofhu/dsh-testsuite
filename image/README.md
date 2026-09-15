# Runtime image

每个 dsh 版本一份 Dockerfile：`image/<ver>/Dockerfile`。控制面不在这里执行 `docker build`。

```bash
# 在仓库根目录
make image DSH_VERSION=0.1.6-alpha.1
```

加新版本：复制最近的 `image/<ver>/` 目录并改到能构建，再把版本号追加到 [versions.txt](versions.txt)，然后打 GitHub Release（tag = 该版本）。已发布的 GHCR tag 默认不重打，见 [docs/images.md](../docs/images.md)。

## 补丁的两个版本族

`patch-frontend.mjs` 按 dsh 内部结构分两族，复制目录时务必选同族的模板：

- **旧族** `0.0.1-rc.1` ~ `0.1.0-rc.3`（以及冻结的 `0.1.0-rc.6` ~ `0.1.1-rc.2`）：host 侧请求围栏把 `isTrustedApiRequest(request, [])` 写死为空列表，客户端 `connection.isLoopback` 只看 hostname。补丁同时改写 host 两处围栏与客户端 `isLoopback`。
- **新族** `0.1.2-alpha.2` 起：上游已移除 `PRIVILEGED_METHODS`，host 侧直接使用 `isTrustedApiRequest(request, this.trustedHosts)`（`--trusted-host` 生效），客户端新增 `transport?.ownsHost === true` 分支。补丁只注入 `crypto.randomUUID` polyfill 并把客户端 `isLoopback` 置为 `true`，不再做 host 侧改写。

前端包名也随族变化：`0.0.1-rc.1`/`rc.2` 为 `@deepseek-ai/dsh-frontend`，`0.0.1-rc.5` 起为 `@deepseek-ai/dsh-web-frontend`。旧族补丁会依次尝试两者，取不到时只跳过 polyfill 并打印告警。
