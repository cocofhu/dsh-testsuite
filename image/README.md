# Runtime image

每个 dsh 版本一份 Dockerfile：`image/<ver>/Dockerfile`。控制面不在这里执行 `docker build`。

```bash
# 在仓库根目录
make image DSH_VERSION=0.1.6-alpha.1
```

加新版本：复制最近的 `image/<ver>/` 目录并改到能构建，再把版本号追加到 [versions.txt](versions.txt)，然后打 GitHub Release（tag = 该版本）。已发布的 GHCR tag 默认不重打，见 [docs/images.md](../docs/images.md)。

## 支持的版本与已知缺口

`versions.txt` 当前列出 19 个可构建版本。**`0.0.1-rc.1` 与 `0.0.1-rc.2` 已从清单中移除**：这两个版本依赖的 `@deepseek-ai/dsh-agent-tool-mode`（以及 `@deepseek-ai/dsh-frontend`）从未发布到公共 npm registry，`npm install -g @deepseek-ai/dsh@<ver>` 会 E404，镜像无法产出。与其在「镜像版本」页登记一个永远拉不到的版本，不如不列出；等上游重发依赖后再补回目录与清单即可。

## entrypoint 的 CLI 能力适配

各版本 `dsh web` 的选项并不一致：`--no-open` 是 0.1.x 才加入的，`0.0.1-rc.5` 传它会直接 `error: unknown option '--no-open'` 并 exit 1。`entrypoint.sh` 因此不硬编码版本分支，而是启动前探测本版本的 help：

```bash
web_help="$(dsh web --help 2>&1 || true)"
if printf '%s' "$web_help" | grep -q -- "--no-open"; then no_open_args+=(--no-open); fi
```

这样 19 份 entrypoint 可以保持同一份内容，又能在运行期按实际 CLI 能力分支。探测不出结果时默认不加 `--no-open`（少一个参数只是不抑制打开浏览器，不会致命；反之传错参数会直接退出）。需要强制时用环境变量 `DSH_WEB_NO_OPEN=1|0`。`--trusted-host` 在 `0.0.1-rc.5` 及以后均存在，仍无条件传入。

## 补丁的两个版本族

`patch-frontend.mjs` 按 dsh 内部结构分两族，复制目录时务必选同族的模板：

- **旧族** `0.0.1-rc.5` ~ `0.1.0-rc.3`（以及冻结的 `0.1.0-rc.6` ~ `0.1.1-rc.2`）：host 侧请求围栏把 `isTrustedApiRequest(request, [])` 写死为空列表，客户端 `connection.isLoopback` 只看 hostname。补丁同时改写 host 两处围栏与客户端 `isLoopback`。
- **新族** `0.1.2-alpha.2` 起：上游已移除 `PRIVILEGED_METHODS`，host 侧直接使用 `isTrustedApiRequest(request, this.trustedHosts)`（`--trusted-host` 生效），客户端新增 `transport?.ownsHost === true` 分支。补丁只注入 `crypto.randomUUID` polyfill 并把客户端 `isLoopback` 置为 `true`，不再做 host 侧改写。

前端包名也随族变化：`0.0.1-rc.5` 起为 `@deepseek-ai/dsh-web-frontend`（更早的 `0.0.1-rc.1`/`rc.2` 用 `@deepseek-ai/dsh-frontend`，但这两版无法构建，已移除）。旧族补丁仍会依次尝试两个包名，取不到时只跳过 polyfill 并打印告警。
