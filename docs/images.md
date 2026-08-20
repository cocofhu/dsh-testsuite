# 镜像构建

本仓库有两类镜像，职责分开：

| 镜像 | 用途 | 构建入口 | 谁执行 docker build |
|---|---|---|---|
| **runtime** `dsh-testsuite-runtime:<dshVersion>` | 环境容器：bake 指定版本的 `dsh` | `image/<ver>/Dockerfile`、`make image`、GitHub Actions | **平台外**（本机或 CI） |
| **控制面** `dsh-testsuite:local` | 管理台 + API，通过 docker.sock 启停环境 | 仓库根 `Dockerfile` / `docker compose` | 可选；日常也可用 `go run` |

控制面 **不会** 在创建环境时执行 `docker build`，登记公开镜像时也 **不会** `docker pull`。UI「镜像版本」可以：

- 从写死的公开 GHCR 列表选择版本，只写入目录
- 或手动登记本机已经打好、`docker images` 能看到的 tag

## Runtime（每版本一份 Dockerfile）

每个 dsh npm 版本对应：

- 目录 `image/<ver>/`（`Dockerfile`、`entrypoint.sh`、`patch-frontend.mjs`）
- 镜像 tag `dsh-testsuite-runtime:<ver>`（CI 推 `ghcr.io/cocofhu/dsh-testsuite-runtime:<ver>`）
- 清单 [image/versions.txt](../image/versions.txt) 里的一行

**没有 default Dockerfile。** 缺 `image/<ver>/Dockerfile` 构建直接失败。新版本复制最近一份目录再改，不要改已经冻结的旧目录来迁就新 dsh。

`image/common/`（`proxy.py`、`allow_builds.py`）由各版本 Dockerfile **自己 COPY**。改 common 不会自动重打旧 tag。

### 本机构建

```bash
make image DSH_VERSION=0.1.0-rc.8
```

等价于：

```bash
docker build \
  --build-arg DSH_VERSION=0.1.0-rc.8 \
  --label dsh-testsuite.runtime=1 \
  --label dsh-testsuite.dsh-version=0.1.0-rc.8 \
  -f image/0.1.0-rc.8/Dockerfile \
  -t dsh-testsuite-runtime:0.1.0-rc.8 \
  ./image
```

### 加一个新 dsh 版本

1. `cp -a image/<最近版本> image/<新版本>`，把新目录 Dockerfile 里的版本号、`COPY <ver>/…` 路径改成新版本。
2. `make image DSH_VERSION=<新版本>`，补丁打不上就只改 **新目录** 里的 `patch-frontend.mjs` / `entrypoint.sh`。
3. 在 `image/versions.txt` 追加一行并合入 `main`（**不会**因此打镜像）。
4. 在 GitHub 打一个 **Release**，tag 用 dsh 版本本身（如 `0.1.0-rc.8`，可带 `v` 前缀）。CI 只构建这一条并推 GHCR。

### GitHub CI（冻结 tag）

Workflow：[`.github/workflows/runtime-image.yml`](../.github/workflows/runtime-image.yml)

- 触发：**GitHub Release published**（tag = dsh 版本），或手动 `workflow_dispatch`
- 推 `main` / 改 `versions.txt` **不会**构建
- 对要发布的版本：GHCR **已有该 tag 则 skip**（不因改 `image/0.1.0-rc.7/**` 而重打）
- 修已发布镜像的 bug：Actions → runtime-image → `dsh_version=<ver>` + **force=true**（覆盖同 tag）
- 脚本：[image/publish.sh](../image/publish.sh)（先核对 npm 上确有 `@deepseek-ai/dsh@<ver>`，构建后 `dsh --version` 必须一致）

首次 push 到 GHCR 的包默认是 **private**。在 GitHub → Packages → `dsh-testsuite-runtime` 改成 **public** 之后别人才能免登录 pull。

本机使用 CI 产物：先自己 `docker pull`，再在管理台 **镜像版本 → 登记镜像** 里从写死的公开列表选版本（只登记，控制面不 pull）：

```bash
docker pull ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.0-rc.8
```

也可以把 `docker.imageRepository` 写成 `ghcr.io/cocofhu/dsh-testsuite-runtime`，手动登记时只填版本。

### 镜像里有什么（各版本 Dockerfile 同类）

1. `node:22` + git / python3 / make / g++
2. `corepack enable`
3. `npm install -g @deepseek-ai/dsh@<该版本>`
4. 该版本的 `patch-frontend.mjs`：`crypto.randomUUID` polyfill；settings/凭证 API 放到 `--trusted-host`；客户端 `isLoopback: true`
5. entrypoint：`settings.yaml` → 预装插件 → `dsh web --host 127.0.0.1 --port 3081` → TCP 代理 `0.0.0.0:3080`

预装 git 源时 pnpm 11 会拦 `prepare`。entrypoint 写 `dangerouslyAllowAllBuilds`，必要时从日志补 `allowBuilds`。逻辑在 `image/common/allow_builds.py`。

### 打完之后

1. 管理台 **镜像版本**：版本填 `0.1.0-rc.8`，镜像填完整 ref（或留空用 `imageRepository:<version>`）。本机列应显示「本机有」。
2. **新环境**选这个版本。
3. **已有环境不会自动换镜像。** Start 会 Destroy+Create，用当前本机该 tag。

### 预装插件

创建环境「预装插件」每行一个 pnpm 源，容器启动时 `dsh plugin --profile web add`。

```
github:owner/plugin#main
@scope/plugin@1.0.0
```

- npm 包：一般不跑 `prepare`
- `github:owner/repo#branch`：会跑 `prepare`；装完 Health 变 Healthy 更慢
- 不要填 GitHub 网页 URL；失败看该环境日志

### 自检

```bash
docker image inspect dsh-testsuite-runtime:0.1.0-rc.8 \
  --format '{{index .Config.Labels "dsh-testsuite.runtime"}} {{index .Config.Labels "dsh-testsuite.dsh-version"}}'
# 期望: 1 0.1.0-rc.8

docker run --rm --entrypoint dsh dsh-testsuite-runtime:0.1.0-rc.8 --version
```

`make test` 会跑 Go 单测和 `image/common/allow_builds_test.py`。

## 控制面（管理台）

日常开发不必打这个镜像：

```bash
export PATH="/usr/local/go/bin:$PATH"
go run ./cmd/dsh-testsuite -config config.yaml
```

静态 UI 从 `web/` 读盘，改 CSS/JS 不用重启；改 Go 要重启进程。

要用 compose 把控制面也容器化（仍须宿主机先有 **runtime**）：

```bash
make image DSH_VERSION=0.1.0-rc.8
docker compose up --build
```

根目录 `Dockerfile`：编 Go 二进制，最终镜像带 `docker-cli`，挂 `/var/run/docker.sock`。

## 标签

| Label | 值 | 含义 |
|---|---|---|
| `dsh-testsuite.runtime` | `1` | 环境用的 runtime 镜像 |
| `dsh-testsuite.dsh-version` | 如 `0.1.0-rc.8` | bake 进去的 dsh 版本 |
| `org.opencontainers.image.revision` | git sha | CI 构建时的 commit |
| `dsh-testsuite.managed` | `1` | 控制面创建的环境容器 |
| `dsh-testsuite.id` | 环境短 id | 打在环境容器上 |
