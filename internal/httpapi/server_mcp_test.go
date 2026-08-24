package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /mcp 的 JSON-RPC 冒烟测试：initialize → tools/list → tools/call 全链路，
// 与 REST 共用同一 Server 装配（同一 env.Service / 存储）。

type mcpClient struct {
	t       *testing.T
	h       http.Handler
	session string
	nextID  int
}

func newMCPClient(t *testing.T, h http.Handler) (*mcpClient, map[string]any) {
	t.Helper()
	c := &mcpClient{t: t, h: h}
	res := c.call("initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test", "version": "0"},
	})
	c.notify("notifications/initialized")
	return c, res
}

func (c *mcpClient) post(body string) (int, http.Header, []byte) {
	c.t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if c.session != "" {
		r.Header.Set("Mcp-Session-Id", c.session)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w.Code, w.Header(), w.Body.Bytes()
}

func (c *mcpClient) call(method string, params any) map[string]any {
	c.t.Helper()
	c.nextID++
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params,
	})
	code, hdr, body := c.post(string(raw))
	if code != http.StatusOK {
		c.t.Fatalf("%s: http %d: %s", method, code, body)
	}
	if s := hdr.Get("Mcp-Session-Id"); s != "" {
		c.session = s
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		c.t.Fatalf("%s: bad json %v: %s", method, err, body)
	}
	if resp.Error != nil {
		c.t.Fatalf("%s: rpc error %d %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result
}

func (c *mcpClient) notify(method string) {
	c.t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	code, _, body := c.post(string(raw))
	if code != http.StatusAccepted {
		c.t.Fatalf("notify %s: http %d: %s", method, code, body)
	}
}

// tool 调一个工具，返回 structuredContent（或 text 兜底）与 isError。
func (c *mcpClient) tool(t *testing.T, name string, args map[string]any) (any, bool) {
	t.Helper()
	res := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	isErr, _ := res["isError"].(bool)
	if res["structuredContent"] != nil {
		return res["structuredContent"], isErr
	}
	content, _ := res["content"].([]any)
	for _, item := range content {
		if m, ok := item.(map[string]any); ok {
			if text, ok := m["text"].(string); ok {
				var v any
				if json.Unmarshal([]byte(text), &v) == nil {
					return v, isErr
				}
				return text, isErr
			}
		}
	}
	return nil, isErr
}

// toolOK 调一个必须成功的工具。
func (c *mcpClient) toolOK(t *testing.T, name string, args map[string]any) any {
	t.Helper()
	out, isErr := c.tool(t, name, args)
	if isErr {
		t.Fatalf("tool %s failed: %v", name, out)
	}
	if out == nil {
		t.Fatalf("tool %s: empty result", name)
	}
	return out
}

// toolFail 调一个必须以 tool error（isError=true）失败的工具并返回错误文本。
func (c *mcpClient) toolFail(t *testing.T, name string, args map[string]any) string {
	t.Helper()
	out, isErr := c.tool(t, name, args)
	if !isErr {
		t.Fatalf("tool %s should fail: %v", name, out)
	}
	if s, ok := out.(string); ok {
		return s
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// toolMap 调一个必须成功且返回对象的工具。
func (c *mcpClient) toolMap(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	out := c.toolOK(t, name, args)
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("tool %s: expected object, got %T", name, out)
	}
	return m
}

// toolSlice 调一个必须成功且返回数组的工具。
func (c *mcpClient) toolSlice(t *testing.T, name string, args map[string]any) []any {
	t.Helper()
	out := c.toolOK(t, name, args)
	a, ok := out.([]any)
	if !ok {
		t.Fatalf("tool %s: expected array, got %T", name, out)
	}
	return a
}

var wantMCPTools = []string{
	// 环境（9）
	"list_environments", "create_environment", "get_environment", "start_environment",
	"stop_environment", "restart_environment", "renew_environment", "destroy_environment",
	"get_environment_logs",
	// 镜像（4）
	"list_images", "list_remote_images", "register_image", "delete_image",
	// 预设（4）+ provider（1）
	"list_presets", "create_preset", "update_preset", "delete_preset", "list_providers",
}

func TestMCPInitializeAndToolCatalog(t *testing.T) {
	s, _ := testAPI(t)
	h := s.Handler()
	c, res := newMCPClient(t, h)
	info, _ := res["serverInfo"].(map[string]any)
	if info == nil || info["name"] != "dsh-testsuite" {
		t.Fatalf("serverInfo=%v", res["serverInfo"])
	}
	caps, _ := res["capabilities"].(map[string]any)
	if caps["tools"] == nil {
		t.Fatalf("capabilities missing tools: %v", caps)
	}

	c.call("ping", map[string]any{})

	names := make([]string, 0)
	if tools, ok := c.call("tools/list", map[string]any{})["tools"].([]any); ok {
		for _, tl := range tools {
			if m, ok := tl.(map[string]any); ok {
				names = append(names, m["name"].(string))
			}
		}
	}
	if len(names) != len(wantMCPTools) {
		t.Fatalf("tool count=%d (%v)", len(names), names)
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, w := range wantMCPTools {
		if !got[w] {
			t.Fatalf("missing tool %q in %v", w, names)
		}
	}

	// 未知工具是 JSON-RPC invalid params，而非 5xx。
	c.nextID++
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": c.nextID, "method": "tools/call",
		"params": map[string]any{"name": "nope", "arguments": map[string]any{}},
	})
	code, _, body := c.post(string(raw))
	if code != http.StatusOK {
		t.Fatalf("unknown tool http %d: %s", code, body)
	}
	if !bytes.Contains(body, []byte("unknown tool")) {
		t.Fatalf("unknown tool body: %s", body)
	}
}

func TestMCPEnvironmentLifecycle(t *testing.T) {
	s, fake := testAPI(t)
	h := s.Handler()
	c, _ := newMCPClient(t, h)

	// 未登记版本 → 与 REST 相同的校验错误。
	msg := c.toolFail(t, "create_environment", map[string]any{
		"name": "e1", "dshVersion": "9.9.9", "apiKey": "sk-secret-key",
		"provider": "deepseek-official", "model": "deepseek-v4-flash",
	})
	if !strings.Contains(msg, "image version not configured") {
		t.Fatalf("unconfigured error: %q", msg)
	}

	// 参数校验错误（缺 name）→ tool error 且带参数说明，不 panic。
	msg = c.toolFail(t, "create_environment", map[string]any{
		"dshVersion": "0.1.0-rc.7", "apiKey": "k", "provider": "deepseek-official", "model": "m",
	})
	if !strings.Contains(msg, "name") {
		t.Fatalf("validation error: %q", msg)
	}

	c.toolOK(t, "register_image", map[string]any{"version": "0.1.0-rc.7", "pull": false})

	created := c.toolMap(t, "create_environment", map[string]any{
		"name": "e1", "dshVersion": "0.1.0-rc.7", "apiKey": "sk-secret-key",
		"provider": "deepseek-official", "model": "deepseek-v4-flash",
		"plugins": []string{"github:cocofhu/skillhub#main"},
	})
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created=%v", created)
	}
	if created["apiKeyHint"] != "****-key" {
		t.Fatalf("apiKeyHint=%v", created["apiKeyHint"])
	}

	// 与 REST 共享存储：MCP 创建的环境立即可经 REST 读到。
	restGet := func() int {
		w := do(t, h, "GET", "/api/environments/"+id, nil)
		return w.Code
	}
	if restGet() != http.StatusOK {
		t.Fatalf("REST get after MCP create: %d", restGet())
	}

	if arr := c.toolSlice(t, "list_environments", nil); len(arr) != 1 {
		t.Fatalf("list_environments=%v", arr)
	}

	got := c.toolMap(t, "get_environment", map[string]any{"id": id})
	if got["id"] != id {
		t.Fatalf("get=%v", got)
	}

	logs := c.toolMap(t, "get_environment_logs", map[string]any{"id": id})
	if s2, _ := logs["logs"].(string); !strings.Contains(s2, "line1") {
		t.Fatalf("logs=%v", logs)
	}
	tailLogs := c.toolMap(t, "get_environment_logs", map[string]any{"id": id, "tail": 1})
	if s2, _ := tailLogs["logs"].(string); !strings.Contains(s2, "line1") {
		t.Fatalf("tail logs=%v", tailLogs)
	}

	renewed := c.toolMap(t, "renew_environment", map[string]any{"id": id})
	if renewed["destroyAt"] == nil {
		t.Fatalf("renew=%v", renewed)
	}

	for _, name := range []string{"stop_environment", "start_environment", "restart_environment"} {
		out := c.toolMap(t, name, map[string]any{"id": id})
		if out["id"] != id {
			t.Fatalf("%s=%v", name, out)
		}
	}
	if len(fake.handles) != 1 {
		t.Fatalf("handles=%v", fake.handles)
	}

	destroyed := c.toolMap(t, "destroy_environment", map[string]any{"id": id})
	if destroyed["status"] != "destroyed" {
		t.Fatalf("destroy=%v", destroyed)
	}
	if restGet() != http.StatusNotFound {
		t.Fatalf("REST get after destroy: %d", restGet())
	}

	// not-found 语义与 REST 一致。
	for _, name := range []string{"get_environment", "start_environment", "stop_environment",
		"restart_environment", "renew_environment", "destroy_environment", "get_environment_logs"} {
		msg := c.toolFail(t, name, map[string]any{"id": "nope"})
		if !strings.Contains(msg, "environment not found") {
			t.Fatalf("%s notfound error: %q", name, msg)
		}
	}
}

