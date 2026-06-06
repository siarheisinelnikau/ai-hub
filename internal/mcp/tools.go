package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ── Media service endpoints ──────────────────────────────────────

// MediaConfig holds base URLs for ai-hub media services.
type MediaConfig struct {
	ComfyUI string // ComfyUI API, e.g. http://127.0.0.1:8188
	Voice   string // Kokoro TTS + Whisper STT, e.g. http://127.0.0.1:8010
	Music   string // ACE-Step (Gradio UI), e.g. http://127.0.0.1:8016
	Threed  string // Hunyuan3D (Gradio app), e.g. http://127.0.0.1:8020
	// OutDir is where generated artifacts are also saved as files.
	OutDir string
	// ThreedOutDir is the host-side mount of the Hunyuan3D output dir
	// (used as a fallback when the file cannot be fetched over HTTP).
	ThreedOutDir string
	// ReadRoots restricts the audio_path/image_path tool arguments (E-01):
	// clients may only read files under one of these roots, not arbitrary
	// host paths (the server runs as root — without the restriction an MCP
	// client could read /srv/ai-hub/token, /root/.ssh, …).
	ReadRoots []string
}

// NewMediaConfig builds per-service URLs from a single host.
func NewMediaConfig(host string) MediaConfig {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	host = strings.TrimRight(host, "/")
	return MediaConfig{
		ComfyUI:      host + ":8188",
		Voice:        host + ":8010",
		Music:        host + ":8017", // ACE-Step REST API (gradio UI on 8016 is optional)
		Threed:       host + ":8020",
		OutDir:       os.TempDir(),
		// E-01 default read area: artifacts + media host dirs + user files.
		// Deliberately NOT /srv/ai-hub (tokens), /root, /etc.
		ReadRoots:    []string{os.TempDir(), "/srv/ai", "/home/user"},
		ThreedOutDir: "/srv/ai/threed/outputs",
	}
}

// MediaAPI is the client-side implementation of all media tools.
type MediaAPI struct {
	Cfg    MediaConfig
	HTTP   *http.Client
	HTTPLo *http.Client // long-running generations
}

// NewMediaAPI creates the media API client.
func NewMediaAPI(cfg MediaConfig) *MediaAPI {
	if cfg.OutDir == "" {
		cfg.OutDir = os.TempDir()
	}
	return &MediaAPI{
		Cfg:    cfg,
		HTTP:   &http.Client{Timeout: 30 * time.Second},
		HTTPLo: &http.Client{Timeout: 40 * time.Minute},
	}
}

// RegisterTools registers all media tools with the MCP server.
func (m *MediaAPI) RegisterTools(s *Server) {
	m.registerImageTools(s)
	m.registerAudioTools(s)
	m.register3DTools(s)
	s.Register(Tool{
		Name:        "media_status",
		Description: "Check which media services (ComfyUI, voice, music, 3D) are running and reachable on ai-hub.",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}, m.status)
}

func errResult(format string, a ...interface{}) *ToolCallResult {
	return &ToolCallResult{
		Content: []ContentBlock{textBlock(fmt.Sprintf(format, a...))},
		IsError: true,
	}
}

func strArg(args map[string]interface{}, key, def string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return def
}

func intArg(args map[string]interface{}, key string, def int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case string:
			if i, err := strconv.Atoi(n); err == nil {
				return i
			}
		}
	}
	return def
}

func floatArg(args map[string]interface{}, key string, def float64) float64 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case string:
			if f, err := strconv.ParseFloat(n, 64); err == nil {
				return f
			}
		}
	}
	return def
}

func boolArg(args map[string]interface{}, key string, def bool) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// SafeRead (E-01) reads a client-supplied host path, but only if it resolves
// to a location under one of Cfg.ReadRoots. The MCP server runs as root, so
// an unrestricted path argument would let any connected client read
// /srv/ai-hub/token, /root/.ssh, /etc/shadow, … — the restriction is the
// access rule for the audio_path / image_path arguments. Returns (data, name).
func (m *MediaAPI) SafeRead(p string) ([]byte, string, error) {
	ap, err := filepath.Abs(p)
	if err != nil {
		return nil, "", err
	}
	// Resolve symlinks so a link inside an allowed root can't point outside it.
	if rp, e := filepath.EvalSymlinks(ap); e == nil {
		ap = rp
	}
	roots := m.Cfg.ReadRoots
	if len(roots) == 0 {
		roots = []string{m.Cfg.OutDir, "/srv/ai", "/home/user"}
	}
	for _, r := range roots {
		rr, err2 := filepath.Abs(r)
		if err2 != nil {
			continue
		}
		if ap == rr || strings.HasPrefix(ap, rr+string(os.PathSeparator)) {
			b, err3 := os.ReadFile(ap)
			if err3 != nil {
				return nil, "", err3
			}
			return b, filepath.Base(ap), nil
		}
	}
	return nil, "", fmt.Errorf("path %q is outside the allowed read roots (%s)", p, strings.Join(roots, ", "))
}

// saveArtifact writes data to OutDir under a name and returns the path.
func (m *MediaAPI) saveArtifact(name string, data []byte) (string, error) {
	if err := os.MkdirAll(m.Cfg.OutDir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(m.Cfg.OutDir, name)
	return p, os.WriteFile(p, data, 0o644)
}

// ── HTTP helpers ─────────────────────────────────────────────────

// do performs an HTTP request and returns body + status code.
func (m *MediaAPI) do(ctx context.Context, client *http.Client, method, u string, headers map[string]string, body []byte) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	return data, resp.StatusCode, err
}

