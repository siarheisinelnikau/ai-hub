// Package models is the hub's model manager: a catalog of every LLM the hub
// knows (download source + on-disk location), resumable HuggingFace downloads,
// and per-model inventory (on-disk / size / download state / hub connection).
//
// It is deliberately separate from registry (which is the serving source of
// truth) so it can be exercised without touching /api/status parity.
package models

import (
	"fmt"
	"os"
	"path/filepath"
)

// Info is the per-model catalog entry: where to download it from and where its
// files live on the server.
type Info struct {
	ID         string
	Short      string
	Backend    string
	Quant      string
	Port       int
	HFRepo     string // "owner/name" — empty when it is not a plain HF download
	LocalPath  string // absolute server path the model files belong in
	Files      []string
	ReadyFiles []string // generated/runtime files required before the serving card is shown
	ExtraRepo  string   // optional companion files from a second repository
	ExtraFiles []string
}

// Catalog covers every LLM profile in the hub. Media services (ComfyUI/Voice/
// Music/3D) are docker-compose bundles with many checkpoints, not single HF
// models, so they are managed separately and not listed here.
var Catalog = map[string]Info{
	"glm53-flash-maya-m-derisked": {
		ID: "glm53-flash-maya-m-derisked", Short: "GLM Derisked", Backend: "Maya", Quant: "Maya-M IQ2_S", Port: 18038,
		HFRepo:    "peasantsmith/GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-GGUF",
		LocalPath: "/home/user/models/GLM-5.3-Flash-Maya-M-Derisked",
		Files: []string{
			"GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00001-of-00003.gguf",
			"GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00002-of-00003.gguf",
			"GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00003-of-00003.gguf",
			"vision/GLM-5.3-Flash-vocab.gguf", "vision/mmproj-GLM-5.3-Flash-F16.gguf",
		},
		ReadyFiles: []string{
			"verified.json", "runtime-ready.json", "pack/dense.bin", "pack/index.txt",
			"GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00001-of-00003.gguf",
			"GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00002-of-00003.gguf",
			"GLM-5.3-Flash-Maya-M-Derisked-IQ2_S-00003-of-00003.gguf",
			"vision/GLM-5.3-Flash-vocab.gguf", "vision/mmproj-GLM-5.3-Flash-F16.gguf",
		},
	},
	"glm53-flash-maya-m": {
		ID: "glm53-flash-maya-m", Short: "GLM 5.3 Flash", Backend: "Maya", Quant: "Maya-M IQ2_S", Port: 18037,
		HFRepo:    "peasantsmith/GLM-5.3-Flash-Maya-GGUF",
		LocalPath: "/home/user/models/GLM-5.3-Flash-Maya-M",
		Files: []string{
			"Maya-M/GLM-5.3-Flash-Maya-M-IQ2_S-00001-of-00003.gguf",
			"Maya-M/GLM-5.3-Flash-Maya-M-IQ2_S-00002-of-00003.gguf",
			"Maya-M/GLM-5.3-Flash-Maya-M-IQ2_S-00003-of-00003.gguf",
			"vision/GLM-5.3-Flash-vocab.gguf", "vision/mmproj-GLM-5.3-Flash-F16.gguf",
		},
		ReadyFiles: []string{
			"verified.json", "runtime-ready.json", "Maya-M/pack/dense.bin", "Maya-M/pack/index.txt",
			"Maya-M/GLM-5.3-Flash-Maya-M-IQ2_S-00001-of-00003.gguf",
			"Maya-M/GLM-5.3-Flash-Maya-M-IQ2_S-00002-of-00003.gguf",
			"Maya-M/GLM-5.3-Flash-Maya-M-IQ2_S-00003-of-00003.gguf",
			"vision/GLM-5.3-Flash-vocab.gguf", "vision/mmproj-GLM-5.3-Flash-F16.gguf",
		},
	},
	"nemotron35-super-uncensored": {
		ID: "nemotron35-super-uncensored", Short: "Nemotron Uncensored", Backend: "llama.cpp", Quant: "AD-Q4_K_S", Port: 18036,
		HFRepo:     "AtomicChat/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Uncensored-GGUF",
		LocalPath:  "/home/user/models/Nemotron-3.5-Super-VL-120B-Uncensored-AD-Q4_K_S",
		Files:      nemotron35UncensoredFiles(),
		ReadyFiles: append(nemotron35UncensoredFiles(), "verified.json"),
	},
	"nemotron35-super-nvfp4": {
		ID: "nemotron35-super-nvfp4", Short: "Nemotron NVFP4", Backend: "SGLang", Quant: "NVFP4", Port: 18035,
		HFRepo:     "nvidia/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-NVFP4",
		LocalPath:  "/home/user/models/Nemotron-3.5-Super-VL-120B-NVFP4",
		Files:      nemotron35NVFP4Files(),
		ReadyFiles: append(nemotron35NVFP4Files(), "verified.json"),
	},
	"nemotron35-super-q4k": {
		ID: "nemotron35-super-q4k", Short: "Nemotron 3.5 Super", Backend: "llama.cpp", Quant: "Q4_K_M", Port: 18034,
		HFRepo:     "lmstudio-community/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-GGUF",
		LocalPath:  "/home/user/models/Nemotron-3.5-Super-VL-120B-Q4_K_M",
		Files:      []string{"NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Q4_K_M-00001-of-00002.gguf", "NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Q4_K_M-00002-of-00002.gguf", "mmproj-NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-BF16.gguf"},
		ReadyFiles: []string{"NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Q4_K_M-00001-of-00002.gguf", "NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Q4_K_M-00002-of-00002.gguf", "mmproj-NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-BF16.gguf", "verified.json"},
	},
	"qwen35-122b-q4k-llama": {
		ID: "qwen35-122b-q4k-llama", Short: "Qwen 3.5 122B", Backend: "llama.cpp", Quant: "Q4_K_M", Port: 18028,
		HFRepo:     "HauhauCS/Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive",
		LocalPath:  "/home/user/models/Qwen3.5-122B-A10B-Uncensored-HauhauCS",
		Files:      []string{"Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive-Q4_K_M.gguf", "mmproj-Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive-f16.gguf"},
		ReadyFiles: []string{"Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive-Q4_K_M-MTP.gguf", "mmproj-Qwen3.5-122B-A10B-Uncensored-HauhauCS-Aggressive-f16.gguf", "mtp-merge-verified.json"},
	},
	"qwen38-35b-q8-llama": {
		ID: "qwen38-35b-q8-llama", Short: "Qwen 3.8 35B", Backend: "llama.cpp", Quant: "Q8_0", Port: 18027,
		HFRepo:    "empero-ai/Qwen3.8-35B-A3B-Distill-GGUF",
		LocalPath: "/home/user/models/Qwen3.8-35B-A3B-Distill-Q8",
		Files:     []string{"Qwen3.8-35B-A3B-Q8_0.gguf", "mmproj-Qwen3.8-35B-A3B-F16.gguf"},
	},
	"gpt-oss-120b": {
		ID: "gpt-oss-120b", Short: "GPT-OSS 120B", Backend: "llama.cpp", Quant: "MXFP4", Port: 18041,
		HFRepo:    "Unrestricted/GPTOSS-120B-Uncensored-HauhauCS-Aggressive",
		LocalPath: "/home/user/models/gpt-oss-120b",
		Files:     []string{"GPTOSS-120B-Uncensored-HauhauCS-Aggressive-MXFP4.gguf"},
	},
	"qwen38-flash-next-iq3-strata": {
		ID: "qwen38-flash-next-iq3-strata", Short: "Flash-Next Strata", Backend: "Strata", Quant: "IQ3_XXS", Port: 18033,
		HFRepo:     "ISTA-DASLab/Qwen3.8-Flash-Next-GSQ-RCO-GGUF",
		LocalPath:  "/home/user/models/Strata-iq3-data",
		Files:      []string{"IQ3_XXS/Qwen3.8-Flash-Next-GSQ-RCO-IQ3_XXS-00001-of-00002.gguf", "IQ3_XXS/Qwen3.8-Flash-Next-GSQ-RCO-IQ3_XXS-00002-of-00002.gguf"},
		ReadyFiles: []string{"models/IQ3_XXS/Qwen3.8-Flash-Next-GSQ-RCO-IQ3_XXS-00001-of-00002.gguf", "models/IQ3_XXS/Qwen3.8-Flash-Next-GSQ-RCO-IQ3_XXS-00002-of-00002.gguf", "packs/iq3_xxs/dense.bin", "packs/iq3_xxs/index.txt"},
	},
	"qwen38-flash-next-uncensored-strata": {
		ID: "qwen38-flash-next-uncensored-strata", Short: "Flash-Next Uncensored", Backend: "Strata", Quant: "UD-IQ4_XS", Port: 18030,
		HFRepo:    "Dyluhn/Qwen3.8-Flash-Next-Uncensored-R9V-IQ4_XS",
		LocalPath: "/home/user/models/Strata-uncensored-iq4-data",
		Files: []string{
			"target/Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00001-of-00003.gguf",
			"target/Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00002-of-00003.gguf",
			"target/Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00003-of-00003.gguf",
		},
		ReadyFiles: []string{"models/UD-IQ4_XS/Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00001-of-00003.gguf", "models/UD-IQ4_XS/Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00002-of-00003.gguf", "models/UD-IQ4_XS/Qwen3.8-Flash-Next-Uncensored-UD-IQ4_XS-00003-of-00003.gguf", "packs/dyluhn-ud-iq4_xs/dense.bin"},
	},
	"qwen38-flash-next-strata": {
		ID: "qwen38-flash-next-strata", Short: "Flash-Next Strata", Backend: "Strata", Quant: "UD-IQ4_XS", Port: 18029,
		HFRepo:     "unsloth/Qwen3.8-Flash-Next-GGUF",
		LocalPath:  "/home/user/models/Strata-data",
		Files:      []string{"UD-IQ4_XS/Qwen3.8-Flash-Next-UD-IQ4_XS-00001-of-00003.gguf", "UD-IQ4_XS/Qwen3.8-Flash-Next-UD-IQ4_XS-00002-of-00003.gguf", "UD-IQ4_XS/Qwen3.8-Flash-Next-UD-IQ4_XS-00003-of-00003.gguf"},
		ReadyFiles: []string{"models/unsloth-UD-IQ4_XS/Qwen3.8-Flash-Next-UD-IQ4_XS-00001-of-00003.gguf", "models/unsloth-UD-IQ4_XS/Qwen3.8-Flash-Next-UD-IQ4_XS-00002-of-00003.gguf", "models/unsloth-UD-IQ4_XS/Qwen3.8-Flash-Next-UD-IQ4_XS-00003-of-00003.gguf", "packs/unsloth-ud-iq4_xs/dense.bin"},
	},
	"qwen-q4k-llama": {
		ID: "qwen-q4k-llama", Short: "Qwen Q4K", Backend: "llama.cpp", Quant: "Q4_K_M", Port: 18021,
		HFRepo:     "orcarouter/Qwen3.8-27B-Uncensored-GGUF",
		LocalPath:  "/home/user/models/Qwen3.8-27B-Uncensored-GGUF",
		Files:      []string{"Qwen3.8-27B-Uncensored-Q4_K_M.gguf"},
		ExtraRepo:  "0bserverx/Qwen3.8-27B-Heretic-Abliterated-Uncensored-GGUF",
		ExtraFiles: []string{"mmproj-Qwen3.8-27B-Q8_0.gguf"},
		ReadyFiles: []string{"Qwen3.8-27B-Uncensored-Q4_K_M.gguf", "mmproj-Qwen3.8-27B-Q8_0.gguf"},
	},
	"qwen-q6k-llama": {
		ID: "qwen-q6k-llama", Short: "Qwen Q6K", Backend: "llama.cpp", Quant: "Q6_K", Port: 18026,
		HFRepo:    "0bserverx/Qwen3.8-27B-Heretic-Abliterated-Uncensored-GGUF",
		LocalPath: "/home/user/models/Qwen3.8-27B-abliterated-Q6K",
		Files:     []string{"RVN-Q6_K-mtp.gguf", "mmproj-Qwen3.8-27B-Q8_0.gguf"},
	},
	"gemma4-26b-q4k-mtp": {
		ID: "gemma4-26b-q4k-mtp", Short: "Gemma4 Q4K", Backend: "llama.cpp", Quant: "Q4_K_M", Port: 18031,
		HFRepo:    "HauhauCS/Gemma4-26B-A4B-Uncensored-HauhauCS-Balanced",
		LocalPath: "/home/user/models/gemma4-26b-uncensored",
		Files:     []string{"Gemma4-26B-A4B-Uncensored-HauhauCS-Balanced-Q4_K_M.gguf", "mmproj-Gemma4-26B-A4B-Uncensored-HauhauCS-Balanced-f16.gguf"},
	},
}

