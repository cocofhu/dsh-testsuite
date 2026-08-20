package settings

import (
	_ "embed"
	"encoding/json"
)

//go:embed catalog_models.json
var catalogModelsJSON []byte

var catalogModels map[string][]ModelOption

func init() {
	if err := json.Unmarshal(catalogModelsJSON, &catalogModels); err != nil {
		panic("settings: catalog_models.json: " + err.Error())
	}
}

// CatalogProviders are the pi-ai routes dsh Models → 提供方 lists
// (catalogProviderTakesApiKey). Kept in lockstep with @deepseek-ai/dsh 0.1.0-rc.8
// / @earendil-works/pi-ai 0.82.1.
var CatalogProviders = []string{
	"amazon-bedrock",
	"ant-ling",
	"anthropic",
	"azure-openai-responses",
	"cerebras",
	"cloudflare-ai-gateway",
	"cloudflare-workers-ai",
	"deepseek",
	"fireworks",
	"github-copilot",
	"google",
	"google-vertex",
	"groq",
	"huggingface",
	"kimi-coding",
	"minimax",
	"minimax-cn",
	"mistral",
	"moonshotai",
	"moonshotai-cn",
	"nvidia",
	"openai",
	"opencode",
	"opencode-go",
	"openrouter",
	"qwen-token-plan",
	"qwen-token-plan-cn",
	"radius",
	"together",
	"vercel-ai-gateway",
	"xai",
	"xiaomi",
	"xiaomi-token-plan-ams",
	"xiaomi-token-plan-cn",
	"xiaomi-token-plan-sgp",
	"zai",
	"zai-coding-cn",
}

// OfficialModels are the default deepseek-official catalog ids.
var OfficialModels = []ModelOption{
	{ID: "deepseek-v4-flash", Name: "DeepSeek-V4-Flash"},
	{ID: "deepseek-v4-pro", Name: "DeepSeek-V4-Pro"},
}

// ModelOption is one selectable model in the create form.
type ModelOption struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// IsOfficial reports the built-in DeepSeek adapter route.
func IsOfficial(id string) bool {
	return id == OfficialProvider
}

// IsCatalog reports a shipped pi-ai provider id (Base URL optional).
func IsCatalog(id string) bool {
	for _, p := range CatalogProviders {
		if p == id {
			return true
		}
	}
	return false
}

// ModelsFor returns the advisory catalog for a provider. Custom routes have none.
func ModelsFor(id string) []ModelOption {
	if IsOfficial(id) {
		return OfficialModels
	}
	return catalogModels[id]
}

// ProviderOption is one row in the create-env 提供方 dropdown.
type ProviderOption struct {
	ID     string        `json:"id"`
	Kind   string        `json:"kind"`
	Label  string        `json:"label"`
	Models []ModelOption `json:"models,omitempty"`
}

// ProviderOptions matches the Models page: official DeepSeek, then the pi-ai
// directory, then a custom route.
func ProviderOptions() []ProviderOption {
	out := make([]ProviderOption, 0, 2+len(CatalogProviders))
	out = append(out, ProviderOption{
		ID:     OfficialProvider,
		Kind:   "official",
		Label:  "DeepSeek（官方）",
		Models: OfficialModels,
	})
	for _, id := range CatalogProviders {
		out = append(out, ProviderOption{
			ID:     id,
			Kind:   "catalog",
			Label:  id,
			Models: ModelsFor(id),
		})
	}
	out = append(out, ProviderOption{ID: "custom", Kind: "custom", Label: "自定义…"})
	return out
}