func (m *MediaAPI) postJSON(ctx context.Context, u string, v interface{}) ([]byte, int, error) {
	body, _ := json.Marshal(v)
	return m.do(ctx, m.HTTP, "POST", u, map[string]string{"Content-Type": "application/json"}, body)
}

func (m *MediaAPI) get(ctx context.Context, u string) ([]byte, int, error) {
	return m.do(ctx, m.HTTP, "GET", u, nil, nil)
}

// ── ComfyUI: image + video generation ────────────────────────────

func (m *MediaAPI) registerImageTools(s *Server) {
	s.Register(Tool{
		Name: "generate_image",
		Description: "Generate an image from a text prompt using FLUX.2-dev (fp8, + Flux2Turbo LoRA) on the local ComfyUI. " +
			"Returns the PNG as base64 (type=image) and saves it under the out dir. " +
			"FLUX.2 is guidance-distilled (no negative prompt). Typical generation time: 30-120s.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt":   map[string]interface{}{"type": "string", "description": "Text description of the image"},
				"negative": map[string]interface{}{"type": "string", "description": "Negative prompt (accepted but ignored — FLUX.2 has no negative path)", "default": ""},
				"width":    map[string]interface{}{"type": "integer", "description": "Width, multiple of 8", "default": 1024},
				"height":   map[string]interface{}{"type": "integer", "description": "Height, multiple of 8", "default": 1024},
				"steps":    map[string]interface{}{"type": "integer", "description": "Sampling steps (Flux2Turbo recipe: 8)", "default": 8},
				"cfg":      map[string]interface{}{"type": "number", "description": "Guidance (FLUX.2: 3.5 recommended, ~4 max)", "default": 3.5},
				"seed":     map[string]interface{}{"type": "integer", "description": "Seed (-1 = random)", "default": -1},
			},
			"required": []string{"prompt"},
		},
	}, m.image)

	s.Register(Tool{
		Name: "generate_video",
		Description: "Generate a short video (webm, vp9) from a text prompt using Wan 2.1 T2V 1.3B on the local ComfyUI. " +
			"Returns the video as base64 (type=video). Frames must be a multiple of 4 (49≈3s, 81≈5s). " +
			"Typical generation time: 1-10 min.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt":   map[string]interface{}{"type": "string", "description": "Text description of the video"},
				"negative": map[string]interface{}{"type": "string", "description": "Negative prompt", "default": ""},
				"width":    map[string]interface{}{"type": "integer", "description": "Width, multiple of 16", "default": 832},
				"height":   map[string]interface{}{"type": "integer", "description": "Height, multiple of 16", "default": 480},
				"frames":   map[string]interface{}{"type": "integer", "description": "Frame count, multiple of 4", "default": 49},
				"fps":      map[string]interface{}{"type": "integer", "description": "Output fps", "default": 16},
				"steps":    map[string]interface{}{"type": "integer", "description": "Sampling steps", "default": 20},
				"seed":     map[string]interface{}{"type": "integer", "description": "Seed (-1 = random)", "default": -1},
			},
			"required": []string{"prompt"},
		},
	}, m.video)
}

// fluxWorkflow — FLUX.2-dev (fp8mixed) on the local ComfyUI.
// FLUX.2 is guidance-distilled: NO negative prompt — it uses FluxGuidance instead of CFG,
// a single Mistral-3-Small text encoder (type "flux2"), and the Flux2Turbo distilled LoRA
// (8-step recipe, per the official image_flux2_fp8 template). Matches the live-verified pipeline.
func fluxWorkflow(prompt string, width, height, steps, seed int, guidance float64) map[string]interface{} {
	return map[string]interface{}{
		"1": map[string]interface{}{"class_type": "CLIPLoader", "inputs": map[string]interface{}{
			"clip_name": "mistral_3_small_flux2_fp8.safetensors", "type": "flux2",
		}},
		"2": map[string]interface{}{"class_type": "UNETLoader", "inputs": map[string]interface{}{
			"unet_name": "flux2_dev_fp8mixed.safetensors", "weight_dtype": "default",
		}},
		"3": map[string]interface{}{"class_type": "LoraLoaderModelOnly", "inputs": map[string]interface{}{
			"model": []interface{}{"2", 0}, "lora_name": "Flux2TurboComfyv2.safetensors", "strength_model": 1.0,
		}},
		"4": map[string]interface{}{"class_type": "VAELoader", "inputs": map[string]interface{}{
			"vae_name": "flux2-vae.safetensors",
		}},
		"5": map[string]interface{}{"class_type": "CLIPTextEncode", "inputs": map[string]interface{}{
			"text": prompt, "clip": []interface{}{"1", 0},
		}},
		"6": map[string]interface{}{"class_type": "FluxGuidance", "inputs": map[string]interface{}{
			"conditioning": []interface{}{"5", 0}, "guidance": guidance,
		}},
		"7": map[string]interface{}{"class_type": "EmptyFlux2LatentImage", "inputs": map[string]interface{}{
			"width": width, "height": height, "batch_size": 1,
		}},
		"8": map[string]interface{}{"class_type": "BasicGuider", "inputs": map[string]interface{}{
			"model": []interface{}{"3", 0}, "conditioning": []interface{}{"6", 0},
		}},
		"9": map[string]interface{}{"class_type": "KSamplerSelect", "inputs": map[string]interface{}{
			"sampler_name": "euler",
		}},
		"10": map[string]interface{}{"class_type": "Flux2Scheduler", "inputs": map[string]interface{}{
			"steps": steps, "width": width, "height": height,
		}},
		"11": map[string]interface{}{"class_type": "RandomNoise", "inputs": map[string]interface{}{
			"noise_seed": seed,
		}},
		"12": map[string]interface{}{"class_type": "SamplerCustomAdvanced", "inputs": map[string]interface{}{
			"noise": []interface{}{"11", 0}, "guider": []interface{}{"8", 0},
			"sampler": []interface{}{"9", 0}, "sigmas": []interface{}{"10", 0},
			"latent_image": []interface{}{"7", 0},
		}},
		"13": map[string]interface{}{"class_type": "VAEDecode", "inputs": map[string]interface{}{
			"samples": []interface{}{"12", 0}, "vae": []interface{}{"4", 0},
		}},
		"14": map[string]interface{}{"class_type": "SaveImage", "inputs": map[string]interface{}{
			"images": []interface{}{"13", 0}, "filename_prefix": "mcp_img",
		}},
	}
}

