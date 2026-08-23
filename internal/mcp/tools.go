package mcp

import (
	"github.com/cocofhu/dsh-testsuite/internal/env"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 输入结构只做 MCP 协议适配：字段名与 REST 载荷保持一致，
// jsonschema tag 提供 agent 可读的参数描述。

type idIn struct {
	ID string `json:"id" jsonschema:"环境 ID"`
}

type versionIn struct {
	Version string `json:"version" jsonschema:"runtime 镜像版本（镜像 tag）"`
}

type logsIn struct {
	ID   string `json:"id" jsonschema:"环境 ID"`
	Tail int    `json:"tail,omitempty" jsonschema:"返回日志的末尾行数，0或缺省表示全部"`
}

type createEnvIn struct {
	Name       string   `json:"name" jsonschema:"环境显示名（必填）"`
	DSHVersion string   `json:"dshVersion" jsonschema:"runtime 镜像版本，须先 register_image 登记（必填）"`
	PresetID   string   `json:"presetId,omitempty" jsonschema:"预设 ID，给出后可省略 apiKey/provider/model"`
	APIKey     string   `json:"apiKey,omitempty" jsonschema:"提供方 API 密钥；任何响应只回显末 4 位"`
	Provider   string   `json:"provider,omitempty" jsonschema:"提供方 ID（小写，见 list_providers）"`
	Model      string   `json:"model,omitempty" jsonschema:"模型 ID，如 deepseek-v4-flash"`
	BaseURL    string   `json:"baseURL,omitempty" jsonschema:"自定义路由的 base URL（可选）"`
	API        string   `json:"api,omitempty" jsonschema:"自定义路由的 API 类型（可选）"`
	Plugins    []string `json:"plugins,omitempty" jsonschema:"要安装的 dsh 插件列表（可选）"`
}

func (in createEnvIn) toRequest() env.CreateRequest {
	return env.CreateRequest{
		Name:       in.Name,
		DSHVersion: in.DSHVersion,
		PresetID:   in.PresetID,
		APIKey:     in.APIKey,
		Provider:   in.Provider,
		Model:      in.Model,
		BaseURL:    in.BaseURL,
		API:        in.API,
		Plugins:    in.Plugins,
	}
}

type registerImageIn struct {
	Version string `json:"version" jsonschema:"runtime 镜像版本（镜像 tag，必填）"`
	Ref     string `json:"ref,omitempty" jsonschema:"完整镜像引用；缺省为本地仓库名:version"`
	Pull    *bool  `json:"pull,omitempty" jsonschema:"本地缺失时是否从 GHCR 拉取，默认 true"`
}

type presetIn struct {
	Name     string `json:"name" jsonschema:"预设显示名（必填）"`
	Provider string `json:"provider" jsonschema:"提供方 ID（小写）"`
	Model    string `json:"model" jsonschema:"模型 ID"`
	BaseURL  string `json:"baseURL,omitempty" jsonschema:"自定义路由的 base URL（可选）"`
	API      string `json:"api,omitempty" jsonschema:"自定义路由的 API 类型（可选）"`
	APIKey   string `json:"apiKey,omitempty" jsonschema:"提供方 API 密钥；响应只回显末 4 位。update 时留空表示保留原密钥"`
}

func (in presetIn) toInput() env.PresetInput {
	return env.PresetInput{
		Name:     in.Name,
		Provider: in.Provider,
		Model:    in.Model,
		BaseURL:  in.BaseURL,
		API:      in.API,
		APIKey:   in.APIKey,
	}
}

type updatePresetIn struct {
	ID string `json:"id" jsonschema:"预设 ID"`
	// 匿名内嵌使预设字段在 JSON 中平铺，与 REST PUT /api/presets/:id 载荷形状一致。
	presetIn
}

// call adapts a (value, error) service call into the ToolHandlerFor return
// shape: service errors become MCP tool errors (isError=true, 文本保留)。
func call[Out any](out Out, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	return nil, out, nil
}
