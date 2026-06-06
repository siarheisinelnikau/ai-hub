package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── fakes ────────────────────────────────────────────────────────

const fakePNG = "fake-png-bytes-123"
const fakeWAV = "fake-wav-bytes-456"
const fakeGLB = "fake-glb-bytes-789"
const fakeMP3 = "fake-mp3-bytes-000"

// fakeComfyUI emulates the ComfyUI API: submit → history → view.
func fakeComfyUI(t *testing.T, savedKey string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/prompt":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"prompt_id":"pid-1","number":1}`))
		case r.Method == "GET" && r.URL.Path == "/history/pid-1":
			out := map[string]interface{}{}
			if savedKey == "images" {
				out["9"] = map[string]interface{}{"images": []interface{}{map[string]interface{}{"filename": "mcp_img_1.png", "subfolder": ""}}}
			} else {
				out["9"] = map[string]interface{}{"gifs": []interface{}{map[string]interface{}{"filename": "mcp_vid_1.webm", "subfolder": ""}}}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"pid-1": map[string]interface{}{
				"status":  map[string]interface{}{"completed": true},
				"outputs": out,
			}})
		case r.Method == "GET" && r.URL.Path == "/view":
			w.Write([]byte(fakePNG))
		default:
			w.WriteHeader(404)
		}
	}))
}

// fakeComfyUIError emulates a ComfyUI that rejects the workflow.
func fakeComfyUIError(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/prompt" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"error":"invalid checkpoint","node_errors":{"2":{"message":"file not found"}}}`))
			return
		}
		w.WriteHeader(404)
	}))
}