func wanWorkflow(prompt, negative string, width, height, frames, fps, steps, seed int) map[string]interface{} {
	return map[string]interface{}{
		"1": map[string]interface{}{"class_type": "CLIPLoader", "inputs": map[string]interface{}{
			"clip_name": "umt5_xxl_fp8.safetensors", "type": "wan",
		}},
		"2": map[string]interface{}{"class_type": "UNETLoader", "inputs": map[string]interface{}{
			"unet_name": "wan2.1_t2v_1.3B_bf16.safetensors", "weight_dtype": "default",
		}},
		"3": map[string]interface{}{"class_type": "VAELoader", "inputs": map[string]interface{}{
			"vae_name": "wan2.1_vae.safetensors",
		}},
		"4": map[string]interface{}{"class_type": "CLIPTextEncode", "inputs": map[string]interface{}{
			"text": prompt, "clip": []interface{}{"1", 0},
		}},
		"5": map[string]interface{}{"class_type": "CLIPTextEncode", "inputs": map[string]interface{}{
			"text": negative, "clip": []interface{}{"1", 0},
		}},
		"6": map[string]interface{}{"class_type": "WanFirstLastFrameToVideo", "inputs": map[string]interface{}{
			"positive": []interface{}{"4", 0}, "negative": []interface{}{"5", 0}, "vae": []interface{}{"3", 0},
			"width": width, "height": height, "length": frames, "batch_size": 1,
		}},
		"7": map[string]interface{}{"class_type": "KSampler", "inputs": map[string]interface{}{
			"model": []interface{}{"2", 0}, "seed": seed, "steps": steps, "cfg": 5.0,
			"sampler_name": "euler", "scheduler": "simple",
			"positive": []interface{}{"6", 0}, "negative": []interface{}{"6", 1},
			"latent_image": []interface{}{"6", 2}, "denoise": 1,
		}},
		"8": map[string]interface{}{"class_type": "VAEDecode", "inputs": map[string]interface{}{
			"samples": []interface{}{"7", 0}, "vae": []interface{}{"3", 0},
		}},
		"9": map[string]interface{}{"class_type": "SaveWEBM", "inputs": map[string]interface{}{
			"images": []interface{}{"8", 0}, "filename_prefix": "mcp_vid", "codec": "vp9", "fps": fps, "crf": 30,
		}},
	}
}

// comfySubmit queues a workflow and returns the prompt id.
func (m *MediaAPI) comfySubmit(ctx context.Context, wf map[string]interface{}) (string, error) {
	data, code, err := m.postJSON(ctx, m.Cfg.ComfyUI+"/prompt", map[string]interface{}{"prompt": wf})
	if err != nil {
		return "", fmt.Errorf("comfyui unreachable at %s: %w", m.Cfg.ComfyUI, err)
	}
	var res struct {
		PromptID string `json:"prompt_id"`
		Error    string `json:"error"`
		NodeErrs map[string]interface{} `json:"node_errors"`
	}
	if jerr := json.Unmarshal(data, &res); jerr != nil {
		return "", fmt.Errorf("comfyui: bad response (HTTP %d): %.200s", code, data)
	}
	if res.PromptID == "" {
		var parts []string
		if res.Error != "" {
			parts = append(parts, res.Error)
		}
		if len(res.NodeErrs) > 0 {
			b, _ := json.Marshal(res.NodeErrs)
			parts = append(parts, string(b))
		}
		if len(parts) == 0 {
			parts = append(parts, fmt.Sprintf("HTTP %d: %.200s", code, data))
		}
		return "", fmt.Errorf("comfyui: %s", strings.Join(parts, " · "))
	}
	return res.PromptID, nil
}