// IDs includes downloadable profiles even when their weights have been removed.
func IDs() []string {
	if configuredOrder != nil {
		return append([]string{}, configuredOrder...)
	}
	order := []string{
		"qwen-q4k-llama", "qwen-q6k-llama",
		"glm53-flash-maya-m",
		"glm53-flash-maya-m-derisked",
		"nemotron35-super-q4k", "nemotron35-super-nvfp4", "nemotron35-super-uncensored",

		"gemma4-26b-q4k-mtp",
		"qwen38-flash-next-strata",
		"qwen38-flash-next-uncensored-strata",
		"qwen35-122b-q4k-llama", "qwen38-35b-q8-llama",
		"gpt-oss-120b", "qwen38-flash-next-iq3-strata",
	}
	out := make([]string, 0, len(order))
	for _, id := range order {
		if _, ok := Catalog[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// Ready checks required files, rather than mistaking installer cache for weights.
func Ready(id string) bool {
	info, ok := Catalog[id]
	if !ok {
		return false
	}
	files := info.ReadyFiles
	if len(files) == 0 {
		files = info.Files
	}
	for _, name := range files {
		fi, err := os.Stat(filepath.Join(info.LocalPath, name))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
			return false
		}
	}
	return len(files) > 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// downloadsDir holds the download manifest + per-model logs. Override with
// AIHUB_DOWNLOADS_DIR (used by tests); defaults to the server path.
func downloadsDir() string {
	return envOr("AIHUB_DOWNLOADS_DIR", "/srv/ai-hub/downloads")
}

func manifestPath() string { return filepath.Join(downloadsDir(), "manifest.json") }
func logPath(id string) string {
	return filepath.Join(downloadsDir(), id+".log")
}

// dirFor returns the absolute local path for a model id.
func dirFor(id string) (string, error) {
	info, ok := Catalog[id]
	if !ok {
		return "", fmt.Errorf("unknown model %q", id)
	}
	return info.LocalPath, nil
}

// repoFor returns the HF repo for a model id ("", "" when there is none).
func repoFor(id string) (string, error) {
	info, ok := Catalog[id]
	if !ok {
		return "", fmt.Errorf("unknown model %q", id)
	}
	return info.HFRepo, nil
}
