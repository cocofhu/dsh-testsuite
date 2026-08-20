# 安全政策

## 支持范围

| 版本 | 支持 |
| --- | --- |
| `main` 最新提交 | 支持 |
| 已发布的 git tag / GitHub Release | 仅在仍被文档推荐时支持 |
| 历史 commit / fork | 请先核对问题是否已在最新提交中修复 |

## 报告漏洞

请通过 GitHub **Security → Report a vulnerability** 私下提交：

https://github.com/cocofhu/dsh-testsuite/security/advisories/new

报告请包含：

- 受影响版本或 commit
- 复现步骤、实际影响、攻击前提
- 可行的缓解或修复建议（如有）

请不要在公开 Issue、讨论区或 Pull Request 中披露未修复漏洞，也不要附带真实 API 密钥、Cookie、内网地址或个人数据。

维护者会尽快确认。请预留合理的修复时间，修复可用后再协商披露。

## 安全边界

本项目是非官方工具，用来在 Docker 里跑 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 测试环境。

- 控制面默认不带认证。不要把 `:8090` 裸暴露到公网。
- `config.yaml`、`data/` 含密钥和环境状态，不要提交到 git。
- 创建环境时注入的 API Key 会进容器环境变量和 `$DSH_HOME`。列表接口只返回密钥末 4 位，完整密钥在宿主机 `data/`。
- runtime 镜像会给 dsh 打 LAN/HTTP 补丁（polyfill、trusted-host）。这是为了从 IP 访问 Web，不是 dsh 官方行为。
- 预装插件来自你填写的 pnpm/git 源。git 源会在容器内执行 `prepare`。只安装你信任的包。
- 控制面通过 docker.sock 创建容器。compose 部署等于把 Docker 管理权交给该进程。