func fakeVoice(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tts":
			w.Header().Set("Content-Type", "audio/wav")
			w.Write([]byte(fakeWAV))
		case "/stt":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"language":"en","text":"hello world"}`))
		case "/health":
			w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
}

func fakeThreed(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/gradio_api/call/generation_all":
			w.Write([]byte(`{"event_id":"ev-3d"}`))
		case r.Method == "GET" && r.URL.Path == "/gradio_api/queue/data":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte(`data: {"msg":"process_completed","success":true,"output":{"data":[{"path":"/workspace/save/model.glb","url":null,"name":"model.glb"}]}}` + "\n\n"))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/file="):
			w.Write([]byte(fakeGLB))
		case r.URL.Path == "/":
			w.Write([]byte("<html>hy3d</html>"))
		default:
			w.WriteHeader(404)
		}
	}))
}

func fakeMusic(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/release_task":
			w.Write([]byte(`{"data":{"task_id":"t-1","status":"queued"},"code":200}`))
		case r.Method == "POST" && r.URL.Path == "/query_result":
			w.Write([]byte(`{"data":[{"task_id":"t-1","status":1,"result":"[{\"file\": \"/v1/audio?path=%2Fout%2Fsong.mp3\"}]"}],"code":200}`))
		case r.Method == "GET" && r.URL.Path == "/v1/audio":
			w.Write([]byte(fakeMP3))
		case r.Method == "GET" && r.URL.Path == "/":
			w.Write([]byte("<html>ace</html>"))
		default:
			w.WriteHeader(404)
		}
	}))
}

// ── helpers ──────────────────────────────────────────────────────

func newTestAPI(t *testing.T, comfy, voice, music, threed string) *MediaAPI {
	t.Helper()
	cfg := MediaConfig{
		ComfyUI: comfy, Voice: voice, Music: music, Threed: threed,
		OutDir: t.TempDir(),
	}
	return NewMediaAPI(cfg)
}

func callTool(t *testing.T, s *Server, name string, args map[string]interface{}) *ToolCallResult {
	t.Helper()
	req := &JSONRPCRequest{JSONRPC: "2.0", Method: "tools/call"}
	b, _ := json.Marshal(map[string]interface{}{"name": name, "arguments": args})
	req.Params = b
	req.ID = rawMsg("1")

	var out *ToolCallResult
	s.HandleRequest(context.Background(), req, &captureWriter{get: func(b []byte) {
		var resp JSONRPCResponse
		if json.Unmarshal(b, &resp) != nil {
			t.Fatalf("bad response: %s", b)
		}
		if resp.Error != nil {
			t.Fatalf("rpc error: %+v", resp.Error)
		}
		raw, _ := json.Marshal(resp.Result)
		if json.Unmarshal(raw, &out) != nil {
			t.Fatalf("bad result: %s", raw)
		}
	}})
	if out == nil {
		t.Fatalf("no result for %s", name)
	}
	return out
}

func rawMsg(s string) *json.RawMessage {
	r := json.RawMessage(s)
	return &r
}

type captureWriter struct {
	buf []byte
	get func([]byte)
}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.buf = append(c.buf, p...)
	if c.get != nil {
		c.get(c.buf)
	}
	return len(p), nil
}

func blockOf(t *testing.T, r *ToolCallResult, typ string) ContentBlock {
	t.Helper()
	for _, b := range r.Content {
		if b.Type == typ {
			return b
		}
	}
	t.Fatalf("no %q block in %+v", typ, r.Content)
	return ContentBlock{}
}

// ── tests ────────────────────────────────────────────────────────

func TestInitializeAndToolsList(t *testing.T) {
	s := New()
	api := newTestAPI(t, "x", "x", "x", "x")
	api.RegisterTools(s)

	req := &JSONRPCRequest{JSONRPC: "2.0", Method: "initialize", ID: rawMsg("1")}
	var buf []byte
	s.HandleRequest(context.Background(), req, &captureWriter{get: func(b []byte) { buf = b }})

	var resp JSONRPCResponse
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, buf)
	}
	res, _ := resp.Result.(map[string]interface{})
	if res["protocolVersion"] == "" {
		t.Fatalf("missing protocolVersion: %v", res)
	}
	if res["serverInfo"] == nil {
		t.Fatal("missing serverInfo")
	}

	// tools/list
	req = &JSONRPCRequest{JSONRPC: "2.0", Method: "tools/list", ID: rawMsg("2")}
	buf = nil
	s.HandleRequest(context.Background(), req, &captureWriter{get: func(b []byte) { buf = b }})
	json.Unmarshal(buf, &resp)
	list, _ := resp.Result.(map[string]interface{})
	tools, _ := list["tools"].([]interface{})
	want := map[string]bool{}
	for _, tt := range tools {
		tm, _ := tt.(map[string]interface{})
		want[tm["name"].(string)] = true
	}
	for _, name := range []string{"generate_image", "generate_video", "text_to_speech",
		"speech_to_text", "generate_music", "image_to_3d", "media_status"} {
		if !want[name] {
			t.Fatalf("tool %s not registered; have %v", name, want)
		}
	}
}

func TestUnknownTool(t *testing.T) {
	s := New()
	req := &JSONRPCRequest{JSONRPC: "2.0", Method: "tools/call", ID: rawMsg("1")}
	req.Params = mustJSON(map[string]interface{}{"name": "nope"})
	var buf []byte
	s.HandleRequest(context.Background(), req, &captureWriter{get: func(b []byte) { buf = b }})
	var resp JSONRPCResponse
	json.Unmarshal(buf, &resp)
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("expected -32601, got %+v", resp.Error)
	}
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestGenerateImage(t *testing.T) {
	comfy := fakeComfyUI(t, "images")
	defer comfy.Close()
	api := newTestAPI(t, comfy.URL, "http://down", "http://down", "http://down")
	s := New()
	api.RegisterTools(s)

	r := callTool(t, s, "generate_image", map[string]interface{}{
		"prompt": "a fox", "width": 512, "height": 512,
	})
	if r.IsError {
		t.Fatalf("unexpected error: %+v", r.Content)
	}
	img := blockOf(t, r, "image")
	if img.MimeType != "image/png" {
		t.Fatalf("mime: %s", img.MimeType)
	}
	raw, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil || string(raw) != fakePNG {
		t.Fatalf("payload: %q err=%v", raw, err)
	}
}

func TestGenerateImageWorkflowError(t *testing.T) {
	comfy := fakeComfyUIError(t)
	defer comfy.Close()
	api := newTestAPI(t, comfy.URL, "http://down", "http://down", "http://down")
	s := New()
	api.RegisterTools(s)

	r := callTool(t, s, "generate_image", map[string]interface{}{"prompt": "x"})
	if !r.IsError {
		t.Fatalf("expected error result, got %+v", r.Content)
	}
	joined := ""
	for _, b := range r.Content {
		joined += b.Text
	}
	if !strings.Contains(joined, "file not found") {
		t.Fatalf("expected node error in message: %s", joined)
	}
}

func TestGenerateImageServiceDown(t *testing.T) {
	api := newTestAPI(t, "http://127.0.0.1:1", "http://down", "http://down", "http://down")
	s := New()
	api.RegisterTools(s)
	r := callTool(t, s, "generate_image", map[string]interface{}{"prompt": "x"})
	if !r.IsError {
		t.Fatalf("expected error, got %+v", r.Content)
	}
}

func TestGenerateVideo(t *testing.T) {
	comfy := fakeComfyUI(t, "gifs")
	defer comfy.Close()
	api := newTestAPI(t, comfy.URL, "http://down", "http://down", "http://down")
	s := New()
	api.RegisterTools(s)

	r := callTool(t, s, "generate_video", map[string]interface{}{
		"prompt": "waves", "frames": 49,
	})
	if r.IsError {
		t.Fatalf("unexpected error: %+v", r.Content)
	}
	v := blockOf(t, r, "video")
	if v.MimeType != "video/webm" {
		t.Fatalf("mime: %s", v.MimeType)
	}
}

func TestTextToSpeech(t *testing.T) {
	voice := fakeVoice(t)
	defer voice.Close()
	api := newTestAPI(t, "http://down", voice.URL, "http://down", "http://down")
	s := New()
	api.RegisterTools(s)

	r := callTool(t, s, "text_to_speech", map[string]interface{}{"text": "hi"})
	if r.IsError {
		t.Fatalf("unexpected error: %+v", r.Content)
	}
	a := blockOf(t, r, "audio")
	if a.MimeType != "audio/wav" {
		t.Fatalf("mime: %s", a.MimeType)
	}
	raw, _ := base64.StdEncoding.DecodeString(a.Data)
	if string(raw) != fakeWAV {
		t.Fatalf("payload: %q", raw)
	}
}

func TestSpeechToText(t *testing.T) {
	voice := fakeVoice(t)
	defer voice.Close()
	api := newTestAPI(t, "http://down", voice.URL, "http://down", "http://down")
	s := New()
	api.RegisterTools(s)

	r := callTool(t, s, "speech_to_text", map[string]interface{}{
		"audio_base64": base64.StdEncoding.EncodeToString([]byte("raw-audio")),
	})
	if r.IsError {
		t.Fatalf("unexpected error: %+v", r.Content)
	}
	joined := ""
	for _, b := range r.Content {
		joined += b.Text
	}
	if !strings.Contains(joined, "hello world") {
		t.Fatalf("missing transcript: %s", joined)
	}
}

func TestSpeechToTextMissingInput(t *testing.T) {
	api := newTestAPI(t, "http://down", "http://down", "http://down", "http://down")
	s := New()
	api.RegisterTools(s)
	r := callTool(t, s, "speech_to_text", map[string]interface{}{})
	if !r.IsError {
		t.Fatalf("expected error, got %+v", r.Content)
	}
}

func TestImageTo3D(t *testing.T) {
	three := fakeThreed(t)
	defer three.Close()
	api := newTestAPI(t, "http://down", "http://down", "http://down", three.URL)
	s := New()
	api.RegisterTools(s)

	r := callTool(t, s, "image_to_3d", map[string]interface{}{
		"image_base64": base64.StdEncoding.EncodeToString([]byte("png-bytes")),
	})
	if r.IsError {
		t.Fatalf("unexpected error: %+v", r.Content)
	}
	b := blockOf(t, r, "image") // GLB carried as binary image block
	if b.MimeType != "model/gltf-binary" {
		t.Fatalf("mime: %s", b.MimeType)
	}
	raw, _ := base64.StdEncoding.DecodeString(b.Data)
	if string(raw) != fakeGLB {
		t.Fatalf("payload: %q", raw)
	}
}

func TestGenerateMusic(t *testing.T) {
	music := fakeMusic(t)
	defer music.Close()
	api := newTestAPI(t, "http://down", "http://down", music.URL, "http://down")
	s := New()
	api.RegisterTools(s)

	r := callTool(t, s, "generate_music", map[string]interface{}{
		"prompt": "lo-fi beats", "lyrics": "la la la", "duration": 30,
	})
	if r.IsError {
		t.Fatalf("unexpected error: %+v", r.Content)
	}
	a := blockOf(t, r, "audio")
	if a.MimeType != "audio/mpeg" {
		t.Fatalf("mime: %s", a.MimeType)
	}
}

func TestMediaStatusAllDown(t *testing.T) {
	api := newTestAPI(t, "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1")
	s := New()
	api.RegisterTools(s)
	r := callTool(t, s, "media_status", map[string]interface{}{})
	if r.IsError {
		t.Fatalf("status should not be an error: %+v", r.Content)
	}
	joined := ""
	for _, b := range r.Content {
		joined += b.Text
	}
	if !strings.Contains(joined, "ComfyUI") || !strings.Contains(joined, "down") {
		t.Fatalf("bad status: %s", joined)
	}
}

func TestNotificationNoResponse(t *testing.T) {
	s := New()
	req := &JSONRPCRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	var buf []byte
	s.HandleRequest(context.Background(), req, &captureWriter{get: func(b []byte) { buf = b }})
	if len(buf) != 0 {
		t.Fatalf("notification must not produce a response, got %s", buf)
	}
}

// ── E-01: SafeRead path restriction ──────────────────────────────

func TestSafeReadWithinRoot(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "sub", "a.wav")
	if err := os.MkdirAll(filepath.Dir(inside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	api := NewMediaAPI(MediaConfig{OutDir: dir, ReadRoots: []string{dir}})

	b, name, err := api.SafeRead(inside)
	if err != nil {
		t.Fatalf("SafeRead(within) = %v", err)
	}
	if string(b) != "audio" || name != "a.wav" {
		t.Fatalf("got (%q,%q), want (audio,a.wav)", b, name)
	}
}

func TestSafeReadOutsideRootRejected(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir() // different temp dir
	p := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(p, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	api := NewMediaAPI(MediaConfig{OutDir: allowed, ReadRoots: []string{allowed}})

	if _, _, err := api.SafeRead(p); err == nil {
		t.Fatalf("SafeRead(outside) = nil error, want rejection")
	}
}

func TestSafeReadSymlinkEscapeRejected(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "flag")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	api := NewMediaAPI(MediaConfig{OutDir: allowed, ReadRoots: []string{allowed}})

	if _, _, err := api.SafeRead(link); err == nil {
		t.Fatalf("SafeRead(symlink-escape) = nil error, want rejection")
	}
}

func TestSafeReadDefaultRootsIncludeTempAndSrvAI(t *testing.T) {
	// Default config (NewMediaConfig) must allow the artifact dir and media
	// host dirs, and must NOT allow /srv/ai-hub (tokens) or /etc.
	cfg := NewMediaConfig("127.0.0.1")
	api := NewMediaAPI(cfg)

	tmp := filepath.Join(os.TempDir(), "e01_probe.bin")
	if err := os.WriteFile(tmp, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)
	if _, _, err := api.SafeRead(tmp); err != nil {
		t.Fatalf("SafeRead(tmp) = %v, want allowed (default root)", err)
	}
	// A path under /etc should be rejected by the default roots.
	if _, _, err := api.SafeRead("/etc/hostname"); err == nil {
		t.Fatalf("SafeRead(/etc/hostname) = nil error, want rejection")
	}
}
