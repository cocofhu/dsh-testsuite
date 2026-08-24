# Runtime image

每个 dsh 版本一份 Dockerfile：`image/<ver>/Dockerfile`。控制面不在这里执行 `docker build`。

```bash
# 在仓库根目录
make image DSH_VERSION=0.1.1-rc.2
```

加新版本：复制最近的 `image/<ver>/` 目录并改到能构建，再把版本号追加到 [versions.txt](versions.txt)，然后打 GitHub Release（tag = 该版本）。已发布的 GHCR tag 默认不重打，见 [docs/images.md](../docs/images.md)。
