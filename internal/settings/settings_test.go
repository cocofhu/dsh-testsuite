package settings

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRenderOfficial(t *testing.T) {
	raw, err := Render(Input{Provider: OfficialProvider, Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "llm-pi-ai") {
		t.Fatalf("official settings should omit llm-pi-ai:\n%s", raw)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	adm := doc["agent-default-model"].(map[string]any)
	if adm["provider"] != OfficialProvider || adm["model"] != "deepseek-v4-flash" {
		t.Fatalf("agent-default-model=%v", adm)
	}
}

func TestRenderCustom(t *testing.T) {
	raw, err := Render(Input{
		Provider: "cpa",
		Model:    "gpt-5.6-sol",
		BaseURL:  "http://example.gw:8317/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc file
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	p, ok := doc.LLMPiAI.Providers["cpa"]
	if !ok {
		t.Fatalf("missing provider:\n%s", raw)
	}
	if p.APIKeyEnv != "DSH_API_KEY" || p.API != "openai-completions" || p.BaseURL != "http://example.gw:8317/v1" {
		t.Fatalf("provider=%+v", p)
	}
	if len(p.Models) != 1 || p.Models[0].ID != "gpt-5.6-sol" {
		t.Fatalf("models=%v", p.Models)
	}
}

func TestRenderCustomRequiresBaseURL(t *testing.T) {
	_, err := Render(Input{Provider: "cpa", Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRenderCatalogNoBaseURL(t *testing.T) {
	raw, err := Render(Input{Provider: "openai", Model: "gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	var doc file
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	p, ok := doc.LLMPiAI.Providers["openai"]
	if !ok || p.APIKeyEnv != "DSH_API_KEY" || p.BaseURL != "" {
		t.Fatalf("provider=%+v ok=%v\n%s", p, ok, raw)
	}
}

func TestProviderOptionsIncludeOfficialAndCatalog(t *testing.T) {
	opts := ProviderOptions()
	if opts[0].ID != OfficialProvider || opts[0].Kind != "official" {
		t.Fatalf("first=%+v", opts[0])
	}
	if !IsCatalog("amazon-bedrock") || !IsCatalog("deepseek") || IsCatalog("deepseek-official") {
		t.Fatal("catalog membership")
	}
	last := opts[len(opts)-1]
	if last.Kind != "custom" {
		t.Fatalf("last=%+v", last)
	}
	if len(opts[0].Models) != 2 || opts[0].Models[0].ID != "deepseek-v4-flash" {
		t.Fatalf("official models=%+v", opts[0].Models)
	}
	var openai ProviderOption
	for _, p := range opts {
		if p.ID == "openai" {
			openai = p
			break
		}
	}
	if len(openai.Models) == 0 {
		t.Fatal("openai catalog models missing")
	}
}

func TestPluginsFile(t *testing.T) {
	got := PluginsFile([]string{" github:cocofhu/skillhub#main ", "", "@cocofhu/skillhub@0.2.9"})
	want := "github:cocofhu/skillhub#main\n@cocofhu/skillhub@0.2.9\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
}