// comfyWait polls /history until the prompt completes (or errors).
func (m *MediaAPI) comfyWait(ctx context.Context, promptID string, timeout time.Duration) (map[string]interface{}, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		data, _, err := m.get(ctx, m.Cfg.ComfyUI+"/history/"+promptID)
		if err == nil {
			var hist map[string]interface{}
			if json.Unmarshal(data, &hist) == nil {
				entry, ok := hist[promptID].(map[string]interface{})
				if ok {
					if st, _ := entry["status"].(map[string]interface{}); st != nil {
						if st["status_str"] == "error" {
							msgs, _ := st["messages"].([]interface{})
							b, _ := json.Marshal(msgs)
							return nil, fmt.Errorf("comfyui generation failed: %.400s", b)
						}
					}
					if st, _ := entry["status"].(map[string]interface{}); st != nil && st["completed"] == true {
						if out, ok := entry["outputs"].(map[string]interface{}); ok {
							return out, nil
						}
					}
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
	return nil, fmt.Errorf("comfyui: timed out after %s", timeout)
}

// comfyFirstFile finds the first saved file reference in node outputs
// (images / gifs / videos keys).
func comfyFirstFile(outputs map[string]interface{}) (filename, subfolder string, ok bool) {
	nodeIDs := make([]string, 0, len(outputs))
	for id := range outputs {
		nodeIDs = append(nodeIDs, id)
	}
	sort.Strings(nodeIDs)
	for _, id := range nodeIDs {
		node, ok := outputs[id].(map[string]interface{})
		if !ok {
			continue
		}
		for _, key := range []string{"images", "gifs", "videos"} {
			items, ok := node[key].([]interface{})
			if !ok || len(items) == 0 {
				continue
			}
			if item, ok := items[0].(map[string]interface{}); ok {
				fn, _ := item["filename"].(string)
				sf, _ := item["subfolder"].(string)
				if fn != "" {
					return fn, sf, true
				}
			}
		}
	}
	return "", "", false
}

func (m *MediaAPI) comfyView(ctx context.Context, filename, subfolder string) ([]byte, error) {
	q := url.Values{}
	q.Set("filename", filename)
	q.Set("subfolder", subfolder)
	q.Set("type", "output")
	data, code, err := m.do(ctx, m.HTTPLo, "GET", m.Cfg.ComfyUI+"/view?"+q.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 || len(data) == 0 {
		return nil, fmt.Errorf("comfyui /view: HTTP %d", code)
	}
	return data, nil
}

func (m *MediaAPI) image(ctx context.Context, args map[string]interface{}) (*ToolCallResult, error) {
	prompt := strArg(args, "prompt", "")
	if prompt == "" {
		return errResult("generate_image: 'prompt' is required"), nil
	}
	_ = strArg(args, "negative", "") // accepted for API compat; FLUX.2 is guidance-distilled and has no negative path
	width := intArg(args, "width", 1024) / 8 * 8
	height := intArg(args, "height", 1024) / 8 * 8
	steps := intArg(args, "steps", 8)   // Flux2Turbo distilled LoRA: 8 steps is the recipe
	guidance := floatArg(args, "cfg", 3.5) // FLUX.2 guidance (3.5 standard, ~4 max)
	seed := intArg(args, "seed", -1)
	if seed < 0 {
		seed = int(time.Now().UnixNano() % 1_000_000_000)
	}

	id, err := m.comfySubmit(ctx, fluxWorkflow(prompt, width, height, steps, seed, guidance))
	if err != nil {
		return errResult("generate_image: %v", err), nil
	}
	outputs, err := m.comfyWait(ctx, id, 15*time.Minute)
	if err != nil {
		return errResult("generate_image: %v", err), nil
	}
	fn, sf, ok := comfyFirstFile(outputs)
	if !ok {
		return errResult("generate_image: no image in ComfyUI outputs"), nil
	}
	data, err := m.comfyView(ctx, fn, sf)
	if err != nil {
		return errResult("generate_image: %v", err), nil
	}
	mime := "image/png"
	if strings.HasSuffix(fn, ".jpg") || strings.HasSuffix(fn, ".jpeg") {
		mime = "image/jpeg"
	}
	path, _ := m.saveArtifact("mcp_img_"+fn, data)
	return &ToolCallResult{Content: []ContentBlock{
		textBlock(fmt.Sprintf("Generated %dx%d image (%d bytes%s).", width, height, len(data), fileNote(path))),
		imageBlock(base64.StdEncoding.EncodeToString(data), mime),
	}}, nil
}

func (m *MediaAPI) video(ctx context.Context, args map[string]interface{}) (*ToolCallResult, error) {
	prompt := strArg(args, "prompt", "")
	if prompt == "" {
		return errResult("generate_video: 'prompt' is required"), nil
	}
	negative := strArg(args, "negative", "")
	width := intArg(args, "width", 832) / 16 * 16
	height := intArg(args, "height", 480) / 16 * 16
	frames := intArg(args, "frames", 49) / 4 * 4
	if frames < 4 {
		frames = 4
	}
	fps := intArg(args, "fps", 16)
	steps := intArg(args, "steps", 20)
	seed := intArg(args, "seed", -1)
	if seed < 0 {
		seed = int(time.Now().UnixNano() % 1_000_000_000)
	}

	id, err := m.comfySubmit(ctx, wanWorkflow(prompt, negative, width, height, frames, fps, steps, seed))
	if err != nil {
		return errResult("generate_video: %v", err), nil
	}
	outputs, err := m.comfyWait(ctx, id, 40*time.Minute)
	if err != nil {
		return errResult("generate_video: %v", err), nil
	}
	fn, sf, ok := comfyFirstFile(outputs)
	if !ok {
		return errResult("generate_video: no video in ComfyUI outputs"), nil
	}
	data, err := m.comfyView(ctx, fn, sf)
	if err != nil {
		return errResult("generate_video: %v", err), nil
	}
	path, _ := m.saveArtifact("mcp_vid_"+fn, data)
	dur := float64(frames) / float64(fps)
	return &ToolCallResult{Content: []ContentBlock{
		textBlock(fmt.Sprintf("Generated %dx%d video, %d frames ≈%.1fs (%d bytes%s).", width, height, frames, dur, len(data), fileNote(path))),
		videoBlock(base64.StdEncoding.EncodeToString(data), "video/webm"),
	}}, nil
}

func fileNote(path string) string {
	if path != "" {
		return " — also saved at " + path
	}
	return ""
}

// ── Voice: TTS + STT ─────────────────────────────────────────────

func (m *MediaAPI) registerAudioTools(s *Server) {
	s.Register(Tool{
		Name: "text_to_speech",
		Description: "Convert text to speech with Kokoro-82M on the local voice service. " +
			"Returns WAV audio as base64 (type=audio, audio/wav).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text":  map[string]interface{}{"type": "string", "description": "Text to speak"},
				"voice": map[string]interface{}{"type": "string", "description": "Voice preset, e.g. af_heart, am_adam", "default": "af_heart"},
				"speed": map[string]interface{}{"type": "number", "description": "Speed multiplier", "default": 1.0},
			},
			"required": []string{"text"},
		},
	}, m.tts)

	s.Register(Tool{
		Name: "speech_to_text",
		Description: "Transcribe audio to text with faster-whisper large-v3-turbo on the local voice service. " +
			"Provide audio_base64 (WAV/MP3/FLAC) or a server-side audio_path.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"audio_base64": map[string]interface{}{"type": "string", "description": "Base64-encoded audio"},
				"audio_path":   map[string]interface{}{"type": "string", "description": "Path to an audio file on the MCP host"},
			},
		},
	}, m.stt)

	s.Register(Tool{
		Name: "generate_music",
		Description: "Generate a song (text → music) with ACE-Step 1.5 on the local music service. " +
			"Returns WAV/MP3 audio as base64 (type=audio). Generation takes 1-5 min.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt":   map[string]interface{}{"type": "string", "description": "Description of the song (style, mood, instruments)"},
				"lyrics":   map[string]interface{}{"type": "string", "description": "Lyrics (empty = instrumental)", "default": ""},
				"duration": map[string]interface{}{"type": "integer", "description": "Target duration in seconds (max 180)", "default": 30},
			},
			"required": []string{"prompt"},
		},
	}, m.music)
}

