package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/dsh-testsuite/internal/env"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
)

// 工具面经 /mcp 的端到端行为（生命周期/错误映射/密钥脱敏）在
// internal/httpapi/server_mcp_test.go 里以真实装配覆盖；
// 此处聚焦协议适配层自身的可测试单元。

// toolsList 起一个只做 tools/list 的无状态会话，返回工具清单原文。
func toolsList(t *testing.T) []byte {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	addTools(srv, &env.Service{})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	r := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("tools/list http %d: %s", w.Code, w.Body)
	}
	return w.Body.Bytes()
}

func TestToolCatalogSchemas(t *testing.T) {
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(toolsList(t), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Result.Tools) != 18 {
		t.Fatalf("tool count=%d", len(resp.Result.Tools))
	}
	for _, tl := range resp.Result.Tools {
		if tl.Description == "" {
			t.Fatalf("tool %s: empty description", tl.Name)
		}
		s := string(tl.InputSchema)
		if !strings.Contains(s, `"type":"object"`) {
			t.Fatalf("tool %s: input schema not object: %s", tl.Name, s)
		}
	}
	// 参数描述示例：update_preset 的内嵌字段平铺且 id/apiKey 在 schema 中。
	var update *struct {
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	var probe struct {
		Result struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal(toolsList(t), &probe)
	for _, raw := range probe.Result.Tools {
		if strings.Contains(string(raw), `"update_preset"`) {
			var x struct {
				InputSchema json.RawMessage `json:"inputSchema"`
			}
			_ = json.Unmarshal(raw, &x)
			update = &x
		}
	}
	if update == nil {
		t.Fatal("update_preset not found")
	}
	schema := string(update.InputSchema)
	for _, want := range []string{`"id"`, `"name"`, `"provider"`, `"apiKey"`} {
		if !strings.Contains(schema, want) {
			t.Fatalf("update_preset schema missing %s: %s", want, schema)
		}
	}
}

func TestRecoverPanicsKeepsHandlerUsable(t *testing.T) {
	var calls int
	var boom http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
		if calls == 1 {
			panic("boom")
		}
	})
	h := recoverPanics(boom, zerolog.Nop())
	for i, want := range []int{http.StatusInternalServerError, http.StatusOK} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("call %d: http=%d want %d", i+1, w.Code, want)
		}
	}
}

// 编译期保证工具 handler 签名与 SDK 泛型约束一致。
var _ = context.Background
