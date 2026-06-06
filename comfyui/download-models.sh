#!/usr/bin/env bash
set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
MODELS_DIR="${MODELS_DIR:-./models}"
HF_TOKEN="${HF_TOKEN:-}"
DRY_RUN=false

DOWNLOAD_ALL=false
DOWNLOAD_IMAGE=false
DOWNLOAD_VIDEO=false
DOWNLOAD_FLUX2=false
DOWNLOAD_SDXL=false
DOWNLOAD_QWEN21=false
DOWNLOAD_WAN=false
DOWNLOAD_LTX=false
DOWNLOAD_MINIMAX=false

RETRY_MAX=3
TIMEOUT_SEC=600

# Parse args
if [ "$#" -eq 0 ]; then
  DOWNLOAD_ALL=true
fi

for arg in "$@"; do
  case "$arg" in
    --all)
      DOWNLOAD_ALL=true
      ;;
    --image)
      DOWNLOAD_IMAGE=true
      ;;
    --video)
      DOWNLOAD_VIDEO=true
      ;;
    --flux2)
      DOWNLOAD_FLUX2=true
      ;;
    --qwen21)
      DOWNLOAD_QWEN21=true
      ;;
    --sdxl)
      DOWNLOAD_SDXL=true
      ;;
    --wan)
      DOWNLOAD_WAN=true
      ;;
    --ltx)
      DOWNLOAD_LTX=true
      ;;
    --minimax)
      DOWNLOAD_MINIMAX=true
      ;;
    --dry-run)
      DRY_RUN=true
      ;;
    --help|-h)
      cat <<'USAGE'
Usage: ./download-models.sh [--all | --image | --video | --flux2 | --sdxl | --qwen21 | --wan | --ltx | --minimax] [--dry-run]

Defaults:
  no flags   -> --all

Options:
  --all      Download all curated ComfyUI assets listed below
  --image    Download image models (SDXL + FLUX.2 + Qwen-Image 2.1)
  --video    Download video models (Wan 2.1 + LTX-2.5 + MiniMax-H3)
  --flux2    Download FLUX.2 only
  --sdxl     Download SDXL only
  --qwen21   Download Qwen-Image 2.1 only
  --wan      Download Wan 2.1 only
  --ltx      Download LTX-2.5 only
  --minimax  Download MiniMax-H3 only
  --dry-run  Verify and print plan, do not download

Environment:
  MODELS_DIR    Target directory (default: ./models)
  HF_TOKEN      Hugging Face token for gated/private endpoints when needed
USAGE
      exit 0
      ;;
    --version)
      echo "comfyui-model-downloader 0.3.0"
      exit 0
      ;;
    *)
      echo "Unknown option: $arg"
      echo "Run with --help"
      exit 1
      ;;
  esac

  done

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

if ! command -v curl >/dev/null 2>&1; then
  log_error "curl not found. Install curl and retry."
  exit 1
fi

if ! command -v stat >/dev/null 2>&1; then
  log_error "stat not found. This script requires GNU/POSIX stat."
  exit 1
fi

HASH_TOOL="sha256sum"
if ! command -v "$HASH_TOOL" >/dev/null 2>&1; then
  if command -v shasum >/dev/null 2>&1; then
    HASH_TOOL="shasum"
  else
    HASH_TOOL=""
  fi
fi

calc_sha256() {
  local file="$1"
  if [ ! -f "$file" ]; then
    echo ""
    return 1
  fi
  if [ "$HASH_TOOL" = "sha256sum" ]; then
    sha256sum "$file" | awk '{print $1}'
    return 0
  fi
  if [ "$HASH_TOOL" = "shasum" ]; then
    shasum -a 256 "$file" | awk '{print $1}'
    return 0
  fi
  return 1
}

required_file_check() {
  local target="$1"
  local expected_size="$2"
  local expected_sha="$3"

  if [ ! -f "$target" ]; then
    return 1
  fi

  if [ -n "$expected_size" ]; then
    local size
    size=$(stat -c%s "$target")
    if [ "$size" != "$expected_size" ]; then
      return 1
    fi
  fi

  if [ -n "$expected_sha" ]; then
    local got
    if ! got=$(calc_sha256 "$target"); then
      return 1
    fi
    if [ "$got" != "$expected_sha" ]; then
      return 1
    fi
  fi

  return 0
}