// multipart posts form fields (string values) to u and returns the body.
func (m *MediaAPI) multipart(ctx context.Context, u string, fields map[string]string) ([]byte, int, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	w.Close()
	return m.do(ctx, m.HTTPLo, "POST", u, map[string]string{"Content-Type": w.FormDataContentType()}, buf.Bytes())
}

// multipartFile posts one form file plus optional string fields.
func (m *MediaAPI) multipartFile(ctx context.Context, u string, filename string, data []byte, mime string, fields map[string]string) ([]byte, int, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	fw, _ := w.CreateFormFile("file", filename)
	fw.Write(data)
	w.Close()
	return m.do(ctx, m.HTTPLo, "POST", u, map[string]string{"Content-Type": w.FormDataContentType()}, buf.Bytes())
}

func (m *MediaAPI) tts(ctx context.Context, args map[string]interface{}) (*ToolCallResult, error) {
	text := strArg(args, "text", "")
	if text == "" {
		return errResult("text_to_speech: 'text' is required"), nil
	}
	voice := strArg(args, "voice", "af_heart")
	speed := floatArg(args, "speed", 1.0)

	data, code, err := m.multipart(ctx, m.Cfg.Voice+"/tts", map[string]string{
		"text": text, "voice": voice, "speed": strconv.FormatFloat(speed, 'f', -1, 64),
	})
	if err != nil {
		return errResult("text_to_speech: voice service unreachable at %s: %v", m.Cfg.Voice, err), nil
	}
	if code != 200 || len(data) == 0 {
		return errResult("text_to_speech: HTTP %d: %.200s", code, data), nil
	}
	path, _ := m.saveArtifact("mcp_tts.wav", data)
	return &ToolCallResult{Content: []ContentBlock{
		textBlock(fmt.Sprintf("Synthesized %d chars of speech (%d bytes WAV%s).", len(text), len(data), fileNote(path))),
		audioBlock(base64.StdEncoding.EncodeToString(data), "audio/wav"),
	}}, nil
}

func (m *MediaAPI) stt(ctx context.Context, args map[string]interface{}) (*ToolCallResult, error) {
	audioB64, _ := args["audio_base64"].(string)
	audioPath, _ := args["audio_path"].(string)

	var data []byte
	var name string
	switch {
	case audioB64 != "":
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(audioB64, "data:audio/wav;base64,"))
		if err != nil {
			return errResult("speech_to_text: bad base64: %v", err), nil
		}
		data, name = raw, "audio.wav"
	case audioPath != "":
		raw, nm, err := m.SafeRead(audioPath)
		if err != nil {
			return errResult("speech_to_text: cannot read %s: %v", audioPath, err), nil
		}
		data, name = raw, nm
	default:
		return errResult("speech_to_text: provide audio_base64 or audio_path"), nil
	}

	resp, code, err := m.multipartFile(ctx, m.Cfg.Voice+"/stt", name, data, "audio/wav", nil)
	if err != nil {
		return errResult("speech_to_text: voice service unreachable at %s: %v", m.Cfg.Voice, err), nil
	}
	if code != 200 {
		return errResult("speech_to_text: HTTP %d: %.200s", code, resp), nil
	}
	var out struct {
		Text string `json:"text"`
		Lang string `json:"language"`
	}
	if jerr := json.Unmarshal(resp, &out); jerr != nil {
		return errResult("speech_to_text: bad response: %.200s", resp), nil
	}
	return &ToolCallResult{Content: []ContentBlock{
		textBlock(fmt.Sprintf("Transcription (%s): %s", out.Lang, out.Text)),
	}}, nil
}

// ── Music: ACE-Step via Gradio ───────────────────────────────────