func TestMCPArgumentTypeMismatchDoesNotPanic(t *testing.T) {
	s, _ := testAPI(t)
	h := s.Handler()
	c, _ := newMCPClient(t, h)

	// name 传数字 → 参数校验失败，返回 tool error 而非 panic。
	msg := c.toolFail(t, "create_environment", map[string]any{
		"name": 42, "dshVersion": "0.1.0-rc.7",
	})
	if !strings.Contains(msg, "name") {
		t.Fatalf("type mismatch error: %q", msg)
	}

	// 之后 REST 仍可用。
	w := do(t, h, "GET", "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("healthz after bad args: %d %s", w.Code, w.Body)
	}
}

func TestMCPImages(t *testing.T) {
	s, fake := testAPI(t)
	h := s.Handler()
	c, _ := newMCPClient(t, h)

	if arr := c.toolSlice(t, "list_images", nil); len(arr) != 0 {
		t.Fatalf("images initially=%v", arr)
	}

	// 默认 pull=true：本地缺失时从 GHCR 拉取并打 tag。
	reg := c.toolMap(t, "register_image", map[string]any{"version": "0.1.1-rc.2"})
	wantPull := "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.1-rc.2"
	if len(fake.pulled) != 1 || fake.pulled[0] != wantPull {
		t.Fatalf("pulled=%v", fake.pulled)
	}
	if len(fake.tagged) != 1 || fake.tagged[0] != wantPull+" dsh-testsuite-runtime:0.1.1-rc.2" {
		t.Fatalf("tagged=%v", fake.tagged)
	}
	if reg["present"] != true {
		t.Fatalf("register=%v", reg)
	}

	remote := c.toolOK(t, "list_remote_images", nil)
	raw, _ := json.Marshal(remote)
	for _, want := range []string{"0.1.0-rc.6", "0.1.0-rc.7", "0.1.0-rc.8", "0.1.1-rc.1", "0.1.1-rc.2"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("remote missing %s: %s", want, raw)
		}
	}

	imgs := c.toolOK(t, "list_images", nil)
	raw, _ = json.Marshal(imgs)
	if !strings.Contains(string(raw), `"present":true`) {
		t.Fatalf("list after register: %s", raw)
	}

	del := c.toolMap(t, "delete_image", map[string]any{"version": "0.1.1-rc.2"})
	if del["status"] != "deleted" {
		t.Fatalf("delete=%v", del)
	}
	msg := c.toolFail(t, "delete_image", map[string]any{"version": "nope"})
	if !strings.Contains(msg, "image version not configured") {
		t.Fatalf("delete missing error: %q", msg)
	}
}