# Download a file from HF with strict validation.
# Args: repo file dest desc expected_size expected_sha require_token
download_hf() {
  local repo="$1"
  local file="$2"
  local dest="$3"
  local desc="$4"
  local expected_size="$5"
  local expected_sha="$6"
  local requires_token="$7"

  local url="https://huggingface.co/${repo}/resolve/main/${file}"
  local temp="${dest}.tmp"
  local headers=()

  if [ -n "$requires_token" ] && [ -z "$HF_TOKEN" ]; then
    log_error "${desc} requires HF_TOKEN"
    return 1
  fi

  if [ -n "$HF_TOKEN" ]; then
    headers+=( -H "Authorization: Bearer ${HF_TOKEN}" )
  fi

  if required_file_check "$dest" "$expected_size" "$expected_sha"; then
    log_info "Already up-to-date: ${desc}"
    return 0
  fi

  mkdir -p "$(dirname "$dest")"
  if [ -f "$dest" ]; then
    log_warn "File exists but validation failed; will re-download: ${dest}"
    rm -f "$dest"
  fi

  if [ "$DRY_RUN" = "true" ]; then
    if required_file_check "$dest" "$expected_size" "$expected_sha"; then
      log_info "Plan OK (dry-run): ${desc}"
      return 0
    fi
    log_warn "Plan (dry-run): missing/invalid: ${desc}"
    return 1
  fi

  log_info "Downloading: ${desc}"
  local attempt=0
  while [ $attempt -lt "$RETRY_MAX" ]; do
    attempt=$((attempt + 1))
    if curl -fL --connect-timeout 20 --max-time "$TIMEOUT_SEC" "${headers[@]}" -o "$temp" "$url"; then
      if required_file_check "$temp" "$expected_size" "$expected_sha"; then
        mv "$temp" "$dest"
        log_info "  saved: ${dest}"
        return 0
      fi
      log_warn "  download ok but validation failed for ${desc} (attempt ${attempt}/${RETRY_MAX})"
      rm -f "$temp"
    else
      rm -f "$temp"
    fi
    if [ $attempt -lt "$RETRY_MAX" ]; then
      log_warn "  retry ${attempt}/${RETRY_MAX} for ${desc}"
      sleep 3
    fi
  done

  log_error "Failed after ${RETRY_MAX} attempts: ${desc}"
  return 1
}

download_group_flux2() {
  log_info "=== FLUX.2 Models ==="
  download_hf "Comfy-Org/flux2-dev" "flux2_dev_fp8mixed.safetensors" \
    "${MODELS_DIR}/diffusion_models/flux2_dev_fp8mixed.safetensors" "FLUX.2 UNet (fp8mixed)" "" "" ""
  download_hf "Comfy-Org/flux2-dev" "mistral_3_small_flux2_fp8.safetensors" \
    "${MODELS_DIR}/text_encoders/mistral_3_small_flux2_fp8.safetensors" "FLUX.2 text encoder (mistral fp8)" "" "" ""
  download_hf "Comfy-Org/flux2-dev" "flux2-vae.safetensors" \
    "${MODELS_DIR}/vae/flux2-vae.safetensors" "FLUX.2 VAE" "" "" ""
  download_hf "Comfy-Org/flux2-dev" "Flux2TurboComfyv2.safetensors" \
    "${MODELS_DIR}/loras/Flux2TurboComfyv2.safetensors" "FLUX.2 Turbo LoRA" "" "" ""
  download_hf "comfyanonymous/flux_text_encoders" "clip_l.safetensors" \
    "${MODELS_DIR}/text_encoders/clip_l.safetensors" "CLIP-L encoder (shared)" "" "" ""
}

