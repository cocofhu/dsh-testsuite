# 参与贡献

感谢你帮助改进 dsh-testsuite。Bug 修复、测试、文档和 Docker/runtime 适配都欢迎。

参与本仓库即表示你同意遵守 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)，并以 [MIT License](LICENSE) 授权你的贡献。

本项目是非官方工具，不是 DeepSeek 或 DeepSeek Harness 的官方产品。

## 开始之前

1. 搜索现有 Issue 和 Pull Request，避免重复劳动。
2. 较大的功能请先开 Issue 讨论。
3. 安全漏洞不要公开提交，请阅读 [SECURITY.md](SECURITY.md)。
4. 不要在 Issue、日志、测试或提交中包含密钥、Cookie、内网地址和个人数据。不要提交 `config.yaml` 或 `data/`。

## 本地开发

需要 Go 1.24+、Docker，以及能访问 npm（打 runtime 镜像时）。

```sh
git clone https://github.com/cocofhu/dsh-testsuite.git
cd dsh-testsuite
make test
cp config.example.yaml config.yaml
# 编辑 server.publicHost 后
go run ./cmd/dsh-testsuite -config config.yaml
```

| 命令 | 作用 |
| --- | --- |
| `make test` | `go vet`、Go 单测、`allow_builds` 脚本测试 |
| `make image DSH_VERSION=0.1.0-rc.8` | 按 `image/<ver>/Dockerfile` 打 runtime |
| `make run` | 用 `config.example.yaml` 起控制面 |

控制面单测不访问 Docker daemon。runtime 镜像按版本冻结，加新 dsh 版本的流程见 [docs/images.md](docs/images.md)。GitHub Release 才会推 GHCR，推 `main` 不会打镜像。

## Pull Request

- 针对一个问题，描述里写清动机和验证步骤
- 保持 `make test` 通过
- 改某个已发布 runtime 目录不会自动重打 GHCR tag；修已发布镜像请说明是否需要 `force` Release
