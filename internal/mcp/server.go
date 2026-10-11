// Package mcp implements a minimal, stateless Model Context Protocol (MCP)
// server using the Streamable HTTP transport (JSON-RPC 2.0 over POST /mcp).
//
// It supports the subset of MCP needed by remote tool clients: initialize,
// notifications/initialized, ping, tools/list and tools/call. Responses are
// returned as application/json (a valid Streamable HTTP response type).
package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ProtocolVersion is the MCP protocol version this server implements.
const ProtocolVersion = "2025-06-18"

// supportedVersions lists protocol versions the server can echo back to a client.
var supportedVersions = map[string]bool{
	"2025-06-18": true,
	"2025-03-26": true,
	"2024-11-05": true,
}

const maxBodyBytes = 4 << 20 // 4 MiB

// Tool is a callable tool exposed over MCP.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	// Call executes the tool. It returns the textual result and whether the
	// result represents a tool-level error (isError in the MCP response).
	Call func(ctx context.Context, args json.RawMessage) (string, bool)
}

// Server is a stateless MCP server.
type Server struct {
	name    string
	version string
	tools   []Tool
	byName  map[string]Tool
}

// New creates a Server with the given server name/version and tools.
func New(name, version string, tools []Tool) *Server {
	byName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}
	return &Server{name: name, version: version, tools: tools, byName: byName}
}

// --- JSON-RPC wire types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (r rpcRequest) isNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

// ServeHTTP implements the Streamable HTTP transport entry point.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return
	}

	var responses []rpcResponse
	if strings.HasPrefix(trimmed, "[") {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil {
			writeRPC(w, http.StatusBadRequest, rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			return
		}
		for _, raw := range batch {
			if resp, ok := s.dispatchRaw(r.Context(), raw); ok {
				responses = append(responses, resp)
			}
		}
	} else {
		if resp, ok := s.dispatchRaw(r.Context(), body); ok {
			responses = append(responses, resp)
		}
	}

	// Only notifications in the request — per spec, respond 202 with no body.
	if len(responses) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if len(responses) == 1 {
		writeRPC(w, http.StatusOK, responses[0])
		return
	}
	writeRPC(w, http.StatusOK, responses)
}

func (s *Server) dispatchRaw(ctx context.Context, raw json.RawMessage) (rpcResponse, bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
		}, true
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32600, Message: "invalid request: unsupported jsonrpc version"},
		}, true
	}

	// Notifications never produce a response.
	if req.isNotification() {
		s.handleNotification(req)
		return rpcResponse{}, false
	}

	result, rpcErr := s.handleRequest(ctx, req)
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}, true
}

func (s *Server) handleNotification(req rpcRequest) {
	// All supported notifications (notifications/initialized, notifications/cancelled)
	// require no action in a stateless server.
	_ = req
}

func (s *Server) handleRequest(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.toolList()}, nil
	case "tools/call":
		return s.callTool(ctx, req.Params)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
}

func (s *Server) initialize(params json.RawMessage) map[string]any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p) //nolint:errcheck

	version := ProtocolVersion
	if supportedVersions[p.ProtocolVersion] {
		version = p.ProtocolVersion
	}

	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    s.name,
			"version": s.version,
		},
		"instructions": "Read-only Kubernetes and klyra diagnostics. Use the tools to investigate " +
			"firing monitors: inspect pods, logs, events, nodes and metrics, then correlate with " +
			"klyra monitors, incidents, silences and history.",
	}
}

func (s *Server) toolList() []map[string]any {
	out := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": schema,
		})
	}
	return out
}

func (s *Server) callTool(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
	}
	tool, ok := s.byName[p.Name]
	if !ok {
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
	}

	text, isErr := tool.Call(ctx, p.Arguments)
	if text == "" {
		text = "(no output)"
	}
	result := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	}
	if isErr {
		result["isError"] = true
	}
	return result, nil
}

func writeRPC(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