download_group_qwen21() {
  log_info "=== Qwen-Image 2.1 Models (T2I + image edit) ==="
  download_hf "Comfy-Org/Qwen-Image-2.1" "diffusion_models/qwen_image_2.1_int8_convrot.safetensors" \
    "${MODELS_DIR}/diffusion_models/qwen_image_2.1_int8_convrot.safetensors" "Qwen-Image 2.1 UNet (int8)" \
    "7256783064" "cb74113cb03faecd79611b01fd7fd642f0aa60d6f0b95086abee214d75eaa57d" ""
  download_hf "Comfy-Org/Qwen-Image-2.1" "text_encoders/qwen3vl_8b_int8_convrot.safetensors" \
    "${MODELS_DIR}/text_encoders/qwen3vl_8b_int8_convrot.safetensors" "Qwen-Image 2.1 text encoder (Qwen3-VL 8B int8)" \
    "9350798360" "8bfd0f6e12abf2d2d697ecc888e5e90b0d6741d6708f05799f53afa560452e8f" ""
  download_hf "Comfy-Org/Qwen-Image-2.1" "vae/qwen_image_2.1_vae_bf16.safetensors" \
    "${MODELS_DIR}/vae/qwen_image_2.1_vae_bf16.safetensors" "Qwen-Image 2.1 VAE" \
    "675509688" "bb21f7473051e1ac368515dd3f2e15cd44d7a11748ee8823e1ddca3e4876b7c9" ""
  # Optional prompt-rewriter (PE) encoders — used with TextGenerate + the aihub Qwen21PEPrompt node.
  # Both are 9.47G Qwen3.5-9B VL fine-tunes: PE-T2I rewrites a brief into a detailed prompt +
  # aspect ratio, PE-I2I turns a vague edit instruction (+image) into a precise edit prompt.
  download_hf "Comfy-Org/Qwen-Image-2.1" "text_encoders/qwen3.5_9b_qwen_image_2.1_pe_t2i.int8_convrot.safetensors" \
    "${MODELS_DIR}/text_encoders/qwen3.5_9b_qwen_image_2.1_pe_t2i.int8_convrot.safetensors" "Qwen-Image 2.1 prompt enhancer PE-T2I (Qwen3.5 9B)" \
    "9471072252" "9182abae56fe05459840a86d22abd21f972061c92fce032630af680c8c5178d3" ""
  download_hf "Comfy-Org/Qwen-Image-2.1" "text_encoders/qwen3.5_9b_qwen_image_2.1_pe_i2i.int8_convrot.safetensors" \
    "${MODELS_DIR}/text_encoders/qwen3.5_9b_qwen_image_2.1_pe_i2i.int8_convrot.safetensors" "Qwen-Image 2.1 prompt enhancer PE-I2I (Qwen3.5 9B)" \
    "9471072252" "32707d01b427e488af252b95c551989aad59f9fec611a694f5db6bde7f0f1f6c" ""
}

# Keep FLUX.1 artifacts for potential legacy fallback if present.
# They are intentionally not downloaded by default.


download_group_sdxl() {
  log_info "=== SDXL Models ==="
  download_hf "stabilityai/stable-diffusion-xl-base-1.0" "sd_xl_base_1.0.safetensors" \
    "${MODELS_DIR}/checkpoints/sd_xl_base_1.0.safetensors" "SDXL Base 1.0" "" "" ""
  download_hf "stabilityai/stable-diffusion-xl-refiner-1.0" "sd_xl_refiner_1.0.safetensors" \
    "${MODELS_DIR}/checkpoints/sd_xl_refiner_1.0.safetensors" "SDXL Refiner 1.0" "" "" ""
  download_hf "stabilityai/sdxl-vae" "sdxl_vae.safetensors" \
    "${MODELS_DIR}/vae/sdxl_vae.safetensors" "SDXL VAE" "" "" ""
}


