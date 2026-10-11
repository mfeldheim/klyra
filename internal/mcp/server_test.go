package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testServer() *Server {
	return New("klyra-test", "1.2.3", []Tool{
		{
			Name:        "echo",
			Description: "Echoes the provided text.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
			Call: func(_ context.Context, args json.RawMessage) (string, bool) {
				var p struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(args, &p); err != nil {
					return "bad args", true
				}
				return p.Text, false
			},
		},
	})
}

func post(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestInitializeNegotiatesVersion(t *testing.T) {
	rec := post(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	res := decode(t, rec)["result"].(map[string]any)
	if res["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want echo of 2024-11-05", res["protocolVersion"])
	}
	info := res["serverInfo"].(map[string]any)
	if info["name"] != "klyra-test" || info["version"] != "1.2.3" {
		t.Errorf("unexpected serverInfo: %v", info)
	}
}

func TestInitializeUnknownVersionFallsBack(t *testing.T) {
	rec := post(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	res := decode(t, rec)["result"].(map[string]any)
	if res["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want default %s", res["protocolVersion"], ProtocolVersion)
	}
}

func TestToolsList(t *testing.T) {
	rec := post(t, testServer(), `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	res := decode(t, rec)["result"].(map[string]any)
	tools := res["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "echo" {
		t.Errorf("tool name = %v", tool["name"])
	}
	if _, ok := tool["inputSchema"]; !ok {
		t.Error("tool missing inputSchema")
	}
}

func TestToolsCall(t *testing.T) {
	rec := post(t, testServer(), `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hello"}}}`)
	res := decode(t, rec)["result"].(map[string]any)
	content := res["content"].([]any)
	first := content[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "hello" {
		t.Errorf("unexpected content: %v", first)
	}
	if res["isError"] == true {
		t.Error("did not expect isError")
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	rec := post(t, testServer(), `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	errObj := decode(t, rec)["error"].(map[string]any)
	if errObj["code"].(float64) != -32602 {
		t.Errorf("error code = %v, want -32602", errObj["code"])
	}
}

func TestToolsCallToolError(t *testing.T) {
	s := New("klyra-test", "1", []Tool{{
		Name: "boom",
		Call: func(context.Context, json.RawMessage) (string, bool) { return "kaboom", true },
	}})
	rec := post(t, s, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"boom"}}`)
	res := decode(t, rec)["result"].(map[string]any)
	if res["isError"] != true {
		t.Error("expected isError=true for tool failure")
	}
}

func TestNotificationReturns202(t *testing.T) {
	rec := post(t, testServer(), `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body, got %q", rec.Body.String())
	}
}

func TestPing(t *testing.T) {
	rec := post(t, testServer(), `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, ok := decode(t, rec)["result"]; !ok {
		t.Error("expected result for ping")
	}
}

func TestUnknownMethod(t *testing.T) {
	rec := post(t, testServer(), `{"jsonrpc":"2.0","id":7,"method":"resources/list"}`)
	errObj := decode(t, rec)["error"].(map[string]any)
	if errObj["code"].(float64) != -32601 {
		t.Errorf("error code = %v, want -32601", errObj["code"])
	}
}

func TestMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Allow"), "POST") {
		t.Errorf("Allow header = %q, want POST", rec.Header().Get("Allow"))
	}
}

func TestBatchRequest(t *testing.T) {
	body := `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`
	rec := post(t, testServer(), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode batch: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d responses, want 2", len(out))
	}
}