func TestMCPPresetsAndProviders(t *testing.T) {
	s, _ := testAPI(t)
	h := s.Handler()
	c, _ := newMCPClient(t, h)

	providers := c.toolMap(t, "list_providers", nil)
	raw, _ := json.Marshal(providers)
	for _, want := range []string{"deepseek-official", "amazon-bedrock", "deepseek-v4-flash", "custom"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("providers missing %s: %s", want, raw)
		}
	}

	created := c.toolMap(t, "create_preset", map[string]any{
		"name": "flash", "provider": "deepseek-official", "model": "deepseek-v4-flash",
		"apiKey": "sk-secret-key",
	})
	pid, _ := created["id"].(string)
	if created["apiKeyHint"] != "****-key" {
		t.Fatalf("preset=%v", created)
	}
	if strings.Contains(string(raw), "sk-secret-key") {
		t.Fatalf("preset leaked key: %s", raw)
	}

	// update 留空 apiKey → 保留原密钥（用该预设建环境验证 hint 不变）。
	updated := c.toolMap(t, "update_preset", map[string]any{
		"id": pid, "name": "flash2", "provider": "deepseek-official", "model": "deepseek-v4-pro",
	})
	if updated["name"] != "flash2" || updated["apiKeyHint"] != "****-key" {
		t.Fatalf("updated=%v", updated)
	}

	c.toolOK(t, "register_image", map[string]any{"version": "0.1.0-rc.7", "pull": false})
	envOut := c.toolMap(t, "create_environment", map[string]any{
		"name": "from-preset", "dshVersion": "0.1.0-rc.7", "presetId": pid,
	})
	if envOut["apiKeyHint"] != "****-key" || envOut["model"] != "deepseek-v4-pro" {
		t.Fatalf("env from preset=%v", envOut)
	}

	// list_presets / list_environments 均不回显完整密钥。
	for _, name := range []string{"list_presets", "list_environments"} {
		b, _ := json.Marshal(c.toolOK(t, name, nil))
		if strings.Contains(string(b), "sk-secret-key") {
			t.Fatalf("%s leaked key: %s", name, b)
		}
	}

	msg := c.toolFail(t, "update_preset", map[string]any{
		"id": "nope", "name": "x", "provider": "deepseek-official", "model": "m",
	})
	if !strings.Contains(msg, "not found") {
		t.Fatalf("update missing error: %q", msg)
	}

	del := c.toolMap(t, "delete_preset", map[string]any{"id": pid})
	if del["status"] != "deleted" {
		t.Fatalf("delete=%v", del)
	}
	msg = c.toolFail(t, "delete_preset", map[string]any{"id": pid})
	if !strings.Contains(msg, "not found") {
		t.Fatalf("delete missing error: %q", msg)
	}
}