download_group_wan() {
  log_info "=== Wan 2.1 Video Models ==="
  download_hf "Comfy-Org/Wan2.1" "umt5_xxl_fp8.safetensors" \
    "${MODELS_DIR}/text_encoders/umt5_xxl_fp8.safetensors" "Wan2.1 T5 text encoder (UMT5 fp8)" "" "" ""
  download_hf "Comfy-Org/Wan2.1" "wan2.1_t2v_1.3B_bf16.safetensors" \
    "${MODELS_DIR}/diffusion_models/wan2.1_t2v_1.3B_bf16.safetensors" "Wan2.1 T2V 1.3B BF16" "" "" ""
  download_hf "Comfy-Org/Wan2.1" "wan2.1_i2v_480p_14B_fp16.safetensors" \
    "${MODELS_DIR}/diffusion_models/wan2.1_i2v_480p_14B_fp16.safetensors" "Wan2.1 I2V 14B BF16" "" "" ""
  download_hf "Comfy-Org/Wan2.1" "clip_vision_h.safetensors" \
    "${MODELS_DIR}/clip_vision/clip_vision_h.safetensors" "Wan2.1 Clip Vision" "" "" ""
  download_hf "Comfy-Org/Wan2.1" "wan_2.1_vae.safetensors" \
    "${MODELS_DIR}/vae/wan_2.1_vae.safetensors" "Wan2.1 VAE" "" "" ""
}


download_group_ltx25() {
  log_info "=== LTX-2.5 Video Models ==="
  download_hf "Lightricks/LTX-2.5" "ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors" \
    "${MODELS_DIR}/diffusion_models/ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors" "LTX-2.5 Distilled Transformer (int8-convrot)" "" "" ""
  download_hf "Lightricks/LTX-2.5" "gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors" \
    "${MODELS_DIR}/text_encoders/gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors" "LTX-2.5 Gemma projection text encoder" "" "" ""
  download_hf "Lightricks/LTX-2.5" "gemma4_e2b_it_int8_convrot.safetensors" \
    "${MODELS_DIR}/text_encoders/gemma4_e2b_it_int8_convrot.safetensors" "LTX-2.5 reference Gemma tokenizer" "" "" ""
  download_hf "Lightricks/LTX-2.5" "ltx-2.5-video-vae-bf16.safetensors" \
    "${MODELS_DIR}/vae/ltx-2.5-video-vae-bf16.safetensors" "LTX-2.5 video VAE" "" "" ""
  download_hf "Lightricks/LTX-2.5" "ltx-2.5-audio-vae-bf16.safetensors" \
    "${MODELS_DIR}/vae/ltx-2.5-audio-vae-bf16.safetensors" "LTX-2.5 audio VAE" "" "" ""
  download_hf "Lightricks/LTX-2.5" "ltx-2.5-latent-spatial-upscaler-x2-bf16-1.0.safetensors" \
    "${MODELS_DIR}/latent_upscale_models/ltx-2.5-latent-spatial-upscaler-x2-bf16-1.0.safetensors" "LTX-2.5 upscaler (x2)" "" "" ""
}


download_group_minimax() {
  log_info "=== MiniMax-H3 Models ==="
  download_hf "MiniMaxAI/MiniMax-H3" "minimax_h3_fl2va_pruned_int8_convrot.safetensors" \
    "${MODELS_DIR}/diffusion_models/minimax_h3_fl2va_pruned_int8_convrot.safetensors" "MiniMax-H3 FL2VA" "" "" ""
  download_hf "MiniMaxAI/MiniMax-H3" "minimax_h3_ref2va_pruned_int8_convrot.safetensors" \
    "${MODELS_DIR}/diffusion_models/minimax_h3_ref2va_pruned_int8_convrot.safetensors" "MiniMax-H3 REF2VA" "" "" ""
  download_hf "MiniMaxAI/MiniMax-H3" "qwen3vl_32b_minimax_h3_nvfp4_awq.safetensors" \
    "${MODELS_DIR}/text_encoders/qwen3vl_32b_minimax_h3_nvfp4_awq.safetensors" "MiniMax-H3 Qwen3-VL AWQ" "" "" ""
  download_hf "MiniMaxAI/MiniMax-H3" "minimax_h3_video_vae_fp16.safetensors" \
    "${MODELS_DIR}/vae/minimax_h3_video_vae_fp16.safetensors" "MiniMax-H3 Video VAE" "" "" ""
  download_hf "MiniMaxAI/MiniMax-H3" "minimax_h3_audio_vae_fp32.safetensors" \
    "${MODELS_DIR}/vae/minimax_h3_audio_vae_fp32.safetensors" "MiniMax-H3 Audio VAE" "" "" ""
  download_hf "MiniMaxAI/MiniMax-H3-Turbo-Lora" "minimax_h3_fl2v_turbo_8step_v1.0_comfyui_bf16.safetensors" \
    "${MODELS_DIR}/loras/minimax_h3_fl2v_turbo_8step_v1.0_comfyui_bf16.safetensors" "MiniMax-H3 Turbo LoRA (T2V/I2V)" "" "" ""
  download_hf "MiniMaxAI/MiniMax-H3-Turbo-Lora" "minimax_h3_ref2v_turbo_4step_v0.1_comfyui_bf16.safetensors" \
    "${MODELS_DIR}/loras/minimax_h3_ref2v_turbo_4step_v0.1_comfyui_bf16.safetensors" "MiniMax-H3 Turbo LoRA (R2V)" "" "" ""
}

