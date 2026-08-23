// Package mcp exposes the control plane as MCP tools on /mcp.
// It is a thin protocol adapter over the same env.Service the REST API uses.
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/cocofhu/dsh-testsuite/internal/env"
	"github.com/cocofhu/dsh-testsuite/internal/settings"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
)

// ServerVersion is reported in MCP initialize serverInfo.
const ServerVersion = "0.1.0"

// NewHandler builds the Streamable HTTP handler for /mcp.
// The returned handler is safe to mount on the existing control-plane mux;
// panics inside tool handling are recovered so the REST surface stays up.
func NewHandler(svc *env.Service, log zerolog.Logger) http.Handler {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "dsh-testsuite",
		Version: ServerVersion,
	}, nil)
	addTools(srv, svc)
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		// Non-streaming responses keep the wire format plain JSON; valid per
		// the Streamable HTTP spec (§2.1.5) and handled by every MCP client.
		JSONResponse: true,
	})
	return recoverPanics(h, log)
}

// recoverPanics keeps a panicking tool from taking the whole control plane
// down: the /mcp surface answers 500 while REST routes stay up.
func recoverPanics(h http.Handler, log zerolog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().Interface("panic", rec).Bytes("stack", debug.Stack()).
					Str("path", r.URL.Path).Msg("mcp panic recovered")
				http.Error(w, fmt.Sprintf("internal error: %v", rec), http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}

func addTools(srv *mcp.Server, svc *env.Service) {
	// —— 环境生命周期（9）——
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_environments",
		Description: "列出全部 dsh 环境（密钥仅回显末 4 位）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return call(svc.List(ctx), nil)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "create_environment",
		Description: "创建一个 dsh 环境（可传 presetId 复用预设，或直接给 apiKey/provider/model）。" +
			"行为与 REST POST /api/environments 一致。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createEnvIn) (*mcp.CallToolResult, any, error) {
		return call(svc.Create(ctx, in.toRequest()))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_environment",
		Description: "查询单个环境详情（状态、端口、健康、到期时间等）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
		return call(svc.Get(ctx, in.ID))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_environment",
		Description: "启动一个已停止的环境（重建容器以吃到新镜像）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
		return call(svc.Start(ctx, in.ID))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "stop_environment",
		Description: "停止一个运行中的环境（保留数据与记录）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
		return call(svc.Stop(ctx, in.ID))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "restart_environment",
		Description: "重启环境（先停止再重建启动）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
		return call(svc.Restart(ctx, in.ID))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "renew_environment",
		Description: "续期环境：到期时间在当前基础上再延长 6 小时。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
		return call(svc.Renew(ctx, in.ID))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "destroy_environment",
		Description: "销毁环境：删除容器、卷与记录，不可恢复。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
		if err := svc.Destroy(ctx, in.ID); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"status": "destroyed", "id": in.ID}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_environment_logs",
		Description: "查看环境容器日志；tail 为返回的行数，0 或缺省表示全部。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in logsIn) (*mcp.CallToolResult, any, error) {
		out, err := svc.Logs(ctx, in.ID, in.Tail)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"logs": out}, nil
	})

	// —— 镜像（4）——
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_images",
		Description: "列出已登记的 runtime 镜像目录及本地是否可用。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return call(svc.ListImages(ctx))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_remote_images",
		Description: "列出内置可选的远程 runtime 版本（GHCR），含是否已登记/本地是否存在。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return call(svc.ListRemoteImages(ctx))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name: "register_image",
		Description: "登记一个 runtime 镜像版本。ref 缺省为本地仓库名:version；" +
			"pull 默认 true：本地缺失时从 GHCR 拉取并打 tag（与 REST POST /api/images 一致）。",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in registerImageIn) (*mcp.CallToolResult, any, error) {
		pull := true
		if in.Pull != nil {
			pull = *in.Pull
		}
		return call(svc.RegisterImage(ctx, env.ImageConfig{Version: in.Version, Ref: in.Ref}, pull))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_image",
		Description: "从镜像目录移除一个版本（不删除 docker 镜像本身）。",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in versionIn) (*mcp.CallToolResult, any, error) {
		if err := svc.DeleteImage(in.Version); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"status": "deleted", "version": in.Version}, nil
	})

	// —— 预设（4）——
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_presets",
		Description: "列出已保存的模型预设（密钥仅回显末 4 位）。",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return call(svc.ListPresets(), nil)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_preset",
		Description: "新建模型预设（provider/model/apiKey 等），供创建环境时按 presetId 复用。",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in presetIn) (*mcp.CallToolResult, any, error) {
		return call(svc.CreatePreset(in.toInput()))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_preset",
		Description: "更新模型预设；apiKey 留空表示保留原密钥（与 REST PUT /api/presets/:id 一致）。",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in updatePresetIn) (*mcp.CallToolResult, any, error) {
		return call(svc.UpdatePreset(in.ID, in.presetIn.toInput()))
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_preset",
		Description: "删除模型预设（不影响已创建的环境）。",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in idIn) (*mcp.CallToolResult, any, error) {
		if err := svc.DeletePreset(in.ID); err != nil {
			return nil, nil, err
		}
		return nil, map[string]string{"status": "deleted", "id": in.ID}, nil
	})

	// —— Provider 目录（1）——
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_providers",
		Description: "列出创建环境可选的提供方目录（官方 DeepSeek、pi-ai 目录、自定义）。",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"providers": settings.ProviderOptions()}, nil
	})
}
