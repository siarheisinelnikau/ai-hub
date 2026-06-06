FROM nvidia/cuda:12.4.1-devel-ubuntu22.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        python3.11 python3.11-venv python3.11-dev python3-pip \
        git wget curl ca-certificates \
        libgl1 libglib2.0-0 libsm6 libxext6 libxrender1 \
        ninja-build build-essential \
    && rm -rf /var/lib/apt/lists/* \
    && update-alternatives --install /usr/bin/python3 python3 /usr/bin/python3.11 1 \
    && update-alternatives --install /usr/bin/python  python  /usr/bin/python3.11 1
ENV VIRTUAL_ENV=/home/comfy/venv
RUN python3.11 -m venv ${VIRTUAL_ENV}
ENV PATH="${VIRTUAL_ENV}/bin:${PATH}"
RUN pip install --no-cache-dir --upgrade pip setuptools wheel
RUN pip install --no-cache-dir \
    torch==2.9.1+cu130 torchvision torchaudio==2.9.1+cu130 \
    --index-url https://download.pytorch.org/whl/cu130
ARG COMFYUI_REPO=https://github.com/comfyanonymous/ComfyUI.git
# Pin the release: v0.37.0 = first version with Qwen-Image 2.1 support (CORE-423),
# frontend 1.52.7 / workflow-templates 0.11.66 / comfy-kitchen 0.2.35 / comfy-aimdo 0.5.5.
ARG COMFYUI_REF=v0.37.0
WORKDIR /home/comfy/ComfyUI
RUN git clone --depth 1 --branch ${COMFYUI_REF} ${COMFYUI_REPO} .
RUN pip install --no-cache-dir -r requirements.txt
# ComfyUI Manager
RUN git clone --depth 1 https://github.com/Comfy-Org/ComfyUI-Manager.git \
        custom_nodes/ComfyUI-Manager && \
    pip install --no-cache-dir -r custom_nodes/ComfyUI-Manager/requirements.txt || true
# KJNodes
RUN git clone --depth 1 https://github.com/kijai/ComfyUI-KJNodes.git \
        custom_nodes/ComfyUI-KJNodes && \
    pip install --no-cache-dir -r custom_nodes/ComfyUI-KJNodes/requirements.txt || true
# VideoHelperSuite
RUN git clone --depth 1 https://github.com/Kosinkadink/ComfyUI-VideoHelperSuite.git \
        custom_nodes/ComfyUI-VideoHelperSuite && \
    pip install --no-cache-dir -r custom_nodes/ComfyUI-VideoHelperSuite/requirements.txt || true
# GGUF support
RUN git clone --depth 1 https://github.com/city96/ComfyUI-GGUF.git \
        custom_nodes/ComfyUI-GGUF && \
    pip install --no-cache-dir -r custom_nodes/ComfyUI-GGUF/requirements.txt || true
# Impact Pack
RUN git clone --depth 1 https://github.com/ltdrdata/ComfyUI-Impact-Pack.git \
        custom_nodes/ComfyUI-Impact-Pack && \
    pip install --no-cache-dir -r custom_nodes/ComfyUI-Impact-Pack/requirements.txt || true
# rgthree
RUN git clone --depth 1 https://github.com/rgthree/rgthree-comfy.git \
        custom_nodes/rgthree-comfy || true
# Patch comfy_kitchen: add typing import + list[int] -> typing.List[int] for torch.library.infer_schema
RUN NA=/home/comfy/venv/lib/python3.11/site-packages/comfy_kitchen/backends/eager/na.py && \
    grep -q "^import typing" "$NA" || sed -i '1i import typing' "$NA" && \
    sed -i 's/list\[int\]/typing.List[int]/g; s/list\[bool\]/typing.List[bool]/g; s/float | None/typing.Optional[float]/g' "$NA"
# Pin kornia to avoid torch.jit.script segfault on Xeon E5
RUN pip install --no-cache-dir 'kornia==0.7.4'
# numba: CPU path for the UV-atlas packer (`pack_bitmap_concat`). Without it ComfyUI uses the
# GPU torch fallback whose pass-1 tensors `(N, Vmax, 36)` OOM a 24 GB card on meshes with many
# charts (Pixal3D/TRELLIS.2 templates, e.g. 44k charts).
RUN pip install --no-cache-dir numba
# Patch video_types.py: sanitize NaN/Inf in audio before AAC/Opus encode
# (LTX-2.5 / MiniMax AudioVAE can emit non-finite samples -> av.error: [aac] Input contains (near) NaN/+-Inf)
COPY patch_video_audio.py /tmp/patch_video_audio.py
RUN python3 /tmp/patch_video_audio.py && rm /tmp/patch_video_audio.py
COPY healthcheck.py /home/comfy/ComfyUI/healthcheck.py
COPY entrypoint.sh /home/comfy/entrypoint.sh
RUN chmod +x /home/comfy/entrypoint.sh
RUN useradd -m -u 1000 -d /home/comfy comfy
RUN chown -R comfy:comfy /home/comfy
USER comfy
EXPOSE 8188
HEALTHCHECK --interval=30s --timeout=10s --start-period=90s --retries=3 \
    CMD python /home/comfy/ComfyUI/healthcheck.py || exit 1
ENTRYPOINT ["/home/comfy/entrypoint.sh"]
CMD ["python", "main.py", "--listen", "0.0.0.0", "--port", "8188", "--preview-method", "auto"]
