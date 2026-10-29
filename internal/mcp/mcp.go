// Package mcp implements a Model Context Protocol (MCP) server that exposes
// ai-hub's media models (ComfyUI image/video, Kokoro TTS, Whisper STT,
// ACE-Step music, Hunyuan3D) as callable tools for LLMs.
// Supports both HTTP and stdio transports.
//
// MCP spec: https://spec.modelcontextprotocol.io/
// Tools are exposed via the "tools/list" and "tools/call" methods.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"aihub/internal/buildinfo"
)

var protocolVersion = "2025-06-18"

// ── MCP JSON-RPC types ───────────────────────────────────────────

type JSONRPCRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// HasID reports whether the request carries an "id" field (requests do,
// notifications don't).
func (r *JSONRPCRequest) HasID() bool { return r.ID != nil }

type JSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// ── MCP types ────────────────────────────────────────────────────

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is one piece of tool result content. Binary payloads
// (image/audio/video) are base64-encoded in Data per the MCP spec.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`     // base64 payload
	MimeType string `json:"mimeType,omitempty"` // e.g. image/png, audio/wav
}

func textBlock(s string) ContentBlock { return ContentBlock{Type: "text", Text: s} }
func imageBlock(b64, mime string) ContentBlock {
	return ContentBlock{Type: "image", Data: b64, MimeType: mime}
}
func audioBlock(b64, mime string) ContentBlock {
	return ContentBlock{Type: "audio", Data: b64, MimeType: mime}
}
func videoBlock(b64, mime string) ContentBlock {
	return ContentBlock{Type: "video", Data: b64, MimeType: mime}
}

// ── Tool registry & handler ──────────────────────────────────────

// ToolHandler processes a tool call and returns the result.
type ToolHandler func(ctx context.Context, args map[string]interface{}) (*ToolCallResult, error)

// Server is an MCP server that manages tools and handles JSON-RPC requests.
type Server struct {
	info         ServerInfo
	capabilities Capabilities
	tools        []Tool
	handlers     map[string]ToolHandler
	mu           sync.RWMutex
}

// New creates a new MCP server with standard ai-hub info.
func New() *Server {
	return &Server{
		info: ServerInfo{Name: "ai-hub-mcp", Version: buildinfo.Version},
		capabilities: Capabilities{
			Tools: &ToolsCapability{ListChanged: false},
		},
		handlers: make(map[string]ToolHandler),
	}
}

// Register adds a tool with its handler to the server.
func (s *Server) Register(t Tool, h ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, t)
	s.handlers[t.Name] = h
}

// ── JSON-RPC dispatch ────────────────────────────────────────────

// HandleRequest processes a single JSON-RPC request and writes the response.
// Notifications (no id) are processed but produce no response.
func (s *Server) HandleRequest(ctx context.Context, req *JSONRPCRequest, w io.Writer) {
	if !req.HasID() {
		if req.Method == "notifications/initialized" {
			return // pure notification, nothing to do
		}
	}

	var resp JSONRPCResponse
	resp.JSONRPC = "2.0"
	resp.ID = req.ID

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if req.Params != nil {
			json.Unmarshal(req.Params, &params)
		}
		if params.ProtocolVersion != "" {
			protocolVersion = params.ProtocolVersion
		}
		resp.Result = map[string]interface{}{
			"protocolVersion": protocolVersion,
			"serverInfo":      s.info,
			"capabilities":    s.capabilities,
		}
	case "notifications/initialized":
		return // notification — no response body
	case "tools/list":
		s.mu.RLock()
		tools := make([]Tool, len(s.tools))
		copy(tools, s.tools)
		s.mu.RUnlock()
		resp.Result = map[string]interface{}{"tools": tools}
	case "tools/call":
		var params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &RPCError{Code: -32602, Message: "Invalid params: " + err.Error()}
			break
		}
		s.mu.RLock()
		handler, ok := s.handlers[params.Name]
		s.mu.RUnlock()
		if !ok {
			resp.Error = &RPCError{Code: -32601, Message: "Unknown tool: " + params.Name}
			break
		}
		result, err := handler(ctx, params.Arguments)
		if err != nil {
			resp.Error = &RPCError{Code: -32000, Message: err.Error()}
			break
		}
		resp.Result = result
	case "ping":
		resp.Result = map[string]interface{}{}
	default:
		if req.HasID() {
			resp.Error = &RPCError{Code: -32601, Message: "Method not found: " + req.Method}
		} else {
			return // unknown notification — ignore
		}
	}

	if !req.HasID() {
		return
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	w.Write(data)
}

// ── HTTP transport ───────────────────────────────────────────────

// Handler returns an http.Handler that serves the MCP endpoint.
// POST /mcp — receives JSON-RPC requests, returns JSON-RPC responses
// GET  /mcp — SSE stream for server-initiated messages
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", s.handlePost)
	mux.HandleFunc("GET /mcp", s.handleSSE)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			OK      bool   `json:"ok"`
			Service string `json:"service"`
			buildinfo.Info
		}{OK: true, Service: "ai-hub-mcp", Info: buildinfo.Current()})
	})
	return mux
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: -32700, Message: "Parse error: " + err.Error()},
		}
		data, _ := json.Marshal(resp)
		w.Write(data)
		return
	}

	s.HandleRequest(r.Context(), &req, w)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprintf(w, "event: endpoint\ndata: /mcp\n\n")
	fl, _ := w.(http.Flusher)
	if fl != nil {
		fl.Flush()
	}
	<-r.Context().Done()
}

// ── Stdio transport (for local MCP clients like Claude Desktop) ──

// ServeStdio reads JSON-RPC messages from stdin and writes to stdout.
// Logs are expected to be directed to stderr (main does this).
func (s *Server) ServeStdio(ctx context.Context) {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var req JSONRPCRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("MCP decode error: %v", err)
			// Send a parse-error response if this looked like a request
			resp := JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &RPCError{Code: -32700, Message: "Parse error"},
			}
			encoder.Encode(resp)
			continue
		}
		s.HandleRequest(ctx, &req, os.Stdout)
	}
}