// Gradio 5.x queue protocol (verified on gradio 5.33):
//  1. POST /gradio_api/call/<fn>  {"data":[...args],"session_hash":s}  →  {"event_id":...}
//  2. GET  /gradio_api/queue/data?session_hash=s  — SSE stream of
//     data: {...} messages until process_completed (with "data": outputs).
func (m *MediaAPI) gradioCall(ctx context.Context, base, fn string, args []interface{}) (sessionHash string, err error) {
	sh := newSessionHash()
	body, _ := json.Marshal(map[string]interface{}{"data": args, "session_hash": sh})
	data, code, err := m.do(ctx, m.HTTP, "POST", base+"/gradio_api/call/"+fn,
		map[string]string{"Content-Type": "application/json"}, body)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("gradio call %s: HTTP %d: %.200s", fn, code, data)
	}
	var res struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(data, &res); err != nil || res.EventID == "" {
		return "", fmt.Errorf("gradio call %s: no event_id in %.200s", fn, data)
	}
	return sh, nil
}

// gradioPoll reads the SSE stream for the session until the job completes.
func (m *MediaAPI) gradioPoll(ctx context.Context, base, sessionHash string, timeout time.Duration) (json.RawMessage, error) {
	u := base + "/gradio_api/queue/data?session_hash=" + url.QueryEscape(sessionHash)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gradio queue/data: HTTP %d: %.200s", resp.StatusCode, b)
	}

	deadline := time.Now().Add(timeout)
	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if time.Now().After(deadline) {
				break
			}
			// heartbeat gap or stream closed — reconnect once and keep waiting
			if time.Now().Before(deadline) {
				resp2, err2 := client.Do(req.Clone(ctx))
				if err2 != nil {
					return nil, err2
				}
				if resp2.StatusCode == 200 {
					resp.Body.Close()
					resp = resp2
					reader = bufio.NewReaderSize(resp.Body, 64*1024)
					continue
				}
				resp2.Body.Close()
			}
			return nil, fmt.Errorf("gradio stream: %w", err)
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		msg := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if msg == "" {
			continue
		}
		var ev struct {
			Msg     string          `json:"msg"`
			Success bool            `json:"success"`
			Output  json.RawMessage `json:"output"`
			Error   json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(msg), &ev) != nil {
			continue
		}
		switch {
		case ev.Msg == "process_completed" || strings.HasSuffix(ev.Msg, "completed"):
			// payload: {"data": [outputs...]} or {"error": "..."}
			var out struct {
				Data  []json.RawMessage `json:"data"`
				Error string            `json:"error"`
			}
			if len(ev.Output) > 0 {
				if json.Unmarshal(ev.Output, &out) == nil {
					if out.Error != "" {
						return nil, fmt.Errorf("gradio job failed: %s", out.Error)
					}
					if len(out.Data) > 0 {
						return out.Data[0], nil
					}
				}
			}
			if len(ev.Error) > 0 {
				return nil, fmt.Errorf("gradio job failed: %.400s", ev.Error)
			}
			return nil, fmt.Errorf("gradio job completed with no data (success=%v)", ev.Success)
		case strings.Contains(ev.Msg, "error"):
			if len(ev.Error) > 0 {
				return nil, fmt.Errorf("gradio job failed: %.400s", ev.Error)
			}
			return nil, fmt.Errorf("gradio job failed: %s", ev.Msg)
		}
		// progress / heartbeat — keep reading
	}
	return nil, fmt.Errorf("gradio: timed out after %s", timeout)
}

func newSessionHash() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *MediaAPI) music(ctx context.Context, args map[string]interface{}) (*ToolCallResult, error) {
	prompt := strArg(args, "prompt", "")
	if prompt == "" {
		return errResult("generate_music: 'prompt' is required"), nil
	}
	lyrics := strArg(args, "lyrics", "")
	duration := intArg(args, "duration", 30)
	if duration > 180 {
		duration = 180
	}
	useLM := boolArg(args, "use_lm", false)
	seed := intArg(args, "seed", -1)

	// 1) create the task (ACE-Step 1.5 REST API)
	body := map[string]interface{}{
		"prompt":          prompt,
		"lyrics":          lyrics,
		"thinking":        useLM,
		"audio_duration":  float64(duration),
		"task_type":       "text2music",
		"use_random_seed": seed < 0,
	}
	if seed >= 0 {
		body["seed"] = seed
	}
	data, code, err := m.postJSON(ctx, m.Cfg.Music+"/release_task", body)
	if err != nil {
		return errResult("generate_music: music service unreachable at %s: %v", m.Cfg.Music, err), nil
	}
	var env struct {
		Data  struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if code != 200 || json.Unmarshal(data, &env) != nil || env.Data.TaskID == "" {
		return errResult("generate_music: HTTP %d: %.300s", code, data), nil
	}
	taskID := env.Data.TaskID

	// 2) poll query_result until success (1) / failure (2)
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return errResult("generate_music: %v", ctx.Err()), nil
		default:
		}
		time.Sleep(3 * time.Second)
		q, _, qerr := m.postJSON(ctx, m.Cfg.Music+"/query_result", map[string]interface{}{"task_id_list": []string{taskID}})
		if qerr != nil {
			continue
		}
		var res struct {
			Data []struct {
				Status int             `json:"status"`
				Result json.RawMessage `json:"result"`
			} `json:"data"`
		}
		if json.Unmarshal(q, &res) != nil || len(res.Data) == 0 {
			continue
		}
		switch res.Data[0].Status {
		case 1:
			audio, name, aerr := m.musicExtractAudio(ctx, res.Data[0].Result)
			if aerr != nil {
				return errResult("generate_music: %v", aerr), nil
			}
			path, _ := m.saveArtifact("mcp_music_"+name, audio)
			return &ToolCallResult{Content: []ContentBlock{
				textBlock(fmt.Sprintf("Generated %s (%d bytes%s).", name, len(audio), fileNote(path))),
				audioBlock(base64.StdEncoding.EncodeToString(audio), audioMime(name)),
			}}, nil
		case 2:
			return errResult("generate_music: task failed: %.300s", res.Data[0].Result), nil
		}
		// 0 = still running — keep polling
	}
	return errResult("generate_music: timed out after 15m (task %s)", taskID), nil
}

