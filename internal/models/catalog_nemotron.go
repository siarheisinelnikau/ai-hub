package models

import "fmt"

func nemotron35UncensoredFiles() []string {
	const name = "NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Uncensored-AD-Q4_K_S"
	files := make([]string, 0, 60)
	for shard := 1; shard <= 59; shard++ {
		files = append(files, fmt.Sprintf("%s/%s-%05d-of-00059.gguf", name, name, shard))
	}
	return append(files, "mmproj-NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Uncensored-BF16.gguf")
}

// The pinned installer downloads and verifies the entire NVIDIA snapshot.
// Readiness requires every shard and runtime sidecar; zero-length __init__.py
// is excluded because the common readiness check requires nonempty files.
func nemotron35NVFP4Files() []string {
	files := []string{
		"chat_template.jinja", "config.json", "configuration_nemotron_h_omni.py",
		"configuration_radio.py", "generation_config.json", "hf_quant_config.json",
		"image_processing_nemotron_h_omni.py", "model.safetensors.index.json",
		"modeling_nemotron_h_omni.py", "modeling_radio.py", "preprocessor_config.json",
		"processing_nemotron_h_omni.py", "processor_config.json", "tokenizer.json",
		"tokenizer_config.json", "ultra_v3_reasoning_parser.py",
	}
	for shard := 1; shard <= 88; shard++ {
		files = append(files, fmt.Sprintf("model-%05d-of-00088.safetensors", shard))
	}
	return files
}
