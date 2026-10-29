###############################################################################
# Hunyuan3D-2.1 Docker Image
# Based on official Tencent-Hunyuan/Hunyuan3D-2.1 Dockerfile
# Adapted for ai-hub: port 8020, health checks, RTX 3090 (24GB VRAM)
#
# Build:  docker build -t ai-hub-threed:latest .
# Run:    docker run -it --gpus all -p 8020:8020 -v /srv/ai/models:/models ai-hub-threed:latest
#
# Model cache: /models/huggingface (mounted volume, persists across rebuilds)
# First run downloads ~15 GB of model weights.
###############################################################################

FROM nvidia/cuda:12.4.1-devel-ubuntu22.04

LABEL name="ai-hub-threed" \
      maintainer="ai-hub" \
      description="Hunyuan3D-2.1 3D asset generation (image-to-3D, text-to-3D)"

# ── Workspace ────────────────────────────────────────────────────────────────
RUN mkdir -p /workspace
WORKDIR /workspace

# ── System dependencies ──────────────────────────────────────────────────────
# Build tools + OpenGL/EGL for headless rendering + compilation libs
RUN apt-get update && apt-get install -y \
        build-essential git wget vim unzip git-lfs curl \
        libegl1-mesa-dev libgl1-mesa-dev libgles2-mesa-dev \
        libegl1 libgl1 libglx0 libgles2 libglvnd0 libglvnd-dev \
        cmake pkg-config \
        libeigen3-dev python3-dev python3-setuptools libcgal-dev \
        libxi6 libgconf-2-4 libxkbcommon-x11-0 libsm6 libxext6 libxrender-dev \
        mesa-utils-extra \
    && rm -rf /var/lib/apt/lists/*

# ── Environment ──────────────────────────────────────────────────────────────
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYOPENGL_PLATFORM=egl \
    CUDA_HOME=/usr/local/cuda \
    TORCH_CUDA_ARCH_LIST="6.0;6.1;7.0;7.5;8.0;8.6;8.9;9.0" \
    HF_HOME=/models/huggingface \
    HF_HUB_CACHE=/models/huggingface/hub

ENV PATH="${CUDA_HOME}/bin:${PATH}" \
    LD_LIBRARY_PATH="${CUDA_HOME}/lib64:/usr/lib64:${LD_LIBRARY_PATH}"

# ── Miniconda + Python 3.10 ─────────────────────────────────────────────────
RUN wget -q https://repo.anaconda.com/miniconda/Miniconda3-latest-Linux-x86_64.sh \
    && chmod +x Miniconda3-latest-Linux-x86_64.sh \
    && ./Miniconda3-latest-Linux-x86_64.sh -b -p /workspace/miniconda3 \
    && rm Miniconda3-latest-Linux-x86_64.sh

ENV PATH="/workspace/miniconda3/bin:${PATH}"

RUN conda init bash \
    && conda config --set always_yes true \
    && conda tos accept --channel https://repo.anaconda.com/pkgs/main \
    && conda tos accept --channel https://repo.anaconda.com/pkgs/r

# Create hunyuan3d env
RUN conda create -n hunyuan3d python=3.10 -y \
    && conda install -n hunyuan3d ninja -c nvidia/label/cuda-12.4.1 -y \
    && conda install -n hunyuan3d cuda -c nvidia/label/cuda-12.4.1 -y \
    && conda install -n hunyuan3d -c conda-forge libstdcxx-ng -y

# Activate env by default (bash_login for --login shell)
RUN echo "source /workspace/miniconda3/etc/profile.d/conda.sh" >> ~/.bash_profile \
    && echo "conda activate hunyuan3d" >> ~/.bash_profile
SHELL ["/bin/bash", "--login", "-c"]

# ── PyTorch (CUDA 12.4) — use pip in conda env, not conda install ─────────────
# conda install pulls conflicting cpu libs. pip wheel is self-contained.
RUN /workspace/miniconda3/envs/hunyuan3d/bin/pip install \
    torch==2.5.1 torchvision==0.20.1 torchaudio==2.5.1 \
    --index-url https://download.pytorch.org/whl/cu124

# ── Clone Hunyuan3D-2.1 ─────────────────────────────────────────────────────
RUN git clone https://github.com/Tencent-Hunyuan/Hunyuan3D-2.1.git /workspace/Hunyuan3D-2.1

# ── Python dependencies ──────────────────────────────────────────────────────
# Note: bpy (Blender Python API) has no cp310 wheels on PyPI (all 4.2+ are cp311+),
# and the env is Python 3.10. bpy is only used by convert_obj_to_glb (save_glb=True);
# the default path converts via obj2gltf, so make the import optional.
RUN cd /workspace/Hunyuan3D-2.1 \
    && sed -i '/^bpy==/d' requirements.txt \
    && /workspace/miniconda3/envs/hunyuan3d/bin/pip install -r requirements.txt \
    && /workspace/miniconda3/envs/hunyuan3d/bin/pip install "setuptools<81" \
    && /workspace/miniconda3/envs/hunyuan3d/bin/python -c "import pathlib; p=pathlib.Path('/workspace/Hunyuan3D-2.1/hy3dpaint/DifferentiableRenderer/mesh_utils.py'); s=p.read_text(); s=s.replace('import bpy\n', 'try:\n    import bpy\nexcept ImportError:  # bpy needs Python 3.11+; only used when save_glb=True\n    bpy = None\n', 1); p.write_text(s)" \
    && /workspace/miniconda3/envs/hunyuan3d/bin/python -c "import pathlib; p=pathlib.Path('/workspace/Hunyuan3D-2.1/gradio_app.py'); s=p.read_text(); s=s.replace('conf = Hunyuan3DPaintConfig(max_num_view=8, resolution=768)', 'conf = Hunyuan3DPaintConfig(max_num_view=6, resolution=512)  # RTX 3090 24GB: default 8x768 OOMs', 1); p.write_text(s)"

# ── Compile custom_rasterizer (CUDA extension for texture pipeline) ──────────
RUN cd /workspace/Hunyuan3D-2.1/hy3dpaint/custom_rasterizer \
    && export TORCH_CUDA_ARCH_LIST="6.0;6.1;7.0;7.5;8.0;8.6;8.9;9.0" \
    && export CUDA_NVCC_FLAGS="-allow-unsupported-compiler" \
    && export PYTHONPATH="/workspace/miniconda3/envs/hunyuan3d/lib/python3.10/site-packages:${PYTHONPATH}" \
    && /workspace/miniconda3/envs/hunyuan3d/bin/python setup.py build_ext --inplace \
    && /workspace/miniconda3/envs/hunyuan3d/bin/python setup.py install

# ── Compile DifferentiableRenderer (mesh painting CUDA kernels) ──────────────
RUN cd /workspace/Hunyuan3D-2.1/hy3dpaint/DifferentiableRenderer \
    && bash compile_mesh_painter.sh

# ── Download RealESRGAN model (texture upscaling) ────────────────────────────
RUN cd /workspace/Hunyuan3D-2.1/hy3dpaint \
    && mkdir -p ckpt \
    && wget -q https://github.com/xinntao/Real-ESRGAN/releases/download/v0.1.0/RealESRGAN_x4plus.pth \
       -P ckpt

# ── Fix relative paths for container ─────────────────────────────────────────
# textureGenPipeline.py: cfgs/ -> hy3dpaint/cfgs/
RUN cd /workspace/Hunyuan3D-2.1/hy3dpaint \
    && sed -i 's|self\.multiview_cfg_path = "cfgs/hunyuan-paint-pbr\.yaml"|self.multiview_cfg_path = "hy3dpaint/cfgs/hunyuan-paint-pbr.yaml"|' textureGenPipeline.py

# multiview_utils.py: fix custom_pipeline path
RUN cd /workspace/Hunyuan3D-2.1/hy3dpaint/utils \
    && sed -i 's|custom_pipeline = config\.custom_pipeline|custom_pipeline = os.path.join(os.path.dirname(__file__),"..","hunyuanpaintpbr")|' multiview_utils.py

# ── Copy entrypoint and health check ─────────────────────────────────────────
COPY entrypoint.sh /workspace/entrypoint.sh
COPY healthcheck.sh /workspace/healthcheck.sh
RUN chmod +x /workspace/entrypoint.sh /workspace/healthcheck.sh

# ── Working directory ────────────────────────────────────────────────────────
WORKDIR /workspace/Hunyuan3D-2.1
RUN mkdir -p gradio_cache /models/huggingface

# ── Expose port ──────────────────────────────────────────────────────────────
EXPOSE 8020

# ── Health check ─────────────────────────────────────────────────────────────
HEALTHCHECK --interval=30s --timeout=10s --start-period=120s --retries=3 \
    CMD /workspace/healthcheck.sh

# ── Entrypoint ───────────────────────────────────────────────────────────────
ENTRYPOINT ["/workspace/entrypoint.sh"]
CMD ["/workspace/miniconda3/envs/hunyuan3d/bin/python", "gradio_app.py", "--port", "8020", "--host", "0.0.0.0", "--low_vram_mode"]