// musicExtractAudio pulls the audio file out of a query_result entry.
// The "result" field is a JSON-encoded string containing an array of
// entries; the first entry has "file": "/v1/audio?path=...".
func (m *MediaAPI) musicExtractAudio(ctx context.Context, resultRaw json.RawMessage) ([]byte, string, error) {
	var arr []map[string]interface{}
	var s string
	if err := json.Unmarshal(resultRaw, &s); err == nil {
		if err := json.Unmarshal([]byte(s), &arr); err != nil {
			return nil, "", fmt.Errorf("bad result payload: %.200s", s)
		}
	} else if err := json.Unmarshal(resultRaw, &arr); err != nil {
		return nil, "", fmt.Errorf("unexpected result: %.200s", resultRaw)
	}
	if len(arr) == 0 {
		return nil, "", fmt.Errorf("no entries in result")
	}
	e := arr[0]
	file, _ := e["file"].(string)
	if file == "" {
		return nil, "", fmt.Errorf("no file in result entry: %.200s", e)
	}
	u := file
	if strings.HasPrefix(u, "/") {
		u = m.Cfg.Music + u
	}
	data, code, err := m.do(ctx, m.HTTPLo, "GET", u, nil, nil)
	if err != nil {
		return nil, "", err
	}
	if code != 200 || len(data) == 0 {
		return nil, "", fmt.Errorf("audio download: HTTP %d", code)
	}
	name := filepath.Base(strings.SplitN(u, "?", 2)[0])
	if q := queryPath(u); q != "" {
		if b := filepath.Base(q); b != "/" && b != "" {
			name = b
		}
	}
	if name == "/" || name == "" || name == "audio" {
		name = "song.mp3"
	}
	return data, name, nil
}

// queryPath extracts the "path" query parameter from a /v1/audio URL.
func queryPath(u string) string {
	i := strings.IndexByte(u, '?')
	if i < 0 {
		return ""
	}
	for _, kv := range strings.Split(u[i+1:], "&") {
		p := strings.SplitN(kv, "=", 2)
		if len(p) == 2 && p[0] == "path" {
			if d, err := url.QueryUnescape(p[1]); err == nil {
				return d
			}
			return p[1]
		}
	}
	return ""
}

func audioMime(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	default:
		return "audio/wav"
	}
}

// ── Hunyuan3D: image → 3D ────────────────────────────────────────

func (m *MediaAPI) register3DTools(s *Server) {
	s.Register(Tool{
		Name: "image_to_3d",
		Description: "Convert a 2D image to a textured 3D model (GLB) with Hunyuan3D-2.1 on the local 3D service. " +
			"Provide image_base64 (PNG/JPG) or a server-side image_path. " +
			"Returns the GLB as base64 (type=image, model/gltf-binary) — can be large (10-50 MB). " +
			"Generation takes 2-10 min.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image_base64": map[string]interface{}{"type": "string", "description": "Base64-encoded image (PNG/JPG)"},
				"image_path":   map[string]interface{}{"type": "string", "description": "Path to an image file on the MCP host"},
				"steps":        map[string]interface{}{"type": "integer", "description": "Diffusion steps", "default": 30},
				"seed":         map[string]interface{}{"type": "integer", "description": "Seed", "default": 1234},
			},
		},
	}, m.to3d)
}

func (m *MediaAPI) to3d(ctx context.Context, args map[string]interface{}) (*ToolCallResult, error) {
	b64, _ := args["image_base64"].(string)
	path, _ := args["image_path"].(string)

	var img []byte
	var name string
	switch {
	case b64 != "":
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(b64, "data:image/png;base64,"))
		if err != nil {
			return errResult("image_to_3d: bad base64: %v", err), nil
		}
		img, name = raw, "image.png"
	case path != "":
		raw, nm, err := m.SafeRead(path)
		if err != nil {
			return errResult("image_to_3d: cannot read %s: %v", path, err), nil
		}
		img, name = raw, nm
	default:
		return errResult("image_to_3d: provide image_base64 or image_path"), nil
	}

	steps := intArg(args, "steps", 30)
	seed := intArg(args, "seed", 1234)

	fileData := map[string]interface{}{
		"path":       nil,
		"url":        "data:image/png;base64," + base64.StdEncoding.EncodeToString(img),
		"name":       name,
		"size":       len(img),
		"mimetype":   "image/png",
		"orig_name":  name,
	}
	glsn := beforeFiles(ctx, m.Cfg.ThreedOutDir)
	// 13 args: [0] is a hidden gradio State (caption slot, pass nil),
	// then image + 4 multiview slots, then the numeric/bool controls.
	args3d := []interface{}{
		nil, fileData, nil, nil, nil, nil,
		steps, 5.0, seed, 256, true, 8000, false,
	}
	session, err := m.gradioCall(ctx, m.Cfg.Threed, "generation_all", args3d)
	if err != nil {
		return errResult("image_to_3d: %v", err), nil
	}
	var pollErr error
	var outRaw json.RawMessage
	outRaw, pollErr = m.gradioPoll(ctx, m.Cfg.Threed, session, 30*time.Minute)

	// Extract the GLB: from the gradio result, or (fallback) from a new
	// file in the host-side output dir. The host fallback also covers
	// partial successes where the shape was saved but a later stage
	// (e.g. texturing) reported an error.
	data, glbName, exErr := m.threedExtractGLB(ctx, outRaw, glsn)
	switch {
	case data != nil && pollErr == nil:
		outPath, _ := m.saveArtifact("mcp_3d_"+glbName, data)
		return &ToolCallResult{Content: []ContentBlock{
			textBlock(fmt.Sprintf("Generated 3D model %s (%d bytes%s).", glbName, len(data), fileNote(outPath))),
			ContentBlock{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: "model/gltf-binary"},
		}}, nil
	case data != nil:
		outPath, _ := m.saveArtifact("mcp_3d_"+glbName, data)
		return &ToolCallResult{Content: []ContentBlock{
			textBlock(fmt.Sprintf("Generated 3D model %s (%d bytes%s) — note: job reported %v (model may be partial).", glbName, len(data), fileNote(outPath), pollErr)),
			ContentBlock{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: "model/gltf-binary"},
		}}, nil
	case pollErr != nil:
		return errResult("image_to_3d: %v", pollErr), nil
	default:
		return errResult("image_to_3d: %v", exErr), nil
	}
}

