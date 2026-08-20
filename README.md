# dsh-testsuite

[![CI](https://github.com/cocofhu/dsh-testsuite/actions/workflows/ci.yml/badge.svg)](https://github.com/cocofhu/dsh-testsuite/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

给 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 插件准备可复用的在线测试环境。按版本预构建 runtime 镜像，在管理台登记后创建 Docker 容器，注入 API Key、提供方、模型和预装插件，然后打开 `dsh web`。

<img src="docs/screenshots/environments.png" alt="环境列表：运行中、启动中和已停止的 dsh 环境" width="920" />

本项目是非官方工具，不是 DeepSeek Harness 的官方产品。

## 怎么工作

- **控制面**（本仓库 Go 服务 + Web UI）：登记镜像，创建 / 启停 / 重启 / 销毁环境，看日志。
- **Runtime 镜像**（每版本一份 `image/<ver>/Dockerfile`）：在平台外用 `make image` 构建，或从 GitHub Release 推到 GHCR。已发布 tag 默认冻结。说明见 [docs/images.md](docs/images.md)。

每个环境把容器内 `3080` 映射到宿主机随机端口。容器进入 `running` 后还会探测端口，Health 变为 Healthy 才允许「打开」（dsh Web 是根路径 SPA，启动还要几秒）。

镜像预先构建好，创建环境只选已登记镜像并注入配置。当前只实现 Docker。**控制面不执行 docker build，登记时也不会 docker pull。**

## 快速开始

需要 Docker 和 Go 1.24+。

```bash
make image DSH_VERSION=0.1.0-rc.8    # 在平台外打 runtime；细节见 docs/images.md
cp config.example.yaml config.yaml   # 本地配置，不要提交
make run                             # 控制面 http://127.0.0.1:8090
```

1. 打开「镜像版本」，登记例如版本 `0.1.0-rc.8`、镜像 `dsh-testsuite-runtime:0.1.0-rc.8`（本机有该 tag 才显示「本机有」）。
2. 回到「环境」，创建并等待 Health 变为 Healthy，再点「打开」。

<img src="docs/screenshots/images.png" alt="镜像版本：登记本机已有的 runtime，或从公开列表选择" width="920" />

也可以先 pull CI 产物：

```bash
docker pull ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.0-rc.8
```

首次推到 GHCR 的包默认是 private，需要在 GitHub Packages 设为 public。

用 Compose 把控制面也跑进容器（需挂 docker.sock；runtime 仍在宿主机构建或 pull）：

```bash
make image DSH_VERSION=0.1.0-rc.8
docker compose up --build
```

## 创建环境

<img src="docs/screenshots/create.png" alt="创建环境：选择已登记的 dsh 版本，填入密钥、提供方、模型和预装插件" width="920" />

| 字段 | 说明 |
|---|---|
| dsh 版本 | 只能选「镜像版本」里已登记的条目 |
| API 密钥 | 注入为 `DSH_API_KEY`；官方 provider 同时设 `DEEPSEEK_API_KEY`。列表只显示末 4 位 |
| Provider / Model | 与 dsh 设置 → 模型页同一份目录；模型按提供方下拉。仅「自定义…」才填 Provider ID / API 地址 |
| 预装插件 | 每行一个 pnpm 源，如 `github:owner/plugin#main`。git 源的 `prepare` 会由 runtime 写入 profile 的 `allowBuilds` |

空闲 TTL 默认 2 小时（`limits.idleTTL`），超时会销毁容器和该环境的 `$DSH_HOME`。

控制面默认无认证，不要把 `:8090` 裸暴露到公网。`config.yaml` 和 `data/` 含密钥，已 gitignore。

## 远程访问

从浏览器访问这台机器的 IP 时：

1. `docker.bindIP` 设为 `0.0.0.0`，否则端口只绑在 127.0.0.1，外网会 `ERR_EMPTY_RESPONSE` / 连不上。
2. `server.publicHost` 填这台机器的 IP 或域名（也可用 `DSHTS_PUBLIC_HOST`）。「打开」链接和容器内 `dsh --trusted-host` 都用它。
3. 上游 dsh 拒绝 `--host 0.0.0.0`。runtime 让 dsh 听 `127.0.0.1:3081`，entrypoint 再用 TCP 代理转到容器 `3080` 给 `docker -p`。
4. 用 IP + HTTP 打开时浏览器不是 secure context，没有 `crypto.randomUUID`。镜像会注入 polyfill（与社区 [dsh-web-lan-access](https://github.com/AcidGr/dsh-web-lan-access) 同一思路），并把设置 / 凭证 API 放到 `--trusted-host` 上，否则模型页会报 `settings are unavailable in this browser`。

```bash
cp config.example.yaml config.yaml
# 编辑 server.publicHost
export PATH="/usr/local/go/bin:$PATH"
go run ./cmd/dsh-testsuite -config config.yaml
```

## API

```
GET    /healthz
GET    /api/providers
GET    /api/images
GET    /api/images/remote           内置可选版本列表（不请求 GitHub、不 docker pull）
POST   /api/images                  { "version": "0.1.0-rc.8", "ref": "dsh-testsuite-runtime:0.1.0-rc.8" }
DELETE /api/images/:version
POST   /api/environments            { name, dshVersion, apiKey, provider, model, baseURL?, api?, plugins[] }
GET    /api/environments
GET    /api/environments/:id
POST   /api/environments/:id/start
POST   /api/environments/:id/stop
POST   /api/environments/:id/restart
DELETE /api/environments/:id
GET    /api/environments/:id/logs
```

## 配置

见 [config.example.yaml](config.example.yaml)。`docker.imageRepository` 只在登记时没填 `ref` 时用来拼默认 `仓库:版本`。

## 参与贡献

见 [CONTRIBUTING.md](CONTRIBUTING.md)。安全问题请按 [SECURITY.md](SECURITY.md) 私下报告。

## 许可证

[MIT](LICENSE)
