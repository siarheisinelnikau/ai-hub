# Real-server validation and benchmarks

ai-hub runs on a Linux server with **4 x NVIDIA RTX 3090, 24 GB each**.
The results in [benchmarks/context.json](../benchmarks/context.json) are
measured inference results from this hardware. The dataset is dated
**2026-10-22**; it contains ten model profiles and retains incomplete curves.

## Measured decode throughput

These are the dataset's median decode rates at approximately 20,480 input
tokens. Each listed measurement uses **4 GPUs (0-3)** and **one concurrent
request**. Prefill time is excluded from decode throughput.

| Model profile | Quantization | Backend | Decode tokens/s | Recorded points | Curve status |
| --- | --- | --- | ---: | ---: | --- |
| Qwen 3.8 Flash-Next 125B | UD-IQ4_XS | Strata | 110.1 | 5 | Complete |
| Qwen 3.8 27B | Q4_K_M | llama.cpp | 41.9 | 5 | Complete |
| Qwen 3.8 27B | Q6_K | llama.cpp | 39.0 | 5 | Complete |
| Gemma4 26B | Q4_K_M | llama.cpp | 82.4 | 5 | Complete |
| Qwen 3.8 Flash-Next 125B Uncensored | UD-IQ4_XS | Strata | 106.1 | 5 | Complete |
| Nemotron 3.5 Super VL 120B | Q4_K_M | llama.cpp | 70.3 | 10 | Complete |
| Nemotron 3.5 Super VL 120B | NVFP4 | SGLang | 71.7 | 9 | Complete |
| Nemotron 3.5 Super VL 120B Uncensored | AD-Q4_K_S | llama.cpp b11669 | 60.2 | 1 | Partial |
| GLM 5.3 Flash 321B | Maya-M IQ2_S | Maya | 59.4 | 5 | Complete |
| GLM 5.3 Flash 321B Derisked | Maya-M IQ2_S | Maya | 56.4 | 2 | Partial |

"Complete" describes the recorded curve's planned test points, not every
context length or hardware configuration supported by the model.

## Measurement protocol

The dataset records actual input tokens rather than assuming the requested
context size was filled exactly. The standard run generates 512 output
tokens at temperature 0 with thinking disabled and no prefix reuse. It uses
three trials near 20k and two at other lengths, unless a point or curve says
otherwise. Medians, minimum/maximum rates, trial counts, retrieval checks,
and available prefill measurements are retained in the JSON.

The test input is a constructed engineering archive with a retrieval task
and a story-generation request. A synthetic workload here means repeatable
input text: the model execution, timing, GPU usage, and throughput are real
measurements. Results depend on the model, backend, quantization, context,
launch settings, and speculative-decoding acceptance.

## Deployment validation and UI previews

Live release checks on the server verify authenticated streaming inference,
client connections, GPU status, MCP initialization/tool discovery, access
restrictions, and the deployed frontend assets. Automated Linux/Windows
checks cover the code separately from these hardware runs.

The README screenshot and `python scripts/demo.py` use sample display data
to demonstrate the UI without exposing installation details. They do not
supply the measured benchmark dataset above.
