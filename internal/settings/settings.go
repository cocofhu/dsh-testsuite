// Package settings renders $DSH_HOME/settings.yaml for a new environment.
package settings

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// OfficialProvider is dsh's built-in DeepSeek catalog route.
const OfficialProvider = "deepseek-official"

// Input is the LLM routing the control plane injects at start.
type Input struct {
	Provider string
	Model    string
	BaseURL  string
	API      string
}

type file struct {
	AgentDefaultModel agentDefaultModel `yaml:"agent-default-model"`
	LLMPiAI           *llmPiAI          `yaml:"llm-pi-ai,omitempty"`
}

type agentDefaultModel struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

type llmPiAI struct {
	Providers map[string]provider `yaml:"providers"`
}

type provider struct {
	APIKeyEnv string    `yaml:"apiKeyEnv"`
	API       string    `yaml:"api,omitempty"`
	BaseURL   string    `yaml:"baseURL,omitempty"`
	Models    []modelID `yaml:"models"`
}

type modelID struct {
	ID string `yaml:"id"`
}

// Render returns settings.yaml contents. Official DeepSeek only needs
// agent-default-model; a pi-ai catalog id (openai, anthropic, …) writes
// llm-pi-ai without requiring Base URL; any other id is a custom route.
func Render(in Input) (string, error) {
	in.Provider = strings.TrimSpace(in.Provider)
	in.Model = strings.TrimSpace(in.Model)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.API = strings.TrimSpace(in.API)
	if in.Provider == "" {
		return "", fmt.Errorf("provider is required")
	}
	if in.Model == "" {
		return "", fmt.Errorf("model is required")
	}
	doc := file{
		AgentDefaultModel: agentDefaultModel{
			Provider: in.Provider,
			Model:    in.Model,
		},
	}
	if !IsOfficial(in.Provider) {
		api := in.API
		if api == "" && !IsCatalog(in.Provider) {
			api = "openai-completions"
		}
		if in.BaseURL == "" && !IsCatalog(in.Provider) {
			return "", fmt.Errorf("baseURL is required for custom provider %q", in.Provider)
		}
		p := provider{
			APIKeyEnv: "DSH_API_KEY",
			API:       api,
			BaseURL:   in.BaseURL,
			Models:    []modelID{{ID: in.Model}},
		}
		doc.LLMPiAI = &llmPiAI{
			Providers: map[string]provider{in.Provider: p},
		}
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// PluginsFile joins plugin sources, one per line, skipping blanks.
func PluginsFile(plugins []string) string {
	var b strings.Builder
	for _, p := range plugins {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return b.String()
}