// beforeFiles snapshots the recursive file list of a host dir (relative
// paths) so the extractor can detect files created by the new job.
func beforeFiles(ctx context.Context, dir string) map[string]bool {
	set := map[string]bool{}
	if dir == "" {
		return set
	}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			if rel, rerr := filepath.Rel(dir, path); rerr == nil {
				set[filepath.ToSlash(rel)] = true
			}
		}
		return nil
	})
	return set
}

// threedExtractGLB finds the generated GLB: via gradio file URL, /file= route,
// or a new file in the host-side output dir. outRaw may be nil (poll failed
// before a result) — the host-dir fallback still applies.
func (m *MediaAPI) threedExtractGLB(ctx context.Context, outRaw json.RawMessage, before map[string]bool) ([]byte, string, error) {
	var item struct {
		Path string `json:"path"`
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if len(outRaw) > 0 {
		if err := json.Unmarshal(outRaw, &item); err != nil {
			var arr []json.RawMessage
			if json.Unmarshal(outRaw, &arr) == nil && len(arr) > 0 {
				json.Unmarshal(arr[0], &item)
			}
		}
	}
	name := item.Name
	if name == "" && item.Path != "" {
		name = filepath.Base(item.Path)
	}

	if item.URL != "" && strings.HasPrefix(item.URL, "http") {
		if data, _, err := m.do(ctx, m.HTTPLo, "GET", item.URL, nil, nil); err == nil && len(data) > 0 {
			return data, name, nil
		}
	}
	if item.Path != "" {
		u := m.Cfg.Threed + "/file=" + url.PathEscape(item.Path)
		if data, code, err := m.do(ctx, m.HTTPLo, "GET", u, nil, nil); err == nil && code == 200 && len(data) > 0 {
			return data, name, nil
		}
	}
	// Host-side fallback: the outputs dir holds one folder per job
	// (<uuid>/white_mesh.glb, ...). Pick the newest .glb/.obj not seen before.
	if m.Cfg.ThreedOutDir != "" {
		var newest string
		var newestMod time.Time
		walkErr := filepath.Walk(m.Cfg.ThreedOutDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			rel := filepath.ToSlash(filepath.Base(path))
			if r, rerr := filepath.Rel(m.Cfg.ThreedOutDir, path); rerr == nil {
				rel = filepath.ToSlash(r)
			}
			if before[rel] {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(base))
			if ext != ".glb" && ext != ".obj" {
				return nil
			}
			if info.ModTime().After(newestMod) {
				newest, newestMod = path, info.ModTime()
			}
			return nil
		})
		if walkErr == nil && newest != "" {
			data, err := os.ReadFile(newest)
			if err == nil && len(data) > 0 {
				return data, filepath.Base(newest), nil
			}
		}
	}
	return nil, "", fmt.Errorf("could not retrieve GLB (result: %.200s)", outRaw)
}

// ── Status ───────────────────────────────────────────────────────

func (m *MediaAPI) status(ctx context.Context, args map[string]interface{}) (*ToolCallResult, error) {
	type svc struct {
		name, url, probe string
	}
	services := []svc{
		{"ComfyUI (FLUX images · Wan video)", m.Cfg.ComfyUI, "/system_stats"},
		{"Voice (Kokoro TTS · Whisper STT)", m.Cfg.Voice, "/health"},
		{"Music (ACE-Step 1.5)", m.Cfg.Music, "/health"},
		{"3D (Hunyuan3D-2.1)", m.Cfg.Threed, "/"},
	}
	var b strings.Builder
	b.WriteString("ai-hub media services:\n")
	anyUp := false
	for _, s := range services {
		ctxT, cancel := context.WithTimeout(ctx, 4*time.Second)
		_, code, err := m.do(ctxT, m.HTTP, "GET", s.url+s.probe, nil, nil)
		cancel()
		st := "❌ down/unreachable"
		if err == nil && (code == 200 || code == 206 || code == 302 || code == 307) {
			st = "✅ running"
			anyUp = true
		} else if err == nil {
			st = fmt.Sprintf("⚠️ HTTP %d", code)
		}
		fmt.Fprintf(&b, "  %-34s %s — %s\n", s.name, s.url, st)
	}
	if !anyUp {
		b.WriteString("\nStart with: cd /srv/ai/stack && docker compose up -d comfyui voice music threed")
	}
	return &ToolCallResult{Content: []ContentBlock{textBlock(b.String())}}, nil
}