run_downloads() {
  local requested=0
  local ok=0

  if $DOWNLOAD_ALL || $DOWNLOAD_IMAGE || $DOWNLOAD_FLUX2 || $DOWNLOAD_SDXL; then
    requested=$((requested + 1))
    if ! download_group_sdxl; then
      log_error "SDXL group failed"
    else
      ok=$((ok + 1))
    fi
    if ! download_group_flux2; then
      log_error "FLUX.2 group failed"
    else
      ok=$((ok + 1))
    fi
  fi

  if $DOWNLOAD_ALL || $DOWNLOAD_IMAGE || $DOWNLOAD_QWEN21; then
    requested=$((requested + 1))
    if ! download_group_qwen21; then
      log_error "Qwen-Image 2.1 group failed"
    else
      ok=$((ok + 1))
    fi
  fi

  if $DOWNLOAD_ALL || $DOWNLOAD_VIDEO || $DOWNLOAD_WAN; then
    requested=$((requested + 1))
    if ! download_group_wan; then
      log_error "Wan group failed"
    else
      ok=$((ok + 1))
    fi
  fi

  if $DOWNLOAD_ALL || $DOWNLOAD_VIDEO || $DOWNLOAD_LTX; then
    requested=$((requested + 1))
    if ! download_group_ltx25; then
      log_error "LTX group failed"
    else
      ok=$((ok + 1))
    fi
  fi

  if $DOWNLOAD_ALL || $DOWNLOAD_VIDEO || $DOWNLOAD_MINIMAX; then
    requested=$((requested + 1))
    if ! download_group_minimax; then
      log_error "MiniMax group failed"
    else
      ok=$((ok + 1))
    fi
  fi

  return $(( requested - ok ))
}

if $DRY_RUN; then
  log_info "Dry-run mode: no files will be downloaded"
fi

log_info "ComfyUI model set for download"
log_info "Root: ${MODELS_DIR}"
if $DOWNLOAD_ALL || $DOWNLOAD_IMAGE || $DOWNLOAD_VIDEO || $DOWNLOAD_FLUX2 || $DOWNLOAD_SDXL || $DOWNLOAD_QWEN21 || $DOWNLOAD_WAN || $DOWNLOAD_LTX || $DOWNLOAD_MINIMAX; then
  mkdir -p "${MODELS_DIR}"/{checkpoints,diffusion_models,text_encoders,vae,loras,controlnet,embeddings,clip_vision,latent_upscale_models}
  if ! run_downloads; then
    echo ""
    log_error "Some downloads failed. Re-run with -x and inspect logs."
    exit 1
  fi
else
  log_warn "No download group selected."
  log_warn "Use --all or one of --image/--video/--flux2/--sdxl/--qwen21/--wan/--ltx/--minimax"
fi

echo ""
log_info "=== Summary ==="
if $DRY_RUN; then
  log_info "Mode: dry-run"
else
  log_info "Mode: download"
fi
if [ -d "$MODELS_DIR" ]; then
  model_count=$(find "${MODELS_DIR}" -type f -name "*.safetensors" | wc -l | tr -d '[:space:]')
  model_size=$(du -sh "${MODELS_DIR}" 2>/dev/null | cut -f1)
  log_info "Safetensors files in ${MODELS_DIR}: ${model_count}"
  log_info "Total size: ${model_size}"
else
  log_warn "Directory not created: ${MODELS_DIR}"
fi

exit 0
