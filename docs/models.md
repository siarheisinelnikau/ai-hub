# Connect your first model

The panel manages installed services. Install an engine and weights, then
register its systemd unit, port and optional download source.

## Native llama.cpp example

1. Install NVIDIA drivers/CUDA if using a GPU and verify `nvidia-smi`.
2. Build a model-compatible llama.cpp revision using its
   [official build guide](https://github.com/ggml-org/llama.cpp/blob/master/docs/build.md).
   Record the revision. The example expects
   `/opt/llama.cpp/build/bin/llama-server`.
3. Download a licensed GGUF file, verify its publisher checksum, and place it
   at `/srv/ai/models/local-llm/model.gguf`. Choose a model that fits your RAM/VRAM.
4. Create a dedicated service account (reuse it if it already exists):

   ```sh
   sudo useradd --system --no-create-home --shell /usr/sbin/nologin aihub-model
   sudo chown -R aihub-model:aihub-model /srv/ai/models/local-llm
   ```

   Grant GPU device access using your distribution's permissions as needed.
5. Review `examples/ai-local-llm.service`: paths, GPU layers, context and port.
   It uses one request slot and 8k context. For a CPU build use GPU layers `0`.

   ```sh
   sudo install -m 0644 examples/ai-local-llm.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl start ai-local-llm
   curl --fail http://127.0.0.1:18021/health
   ```

6. Copy `examples/registry.llama.yaml` to `registry.local.yaml` in the ai-hub
   installation. Set `AIHUB_REGISTRY=registry.local.yaml` in `.env`; restart
   ai-hub. Verify Online, a Stop/Start cycle and a streaming request through
   the authenticated hub endpoint.

`native: true` selects process-based lifecycle handling; `backend` selects
metrics parsing. Keep the service on loopback. See the upstream
[server documentation](https://github.com/ggml-org/llama.cpp/tree/master/tools/server)
for aliases, metrics, slots, and engine options.

## Registry

Each serving entry uses a unique safe `id`, display `name`, `category`,
`backend`, `port`, and `sysd` without the `.service` suffix. For Docker, the
container should match `id`; native units should follow `ai-<id>` for the
proxy's start-time selection. `in`/`out` describe modalities; `hf` links a source.

Only enable `parallel` controls when the wrapper actually reads saved
`gpus`, `ctx`, and `parallel` values. Memory estimates are advisory, especially
with mixed GPU sizes or CPU-offloaded experts. The fixed-command example
intentionally does not advertise adjustable launch controls.

Reference profiles in the root registry include specific llama.cpp, vLLM,
SGLang, Strata, Maya and media installations. Review their paths, engine builds,
GPU assumptions, wrappers, units and client settings before enabling them.

## Optional download catalog

Copy `examples/catalog.yaml` to `catalog.local.yaml` and set
`AIHUB_CATALOG=catalog.local.yaml`. Example for a plain Hugging Face download:

```yaml
models:
  - id: local-llm
    short: Local LLM
    backend: llama.cpp
    quant: Your chosen quantization
    port: 18021
    hf_repo: OWNER/MODEL-REPOSITORY
    local_path: /srv/ai/models/local-llm
    files:
      - EXACT-PUBLISHED-FILENAME.gguf
```

Replace uppercase placeholders with publisher values and update the service's
model filename. Install the `hf` CLI in the service PATH. The downloader
prefers `aria2c` when present; for gated models use the `hf` path with `HF_TOKEN`.
Downloads do not install services or establish model trust; verify checksums
and licenses before use. Only explicit relative file names are accepted.

Removing weights should retain catalog and serving configuration. Permanent
profile removal also requires reviewing units, wrappers, state, client
connections and benchmarks. Specialized installers may use different workflows.

## Media and cloud

Media recipes are optional integrations with their own dependencies and weights.
Defaults: ComfyUI 8188, voice 8010, ACE-Step REST 8017 (UI 8016), 3D 8020.
Verify each service before connecting MCP. Cloud configuration consumes a
trusted operator-provided endpoint; it is separate from local Universal routing.
